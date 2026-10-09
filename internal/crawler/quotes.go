package crawler

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

// Quote 场内 ETF 行情 + 参考溢价。
// 溢价口径(V1): 现价/最新净值 - 1; QDII ETF 净值为 T-1, 展示时必须标注"仅供参考"。
type Quote struct {
	Code       string
	Name       string
	Price      float64
	PrevClose  float64
	ChangePct  float64
	Time       string
	Nav        float64
	NavDate    string
	PremiumPct float64 // 百分数(如 11.70); HasPremium=false 时无效
	HasPremium bool
}

var (
	reTencentLine = regexp.MustCompile(`v_(sz|sh)(\d{6})="(.*)"`)
	reNetWorthArr = regexp.MustCompile(`(?s)Data_netWorthTrend\s*=\s*(\[.*?\]);`)
	reNavPoint    = regexp.MustCompile(`\{"x":(\d+),"y":([\d.]+)`)
)

// FetchQuotes 腾讯行情(1 次批量) + 逐只 ETF 净值(pingzhongdata), 返回溢价。
// 行情字段下标(spike 实测): 1=名称 3=现价 4=昨收 30=时间戳 32=涨跌幅。
func FetchQuotes(etfCodes []string) ([]*Quote, error) {
	if len(etfCodes) == 0 {
		return nil, nil
	}
	symbols := make([]string, len(etfCodes))
	for i, c := range etfCodes {
		symbols[i] = etfSymbol(c)
	}
	body, err := FetchRaw("https://qt.gtimg.cn/q=" + strings.Join(symbols, ","))
	if err != nil {
		return nil, fmt.Errorf("腾讯行情失败: %w", err)
	}
	rd, err := charset.NewReaderLabel("gbk", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	text, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}

	quotes := make([]*Quote, 0, len(etfCodes))
	for _, m := range reTencentLine.FindAllSubmatch(text, -1) {
		f := strings.Split(string(m[3]), "~")
		if len(f) < 33 {
			continue
		}
		q := &Quote{Code: string(m[2]), Name: f[1]}
		q.Price, _ = strconv.ParseFloat(f[3], 64)
		q.PrevClose, _ = strconv.ParseFloat(f[4], 64)
		q.ChangePct, _ = strconv.ParseFloat(f[32], 64)
		if t, err := time.Parse("20060102150405", f[30]); err == nil {
			q.Time = t.Format("01-02 15:04:05")
		} else {
			q.Time = f[30]
		}
		quotes = append(quotes, q)
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("行情解析结果为空(响应前100字节: %.100s)", text)
	}

	for _, q := range quotes {
		nav, navDate, err := fetchEtfNav(q.Code)
		if err != nil {
			q.NavDate = fmt.Sprintf("净值获取失败: %v", err)
			continue
		}
		q.Nav, q.NavDate = nav, navDate
		if nav > 0 {
			q.PremiumPct = (q.Price/nav - 1) * 100
			q.HasPremium = true
		}
		time.Sleep(500 * time.Millisecond) // 爬虫礼仪
	}
	return quotes, nil
}

// fetchEtfNav 从 pingzhongdata 的净值走势数组取末点 = 最新单位净值。
func fetchEtfNav(code string) (nav, navDate string, err error) {
	body, err := FetchRaw("https://fund.eastmoney.com/pingzhongdata/" + code + ".js")
	if err != nil {
		return "", "", err
	}
	arr := reNetWorthArr.FindSubmatch(body)
	if arr == nil {
		return "", "", fmt.Errorf("未找到 Data_netWorthTrend")
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

// etfSymbol 场内代码→交易所前缀: 5 开头沪市(513500), 其余按深市(159632)。
func etfSymbol(code string) string {
	if strings.HasPrefix(code, "5") {
		return "sh" + code
	}
	return "sz" + code
}
