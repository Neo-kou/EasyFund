package model

import "time"

// Fund 监控池里的基金(config.yaml 核心池为事实源; 自动扩容池由批量接口按类型过滤生成)。
type Fund struct {
	Code    string   `yaml:"code" json:"code"`
	Name    string   `yaml:"name" json:"name"`
	Index   string   `yaml:"index" json:"index"`
	Share   string   `yaml:"share" json:"share"`     // A / C / LOF
	EtfCode string   `yaml:"etf_code" json:"etf_code"` // 同指数场内 ETF, 空串=无
	Tags    []string `yaml:"-" json:"tags"`      // 公司/指数/份额/类型/特征标签(tag 包推导)
	IsCore  bool     `yaml:"-" json:"is_core"`   // true=config.yaml 核心池(etf 映射已核实); false=自动扩入
}

// Status 申购状态归一化枚举。
type Status string

const (
	StatusOpen      Status = "open"
	StatusLimited   Status = "limited"
	StatusSuspended Status = "suspended"
	StatusClosed    Status = "closed"
	StatusListed    Status = "listed"  // 场内交易(ETF 本身)
	StatusRaising   Status = "raising" // 认购期(新基金募集)
	StatusUnknown   Status = "unknown"
)

// StatusWord Status 的中文原文(批量接口用语)。
var StatusWord = map[Status]string{
	StatusOpen:      "开放申购",
	StatusLimited:   "限大额",
	StatusSuspended: "暂停申购",
	StatusClosed:    "封闭期",
	StatusListed:    "场内交易",
	StatusRaising:   "认购期",
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
