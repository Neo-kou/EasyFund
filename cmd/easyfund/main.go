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

	"github.com/neokou/easyfund/internal/config"
	"github.com/neokou/easyfund/internal/crawl"
	"github.com/neokou/easyfund/internal/store"
)

const usage = `用法: easyfund <子命令> [参数]

子命令:
  crawl   每日抓取: 批量接口→ETF行情→交叉验证→入库→变动检测
  serve   HTTP API + 静态页 (开发中)
  verify  数据新鲜度看门狗 (第 2 周实现)

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
	case "crawl", "serve", "verify":
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
		log.Error("serve 尚未实现(计划第 1 周末: Gin + GET /api/qdii)")
		os.Exit(1)
	case "verify":
		log.Error("verify 尚未实现(计划第 2 周: 新鲜度看门狗 + 告警邮件)")
		os.Exit(1)
	}
}

func runCrawl(cfg *config.Config, log *slog.Logger) {
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Error("数据库打开失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if _, err := crawl.Run(cfg, st, log); err != nil {
		log.Error("crawl 失败", "err", err)
		os.Exit(1)
	}
}
