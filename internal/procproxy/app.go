// 按程序名或可执行路径匹配进程，并在它们出现时移入接管 cgroup。
package procproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AddApp 记下一条持久规则。名称匹配进程名，带斜杠的字符串匹配可执行文件路径。
func AddApp(match string) (App, error) {
	match = strings.TrimSpace(match)
	if match == "" {
		return App{}, fmt.Errorf("请指定程序名或可执行路径，例如: sudo partyctl proxy app add firefox")
	}
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
	app := normalizeApp(match)
	for _, existing := range st.Apps {
		if existing.Match == app.Match {
			return existing, nil
		}
	}
	st.Apps = append(st.Apps, app)
	if err := save(st); err != nil {
		return App{}, err
	}
	return app, nil
}

// RemoveApp 删掉一条规则，并把因此进入接管组的进程迁回。
func RemoveApp(match string) (string, error) {
	if err := RequireRoot(); err != nil {
		return "", err
	}
	match = strings.TrimSpace(match)
	mu.Lock()
	defer mu.Unlock()
	st := load()
	var kept []App
	var removed App
	found := false
	for _, a := range st.Apps {
		if a.Match == match || a.Path == match || filepath.Base(a.Match) == match {
			removed = a
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return "", fmt.Errorf("没有按应用接管 %q。可用 partyctl proxy app list 查看", match)
	}
	st.Apps = kept
	via := "app:" + removed.Match
	var errs []string
	restored := 0
	targets := map[int]string{}
	for _, p := range st.Procs {
		if p.Via == via {
			targets[p.PID] = p.Original
		}
	}
	for _, pid := range cgroupMembers() {
		if _, ok := targets[pid]; ok {
			continue
		}
		if app, ok := matchOne(removed, pid); ok && app.Match == removed.Match {
			orig := "/"
			if prev, ok := findProc(st, pid); ok && prev.Original != "" {
				orig = prev.Original
			}
			targets[pid] = orig
		}
	}
	for pid, orig := range targets {
		if !procExists(pid) {
			continue
		}
		if err := movePID(pid, absCgroup(orig)); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		restored++
	}
	old := append([]Proc(nil), st.Procs...)
	next := make([]Proc, 0, len(old))
	for _, p := range old {
		if _, drop := targets[p.PID]; drop {
			continue
		}
		next = append(next, p)
	}
	st.Procs = next
	if err := save(st); err != nil {
		return "", err
	}
	if cgroupEmpty() && len(st.Apps) == 0 {
		if err := disarmLocked(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("已取消 %s，但有进程没能迁回:\n%s", removed.Match, strings.Join(errs, "\n"))
	}
	return fmt.Sprintf("已取消按应用接管 %s，迁回 %d 个进程。", removed.Match, restored), nil
}

// ScanApps 把当前已经在跑、且匹配规则的进程移入接管组。
func ScanApps(skip func(int) bool) (int, error) {
	if err := RequireRoot(); err != nil {
		return 0, err
	}
	mu.Lock()
	defer mu.Unlock()
	if err := ensureCgroup(); err != nil {
		return 0, err
	}
	return scanLocked(skip), nil
}

func scanLocked(skip func(int) bool) int {
	st := load()
	if len(st.Apps) == 0 {
		return 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	moved := 0
	for _, e := range entries {
		pid := atoiPID(e.Name())
		if pid <= 1 {
			continue
		}
		if skip != nil && skip(pid) {
			continue
		}
		app, ok := matchApps(st.Apps, pid)
		if !ok {
			continue
		}
		cg, err := ReadCgroup(pid)
		if err != nil || cg == "/"+CgroupName {
			continue
		}
		if err := movePID(pid, CgroupDir); err != nil {
			continue
		}
		st = upsert(st, Proc{
			PID: pid, RootPID: pid, Original: cg, Cmd: readCmd(pid), Via: "app:" + app.Match,
		})
		moved++
	}
	if moved > 0 {
		_ = save(st)
	}
	return moved
}

func normalizeApp(match string) App {
	app := App{Match: match}
	if !strings.Contains(match, "/") {
		return app
	}
	abs, err := filepath.Abs(match)
	if err != nil {
		return app
	}
	app.Match = abs
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		app.Path = resolved
	} else if _, err := os.Stat(abs); err == nil {
		app.Path = abs
	}
	return app
}

func matchApps(apps []App, pid int) (App, bool) {
	for _, app := range apps {
		if _, ok := matchOne(app, pid); ok {
			return app, true
		}
	}
	return App{}, false
}

func matchOne(app App, pid int) (App, bool) {
	exe := procExe(pid)
	base := filepath.Base(exe)
	comm := readComm(pid)
	if strings.Contains(app.Match, "/") || app.Path != "" {
		want := app.Path
		if want == "" {
			want = app.Match
		}
		if exe != "" && (exe == want || exe == app.Match || samePath(exe, want)) {
			return app, true
		}
		return App{}, false
	}
	if base == app.Match || comm == app.Match {
		return app, true
	}
	return App{}, false
}

func procExe(pid int) string {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(exe, " (deleted)")
}

func readComm(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func samePath(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

func atoiPID(name string) int {
	n := 0
	if name == "" {
		return 0
	}
	for _, c := range name {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
