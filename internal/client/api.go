package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/partyctl/partyctl/internal/config"
)

type Client struct {
	Base   string
	Secret string
	HTTP   *http.Client
}

func New() (*Client, error) {
	s, err := config.LoadSettings()
	if err != nil {
		return nil, err
	}
	base := s.ExternalController
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		Base:   strings.TrimRight(base, "/"),
		Secret: s.Secret,
		// 直连控制器。默认 Transport 会把 127.0.0.1 送进 HTTP_PROXY，代理常回 HTML。
		HTTP: directClient(15 * time.Second),
	}, nil
}

// directClient 不走环境变量里的 HTTP 代理。localhost 例外不含 127.0.0.1。
func directClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

func (c *Client) do(method, path string, body any, query url.Values) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	u := c.Base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, controllerUnavailable(c.Base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取控制器 %s 的响应失败: %w", u, err)
	}
	if !jsonBody(data) {
		return nil, fmt.Errorf("控制器 %s 返回 HTTP %d，但正文不是 JSON。该地址上的程序不是 mihomo（端口可能被其他服务占用）。可执行 ss -ltnp | grep %s 查看，或用 partyctl port set 修改端口", u, resp.StatusCode, controllerPort(c.Base))
	}
	if resp.StatusCode >= 400 {
		return data, fmt.Errorf("控制器 %s 返回 HTTP %d：%s", u, resp.StatusCode, truncate(data, 300))
	}
	return data, nil
}

func controllerUnavailable(base string, err error) error {
	msg := err.Error()
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "context deadline") || strings.Contains(msg, "no such host") {
		return fmt.Errorf("内核未运行，请先启动（无法连接控制器 %s）", base)
	}
	return fmt.Errorf("无法连接控制器 %s：%v", base, err)
}

func jsonBody(b []byte) bool {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return true
	}
	return b[0] == '{' || b[0] == '['
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (c *Client) PatchConfigs(patch map[string]any) error {
	_, err := c.do(http.MethodPatch, "/configs", patch, nil)
	return err
}

func (c *Client) Proxies() (map[string]any, error) {
	data, err := c.do(http.MethodGet, "/proxies", nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("控制器 %s/proxies 的 JSON 无法解析: %w", c.Base, err)
	}
	if _, ok := out["proxies"]; !ok {
		return nil, fmt.Errorf("控制器 %s/proxies 返回 HTTP 200，但不是 mihomo 的接口（缺少 proxies 字段）。请求已带上 secret。端口可能被其他程序占用，可执行 ss -ltnp | grep %s，或用 partyctl port set 修改端口", c.Base, controllerPort(c.Base))
	}
	return out, nil
}

func controllerPort(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Port() == "" {
		return base
	}
	return u.Port()
}

func (c *Client) SelectProxy(group, name string) error {
	_, err := c.do(http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name}, nil)
	return err
}

func (c *Client) ProxyDelay(name, testURL string, timeout int) (map[string]any, error) {
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", fmt.Sprintf("%d", timeout))
	data, err := c.do(http.MethodGet, "/proxies/"+url.PathEscape(name)+"/delay", nil, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out, nil
}

func (c *Client) GroupDelay(name, testURL string, timeout int) (map[string]any, error) {
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", fmt.Sprintf("%d", timeout))
	data, err := c.do(http.MethodGet, "/group/"+url.PathEscape(name)+"/delay", nil, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out, nil
}

func (c *Client) Version() (map[string]any, error) {
	data, err := c.do(http.MethodGet, "/version", nil, nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out, nil
}

// StreamLogs streams GET /logs to w until ctx-less EOF/error.
func (c *Client) StreamLogs(w io.Writer) error {
	u := c.Base + "/logs"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	client := directClient(0) // streaming, still bypass HTTP_PROXY
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("logs: %s: %s", resp.Status, string(b))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// OpenStream opens a long-lived GET to the controller (e.g. /logs, /traffic).
func (c *Client) OpenStream(path string) (*http.Response, error) {
	u := c.Base + path
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	client := directClient(0)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("stream %s: %s: %s", path, resp.Status, string(b))
	}
	return resp, nil
}
