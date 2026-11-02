// Package procproxy 用 cgroup v2 和 nftables 只接管指定进程的 TCP/UDP。
// 不修改系统代理，也不打开全局 TUN。内核停掉时先撤规则，避免被接管进程断网。
package procproxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// 透明代理使用的固定参数。routing-mark 给 mihomo 自己的出站，fwmark 只用于把接管进程导回本机。
const (
	CgroupName = "partyctl"
	CgroupDir  = "/sys/fs/cgroup/partyctl"
	TableName  = "partyctl"
	TProxyPort = 7893
	DNSPort    = 1053
	RouteMark  = 0x2518
	FwMark     = 0x2519
	RouteTable = 9497
	RulePref   = 100
)

// Proc 是一个被接管的进程，以及把它移入接管组之前的 cgroup。
type Proc struct {
	PID      int    `json:"pid"`
	RootPID  int    `json:"root_pid"`
	Original string `json:"original"`
	Cmd      string `json:"cmd"`
	Via      string `json:"via,omitempty"` // app:<匹配串>，空表示按 pid 接管
}

// App 是按程序名或可执行路径持久接管的一条规则。
type App struct {
	Match string `json:"match"`
	Path  string `json:"path,omitempty"`
}

// State 保存在配置目录，重启后据此恢复或清理。
type State struct {
	Enabled   bool   `json:"enabled"`
	DNSPort   int    `json:"dns_port,omitempty"`
	RouteMark int    `json:"route_mark,omitempty"`
	Procs     []Proc `json:"procs"`
	Apps      []App  `json:"apps,omitempty"`
}

var mu sync.Mutex

// statePath 与 partyctl 的配置目录放在一起，方便和内核使用同一份 HOME。
func statePath() string {
	home, _ := os.UserHomeDir()
	if h := os.Getenv("HOME"); h != "" {
		home = h
	}
	return filepath.Join(home, ".config", "partyctl", "proc-proxy.json")
}

// load 读取状态。文件不存在时视为未开启。调用方需已持有 mu。
func load() State {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return State{}
	}
	var st State
	if json.Unmarshal(data, &st) != nil {
		return State{}
	}
	if st.Procs == nil {
		st.Procs = []Proc{}
	}
	return st
}

// save 原子写入状态。调用方需已持有 mu。
func save(st State) error {
	if st.Procs == nil {
		st.Procs = []Proc{}
	}
	if err := os.MkdirAll(filepath.Dir(statePath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := statePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, statePath())
}

// Enabled 报告按进程代理是否处于开启状态（规则可能尚未装上）。
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return load().Enabled
}

// Enable 记下“需要接管”。真正的规则要等内核端口就绪后再装。
func Enable() error {
	mu.Lock()
	defer mu.Unlock()
	st := load()
	st.Enabled = true
	if st.DNSPort == 0 {
		st.DNSPort = DNSPort
	}
	if st.RouteMark == 0 {
		st.RouteMark = RouteMark
	}
	return save(st)
}

// Disable 关闭接管标记。调用方应同时撤掉 nft 规则。
func Disable() error {
	mu.Lock()
	defer mu.Unlock()
	st := load()
	st.Enabled = false
	st.Procs = nil
	st.Apps = nil
	return save(st)
}

// HasApps 报告是否配置了按应用接管。
func HasApps() bool {
	mu.Lock()
	defer mu.Unlock()
	return len(load().Apps) > 0
}
