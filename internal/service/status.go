package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/core"
)

// Report is a point-in-time view of partyctl for `partyctl status`.
type Report struct {
	CoreRunning  bool
	CorePID      int
	CorePIDs     []int
	RunMode      string // systemd, detached, or empty when stopped
	CoreVersion  string
	Profile      string
	Mode         string
	MixedPort    int
	Controller   string
	TUN          bool
	WebRunning   bool
	WebMode      string // system, user, detach; empty when stopped
	WebURL       string
	SettingsPath string
	ProfilePath  string
	WorkConfig   string
	CoreBinary   string
}

// Status collects runtime facts and returns a multi-line summary.
func Status() (string, error) {
	rep, err := Collect()
	if err != nil {
		return "", err
	}
	return FormatStatus(rep), nil
}

// Collect reads settings and local process state.
func Collect() (Report, error) {
	st, err := config.LoadSettings()
	if err != nil {
		return Report{}, err
	}
	rep := Report{
		Mode:         st.Mode,
		MixedPort:    st.MixedPort,
		Controller:   st.ExternalController,
		TUN:          st.TUNEnabled,
		WebURL:       config.WebURL(st),
		SettingsPath: config.SettingsPath(),
		WorkConfig:   config.WorkConfigPath(),
		CoreBinary:   config.CoreBinaryPath(),
	}
	if st.CurrentProfile != "" {
		rep.Profile = st.CurrentProfile
		rep.ProfilePath = config.ProfilePath(st.CurrentProfile)
	}
	if ver, vErr := core.Version(); ver != "" {
		if i := strings.IndexByte(ver, '\n'); i >= 0 {
			ver = ver[:i]
		}
		rep.CoreVersion = strings.TrimSpace(ver)
	} else if vErr != nil {
		rep.CoreVersion = "未安装"
	}
	if rep.CoreVersion == "" {
		rep.CoreVersion = "未安装"
	}

	cores := ListOurCores()
	if pid, active := systemdCore(); active {
		rep.CoreRunning = true
		rep.RunMode = "systemd"
		rep.CorePID = pid
	} else if len(cores) > 0 {
		rep.CoreRunning = true
		rep.RunMode = "detached"
		rep.CorePID = cores[0].PID
	}
	if len(cores) > 0 {
		rep.CoreRunning = true
		rep.CorePIDs = make([]int, len(cores))
		for i, c := range cores {
			rep.CorePIDs[i] = c.PID
		}
		if rep.CorePID <= 0 {
			rep.CorePID = cores[0].PID
		}
		if rep.RunMode == "" {
			rep.RunMode = "detached"
		}
	}
	if pid, ok := findWebPID(); ok {
		rep.WebRunning = true
		rep.WebMode = webModeOf(pid)
	}
	return rep, nil
}

// FormatStatus renders a stable, line-oriented status block.
func FormatStatus(r Report) string {
	var b strings.Builder
	if r.CoreRunning {
		fmt.Fprintf(&b, "核心: 运行中\n")
	} else {
		fmt.Fprintf(&b, "核心: 未运行\n")
	}
	pids := r.CorePIDs
	if len(pids) == 0 && r.CorePID > 0 {
		pids = []int{r.CorePID}
	}
	if r.CoreRunning && len(pids) > 0 {
		fmt.Fprintf(&b, "pid: %s\n", joinPIDs(pids))
	} else {
		fmt.Fprintf(&b, "pid: -\n")
	}
	mode := r.RunMode
	if mode == "" {
		mode = "-"
	}
	fmt.Fprintf(&b, "运行方式: %s\n", mode)
	fmt.Fprintf(&b, "内核版本: %s\n", dash(r.CoreVersion))
	fmt.Fprintf(&b, "当前配置: %s\n", dash(r.Profile))
	fmt.Fprintf(&b, "模式: %s\n", dash(r.Mode))
	fmt.Fprintf(&b, "mixed-port: %d\n", r.MixedPort)
	fmt.Fprintf(&b, "控制器: %s\n", dash(r.Controller))
	if r.TUN {
		fmt.Fprintf(&b, "TUN: 开\n")
	} else {
		fmt.Fprintf(&b, "TUN: 关\n")
	}
	if r.WebRunning {
		fmt.Fprintf(&b, "Web: 运行中\n")
	} else {
		fmt.Fprintf(&b, "Web: 未运行\n")
	}
	webMode := r.WebMode
	if !r.WebRunning {
		webMode = ""
	}
	fmt.Fprintf(&b, "Web 运行方式: %s\n", webModeLabel(webMode))
	fmt.Fprintf(&b, "Web 地址: %s\n", dash(r.WebURL))
	if WebLANOpen(listenFromWebURL(r.WebURL)) {
		fmt.Fprintf(&b, "Web 局域网: 开\n")
	} else {
		fmt.Fprintf(&b, "Web 局域网: 关\n")
	}
	fmt.Fprintf(&b, "设置: %s\n", dash(r.SettingsPath))
	fmt.Fprintf(&b, "当前配置文件: %s\n", dash(r.ProfilePath))
	fmt.Fprintf(&b, "运行配置: %s\n", dash(r.WorkConfig))
	fmt.Fprintf(&b, "内核: %s\n", dash(r.CoreBinary))
	return b.String()
}

func listenFromWebURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return s
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func systemdCore() (pid int, active bool) {
	if !hasSystemdUser() {
		return 0, false
	}
	out, err := exec.Command("systemctl", "--user", "show", unitName, "--property=ActiveState,MainPID", "--no-pager").CombinedOutput()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		return 0, false
	}
	var state string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ActiveState="):
			state = strings.TrimPrefix(line, "ActiveState=")
		case strings.HasPrefix(line, "MainPID="):
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, "MainPID="))
		}
	}
	switch state {
	case "active", "activating", "reloading":
		return pid, true
	default:
		return 0, false
	}
}

type proc struct {
	pid  int
	args []string
}

func listProcs() []proc {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var out []proc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		raw := strings.TrimRight(string(data), "\x00")
		parts := strings.Split(raw, "\x00")
		args := make([]string, 0, len(parts))
		for _, p := range parts {
			if p != "" {
				args = append(args, p)
			}
		}
		if len(args) == 0 {
			continue
		}
		out = append(out, proc{pid: pid, args: args})
	}
	return out
}

func findCorePID(bin, work string) (int, bool) {
	for _, p := range listProcs() {
		if isCoreArgs(p.args, bin, work) {
			return p.pid, true
		}
	}
	return 0, false
}

func findWebPID() (int, bool) {
	for _, p := range listProcs() {
		if isWebServerArgs(p.args) {
			return p.pid, true
		}
	}
	return 0, false
}

func isCoreArgs(args []string, bin, work string) bool {
	if len(args) == 0 || work == "" {
		return false
	}
	base := filepath.Base(args[0])
	if args[0] != bin && base != filepath.Base(bin) && base != "mihomo" {
		return false
	}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-d" && args[i+1] == work {
			return true
		}
	}
	return false
}

// isWebServerArgs 判断命令行是不是正在提供页面的 `partyctl web`。
// `web token`、`web url`、`web enable` 这类子命令不算。
func isWebServerArgs(args []string) bool {
	idx := -1
	for i, a := range args {
		if a == "web" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	rest := args[idx+1:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch a {
		case "--listen":
			i++
			continue
		case "--":
			return false
		}
		if strings.HasPrefix(a, "--listen=") {
			continue
		}
		if a == "-h" || a == "--help" || a == "help" {
			return false
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return false
	}
	return true
}
