// Package notify SMTP 告警邮件(QQ 邮箱授权码, 465 端口隐式 TLS)。
// 触发规则见技术方案 §5: 批量接口失败 / 抽验不一致 ≥2 / verify 超时(第 2 周)。
package notify

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/smtp"
	"strings"
	"time"

	"github.com/neokou/easyfund/internal/config"
)

// Sender SMTP 发信器; 配置不完整时发送降级为日志, 不让告警通道搞挂主流程。
type Sender struct {
	cfg config.SMTP
	log *slog.Logger
}

func NewSender(cfg config.SMTP, log *slog.Logger) *Sender {
	return &Sender{cfg: cfg, log: log}
}

// Enabled 配置齐全才启用。
func (s *Sender) Enabled() bool {
	return s.cfg.Host != "" && s.cfg.Port > 0 && s.cfg.User != "" && s.cfg.Pass != "" && len(s.cfg.To) > 0
}

// Send 发送纯文本告警邮件。
func (s *Sender) Send(subject, body string) error {
	if !s.Enabled() {
		s.log.Warn("SMTP 未配置, 告警仅写日志", "subject", subject)
		return nil
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.cfg.Host})
	if err != nil {
		return fmt.Errorf("连接 %s: %w", addr, err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Auth(smtp.PlainAuth("", s.cfg.User, s.cfg.Pass, s.cfg.Host)); err != nil {
		return fmt.Errorf("SMTP 认证失败(检查授权码): %w", err)
	}
	if err := c.Mail(s.cfg.User); err != nil {
		return err
	}
	for _, to := range s.cfg.To {
		if err := c.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, s.message(subject, body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// message RFC 822 邮件: 中文主题做 RFC 2047 Base64 编码防乱码, 正文 UTF-8 纯文本。
func (s *Sender) message(subject, body string) string {
	var h strings.Builder
	fmt.Fprintf(&h, "From: %s\r\n", s.cfg.User)
	fmt.Fprintf(&h, "To: %s\r\n", strings.Join(s.cfg.To, ","))
	fmt.Fprintf(&h, "Subject: =?UTF-8?B?%s?=\r\n", base64.StdEncoding.EncodeToString([]byte(subject)))
	fmt.Fprintf(&h, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	h.WriteString("MIME-Version: 1.0\r\n")
	h.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	h.WriteString("\r\n")
	h.WriteString(body)
	return h.String()
}
