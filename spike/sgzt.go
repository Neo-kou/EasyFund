package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// SgztRow 对应天天基金「全市场申购状态」数据接口的一行。
//
// 数据接口(2026-10-08 spike 实测确认):
//
//	https://fund.eastmoney.com/Data/Fund_JJJZ_Data.aspx?t=8&page=<页号>,<页大小>&js=reData&sort=fcode,asc
//
// 返回 UTF-8 的 JS 变量赋值(JSONP 风格, datas 部分是合法 JSON):
//
//	var reData={datas:[["000001","华夏成长混合",...共13字段...],...],record:"27704",pages:"278",curpage:"1",showday:["2026-10-08","2026-09-30"]}
//
// 13 个字段下标(实测): 0=代码 1=简称 2=类型 3=净值 4=净值日期 5=申购状态 6=赎回状态
// 7=下一开放日 8=购买起点(元) 9=日累计限定金额(元,原始数字) 10=费率折扣(折) 11=购买按钮态 12=折后手续费
//
// 页大小上限介于 15000~20000 之间, 稳妥取 10000; 全市场 2.7 万+ 条 → 3 次请求拿全量。
// HTML 兜底页面(仅 SSR 前 50 行, 翻页同样走本接口): https://fund.eastmoney.com/Fund_sgzt_bzdm.html
type SgztRow struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Nav            string `json:"nav"`
	NavDate        string `json:"nav_date"`
	PurchaseStatus string `json:"purchase_status"` // 原文: 开放申购/限大额/暂停申购/封闭期...
	RedeemStatus   string `json:"redeem_status"`
	NextOpenDate   string `json:"next_open_date"`
	MinBuy         string `json:"min_buy"`         // 购买起点(元)
	PurchaseLimit  string `json:"purchase_limit"`  // 日累计限定金额(元), 原始数值; 1e11 之类的大数≈无限制
	Discount       string `json:"discount"`        // 费率折扣, 如 "1.0"(折)
	BuyState       string `json:"buy_state"`       // 1=可买 4=暂停(灰色按钮), 语义随按钮态变化, 仅参考
	Fee            string `json:"fee"`             // 折后手续费, 如 "0.12%"
}

// SgztResp 一次全量抓取的汇总。
type SgztResp struct {
	Rows    []SgztRow
	Record  int    // 接口报告的总记录数
	ShowDay string // 数据展示日(showday[0]), 页面"数据截至"用
}

const sgztPageSize = 10000

// probeSgztAPI 分页拉取全市场申购状态, 从中过滤监控池。
// 请求次数 = 3(全量), 每页间隔 1s, 对源站友好。
func probeSgztAPI(watch []WatchFund) (*SgztResp, map[string]SgztRow, error) {
	want := map[string]bool{}
	for _, f := range watch {
		want[f.Code] = true
	}

	resp := &SgztResp{}
	matched := map[string]SgztRow{}
	started := time.Now()
	total := 0

	for page := 1; page <= 5; page++ { // 上限 5 页纯防御, 正常 3 页内结束
		url := fmt.Sprintf("https://fund.eastmoney.com/Data/Fund_JJJZ_Data.aspx?t=8&page=%d,%d&js=reData&sort=fcode,asc", page, sgztPageSize)
		body, err := fetchRaw(url)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 页抓取失败: %w", page, err)
		}
		if page == 1 {
			saveTestdata("sgzt_api_p1.js", body) // 夹具: 供解析单测用
		}
		rows, record, showday, err := parseSgztJSONP(body)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 页解析失败: %w", page, err)
		}
		if resp.Record == 0 {
			resp.Record, resp.ShowDay = record, showday
			fmt.Printf("  接口报告总记录数: %d, 数据展示日: %s\n", record, showday)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			row, ok := sgztRowFromCells(r)
			if !ok {
				continue
			}
			resp.Rows = append(resp.Rows, row)
			if want[row.Code] {
				matched[row.Code] = row
			}
		}
		total += len(rows)
		if record > 0 && total >= record {
			break
		}
		if len(rows) < sgztPageSize {
			break
		}
		time.Sleep(1 * time.Second) // 爬虫礼仪: 翻页间隔 1s
	}

	fmt.Printf("  全市场抓取 %d 条, 耗时 %s\n", total, time.Since(started).Round(time.Millisecond))
	if total == 0 {
		return nil, nil, fmt.Errorf("接口没有返回任何数据, 疑似结构变化")
	}
	return resp, matched, nil
}

var (
	reRecord  = regexp.MustCompile(`record:"(\d+)"`)
	reShowday = regexp.MustCompile(`showday:\["([^"]*)","([^"]*)"\]`)
)

// parseSgztJSONP 从 "var reData={datas:[[...],...],record:...}" 中解出数据。
// datas 数组本身是合法 JSON, 直接用 json.Decoder 从该位置解码一个完整值即可, 无需手写解析。
func parseSgztJSONP(body []byte) ([][]string, int, string, error) {
	i := bytes.Index(body, []byte("datas:"))
	if i < 0 {
		return nil, 0, "", fmt.Errorf("响应里没有 datas(前100字节: %.100s)", body)
	}
	var rows [][]string
	dec := json.NewDecoder(bytes.NewReader(body[i+len("datas:"):]))
	if err := dec.Decode(&rows); err != nil {
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

// limitDisplay 把接口的原始限额转成人类可读文本。
func limitDisplay(raw string) string {
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}
	if v >= 1e8 { // 1亿以上视为事实上的无限制(接口对开放基金填 1000 亿)
		return "无限额"
	}
	return fmt.Sprintf("%g元/日", v)
}
