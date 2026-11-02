package service

// 本文件负责把嵌入式 Web 管理界面拉起来。
// root 且 systemd 可用时写成系统服务（开机自启）；普通用户写成用户服务（登录自启）；
// 没有 systemd 或服务安装失败时，退回到脱离终端的后台进程。

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/partyctl/partyctl/internal/config"
)

const webUnitName = "partyctl-web.service"

const (
	webModeSystem = "system"
	webModeUser   = "user"
	webModeDetach = "detach"
)

// WebEnableResult 是 web enable 的结果，供命令行打印。
type WebEnableResult struct {
	Mode    string
	URL     string
	Message string
	Warning string
}

// webUnitSpec 是写进 systemd 单元的运行参数。
type webUnitSpec struct {
	Exe    string
	Listen string
	Home   string
}

// webBackend 把 systemd 与进程操作抽出来，便于测试编排逻辑。
type webBackend interface {
	DetectMode() string
	InstallUnit(mode, content string) error
	Reload(mode string) error
	Enable(mode string) error
	IsActive(mode string) bool
	Restart(mode string) error
	Start(mode string) error
	DisableNow(mode string) error
	WebPID() (int, bool)
	UnitMainPID(mode string) (int, bool)
	StopPID(pid int) error
	StartDetached(listen string) error
	WaitReady(listen string) error
}

// EnableWeb 确保 Web 正在运行。已经在跑时会停掉旧进程，再用当前二进制拉起。
func EnableWeb() (WebEnableResult, error) {
	st, err := config.LoadSettings()
	if err != nil {
		return WebEnableResult{}, err
	}
	exe, err := currentExecutable()
	if err != nil {
		return WebEnableResult{}, err
	}
	listen := strings.TrimSpace(st.Web.Listen)
	if listen == "" {
		listen = "127.0.0.1:9800"
	}
	spec := webUnitSpec{Exe: exe, Listen: listen, Home: config.HomeDir()}
	return enableWebWith(osWebBackend{}, spec, config.WebURL(st))
}

// StartWebDetached 在后台启动 Web，并等到端口开始监听。
func StartWebDetached(listen string) (int, error) {
	return startWebProcess(listen)
}

// enableWebWith 按探测到的方式启动 Web；systemd 失败时退回后台进程。
func enableWebWith(b webBackend, spec webUnitSpec, pageURL string) (WebEnableResult, error) {
	res := WebEnableResult{URL: pageURL}
	mode := b.DetectMode()
	if mode == webModeSystem || mode == webModeUser {
		content := webUnitContent(mode, spec)
		err := startViaSystemd(b, mode, content)
		if err == nil {
			err = b.WaitReady(spec.Listen)
		}
		if err == nil {
			res.Mode = mode
			res.Message = webReadyMessage(mode, pageURL, "")
			return res, nil
		}
		_ = b.DisableNow(mode)
		res.Warning = err.Error()
	}
	if err := startViaDetach(b, spec.Listen); err != nil {
		if res.Warning != "" {
			return res, fmt.Errorf("systemd 未能启动 Web（%s），改为后台启动也失败: %w", res.Warning, err)
		}
		return res, err
	}
	res.Mode = webModeDetach
	res.Message = webReadyMessage(webModeDetach, pageURL, res.Warning)
	return res, nil
}

// startViaSystemd 写入单元、设为自启，并在已运行时重启到当前二进制。
func startViaSystemd(b webBackend, mode, content string) error {
	if err := stopStrayWeb(b, mode); err != nil {
		return err
	}
	if err := b.InstallUnit(mode, content); err != nil {
		return err
	}
	if err := b.Reload(mode); err != nil {
		return err
	}
	if err := b.Enable(mode); err != nil {
		return err
	}
	if b.IsActive(mode) {
		return b.Restart(mode)
	}
	return b.Start(mode)
}

// stopStrayWeb 停掉不受当前 systemd 单元管理的 Web，避免占用端口。
func stopStrayWeb(b webBackend, mode string) error {
	pid, ok := b.WebPID()
	if !ok {
		return nil
	}
	main, active := b.UnitMainPID(mode)
	if active && main == pid {
		return nil
	}
	return b.StopPID(pid)
}

// startViaDetach 先停掉已有 Web，再用当前程序在后台拉起。
func startViaDetach(b webBackend, listen string) error {
	if pid, ok := b.WebPID(); ok {
		if err := b.StopPID(pid); err != nil {
			return fmt.Errorf("停止已在运行的 Web 失败: %w", err)
		}
	}
	return b.StartDetached(listen)
}

// webReadyMessage 生成给用户看的启动说明。
func webReadyMessage(mode, pageURL, warning string) string {
	var b strings.Builder
	switch mode {
	case webModeSystem:
		fmt.Fprintf(&b, "Web 已设置为开机自启，并已启动（systemd 系统服务 %s）\n", webUnitName)
	case webModeUser:
		fmt.Fprintf(&b, "Web 已设置为登录自启，并已启动（systemd 用户服务 %s）\n", webUnitName)
	default:
		if warning != "" {
			fmt.Fprintf(&b, "Web 已在后台启动。systemd 服务没有配置成功（%s），因此不会开机或登录自启。\n", warning)
		} else {
			b.WriteString("Web 已在后台启动。当前环境没有可用的 systemd，因此不会开机或登录自启。\n")
		}
		b.WriteString("下次开机后如需再打开，请执行: partyctl web enable\n")
		fmt.Fprintf(&b, "后台日志: %s\n", webLogPath())
	}
	fmt.Fprintf(&b, "地址: %s\n", pageURL)
	return b.String()
}

// webUnitContent 生成 partyctl-web.service 内容。
func webUnitContent(mode string, spec webUnitSpec) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=partyctl Web management UI\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s web --listen %s\n", quoteSystemd(spec.Exe), quoteSystemd(spec.Listen))
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=2\n")
	if spec.Home != "" {
		fmt.Fprintf(&b, "Environment=HOME=%s\n", quoteSystemd(spec.Home))
	}
	if mode == webModeSystem {
		b.WriteString("User=root\n")
	}
	b.WriteString("\n[Install]\n")
	if mode == webModeSystem {
		b.WriteString("WantedBy=multi-user.target\n")
	} else {
		b.WriteString("WantedBy=default.target\n")
	}
	return b.String()
}

// chooseWebMode 决定 Web 的托管方式。root 优先系统服务，以便开机拉起。
func chooseWebMode(uid int, systemUp, userUp bool) string {
	if uid == 0 && systemUp {
		return webModeSystem
	}
	if userUp {
		return webModeUser
	}
	return webModeDetach
}

// quoteSystemd 在路径含空格或特殊字符时为 systemd 加上引号。
func quoteSystemd(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"\\$`") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// webModeLabel 把内部运行方式转换成 status 里的中文。
func webModeLabel(mode string) string {
	switch mode {
	case webModeSystem:
		return "systemd（开机自启）"
	case webModeUser:
		return "systemd（登录自启）"
	case webModeDetach:
		return "后台"
	default:
		return "-"
	}
}

// webLogPath 是脱离 systemd 时 Web 的日志文件。
func webLogPath() string {
	return filepath.Join(config.ConfigDir(), "web.log")
}

// currentExecutable 返回当前 partyctl 二进制的真实路径。
func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "", fmt.Errorf("无法定位 partyctl 程序路径")
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		return resolved, nil
	}
	return exe, nil
}

// systemdSystemBus 判断机器上有没有正在运行的系统级 systemd。
func systemdSystemBus() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	fi, err := os.Stat("/run/systemd/system")
	return err == nil && fi.IsDir()
}

// dialAddr 把监听地址换成本机可拨号的地址。
func dialAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// waitDial 等到地址可连接，或进程已经退出。
func waitDial(listen string, timeout time.Duration) error {
	return waitDialPID(0, listen, timeout)
}

// waitDialPID 在 pid 非 0 时，进程退出会立刻返回失败。
func waitDialPID(pid int, listen string, timeout time.Duration) error {
	addr := dialAddr(listen)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid > 0 && !processAlive(pid) {
			return fmt.Errorf("Web 进程已退出")
		}
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pid > 0 && !processAlive(pid) {
		return fmt.Errorf("Web 进程已退出")
	}
	return fmt.Errorf("Web 没有开始监听 %s", addr)
}

// processAlive 判断 pid 是否还在。没有权限发信号时也视为存在。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// stopPID 结束一个 Web 进程，先 TERM 再 KILL。
func stopPID(pid int) error {
	if pid <= 1 || pid == os.Getpid() {
		return fmt.Errorf("拒绝停止进程 %d", pid)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	return nil
}

// startWebProcess 用当前二进制在新会话里启动 Web，并把输出写到 web.log。
func startWebProcess(listen string) (int, error) {
	exe, err := currentExecutable()
	if err != nil {
		return 0, err
	}
	if err := config.EnsureDirs(); err != nil {
		return 0, err
	}
	logf, err := os.OpenFile(webLogPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(exe, "web", "--listen", listen)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf.Close()
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	logf.Close()
	if err := waitDialPID(pid, listen, 5*time.Second); err != nil {
		tail := tailFile(webLogPath(), 800)
		if tail != "" {
			return pid, fmt.Errorf("%w\n%s", err, tail)
		}
		return pid, err
	}
	return pid, nil
}

// tailFile 读取日志末尾，便于启动失败时展示原因。
func tailFile(path string, limit int) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(data))
	if limit > 0 && len(s) > limit {
		s = s[len(s)-limit:]
	}
	return s
}

// webModeOf 根据进程是否由 partyctl-web.service 拉起，判断运行方式。
func webModeOf(pid int) string {
	if pid <= 0 {
		return ""
	}
	if os.Geteuid() == 0 && systemdSystemBus() {
		if main, ok := unitMainPID(false); ok && main == pid {
			return webModeSystem
		}
	}
	if hasSystemdUser() {
		if main, ok := unitMainPID(true); ok && main == pid {
			return webModeUser
		}
	}
	return webModeDetach
}

// unitMainPID 读取 partyctl-web.service 的主进程号。未激活时 ok 为 false。
func unitMainPID(user bool) (int, bool) {
	args := []string{"show", webUnitName, "--property=ActiveState,MainPID", "--no-pager"}
	if user {
		args = append([]string{"--user"}, args...)
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		return 0, false
	}
	var state string
	var pid int
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

// systemctlMode 执行系统级或用户级 systemctl。
func systemctlMode(mode string, args ...string) error {
	full := args
	label := "systemctl"
	if mode == webModeUser {
		full = append([]string{"--user"}, args...)
		label = "systemctl --user"
	}
	cmd := exec.Command("systemctl", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s %s: %v", label, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s: %v: %s", label, strings.Join(args, " "), err, msg)
	}
	return nil
}

// osWebBackend 是运行在本机上的 webBackend。
type osWebBackend struct{}

// DetectMode 判断本机该用系统服务、用户服务还是后台进程。
func (osWebBackend) DetectMode() string {
	uid := os.Geteuid()
	systemUp := systemdSystemBus()
	if uid == 0 && systemUp {
		return webModeSystem
	}
	return chooseWebMode(uid, systemUp, hasSystemdUser())
}

// InstallUnit 把单元文件写到系统目录或用户目录。
func (osWebBackend) InstallUnit(mode, content string) error {
	path := webUnitPath(mode)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// Reload 重新加载 systemd 单元。
func (osWebBackend) Reload(mode string) error {
	return systemctlMode(mode, "daemon-reload")
}

// Enable 设置开机或登录自启。
func (osWebBackend) Enable(mode string) error {
	return systemctlMode(mode, "enable", webUnitName)
}

// IsActive 报告 Web 单元是否处于活动状态。
func (osWebBackend) IsActive(mode string) bool {
	_, ok := unitMainPID(mode == webModeUser)
	return ok
}

// Restart 重启已在运行的 Web 单元，使新二进制生效。
func (osWebBackend) Restart(mode string) error {
	return systemctlMode(mode, "restart", webUnitName)
}

// Start 启动尚未运行的 Web 单元。
func (osWebBackend) Start(mode string) error {
	return systemctlMode(mode, "start", webUnitName)
}

// DisableNow 关闭自启并停止单元，便于失败后退回后台方式。
func (osWebBackend) DisableNow(mode string) error {
	return systemctlMode(mode, "disable", "--now", webUnitName)
}

// WebPID 查找正在提供页面的 partyctl web 进程。
func (osWebBackend) WebPID() (int, bool) {
	return findWebPID()
}

// UnitMainPID 返回单元主进程；单元未运行时 ok 为 false。
func (osWebBackend) UnitMainPID(mode string) (int, bool) {
	return unitMainPID(mode == webModeUser)
}

// StopPID 停止指定进程。
func (osWebBackend) StopPID(pid int) error {
	return stopPID(pid)
}

// StartDetached 在后台启动 Web。
func (osWebBackend) StartDetached(listen string) error {
	_, err := startWebProcess(listen)
	return err
}

// WaitReady 等待 Web 端口开始接受连接。
func (osWebBackend) WaitReady(listen string) error {
	return waitDial(listen, 8*time.Second)
}

// webUnitPath 返回对应模式下的单元文件路径。
func webUnitPath(mode string) string {
	if mode == webModeSystem {
		return filepath.Join("/etc/systemd/system", webUnitName)
	}
	return filepath.Join(config.SystemdUserDir(), webUnitName)
}
