// 接管、撤销和列出进程。规则的安装由调用方在内核就绪后进行。
package procproxy

import (
	"fmt"
	"strings"
)

// Snap 是给命令行和 Web 用的当前接管情况。
type Snap struct {
	Enabled    bool   `json:"enabled"`
	Rules      bool   `json:"rules"`
	DNSPort    int    `json:"dns_port"`
	TProxyPort int    `json:"tproxy_port"`
	RouteMark  int    `json:"route_mark"`
	Procs      []Proc `json:"procs"`
	Apps       []App  `json:"apps"`
}

// Describe 用一行中文说明当前状态，供 partyctl status 使用。
func Describe() string {
	s := Snapshot()
	if !s.Enabled {
		return "关"
	}
	n := len(s.Procs)
	if len(s.Apps) > 0 && s.Rules {
		return fmt.Sprintf("开，%d 个应用，%d 个进程", len(s.Apps), n)
	}
	if s.Rules && n > 0 {
		return fmt.Sprintf("开，%d 个进程", n)
	}
	if s.Rules {
		return "开，规则已装，当前没有进程"
	}
	if n > 0 {
		return fmt.Sprintf("开，规则未装（直连），%d 个进程", n)
	}
	return "开，规则未装（直连）"
}

// Snapshot 读取状态并丢掉已经退出的进程。
func Snapshot() Snap {
	mu.Lock()
	defer mu.Unlock()
	st := load()
	live := make([]Proc, 0, len(st.Procs))
	for _, p := range st.Procs {
		if procExists(p.PID) {
			if p.Cmd == "" {
				p.Cmd = readCmd(p.PID)
			}
			live = append(live, p)
		}
	}
	if len(live) != len(st.Procs) {
		st.Procs = live
		_ = save(st)
	}
	return Snap{
		Enabled:    st.Enabled,
		Rules:      RulesPresent(),
		DNSPort:    nonzero(st.DNSPort, DNSPort),
		TProxyPort: TProxyPort,
		RouteMark:  nonzero(st.RouteMark, RouteMark),
		Procs:      live,
		Apps:       st.Apps,
	}
}

// Text 把接管列表格式化成给终端看的中文。
func (s Snap) Text() string {
	var b strings.Builder
	if !s.Enabled {
		b.WriteString("按进程代理: 关\n")
	} else if s.Rules {
		b.WriteString("按进程代理: 开，规则已安装\n")
	} else {
		b.WriteString("按进程代理: 开，规则未安装（相关进程目前直连）\n")
	}
	fmt.Fprintf(&b, "tproxy 端口: %d    DNS 端口: %d    排除标记: 0x%x\n", s.TProxyPort, s.DNSPort, s.RouteMark)
	if len(s.Apps) == 0 {
		b.WriteString("按应用接管: 无\n")
	} else {
		b.WriteString("按应用接管:\n")
		for _, a := range s.Apps {
			if a.Path != "" && a.Path != a.Match {
				fmt.Fprintf(&b, "  %s  (%s)\n", a.Match, a.Path)
			} else {
				fmt.Fprintf(&b, "  %s\n", a.Match)
			}
		}
	}
	if len(s.Procs) == 0 {
		b.WriteString("当前没有接管任何进程。\n")
		return b.String()
	}
	b.WriteString("接管进程:\n")
	for _, p := range s.Procs {
		cmd := p.Cmd
		if cmd == "" {
			cmd = "-"
		}
		fmt.Fprintf(&b, "  %d  %s\n    原 cgroup: %s\n", p.PID, cmd, emptyDash(p.Original))
	}
	return b.String()
}

// TakeOver 把已有进程和它现在的子孙移入接管 cgroup。之后新 fork 的子进程会跟着进入。
func TakeOver(pids []int, protected map[int]bool) (string, error) {
	if err := RequireRoot(); err != nil {
		return "", err
	}
	if len(pids) == 0 {
		return "", fmt.Errorf("请指定至少一个 pid，例如: sudo partyctl proxy pid 1234")
	}
	mu.Lock()
	defer mu.Unlock()
	if err := ensureCgroup(); err != nil {
		return "", err
	}
	st := load()
	st.Enabled = true
	if st.DNSPort == 0 {
		st.DNSPort = DNSPort
	}
	if st.RouteMark == 0 {
		st.RouteMark = RouteMark
	}
	var notes []string
	var planned []Proc
	for _, root := range pids {
		if root <= 1 {
			return "", fmt.Errorf("不能接管 pid %d", root)
		}
		if !procExists(root) {
			return "", fmt.Errorf("进程 %d 不存在", root)
		}
		if protected[root] {
			return "", fmt.Errorf("不能接管 pid %d（mihomo 或 partyctl 自身），否则会形成流量环路", root)
		}
		for _, pid := range liveDescendants(root) {
			if protected[pid] {
				notes = append(notes, fmt.Sprintf("已跳过 mihomo 进程 %d", pid))
				continue
			}
			orig := ""
			if prev, ok := findProc(st, pid); ok && prev.Original != "" && prev.Original != "/"+CgroupName {
				orig = prev.Original
			} else {
				cg, err := ReadCgroup(pid)
				if err != nil {
					return "", err
				}
				if cg == "/"+CgroupName {
					orig = "/"
				} else {
					orig = cg
				}
			}
			planned = append(planned, Proc{PID: pid, RootPID: root, Original: orig, Cmd: readCmd(pid)})
		}
	}
	var moved []int
	for i, p := range planned {
		cg, _ := ReadCgroup(p.PID)
		if cg != "/"+CgroupName {
			if err := movePID(p.PID, CgroupDir); err != nil {
				for _, done := range planned[:i] {
					_ = movePID(done.PID, absCgroup(done.Original))
				}
				return "", err
			}
		}
		st = upsert(st, p)
		moved = append(moved, p.PID)
	}
	if err := save(st); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("已接管 %d 个进程（含已存在的子进程）。之后由它们创建的子进程也会走代理。", len(moved))
	if len(notes) > 0 {
		msg += "\n" + strings.Join(notes, "\n")
	}
	return msg, nil
}

// Release 撤销一个进程及其仍在接管组里的子孙，并把它送回原来的 cgroup。
func Release(pid int) (string, error) {
	if err := RequireRoot(); err != nil {
		return "", err
	}
	mu.Lock()
	defer mu.Unlock()
	st := load()
	orig := originalFor(st, pid)
	targets := map[int]string{}
	for _, p := range st.Procs {
		if p.PID == pid || p.RootPID == pid || descendantOf(p.PID, pid) {
			targets[p.PID] = p.Original
		}
	}
	for _, m := range cgroupMembers() {
		if m == pid || descendantOf(m, pid) {
			if _, ok := targets[m]; !ok {
				targets[m] = orig
			}
		}
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("没有接管 pid %d。可用 partyctl proxy pid list 查看", pid)
	}
	var errs []string
	restored := 0
	for id, original := range targets {
		if !procExists(id) {
			continue
		}
		if err := movePID(id, absCgroup(original)); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		restored++
	}
	kept := make([]Proc, 0, len(st.Procs))
	for _, p := range st.Procs {
		if _, drop := targets[p.PID]; drop {
			continue
		}
		kept = append(kept, p)
	}
	st.Procs = kept
	if err := save(st); err != nil {
		return "", err
	}
	if cgroupEmpty() && len(st.Apps) == 0 {
		if err := disarmLocked(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("已尝试撤销 pid %d，但有失败:\n%s", pid, strings.Join(errs, "\n"))
	}
	extra := ""
	if cgroupEmpty() && len(st.Apps) == 0 {
		extra = "接管组已空，规则已撤掉，这些进程恢复直连。"
	} else if len(st.Apps) > 0 {
		extra = "仍有按应用接管，规则保留。"
	}
	return fmt.Sprintf("已撤销 pid %d，迁回 %d 个进程。%s", pid, restored, extra), nil
}

// Clear 迁回所有进程、关闭功能并删除规则。
func Clear() (string, error) {
	if err := RequireRoot(); err != nil {
		return "", err
	}
	mu.Lock()
	defer mu.Unlock()
	st := load()
	known := map[int]string{}
	for _, p := range st.Procs {
		known[p.PID] = p.Original
	}
	var errs []string
	for _, m := range cgroupMembers() {
		orig := known[m]
		if orig == "" {
			orig = "/"
		}
		if err := movePID(m, absCgroup(orig)); err != nil {
			errs = append(errs, err.Error())
		}
	}
	st.Enabled = false
	st.Procs = nil
	st.Apps = nil
	if err := save(st); err != nil {
		return "", err
	}
	if err := disarmLocked(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("规则已尽量清理，但仍有进程没能迁回:\n%s", strings.Join(errs, "\n"))
	}
	return "已清理全部按进程代理规则，被接管的进程已恢复直连。", nil
}

func findProc(st State, pid int) (Proc, bool) {
	for _, p := range st.Procs {
		if p.PID == pid {
			return p, true
		}
	}
	return Proc{}, false
}

func upsert(st State, p Proc) State {
	for i := range st.Procs {
		if st.Procs[i].PID == p.PID {
			st.Procs[i] = p
			return st
		}
	}
	st.Procs = append(st.Procs, p)
	return st
}

func originalFor(st State, pid int) string {
	for _, p := range st.Procs {
		if p.PID == pid && p.Original != "" {
			return p.Original
		}
	}
	for _, p := range st.Procs {
		if p.RootPID == pid && p.Original != "" {
			return p.Original
		}
	}
	return "/"
}

func nonzero(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "/"
	}
	return s
}
