package crawl

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neokou/easyfund/internal/config"
	"github.com/neokou/easyfund/internal/crawler"
	"github.com/neokou/easyfund/internal/model"
	"github.com/neokou/easyfund/internal/notify"
	"github.com/neokou/easyfund/internal/store"
	"github.com/neokou/easyfund/internal/tag"
)

// Result crawl 运行摘要。
type Result struct {
	TradeDate     string
	Total         int
	OK            int
	Failed        int
	Changed       int // 与前一交易日相比有变动的基金数
	CrossMismatch int // 交叉验证不一致数(>=2 触发告警邮件, 技术方案 §5)
	Duration      time.Duration
}

// Run 抓取流水线:
//
//	① 批量接口(3 请求) → ② ETF 行情+净值 → ③ 组装快照+校验
//	④ 轮换抽验 2 只基金主页交叉验证 → ⑤ 入库(UPSERT) → ⑥ 与前一日 diff
//
// 日均请求 ≈ 3+2+1+len(ETF) ≈ 18 次, 全部串行+间隔, 对源站友好。
func Run(cfg *config.Config, st *store.Store, log *slog.Logger, alerter *notify.Sender) (*Result, error) {
	started := time.Now()
	res := &Result{Total: len(cfg.Funds)}

	// ① 主源: 全市场申购状态
	sgzt, err := crawler.FetchSgzt()
	if err != nil {
		return nil, fmt.Errorf("主源失败: %w", err)
	}
	log.Info("主源拉取完成", "record", sgzt.Record, "fetched", sgzt.Fetched, "show_day", sgzt.ShowDay)
	res.TradeDate = sgzt.ShowDay
	if res.TradeDate == "" {
		res.TradeDate = time.Now().Format("2006-01-02")
	}

	// 基金池 = config 核心池 + 自动扩容池(QDII+海外指数, 2026-10-10 决策)
	pool := expandPool(cfg, sgzt.Rows)
	res.Total = len(pool)
	nameByCode := map[string]string{}
	for _, f := range pool {
		nameByCode[f.Code] = f.Name
	}
	log.Info("基金池就绪", "total", res.Total, "core", len(cfg.Funds))

	// ② ETF 行情 + 净值 → 溢价
	quotes, err := crawler.FetchQuotes(uniqueEtfs(cfg.Funds))
	if err != nil {
		log.Warn("ETF 行情获取失败, 本次溢价字段留空", "err", err)
	} else {
		log.Info("ETF 行情完成", "count", len(quotes))
	}
	quoteByCode := map[string]*crawler.Quote{}
	for _, q := range quotes {
		quoteByCode[q.Code] = q
	}

	// ③ 组装快照 + 字段校验
	var snaps []model.Snapshot
	var warnings []string
	for _, f := range pool {
		row, ok := sgzt.Rows[f.Code]
		if !ok {
			res.Failed++
			warnings = append(warnings, fmt.Sprintf("%s %s: 未在批量接口命中", f.Code, f.Name))
			continue
		}
		snap, warns := buildSnapshot(f, row, res.TradeDate, quoteByCode)
		snaps = append(snaps, snap)
		res.OK++
		warnings = append(warnings, warns...)
	}
	for _, w := range warnings {
		log.Warn("数据告警", "detail", w)
	}

	// ④ 交叉验证: 按天轮换抽验 N 只基金主页; 不一致 >=2 触发告警邮件(§5)
	checked, mismatched, detail := crossCheck(cfg, sgzt.Rows, log)
	res.CrossMismatch = mismatched
	if mismatched >= 2 && alerter != nil {
		body := fmt.Sprintf("日期: %s\n抽验 %d 只, 不一致 %d 只:\n%s\n\n请登录服务器核对源站数据。",
			res.TradeDate, checked, mismatched, strings.Join(detail, "\n"))
		if err := alerter.Send("【EasyFund】数据异常: 交叉验证不一致", body); err != nil {
			log.Error("告警邮件发送失败", "err", err)
		}
	}

	// ⑤ 入库
	if err := st.UpsertFunds(pool); err != nil {
		return nil, fmt.Errorf("fund 表写入失败: %w", err)
	}
	if err := st.UpsertSnapshots(snaps); err != nil {
		return nil, fmt.Errorf("snapshot 表写入失败: %w", err)
	}

	// ⑥ 与前一交易日 diff(第 5 周把日志换成邮件)
	prev, err := st.PrevSnapshots(res.TradeDate)
	if err != nil {
		log.Warn("历史快照读取失败, 跳过 diff", "err", err)
	}
	if prev == nil {
		log.Info("首次入库, 无历史可对比", "date", res.TradeDate, "rows", len(snaps))
	} else {
		for i := range snaps {
			cur := snaps[i]
			if p, ok := prev[cur.Code]; ok {
				if msgs := diffSnapshot(&p, &cur); len(msgs) > 0 {
					res.Changed++
					log.Info("限购变动", "code", cur.Code, "name", nameByCode[cur.Code],
						"change", strings.Join(msgs, "; "))
				}
			}
		}
	}

	res.Duration = time.Since(started).Round(time.Millisecond)
	log.Info("crawl 完成",
		"trade_date", res.TradeDate, "total", res.Total, "ok", res.OK,
		"failed", res.Failed, "changed", res.Changed, "duration", res.Duration.String())
	return res, nil
}

// buildSnapshot 批量接口行 → 快照; 返回校验告警。
func buildSnapshot(f model.Fund, row crawler.SgztRow, tradeDate string, quotes map[string]*crawler.Quote) (model.Snapshot, []string) {
	var warns []string
	sn := model.Snapshot{
		Code:           f.Code,
		TradeDate:      tradeDate,
		StatusRaw:      row.PurchaseStatus,
		NavDate:        row.NavDate,
		EtfCode:        f.EtfCode,
		FetchedAt:      model.NowRFC3339(),
		PurchaseStatus: model.NormalizeStatus(row.PurchaseStatus),
	}
	if sn.PurchaseStatus == model.StatusUnknown {
		warns = append(warns, fmt.Sprintf("%s %s: 未知状态词 %q", f.Code, f.Name, row.PurchaseStatus))
	}
	// 限额: 原始数字; >=1e8 视为无限额(nil), 0(暂停时)也不适用(nil)
	if v, err := strconv.ParseFloat(row.PurchaseLimit, 64); err == nil && v > 0 && v < 1e8 {
		sn.DailyLimit = &v
	}
	if v, err := strconv.ParseFloat(row.MinBuy, 64); err == nil && v > 0 {
		sn.MinBuy = &v
	}
	// 折后费率: 接口给百分数("0.12%"), 库里存小数; C 类 0.00% 合法
	if s := strings.TrimSuffix(row.Fee, "%"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			if v >= 0 && v <= 15 {
				fv := v / 100
				sn.PurchaseFee = &fv
			} else {
				warns = append(warns, fmt.Sprintf("%s %s: 费率超出合理范围 %q", f.Code, f.Name, row.Fee))
			}
		}
	}
	if v, err := strconv.ParseFloat(row.Nav, 64); err == nil && v > 0 {
		sn.Nav = &v
	}
	if f.EtfCode != "" {
		if q, ok := quotes[f.EtfCode]; ok {
			if q.Price > 0 {
				p := q.Price
				sn.EtfPrice = &p
			}
			if q.HasPremium {
				pv := q.PremiumPct / 100
				sn.EtfPremium = &pv
			}
		}
	}
	return sn, warns
}

// crossCheck 按一年中的第几天轮换抽验, 保证每个基金周期性被人工级数据核对。
// 返回 抽验数/不一致数/不一致明细(告警邮件正文用)。
func crossCheck(cfg *config.Config, rows map[string]crawler.SgztRow, log *slog.Logger) (checked, mismatched int, detail []string) {
	n := len(cfg.Funds)
	if n == 0 {
		return 0, 0, nil
	}
	yday := time.Now().YearDay()
	for i := 0; i < cfg.CrossCheckPerDay && i < n; i++ {
		f := cfg.Funds[(yday+i)%n]
		page, err := crawler.FetchFundPage(f.Code)
		if err != nil {
			log.Warn("交叉验证抓取失败", "code", f.Code, "err", err)
			continue
		}
		row, ok := rows[f.Code]
		if !ok {
			continue
		}
		checked++
		word := model.StatusWord[page.Status]
		if word == "" || strings.Contains(row.PurchaseStatus, word) {
			log.Info("交叉验证一致", "code", f.Code, "batch", row.PurchaseStatus, "page", page.StatusRaw)
		} else {
			mismatched++
			detail = append(detail, fmt.Sprintf("%s %s: 批量=%q 主页=%q", f.Code, f.Name, row.PurchaseStatus, page.StatusRaw))
			log.Warn("交叉验证不一致, 疑似数据异常", "code", f.Code, "batch", row.PurchaseStatus, "page", page.StatusRaw)
		}
		time.Sleep(1 * time.Second)
	}
	return checked, mismatched, detail
}

// diffSnapshot 两日快照对比, 返回人类可读的变动描述。
func diffSnapshot(prev, cur *model.Snapshot) []string {
	var msgs []string
	if prev.PurchaseStatus != cur.PurchaseStatus {
		msgs = append(msgs, fmt.Sprintf("状态 %s(%s)→%s(%s)",
			prev.PurchaseStatus, prev.StatusRaw, cur.PurchaseStatus, cur.StatusRaw))
	}
	if !sameFloatPtr(prev.DailyLimit, cur.DailyLimit) {
		msgs = append(msgs, fmt.Sprintf("限额 %s→%s", limitText(prev.DailyLimit), limitText(cur.DailyLimit)))
	}
	if !sameFloatPtr(prev.PurchaseFee, cur.PurchaseFee) {
		msgs = append(msgs, fmt.Sprintf("费率 %s→%s", feeText(prev.PurchaseFee), feeText(cur.PurchaseFee)))
	}
	return msgs
}

func sameFloatPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func limitText(p *float64) string {
	if p == nil {
		return "无限额/不适用"
	}
	return fmt.Sprintf("%.0f元", *p)
}

func feeText(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", *p*100)
}

func uniqueEtfs(funds []model.Fund) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range funds {
		if f.EtfCode != "" && !seen[f.EtfCode] {
			seen[f.EtfCode] = true
			out = append(out, f.EtfCode)
		}
	}
	return out
}

// expandPool 合并基金池: config 核心池(字段以人工核实为准) + 自动扩容池。
// 扩容口径(2026-10-10 实测): 接口类型含 "QDII" 或为 "指数型-海外股票", 约 738 只;
// 自动池 etf_code 留空(不逐只取净值, 守住爬虫礼仪), 溢价仅核心池有。
func expandPool(cfg *config.Config, rows map[string]crawler.SgztRow) []model.Fund {
	core := map[string]bool{}
	pool := make([]model.Fund, 0, len(rows))
	for _, f := range cfg.Funds {
		f.IsCore = true
		r := rows[f.Code]
		f.Tags = tag.Derive(f.Name, r.Type, r.PurchaseStatus)
		pool = append(pool, f)
		core[f.Code] = true
	}
	for code, r := range rows {
		if core[code] || !inScope(r.Type) {
			continue
		}
		pool = append(pool, model.Fund{
			Code:  code,
			Name:  r.Name,
			Index: tag.Index(r.Name),
			Share: firstShare(tag.Shares(r.Name)),
			Tags:  tag.Derive(r.Name, r.Type, r.PurchaseStatus),
		})
	}
	sort.Slice(pool, func(i, j int) bool { return pool[i].Code < pool[j].Code })
	return pool
}

func inScope(apiType string) bool {
	return strings.Contains(apiType, "QDII") || apiType == "指数型-海外股票"
}

func firstShare(shares []string) string {
	// 份额字段优先 A/C/E 字母, 其次美元/LOF 形态
	for _, s := range shares {
		if len(s) == 1 {
			return s
		}
	}
	if len(shares) > 0 {
		return shares[0]
	}
	return ""
}
