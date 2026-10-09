package crawler

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"

	"github.com/neokou/easyfund/internal/model"
)

// FundPage 单基金主页(fund.eastmoney.com/{code}.html)购买区信息。
// 定位: 交叉验证源。限额精确到分, 状态原文与批量接口互证。
type FundPage struct {
	Code          string
	Status        model.Status
	StatusRaw     string
	DailyLimit    string // 精确到分, 如 "5.00"; 空 = 无
	FeeOriginal   string
	FeeDiscounted string
}

var (
	reLimit = regexp.MustCompile(`单日累计购买上限\s*([\d.]+)\s*元`)
	reFee   = regexp.MustCompile(`([\d.]+)%\s*([\d.]+)%`)
)

// FetchFundPage 抓取并解析单基金主页购买区。
func FetchFundPage(code string) (*FundPage, error) {
	body, err := FetchRaw("https://fund.eastmoney.com/" + code + ".html")
	if err != nil {
		return nil, err
	}
	decoded, err := charset.NewReader(bytes.NewReader(body), "text/html")
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(decoded)
	if err != nil {
		return nil, err
	}

	// 首选: 购买信息区小容器; 兜底: 整页文本按 rune 截取
	statusLine, feeLine := "", ""
	doc.Find("li,div").Each(func(_ int, s *goquery.Selection) {
		if statusLine != "" && feeLine != "" {
			return
		}
		t := norm(s.Text())
		if t == "" || len(t) > 80 || s.Children().Length() > 6 {
			return
		}
		if statusLine == "" && strings.Contains(t, "交易状态") {
			statusLine = t
		}
		if feeLine == "" && strings.Contains(t, "购买手续费") {
			feeLine = t
		}
	})
	if statusLine == "" || feeLine == "" {
		full := norm(doc.Text())
		if statusLine == "" {
			statusLine = sliceFrom(full, "交易状态", 80)
		}
		if feeLine == "" {
			feeLine = sliceFrom(full, "购买手续费", 40)
		}
	}

	info := &FundPage{Code: code, StatusRaw: statusLine}
	// 状态词优先于限额: 实测存在"暂停申购 (单日累计购买上限2.00元)"组合, 语义以状态词为准。
	switch {
	case strings.Contains(statusLine, "暂停申购"):
		info.Status = model.StatusSuspended
	case strings.Contains(statusLine, "封闭"):
		info.Status = model.StatusClosed
	case strings.Contains(statusLine, "限大额"):
		info.Status = model.StatusLimited
	case strings.Contains(statusLine, "开放申购"):
		info.Status = model.StatusOpen
	default:
		info.Status = model.StatusUnknown
	}
	if m := reLimit.FindStringSubmatch(statusLine); m != nil {
		info.DailyLimit = m[1]
	}
	if m := reFee.FindStringSubmatch(feeLine); m != nil {
		info.FeeOriginal, info.FeeDiscounted = m[1], m[2]
	}
	return info, nil
}

func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// sliceFrom 从 marker 起向后取 n 个 rune, 避免按字节截断中文。
func sliceFrom(s, marker string, n int) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	r := []rune(s[i:])
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
