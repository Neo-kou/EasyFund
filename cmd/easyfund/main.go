// easyfund — QDII 限购雷达
//
// 子命令:
//
//	easyfund crawl  -config config.yaml   每日抓取流水线(cron 触发, 跑完退出)
//	easyfund serve  -config config.yaml   常驻 API + 静态页(第 1 周末实现)
//	easyfund verify -config config.yaml   新鲜度看门狗(第 2 周实现)
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/neokou/easyfund/internal/api"
	"github.com/neokou/easyfund/internal/config"
	"github.com/neokou/easyfund/internal/crawl"
	"github.com/neokou/easyfund/internal/notify"
	"github.com/neokou/easyfund/internal/store"
)

const usage = `用法: easyfund <子命令> [参数]

子命令:
  crawl        每日抓取: 批量接口→ETF行情→交叉验证→入库→变动检测
  serve        HTTP API + 静态页 (GET / 与 GET /api/qdii)
  verify       数据新鲜度看门狗 (第 2 周实现)
  notify-test  发送一封 SMTP 告警测试邮件 (验证授权码配置)

参数:
  -config  配置文件路径 (默认 config.yaml)`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		os.Exit(2)
	}
	sub := os.Args[1]

	var cfgPath string
	switch sub {
	case "crawl", "serve", "verify", "notify-test":
		fs := flag.NewFlagSet(sub, flag.ExitOnError)
		fs.StringVar(&cfgPath, "config", "config.yaml", "配置文件路径")
		_ = fs.Parse(os.Args[2:])
	default:
		fmt.Println(usage)
		os.Exit(2)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Error("配置加载失败", "err", err)
		os.Exit(1)
	}

	switch sub {
	case "crawl":
		runCrawl(cfg, log)
	case "serve":
		runServe(cfg, log)
	case "notify-test":
		runNotifyTest(cfg, log)
	case "verify":
		log.Error("verify 尚未实现(计划第 2 周: 新鲜度看门狗 + 告警邮件)")
		os.Exit(1)
	}
}

func runCrawl(cfg *config.Config, log *slog.Logger) {
	sender := notify.NewSender(cfg.SMTP, log)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		failAlert(sender, log, "数据库打开失败", err)
	}
	defer st.Close()

	if _, err := crawl.Run(cfg, st, log, sender); err != nil {
		failAlert(sender, log, "crawl 失败", err)
	}
}

// failAlert crawl 主流程失败的兜底: 记日志 + 发告警邮件(技术方案 §5 触发规则) 后退出。
func failAlert(sender *notify.Sender, log *slog.Logger, msg string, err error) {
	log.Error(msg, "err", err)
	body := fmt.Sprintf("%s\n错误: %v\n时间: %s\n\n请登录服务器查看日志。",
		msg, err, time.Now().Format("2006-01-02 15:04:05"))
	if serr := sender.Send("【EasyFund】crawl 告警", body); serr != nil {
		log.Error("告警邮件发送失败", "err", serr)
	}
	os.Exit(1)
}

// runNotifyTest 发一封测试邮件, 验证 SMTP 授权码可用(部署/cron 排障用)。
func runNotifyTest(cfg *config.Config, log *slog.Logger) {
	sender := notify.NewSender(cfg.SMTP, log)
	if !sender.Enabled() {
		log.Error("SMTP 未配置完整(需要 smtp.host/user/pass/to; 真实授权码放 config.prod.yaml)")
		os.Exit(1)
	}
	body := fmt.Sprintf("这是一封 EasyFund 告警测试邮件。\n时间: %s\n收到即说明 SMTP 配置可用。",
		time.Now().Format("2006-01-02 15:04:05"))
	if err := sender.Send("【EasyFund】告警测试", body); err != nil {
		log.Error("测试邮件发送失败", "err", err)
		os.Exit(1)
	}
	log.Info("测试邮件已发送", "to", cfg.SMTP.To)
}

func runServe(cfg *config.Config, log *slog.Logger) {
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Error("数据库打开失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	addr := cfg.Server.Addr
	if addr == "" {
		addr = ":8080"
	}
	log.Info("serve 启动", "addr", addr, "db", cfg.DBPath)
	if err := api.NewRouter(st, log).Run(addr); err != nil {
		log.Error("serve 退出", "err", err)
		os.Exit(1)
	}
}
