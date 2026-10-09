package main

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

// EtfQuote 场内 ETF 行情 + 参考溢价。
// 溢价口径(V1): 现价 / 最新净值 - 1。QDII ETF 净值为 T-1, 页面须标注"仅供参考"。
type EtfQuote struct {
	Symbol      string `json:"symbol"` // sz159632
	Code        string `json:"code"`
	Name        string `json:"name"`
	Price       string `json:"price"`
	PrevClose   string `json:"prev_close"`
	Time        string `json:"time"`
	ChangePct   string `json:"change_pct"`
	Nav         string `json:"nav"`
	NavDate     string `json:"nav_date"`
	PremiumPct  string `json:"premium_pct"`
}

var (
	reTencentLine = regexp.MustCompile(`v_(sz|sh)(\d{6})="(.*)"`)
	// pingzhongdata: var Data_netWorthTrend = [{"x":1375488000000,"y":1.0,...},...];
	reNetWorthArr = regexp.MustCompile(`(?s)Data_netWorthTrend\s*=\s*(\[.*?\]);`)
	reNavPoint    = regexp.MustCompile(`\{"x":(\d+),"y":([\d.]+)`)
)

// probeTencentQuotes 腾讯行情接口, 返回 GBK 文本, 字段以 ~ 分隔。
// 已确认下标: 1=名称 2=代码 3=现价 4=昨收 30=时间(yyyymmddHHMMSS) 32=涨跌幅。
// 尾部存在疑似基金净值字段, 首个行情会 dump 全部下标辅助定位。
func probeTencentQuotes(etfCodes []string) ([]*EtfQuote, error) {
	if len(etfCodes) == 0 {
		return nil, fmt.Errorf("监控列表未配置 ETF 代码")
	}
	symbols := make([]string, len(etfCodes))
	for i, c := range etfCodes {
		symbols[i] = etfSymbol(c)
	}
	body, err := fetchRaw("https://qt.gtimg.cn/q=" + strings.Join(symbols, ","))
	if err != nil {
		return nil, err
	}
	rd, err := charset.NewReaderLabel("gbk", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	text, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}

	quotes := []*EtfQuote{}
	for i, m := range reTencentLine.FindAllSubmatch(text, -1) {
		fields := strings.Split(string(m[3]), "~")
		if len(fields) < 33 {
			continue
		}
		q := &EtfQuote{
			Symbol:    string(m[1]) + string(m[2]),
			Code:      string(m[2]),
			Name:      fields[1],
			Price:     fields[3],
			PrevClose: fields[4],
			Time:      prettyQuoteTime(fields[30]),
			ChangePct: fields[32],
		}
		if i == 0 {
			fmt.Println("  [行情字段dump] 腾讯返回字段(下标:值, 空字段已省略):")
			for idx, v := range fields {
				if v != "" {
					fmt.Printf("    [%d] %s\n", idx, v)
				}
			}
		}
		quotes = append(quotes, q)
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("行情解析结果为空, 原始响应前100字节: %.100s", text)
	}
	return quotes, nil
}

// probeEtfNav 从 pingzhongdata 的净值走势数组取末点 = 最新单位净值。
func probeEtfNav(code string) (nav, navDate string, err error) {
	body, err := fetchRaw("http://fund.eastmoney.com/pingzhongdata/" + code + ".js")
	if err != nil {
		return "", "", err
	}
	arr := reNetWorthArr.FindSubmatch(body)
	if arr == nil {
		return "", "", fmt.Errorf("未找到 Data_netWorthTrend(响应前80字节: %.80s)", body)
	}
	pts := reNavPoint.FindAllSubmatch(arr[1], -1)
	if len(pts) == 0 {
		return "", "", fmt.Errorf("净值走势数组为空")
	}
	last := pts[len(pts)-1]
	ms, err := strconv.ParseInt(string(last[1]), 10, 64)
	if err != nil {
		return "", "", err
	}
	return string(last[2]), time.UnixMilli(ms).Format("2006-01-02"), nil
}

func etfSymbol(code string) string {
	// 沪市基金 5 开头(如 513500), 其余按深市处理(如 159632)
	if strings.HasPrefix(code, "5") {
		return "sh" + code
	}
	return "sz" + code
}

func priceOverNav(price, nav string) string {
	p, err1 := strconv.ParseFloat(price, 64)
	n, err2 := strconv.ParseFloat(nav, 64)
	if err1 != nil || err2 != nil || n <= 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", (p/n-1)*100)
}

func prettyQuoteTime(s string) string {
	if t, err := time.Parse("20060102150405", s); err == nil {
		return t.Format("01-02 15:04:05")
	}
	return s
}
