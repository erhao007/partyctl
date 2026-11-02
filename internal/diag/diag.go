// 通过代理和直连分别访问常用站点，汇总延迟、状态码、节点和规则。
package diag

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/partyctl/partyctl/internal/client"
	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/service"
	"gopkg.in/yaml.v3"
)

// DefaultURLs 是不指定地址时检查的站点。
var DefaultURLs = []string{
	"https://www.google.com",
	"https://github.com",
	"https://api.github.com",
	"https://www.youtube.com",
}

// Attempt 是一次 HTTP 访问的结果。
type Attempt struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status,omitempty"`
	Latency int64  `json:"latency_ms"`
	Error   string `json:"error,omitempty"`
}

// Probe 对比同一个 URL 的直连和代理结果。
type Probe struct {
	URL    string  `json:"url"`
	Direct Attempt `json:"direct"`
	Proxy  Attempt `json:"proxy"`
	Node   string  `json:"node,omitempty"`
	Rule   string  `json:"rule,omitempty"`
	Advice string  `json:"advice"`
}

// Group 是诊断里展示的策略组。
type Group struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Now    string `json:"now"`
	Fixed  string `json:"fixed,omitempty"`
	Manual bool   `json:"manual"`
}

// Report 是一次完整诊断。
type Report struct {
	CoreRunning    bool    `json:"core_running"`
	PIDs           []int   `json:"pids,omitempty"`
	MixedPort      int     `json:"mixed_port"`
	MixedListening bool    `json:"mixed_listening"`
	Controller     string  `json:"controller"`
	ControllerOK   bool    `json:"controller_ok"`
	ControllerErr  string  `json:"controller_error,omitempty"`
	Mode           string  `json:"mode"`
	DNS            string  `json:"dns"`
	Groups         []Group `json:"groups"`
	Probes         []Probe `json:"probes"`
	Summary        string  `json:"summary"`
}

// Run 执行诊断。urls 为空时使用默认站点。timeout 是单次请求超时。
func Run(urls []string, timeout time.Duration) (Report, error) {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if len(urls) == 0 {
		urls = append([]string(nil), DefaultURLs...)
	}
	st, err := config.LoadSettings()
	if err != nil {
		return Report{}, err
	}
	rep := Report{
		MixedPort:  st.MixedPort,
		Controller: st.ExternalController,
		Mode:       st.Mode,
		DNS:        dnsSummary(),
	}
	cores := service.ListOurCores()
	rep.CoreRunning = len(cores) > 0
	for _, c := range cores {
		rep.PIDs = append(rep.PIDs, c.PID)
	}
	rep.MixedListening = tcpOpen(st.MixedPort)
	var api *client.Client
	if c, err := client.New(); err == nil {
		api = c
		if _, err := c.Version(); err != nil {
			rep.ControllerErr = err.Error()
		} else if proxies, err := c.Proxies(); err != nil {
			rep.ControllerErr = err.Error()
		} else {
			rep.ControllerOK = true
			rep.Groups = groupsFrom(proxies)
		}
	} else {
		rep.ControllerErr = err.Error()
	}
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p := Probe{URL: raw}
		p.Direct = fetch(raw, nil, timeout)
		if rep.CoreRunning && rep.MixedListening {
			proxyURL := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", st.MixedPort)}
			p.Proxy = fetch(raw, proxyURL, timeout)
			if api != nil {
				p.Node, p.Rule = lookupConn(api, raw)
			}
		} else if !rep.CoreRunning {
			p.Proxy.Error = "内核未运行"
		} else {
			p.Proxy.Error = "mixed-port 未在监听"
		}
		p.Advice = Advise(p.Direct, p.Proxy, rep.CoreRunning)
		rep.Probes = append(rep.Probes, p)
	}
	rep.Summary = summarize(rep)
	return rep, nil
}

// Advise 根据直连和代理的结果给出中文建议。
func Advise(direct, via Attempt, core bool) string {
	if !core {
		if direct.OK {
			return "内核未运行，目前只能直连。执行 partyctl start 后再诊断代理。"
		}
		return "内核未运行，直连也失败。先确认本机能上网，再 partyctl start。"
	}
	if via.OK && direct.OK {
		return "代理和直连都成功。"
	}
	if via.OK && !direct.OK {
		return "代理可用。直连失败通常是目标在当前网络被重置或超时，这正是需要代理的情况。"
	}
	if !via.OK && direct.OK {
		if strings.Contains(via.Error, "refused") || strings.Contains(via.Error, "未监听") {
			return "连不上 mixed-port。确认 partyctl status 里内核在运行，或用 partyctl port 查看端口。"
		}
		return "直连正常但代理失败。看当前节点是否可用（partyctl node list），或这条规则是否把网站送到了不可用节点。可先 partyctl node select 换一个节点再试。"
	}
	if via.Status >= 400 && direct.Status >= 400 {
		return fmt.Sprintf("两边都得到 HTTP %d / %d。网络是通的，站点本身拒绝了访问。", direct.Status, via.Status)
	}
	return "代理和直连都失败。先看本机网络，再看内核日志 partyctl log。"
}

// Text 把报告打成终端可读的中文。
func (r Report) Text() string {
	var b strings.Builder
	if r.CoreRunning {
		fmt.Fprintf(&b, "内核: 运行中 pid %s\n", join(r.PIDs))
	} else {
		b.WriteString("内核: 未运行\n")
	}
	listen := "未监听"
	if r.MixedListening {
		listen = "正在监听"
	}
	fmt.Fprintf(&b, "mixed-port: %d（%s）\n", r.MixedPort, listen)
	if r.ControllerOK {
		fmt.Fprintf(&b, "控制器: %s 正常\n", r.Controller)
	} else {
		fmt.Fprintf(&b, "控制器: %s 异常", r.Controller)
		if r.ControllerErr != "" {
			fmt.Fprintf(&b, "（%s）", r.ControllerErr)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "模式: %s\n", dash(r.Mode))
	fmt.Fprintf(&b, "DNS: %s\n", dash(r.DNS))
	if len(r.Groups) == 0 {
		b.WriteString("策略组: 无\n")
	} else {
		b.WriteString("策略组:\n")
		for _, g := range r.Groups {
			extra := ""
			if g.Manual {
				extra = " 手动"
			}
			if g.Fixed != "" {
				extra = " 已固定=" + g.Fixed
			}
			fmt.Fprintf(&b, "  %-16s %-10s 当前 %s%s\n", g.Name, g.Type, dash(g.Now), extra)
		}
	}
	b.WriteString("\n")
	for _, p := range r.Probes {
		fmt.Fprintf(&b, "%s\n", p.URL)
		fmt.Fprintf(&b, "  直连: %s\n", formatAttempt(p.Direct))
		fmt.Fprintf(&b, "  代理: %s\n", formatAttempt(p.Proxy))
		if p.Node != "" || p.Rule != "" {
			fmt.Fprintf(&b, "  节点: %s    规则: %s\n", dash(p.Node), dash(p.Rule))
		}
		fmt.Fprintf(&b, "  建议: %s\n\n", p.Advice)
	}
	fmt.Fprintf(&b, "结论: %s\n", r.Summary)
	return b.String()
}

func fetch(raw string, proxy *url.URL, timeout time.Duration) Attempt {
	start := time.Now()
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxy),
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: -1,
		}).DialContext,
		TLSHandshakeTimeout:   timeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: timeout,
	}
	if proxy == nil {
		transport.Proxy = nil
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向过多")
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return Attempt{Error: err.Error(), Latency: time.Since(start).Milliseconds()}
	}
	req.Header.Set("User-Agent", "partyctl-diag")
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return Attempt{Error: shorten(err.Error()), Latency: latency}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
	return Attempt{OK: resp.StatusCode > 0 && resp.StatusCode < 400, Status: resp.StatusCode, Latency: latency}
}

func lookupConn(c *client.Client, raw string) (node, rule string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	host := u.Hostname()
	body, err := c.Connections()
	if err != nil {
		return "", ""
	}
	list, _ := body["connections"].([]any)
	for _, item := range list {
		m, _ := item.(map[string]any)
		if m == nil || !connMatches(m, host) {
			continue
		}
		if chains, _ := m["chains"].([]any); len(chains) > 0 {
			if s, _ := chains[0].(string); s != "" {
				node = s
			}
		}
		if s, _ := m["rule"].(string); s != "" {
			rule = s
			if payload, _ := m["rulePayload"].(string); payload != "" {
				rule += " " + payload
			}
		}
		return node, rule
	}
	return "", ""
}

func connMatches(m map[string]any, host string) bool {
	meta, _ := m["metadata"].(map[string]any)
	if meta == nil {
		return false
	}
	h, _ := meta["host"].(string)
	return strings.EqualFold(h, host)
}

func groupsFrom(proxies map[string]any) []Group {
	pm, _ := proxies["proxies"].(map[string]any)
	var out []Group
	for name, raw := range pm {
		info, _ := raw.(map[string]any)
		if info == nil {
			continue
		}
		if _, ok := info["all"]; !ok {
			continue
		}
		typ := client.GroupType(info)
		now, _ := info["now"].(string)
		g := Group{Name: name, Type: typ, Now: now, Fixed: client.FixedProxy(info)}
		if strings.EqualFold(typ, "Selector") {
			g.Manual = true
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manual != out[j].Manual {
			return out[i].Manual
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func dnsSummary() string {
	data, err := os.ReadFile(config.WorkConfigPath())
	if err != nil {
		return "未生成运行配置"
	}
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) != nil {
		return "运行配置无法解析"
	}
	dns, _ := doc["dns"].(map[string]any)
	if dns == nil {
		return "运行配置里没有 dns 段（跟随订阅或系统解析）"
	}
	listen, _ := dns["listen"].(string)
	mode, _ := dns["enhanced-mode"].(string)
	return fmt.Sprintf("enable=%v listen=%s mode=%s", dns["enable"], dash(listen), dash(mode))
}

func tcpOpen(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func summarize(r Report) string {
	if !r.CoreRunning {
		return "内核未运行，代理诊断只完成了直连部分。"
	}
	ok, fail := 0, 0
	for _, p := range r.Probes {
		if p.Proxy.OK {
			ok++
		} else {
			fail++
		}
	}
	if fail == 0 && ok > 0 {
		return fmt.Sprintf("代理访问 %d 个站点都成功。", ok)
	}
	return fmt.Sprintf("代理成功 %d 个，失败 %d 个。失败项见上面的建议。", ok, fail)
}

func formatAttempt(a Attempt) string {
	if a.Error != "" && a.Status == 0 {
		return fmt.Sprintf("失败 %dms %s", a.Latency, a.Error)
	}
	mark := "失败"
	if a.OK {
		mark = "成功"
	}
	return fmt.Sprintf("%s HTTP %d %dms", mark, a.Status, a.Latency)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func join(pids []int) string {
	if len(pids) == 0 {
		return "-"
	}
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(parts, ", ")
}

func shorten(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 180 {
		return s[:180] + "…"
	}
	return s
}
