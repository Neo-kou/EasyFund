package main

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
)

// FundPageInfo 单基金主页(fund.eastmoney.com/{code}.html)购买区信息。
// 状态原文含精确限额, 如 "限大额 (单日累计购买上限5.00元)"; 费率为 "1.20% 0.12%"(原→折后)。
// 定位: 交叉验证源(限额精确到分), 主源是 Fund_JJJZ_Data.aspx 批量接口。
type FundPageInfo struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Status        string `json:"status"`      // 归一化: open / limited / suspended / closed / unknown
	StatusRaw     string `json:"status_raw"`  // 页面原文
	DailyLimit    string `json:"daily_limit"` // 单日累计购买上限(元), 与状态独立记录
	FeeOriginal   string `json:"fee_original"`
	FeeDiscounted string `json:"fee_discounted"`
}

var (
	reLimit = regexp.MustCompile(`单日累计购买上限\s*([\d.]+)\s*元`)
	reFee   = regexp.MustCompile(`([\d.]+)%\s*([\d.]+)%`)
	reName  = regexp.MustCompile(`^(.*?)\(\d{6}\)`)
)

func probeFundPage(code string) (*FundPageInfo, error) {
	body, err := fetchRaw("https://fund.eastmoney.com/" + code + ".html")
	if err != nil {
		return nil, err
	}
	if code == "040046" {
		saveTestdata("fund_040046.html", body) // 存一份夹具给单测用
	}

	decoded, err := charset.NewReader(bytes.NewReader(body), "text/html")
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(decoded)
	if err != nil {
		return nil, err
	}

	name := "未知"
	if m := reName.FindStringSubmatch(strings.TrimSpace(doc.Find("title").Text())); m != nil {
		name = m[1]
	}

	// 首选: 购买信息区里包含目标标签的小块(li/div); 兜底: 整页文本按 rune 截取
	statusLine, feeLine := "", ""
	doc.Find("li,div").Each(func(_ int, s *goquery.Selection) {
		if statusLine != "" && feeLine != "" {
			return
		}
		t := norm(s.Text())
		if len(t) == 0 || len(t) > 80 || s.Children().Length() > 6 {
			return // 跳过大容器, 只认信息行
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

	info := &FundPageInfo{Code: code, Name: name, StatusRaw: statusLine}
	// 状态词优先于限额: 实测存在 "暂停申购 (单日累计购买上限2.00元)" 这类组合,
	// 语义以状态词为准(暂停就是暂停), 限额作为辅助信息独立记录。
	switch {
	case strings.Contains(statusLine, "暂停申购"):
		info.Status = "suspended"
	case strings.Contains(statusLine, "封闭"):
		info.Status = "closed"
	case strings.Contains(statusLine, "限大额"):
		info.Status = "limited"
	case strings.Contains(statusLine, "开放申购"):
		info.Status = "open"
	default:
		info.Status = "unknown"
	}
	if m := reLimit.FindStringSubmatch(statusLine); m != nil {
		info.DailyLimit = m[1]
	}
	if m := reFee.FindStringSubmatch(feeLine); m != nil {
		info.FeeOriginal, info.FeeDiscounted = m[1], m[2]
	}
	return info, nil
}

func norm(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// sliceFrom 从 marker 出现处向后取 n 个 rune, 避免按字节截断把中文切坏。
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
