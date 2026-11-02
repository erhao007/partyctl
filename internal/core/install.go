// 下载并安装 mihomo 内核。默认先走 PARTYCTL_MIRROR（get.251819788.xyz），失败再回退 GitHub。
package core

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/mirror"
)

const githubAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases"

var (
	tagPattern  = regexp.MustCompile(`^v[0-9A-Za-z._-]+$`)
	namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

	downloadAttempts = 3
	retryWait        = time.Second
	headerTimeout    = 20 * time.Second
	stallTimeout     = 45 * time.Second
	overallTimeout   = 10 * time.Minute
)

type ghAsset struct {
	Name string `json:"name"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// source 是一处内核下载源：版本查询和资产地址都由它生成。
type source struct {
	name     string
	latest   string
	tagURL   func(tag string) string
	assetURL func(tag, name string) string
}

// Install 按当前镜像设置下载 mihomo。镜像失败时回退 GitHub。
func Install(version string) (string, error) {
	return InstallWith(version, mirror.Base())
}

// InstallWith 使用指定镜像原点下载。mirrorBase 为空时只访问 GitHub。
func InstallWith(version, mirrorBase string) (string, error) {
	return installUsing(version, sourcesFor(mirrorBase))
}

// sourcesFor 决定下载顺序。有镜像时先镜像，再 GitHub。
func sourcesFor(mirrorBase string) []source {
	var out []source
	if mirrorBase != "" {
		out = append(out, mirrorSource(mirrorBase))
	}
	out = append(out, githubSource())
	return out
}

func mirrorSource(base string) source {
	return source{
		name:   "镜像",
		latest: base + "/mihomo/releases/latest",
		tagURL: func(tag string) string {
			return base + "/mihomo/releases/tags/" + tag
		},
		assetURL: func(tag, name string) string {
			return base + "/mihomo/download/" + tag + "/" + name
		},
	}
}

func githubSource() source {
	return source{
		name:   "GitHub",
		latest: githubAPI + "/latest",
		tagURL: func(tag string) string {
			return githubAPI + "/tags/" + tag
		},
		assetURL: func(tag, name string) string {
			return "https://github.com/MetaCubeX/mihomo/releases/download/" + tag + "/" + url.PathEscape(name)
		},
	}
}

func installUsing(version string, sources []source) (string, error) {
	if err := config.EnsureDirs(); err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", fmt.Errorf("没有可用的内核下载源")
	}
	var errs []string
	for i, src := range sources {
		tag, err := installFromSource(version, src)
		if err == nil {
			return tag, nil
		}
		errs = append(errs, src.name+": "+err.Error())
		if i < len(sources)-1 {
			fmt.Fprintf(os.Stderr, "从 %s 下载内核失败（%v），改试下一个来源…\n", src.name, err)
		}
	}
	return "", fmt.Errorf("下载内核失败: %s", strings.Join(errs, "；"))
}

func installFromSource(version string, src source) (string, error) {
	rel, err := fetchRelease(src, version)
	if err != nil {
		return "", err
	}
	if !tagPattern.MatchString(rel.TagName) {
		return "", fmt.Errorf("无效的版本号 %q", rel.TagName)
	}
	name, err := pickAssetName(rel.Assets, archAsset())
	if err != nil {
		return "", err
	}
	if !namePattern.MatchString(name) {
		return "", fmt.Errorf("无效的文件名 %q", name)
	}
	rawURL := src.assetURL(rel.TagName, name)
	fmt.Fprintf(os.Stderr, "正在从 %s 下载 %s …\n", src.name, name)
	tmpGz := filepath.Join(config.DataDir(), name)
	if err := downloadFile(rawURL, tmpGz); err != nil {
		return "", err
	}
	defer os.Remove(tmpGz)

	dest := config.CoreBinaryPath()
	if err := gunzipTo(tmpGz, dest); err != nil {
		return "", err
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func archAsset() string {
	switch runtime.GOARCH {
	case "amd64":
		return "linux-amd64"
	case "arm64":
		return "linux-arm64"
	default:
		return "linux-" + runtime.GOARCH
	}
}

// pickAssetName 选择当前架构的 mihomo .gz，跳过 compatible 和带 Go 版本的变体。
func pickAssetName(assets []ghAsset, want string) (string, error) {
	want = strings.ToLower(want)
	fallback := ""
	for _, a := range assets {
		n := strings.ToLower(a.Name)
		if !strings.Contains(n, want) || !strings.HasSuffix(n, ".gz") || strings.HasSuffix(n, ".tar.gz") {
			continue
		}
		if strings.Contains(n, "go") {
			continue
		}
		if strings.Contains(n, "compatible") {
			if fallback == "" {
				fallback = a.Name
			}
			continue
		}
		return a.Name, nil
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("没有匹配 %s 的 .gz 资源", want)
}

func fetchRelease(src source, version string) (*ghRelease, error) {
	rawURL := src.latest
	if version != "" && version != "latest" {
		tag := version
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		if !tagPattern.MatchString(tag) {
			return nil, fmt.Errorf("无效的版本号 %q", version)
		}
		rawURL = src.tagURL(tag)
	}
	body, err := getBytes(rawURL)
	if err != nil {
		return nil, err
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("解析版本信息: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("版本信息缺少 tag_name")
	}
	return &rel, nil
}

func getBytes(rawURL string) ([]byte, error) {
	var last error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		body, err := getOnce(rawURL)
		if err == nil {
			return body, nil
		}
		last = err
		if !retryable(err) || attempt == downloadAttempts {
			break
		}
		time.Sleep(retryWait)
	}
	return nil, last
}

func downloadFile(rawURL, dest string) error {
	var last error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		err := downloadOnce(rawURL, dest)
		if err == nil {
			return nil
		}
		last = err
		os.Remove(dest)
		if !retryable(err) || attempt == downloadAttempts {
			break
		}
		time.Sleep(retryWait)
	}
	return last
}

func getOnce(rawURL string) ([]byte, error) {
	resp, err := doGet(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg := readLimited(resp.Body, 512)
		err := fmt.Errorf("GET %s: %s: %s", rawURL, resp.Status, msg)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, &retryError{err: err}
		}
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &retryError{err: fmt.Errorf("读取版本信息: %w", err)}
	}
	return body, nil
}

func downloadOnce(rawURL, dest string) error {
	resp, err := doGet(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg := readLimited(resp.Body, 512)
		err := fmt.Errorf("下载 %s: %s: %s", rawURL, resp.Status, msg)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return &retryError{err: err}
		}
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := copyStall(f, resp.Body, stallTimeout); err != nil {
		return &retryError{err: fmt.Errorf("下载中断: %w", err)}
	}
	return nil
}

func doGet(rawURL string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), overallTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("User-Agent", "partyctl")
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream;q=0.9, */*;q=0.8")
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: headerTimeout,
			TLSHandshakeTimeout:   15 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, &retryError{err: err}
	}
	// 调用方关闭 body 时一并取消 context。
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	if c.ReadCloser == nil {
		return nil
	}
	err := c.ReadCloser.Close()
	c.ReadCloser = nil
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return err
}

type retryError struct{ err error }

func (e *retryError) Error() string { return e.err.Error() }
func (e *retryError) Unwrap() error { return e.err }

func retryable(err error) bool {
	var re *retryError
	return errors.As(err, &re)
}

func readLimited(r io.Reader, n int64) string {
	b, _ := io.ReadAll(io.LimitReader(r, n))
	return strings.TrimSpace(string(b))
}

// copyStall 复制响应体；连续 stall 没有新数据则失败，避免慢速被墙连接一直占着超时。
func copyStall(dst io.Writer, src io.ReadCloser, stall time.Duration) error {
	buf := make([]byte, 32*1024)
	for {
		type result struct {
			n   int
			err error
		}
		ch := make(chan result, 1)
		go func() {
			n, err := src.Read(buf)
			ch <- result{n, err}
		}()
		timer := time.NewTimer(stall)
		select {
		case r := <-ch:
			timer.Stop()
			if r.n > 0 {
				if _, err := dst.Write(buf[:r.n]); err != nil {
					return err
				}
			}
			if r.err == io.EOF {
				return nil
			}
			if r.err != nil {
				return r.err
			}
		case <-timer.C:
			_ = src.Close()
			return fmt.Errorf("超过 %s 没有收到数据", stall)
		}
	}
}

func gunzipTo(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, gr); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, dest)
}

// Version 运行 mihomo -v。
func Version() (string, error) {
	bin := config.CoreBinaryPath()
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("mihomo not installed at %s", bin)
	}
	out, err := exec.Command(bin, "-v").CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
