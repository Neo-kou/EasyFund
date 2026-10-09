package crawler

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// UserAgent 如实标识: 日频、单机、礼貌抓取是项目红线(不伪装成普通浏览器之外的爬虫大军)。
const UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 EasyFund/0.1"

var client = &http.Client{Timeout: 20 * time.Second}

// FetchRaw 匿名 GET, 只用于公开页面/接口, 绝不带登录态。
func FetchRaw(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}
