package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
		HTTP:   &http.Client{Timeout: 30 * time.Second},
	}, nil
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
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return data, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, string(data))
	}
	return data, nil
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
		return nil, err
	}
	return out, nil
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
	client := &http.Client{Timeout: 0} // streaming
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
	client := &http.Client{Timeout: 0}
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
