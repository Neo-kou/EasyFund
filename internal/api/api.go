// Package api Gin serve: GET /api/qdii 最新快照 + go:embed 静态页。
package api

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/neokou/easyfund/internal/model"
	"github.com/neokou/easyfund/internal/store"
	"github.com/neokou/easyfund/web"
)

// fundView GET /api/qdii 的单只基金视图(技术方案 §6); 数值字段 nil 序列化为 null, 前端显示 "—"。
type fundView struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Index          string   `json:"index"`
	Share          string   `json:"share"`
	Tags           []string `json:"tags"`
	IsCore         bool     `json:"is_core"`
	PurchaseStatus string   `json:"purchase_status"`
	StatusText     string   `json:"status_text"`
	DailyLimit     *float64 `json:"daily_limit"`
	MinBuy         *float64 `json:"min_buy"`
	PurchaseFee    *float64 `json:"purchase_fee"`
	Nav            *float64 `json:"nav"`
	NavDate        string   `json:"nav_date"`
	EtfCode        string   `json:"etf_code"`
	EtfPrice       *float64 `json:"etf_price"`
	EtfPremium     *float64 `json:"etf_premium"`
}

// NewRouter 只挂 Recovery + slog 请求日志两个 middleware(技术方案 §6)。
func NewRouter(st *store.Store, log *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), requestLog(log))

	r.GET("/api/qdii", qdiiHandler(st))

	static := http.FS(web.FS)
	// index.html 经 FileServer 会 301 到 "/", 故直接读 embed 字节返回
	indexHTML, err := web.FS.ReadFile("index.html")
	if err != nil {
		panic("web/index.html 未嵌入: " + err.Error())
	}
	r.GET("/", func(c *gin.Context) { c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML) })
	r.GET("/app.js", func(c *gin.Context) { c.FileFromFS("app.js", static) })
	r.GET("/style.css", func(c *gin.Context) { c.FileFromFS("style.css", static) })
	return r
}

func qdiiHandler(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		snaps, date, err := st.LatestSnapshots()
		if err != nil {
			log500(c, "读取快照失败", err)
			return
		}
		funds, err := st.Funds()
		if err != nil {
			log500(c, "读取基金池失败", err)
			return
		}
		views := make([]fundView, 0, len(funds))
		for _, f := range funds {
			v := fundView{
				Code: f.Code, Name: f.Name, Index: f.Index, Share: f.Share,
				Tags: f.Tags, IsCore: f.IsCore,
				PurchaseStatus: string(model.StatusUnknown), StatusText: "无数据",
				EtfCode: f.EtfCode,
			}
			if sn, ok := snaps[f.Code]; ok {
				v.PurchaseStatus = string(sn.PurchaseStatus)
				v.StatusText = sn.StatusRaw
				v.DailyLimit = sn.DailyLimit
				v.MinBuy = sn.MinBuy
				v.PurchaseFee = sn.PurchaseFee
				v.Nav = sn.Nav
				v.NavDate = sn.NavDate
				v.EtfPrice = sn.EtfPrice
				v.EtfPremium = sn.EtfPremium
			}
			views = append(views, v)
		}
		sort.Slice(views, func(i, j int) bool { return views[i].Code < views[j].Code })
		c.JSON(http.StatusOK, gin.H{"updated_at": date, "funds": views})
	}
}

func log500(c *gin.Context, msg string, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
	_ = c.Error(err)
}

func requestLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http", "method", c.Request.Method, "path", c.Request.URL.Path,
			"status", c.Writer.Status(), "dur", time.Since(start).Round(time.Microsecond).String(),
			"ip", c.ClientIP())
	}
}
