package crawler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// SgztRow 天天基金「全市场申购状态」批量接口的一行。
//
// 接口(2026-10-08 spike 实测):
//
//	https://fund.eastmoney.com/Data/Fund_JJJZ_Data.aspx?t=8&page=<页号>,<页大小>&js=reData&sort=fcode,asc
//
// 返回 UTF-8 JS 赋值: var reData={datas:[[...13字段...],...],record:"27704",pages:"278",curpage:"1",showday:["2026-10-08","2026-09-30"]}
// 字段下标: 0=代码 1=简称 2=类型 3=净值 4=净值日期 5=申购状态 6=赎回状态 7=下一开放日
//
//	8=购买起点(元) 9=日累计限定金额(元,原始数字; >=1e8 视为无限额) 10=费率折扣 11=购买按钮态 12=折后手续费
type SgztRow struct {
	Code           string
	Name           string
	Type           string
	Nav            string
	NavDate        string
	PurchaseStatus string
	RedeemStatus   string
	NextOpenDate   string
	MinBuy         string
	PurchaseLimit  string
	Discount       string
	BuyState       string
	Fee            string
}

// SgztResult 一次全量抓取的汇总。
type SgztResult struct {
	Rows    map[string]SgztRow // 全市场(约 2.7 万只), 代码→行
	Record  int                // 接口报告的总记录数
	Fetched int                // 实际拉到的行数
	ShowDay string             // 数据展示日(YYYY-MM-DD), 作为 trade_date
}

const sgztPageSize = 10000 // 上限在 15000~20000 间, 稳妥取 1 万

// FetchSgzt 分页拉取全市场申购状态(正常 3 次请求), 页间隔 1s。
func FetchSgzt() (*SgztResult, error) {
	res := &SgztResult{Rows: map[string]SgztRow{}}
	total := 0
	for page := 1; page <= 5; page++ { // 上限 5 页纯防御
		url := fmt.Sprintf("https://fund.eastmoney.com/Data/Fund_JJJZ_Data.aspx?t=8&page=%d,%d&js=reData&sort=fcode,asc", page, sgztPageSize)
		body, err := FetchRaw(url)
		if err != nil {
			return nil, fmt.Errorf("批量接口第 %d 页失败: %w", page, err)
		}
		rows, record, showday, err := parseSgztJSONP(body)
		if err != nil {
			return nil, fmt.Errorf("批量接口第 %d 页解析失败: %w", page, err)
		}
		if res.Record == 0 {
			res.Record, res.ShowDay = record, showday
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			if row, ok := sgztRowFromCells(r); ok {
				res.Rows[row.Code] = row
			}
		}
		total += len(rows)
		if record > 0 && total >= record {
			break
		}
		if len(rows) < sgztPageSize {
			break
		}
		time.Sleep(1 * time.Second) // 爬虫礼仪
	}
	res.Fetched = len(res.Rows)
	if res.Fetched == 0 {
		return nil, fmt.Errorf("批量接口没有返回任何数据, 疑似结构变化")
	}
	return res, nil
}

var (
	reRecord  = regexp.MustCompile(`record:"(\d+)"`)
	reShowday = regexp.MustCompile(`showday:\["([^"]*)","([^"]*)"\]`)
)

// parseSgztJSONP 从 "var reData={datas:[...],record:...}" 解出数据。
// datas 数组本身是合法 JSON, 直接 json.Decode, 不手写解析。
func parseSgztJSONP(body []byte) ([][]string, int, string, error) {
	i := bytes.Index(body, []byte("datas:"))
	if i < 0 {
		return nil, 0, "", fmt.Errorf("响应里没有 datas(前100字节: %.100s)", body)
	}
	var rows [][]string
	if err := json.NewDecoder(bytes.NewReader(body[i+len("datas:"):])).Decode(&rows); err != nil {
		return nil, 0, "", fmt.Errorf("datas 数组解码失败: %w", err)
	}
	record := 0
	if m := reRecord.FindSubmatch(body); m != nil {
		record, _ = strconv.Atoi(string(m[1]))
	}
	showday := ""
	if m := reShowday.FindSubmatch(body); m != nil {
		showday = string(m[1])
	}
	return rows, record, showday, nil
}

func sgztRowFromCells(cells []string) (SgztRow, bool) {
	if len(cells) < 13 || len(cells[0]) != 6 {
		return SgztRow{}, false
	}
	return SgztRow{
		Code:           cells[0],
		Name:           cells[1],
		Type:           cells[2],
		Nav:            cells[3],
		NavDate:        cells[4],
		PurchaseStatus: cells[5],
		RedeemStatus:   cells[6],
		NextOpenDate:   cells[7],
		MinBuy:         cells[8],
		PurchaseLimit:  cells[9],
		Discount:       cells[10],
		BuyState:       cells[11],
		Fee:            cells[12],
	}, true
}
