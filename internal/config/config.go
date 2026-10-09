package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/neokou/easyfund/internal/model"
	"gopkg.in/yaml.v3"
)

type SMTP struct {
	Host string   `yaml:"host"`
	Port int      `yaml:"port"`
	User string   `yaml:"user"`
	Pass string   `yaml:"pass"`
	To   []string `yaml:"to"`
}

type Config struct {
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`
	DBPath           string        `yaml:"db_path"`
	SMTP             SMTP          `yaml:"smtp"`
	CrossCheckPerDay int           `yaml:"cross_check_per_day"`
	Funds            []model.Fund  `yaml:"funds"`
}

var reCode = regexp.MustCompile(`^\d{6}$`)

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	if cfg.CrossCheckPerDay <= 0 {
		cfg.CrossCheckPerDay = 2
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "data/easyfund.db"
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate 启动期校验: 所有问题一次性报全, 不挤牙膏。
func (c *Config) validate() error {
	var errs []string
	if len(c.Funds) == 0 {
		errs = append(errs, "funds 为空")
	}
	seen := map[string]bool{}
	for i, f := range c.Funds {
		prefix := fmt.Sprintf("funds[%d] %s", i, f.Code)
		if !reCode.MatchString(f.Code) {
			errs = append(errs, prefix+": code 必须是 6 位数字")
		}
		if seen[f.Code] {
			errs = append(errs, prefix+": code 重复")
		}
		seen[f.Code] = true
		if f.Name == "" {
			errs = append(errs, prefix+": name 为空")
		}
		if f.Index == "" {
			errs = append(errs, prefix+": index 为空")
		}
		if f.Share == "" {
			errs = append(errs, prefix+": share 为空")
		}
		if f.EtfCode != "" && !reCode.MatchString(f.EtfCode) {
			errs = append(errs, prefix+": etf_code 必须是 6 位数字或留空")
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("配置校验失败(%d 处):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	return nil
}

// NameOf 代码→名称映射(日志友好输出用)。
func (c *Config) NameOf(code string) string {
	for _, f := range c.Funds {
		if f.Code == code {
			return f.Name
		}
	}
	return code
}
