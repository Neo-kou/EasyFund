package model

import "time"

// Fund 监控池里的基金(config.yaml 为唯一事实源, fund 表是运行时副本)。
type Fund struct {
	Code    string `yaml:"code" json:"code"`
	Name    string `yaml:"name" json:"name"`
	Index   string `yaml:"index" json:"index"`
	Share   string `yaml:"share" json:"share"`     // A / C / LOF
	EtfCode string `yaml:"etf_code" json:"etf_code"` // 同指数场内 ETF, 空串=无
}

// Status 申购状态归一化枚举。
type Status string

const (
	StatusOpen      Status = "open"
	StatusLimited   Status = "limited"
	StatusSuspended Status = "suspended"
	StatusClosed    Status = "closed"
	StatusUnknown   Status = "unknown"
)

// StatusWord Status 的中文原文(批量接口用语)。
var StatusWord = map[Status]string{
	StatusOpen:      "开放申购",
	StatusLimited:   "限大额",
	StatusSuspended: "暂停申购",
	StatusClosed:    "封闭期",
}

// NormalizeStatus 把接口状态原文归一化; 未知词返回 unknown(由调用方告警)。
func NormalizeStatus(raw string) Status {
	for s, w := range StatusWord {
		if w == raw {
			return s
		}
	}
	return StatusUnknown
}

// Snapshot 每日快照, (code, trade_date) 唯一。
type Snapshot struct {
	Code           string
	TradeDate      string // YYYY-MM-DD, 用批量接口 showday
	PurchaseStatus Status
	StatusRaw      string
	DailyLimit     *float64 // 单日累计申购上限(元); nil = 无限额或暂停时不适用
	MinBuy         *float64
	PurchaseFee    *float64 // 折后申购费率(小数, 0.0012 = 0.12%)
	Nav            *float64
	NavDate        string
	EtfCode        string
	EtfPrice       *float64
	EtfPremium     *float64 // 场内参考溢价(小数, 0.117 = 11.7%); 基于T-1净值
	FetchedAt      string
}

func NowRFC3339() string { return time.Now().Format(time.RFC3339) }
