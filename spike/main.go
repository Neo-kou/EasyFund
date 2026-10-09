// EasyFund 第 1 周 spike: 验证产品所需核心字段能否全部通过公开接口获取。
// 运行方式(在仓库根目录): go run ./spike
//
// 数据源结论(2026-10-08 实测):
//  1. 天天基金 Fund_JJJZ_Data.aspx —— 全市场申购状态批量接口(核心源), 3 次请求覆盖 2.7 万只
//  2. 天天基金单基金主页 —— 状态原文(含精确到分的限额)+费率折扣, 做交叉验证
//  3. ETF 行情(腾讯 qt.gtimg.cn) + ETF 净值(天天 pingzhongdata) —— 计算场内参考溢价
//  4. 蛋卷基金(雪球旗下) djapi —— 备源, 只能做状态交叉验证(无限额字段)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WatchFund 演示用 5 只。
// 正式 20 只清单在第 1 周从天天基金筛选器导出后填入 config.yaml。
type WatchFund struct {
	Code    string
	Name    string
	Index   string
	EtfCode string // 同指数场内 ETF; 空串 = 待补
}

var watchlist = []WatchFund{
	{"040046", "华安纳斯达克100ETF联接A", "纳斯达克100", "159632"},
	{"270042", "广发纳斯达克100A", "纳斯达克100", "159941"},
	{"000834", "大成纳斯达克100A", "纳斯达克100", ""},
	{"050025", "博时标普500ETF联接A", "标普500", "513500"},
	{"000043", "嘉实美国成长股票人民币", "美国股票", ""},
}

func main() {
	started := time.Now()
	report := map[string]any{}
	fmt.Println("==== EasyFund spike @", started.Format("2006-01-02 15:04:05"), "====")

	// ---- [1] 全市场申购状态批量接口 ----
	fmt.Println("\n---- [1] 天天基金·全市场申购状态接口 Fund_JJJZ_Data.aspx ----")
	sgztOK := false
	sgztResp, matched, err := probeSgztAPI(watchlist)
	if err != nil {
		fmt.Println("  失败:", err)
	} else {
		sgztOK = len(matched) > 0
		fmt.Printf("  命中监控列表: %d/%d\n", len(matched), len(watchlist))
		for _, f := range watchlist {
			if r, ok := matched[f.Code]; ok {
				fmt.Printf("  %s | %s | %s | 状态=%s | 限额=%s | 起点=%s元 | 折后费率=%s | 净值=%s(%s)\n",
					r.Code, r.Name, r.Type, r.PurchaseStatus, limitDisplay(r.PurchaseLimit), r.MinBuy, r.Fee, r.Nav, r.NavDate)
			} else {
				fmt.Printf("  %s 未命中(需人工排查)\n", f.Code)
			}
		}
		report["sgzt_universe"] = map[string]any{"record": sgztResp.Record, "fetched": len(sgztResp.Rows), "show_day": sgztResp.ShowDay}
		report["sgzt"] = matched
	}

	// ---- [2] 单基金主页交叉验证 ----
	fmt.Println("\n---- [2] 天天基金·单基金主页 fund.eastmoney.com/{code}.html (交叉验证) ----")
	fundPages := map[string]*FundPageInfo{}
	for _, f := range watchlist {
		info, err := probeFundPage(f.Code)
		if err != nil {
			fmt.Printf("  %s 失败: %v\n", f.Code, err)
			continue
		}
		fundPages[f.Code] = info
		limit := info.DailyLimit
		if limit == "" {
			limit = "-"
		}
		fmt.Printf("  %s | 状态=%s | 单日上限=%s元 | 费率=%s%%→%s%% | 原文: %s\n",
			info.Code, info.Status, limit, info.FeeOriginal, info.FeeDiscounted, info.StatusRaw)
		time.Sleep(1 * time.Second) // 爬虫礼仪: 逐只间隔 1s
	}
	report["fund_page"] = fundPages

	// ---- [2.5] 主源与交叉源一致性检查 ----
	fmt.Println("\n---- [2.5] 主源(批量接口) vs 交叉源(单基金页) 一致性 ----")
	for _, f := range watchlist {
		batch, okB := matched[f.Code]
		page, okP := fundPages[f.Code]
		if !okB || !okP {
			continue
		}
		statusWord := map[string]string{
			"open": "开放申购", "limited": "限大额", "suspended": "暂停申购", "closed": "封闭",
		}[page.Status]
		agree := statusWord != "" && strings.Contains(batch.PurchaseStatus, statusWord)
		fmt.Printf("  %s 批量=%s 主页=%s(%s) → %s\n",
			f.Code, batch.PurchaseStatus, statusWord, page.Status, verdict(agree))
	}

	// ---- [3] ETF 行情 + 净值 → 参考溢价 ----
	fmt.Println("\n---- [3] ETF 行情(qt.gtimg.cn) + ETF 净值(pingzhongdata) → 参考溢价 ----")
	quotesOK, premiumOK := false, false
	quotes, err := probeTencentQuotes(uniqueEtfs(watchlist))
	if err != nil {
		fmt.Println("  行情失败:", err)
	} else {
		quotesOK = len(quotes) > 0
		for i := range quotes {
			q := quotes[i]
			fmt.Printf("  %s | %s | 现价=%s | 昨收=%s | 涨跌=%s%% | 时间=%s\n",
				q.Symbol, q.Name, q.Price, q.PrevClose, q.ChangePct, q.Time)
			nav, navDate, err := probeEtfNav(q.Code)
			if err != nil {
				fmt.Printf("    净值获取失败: %v\n", err)
				continue
			}
			q.Nav, q.NavDate, q.PremiumPct = nav, navDate, priceOverNav(q.Price, nav)
			if q.PremiumPct != "N/A" {
				premiumOK = true
			}
			fmt.Printf("    最新净值=%s (%s) | 参考溢价=%s%% (基于T-1净值, 仅供参考)\n", nav, navDate, q.PremiumPct)
		}
		report["etf_quotes"] = quotes
		time.Sleep(1 * time.Second)
	}

	// ---- [4] 蛋卷基金 djapi 备源 ----
	fmt.Println("\n---- [4] 备源: 蛋卷基金 danjuanfunds.com/djapi/fund/{code} ----")
	danjuanOK := false
	// 040046 当前限大额、000055 当前暂停申购, 两者对照可推断字段语义
	for _, code := range []string{"040046", "000055"} {
		dj, err := probeDanjuan(code)
		if err != nil {
			fmt.Printf("  %s 失败: %v\n", code, err)
			continue
		}
		danjuanOK = true
		fmt.Printf("  %s | subscribe_status=%s | can_buy=%v | 净值=%s(%s) | 原费率=%s%% 折扣=%s\n",
			code, dj.SubscribeStatus, dj.CanBuy, dj.UnitNav, dj.EndDate, dj.DeclareRate, dj.DeclareDiscount)
		time.Sleep(1 * time.Second)
	}

	// ---- [5] 结论: 核心字段数据源矩阵 ----
	fmt.Println("\n---- [5] 结论: 产品核心字段获取路径 ----")
	fmt.Printf("  [%s] 申购状态      主源: Fund_JJJZ_Data.aspx 批量接口(3次请求覆盖全市场)\n", tick(sgztOK))
	fmt.Printf("  [%s] 单日申购限额  主源: 批量接口原始数值; 交叉: 单基金主页(精确到分)\n", tick(sgztOK && len(fundPages) > 0))
	fmt.Printf("  [%s] 折后申购费率  主源: 批量接口手续费列; 交叉: 单基金主页\n", tick(sgztOK && len(fundPages) > 0))
	fmt.Printf("  [%s] ETF 场内价格  腾讯 qt.gtimg.cn (GBK, ~分隔文本)\n", tick(quotesOK))
	fmt.Printf("  [%s] ETF 净值/溢价 pingzhongdata 的 Data_netWorthTrend 末点\n", tick(premiumOK))
	fmt.Printf("  [%s] 备源可用性    蛋卷 djapi(有状态无限额, 仅作交叉验证)\n", tick(danjuanOK))

	out, _ := json.MarshalIndent(report, "", "  ")
	_ = os.WriteFile(filepath.Join("spike", "demo_result.json"), out, 0o644)
	fmt.Printf("\n原始响应夹具已存 spike/testdata/, 汇总已存 spike/demo_result.json, 总耗时 %s\n",
		time.Since(started).Round(time.Second))
}

func tick(b bool) string {
	if b {
		return "OK"
	}
	return "!!"
}

func verdict(b bool) string {
	if b {
		return "一致"
	}
	return "不一致(需排查)"
}

func uniqueEtfs(watch []WatchFund) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range watch {
		if f.EtfCode != "" && !seen[f.EtfCode] {
			seen[f.EtfCode] = true
			out = append(out, f.EtfCode)
		}
	}
	return out
}
