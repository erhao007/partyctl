// 按应用接管的后台监视：有 systemd 时做成开机服务，否则脱离终端运行。
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/procproxy"
)

const watchUnitName = "partyctl-proxy-watch.service"

// ProxyAppAdd 按程序名或路径持久接管，并确保监视进程在跑。
func ProxyAppAdd(match string) (string, error) {
	if err := procproxy.RequireRoot(); err != nil {
		return "", err
	}
	if err := procproxy.CheckSupport(); err != nil {
		return "", err
	}
	app, err := procproxy.AddApp(match)
	if err != nil {
		return "", err
	}
	var note string
	err = withCoreLock(func() error {
		restarted, freshly, err := ensureProcCoreLocked()
		if err != nil {
			return err
		}
		switch {
		case restarted:
			note = "已重启内核以加载按进程透明代理端口（未修改 TUN 开关和系统代理）。\n"
		case freshly:
			note = "已启动内核以加载按进程透明代理端口（未修改 TUN 开关和系统代理）。\n"
		}
		return procproxy.WaitAndArm()
	})
	if err != nil {
		return "", err
	}
	n, scanErr := procproxy.ScanApps(skipProtected())
	if scanErr != nil {
		return "", scanErr
	}
	watchMsg, werr := EnsureAppWatcher()
	msg := fmt.Sprintf("%s已按应用接管 %s。当前已移入 %d 个正在运行的进程。\n%s\n%s", note, app.Match, n, watchMsg, procproxy.Snapshot().Text())
	if werr != nil {
		return msg, werr
	}
	return msg, nil
}

// ProxyAppRemove 取消一条按应用接管。没有剩余应用时停止监视。
func ProxyAppRemove(match string) (string, error) {
	msg, err := procproxy.RemoveApp(match)
	if err != nil {
		return "", err
	}
	if !procproxy.HasApps() {
		_ = StopAppWatcher()
		msg += "\n已没有按应用接管，监视进程已停止。"
	}
	return msg + "\n" + procproxy.Snapshot().Text(), nil
}

// ProxyAppList 列出按应用接管规则。
func ProxyAppList() (string, error) {
	if os.Geteuid() == 0 {
		_ = procproxy.Heal(IsRunning())
	}
	return procproxy.Snapshot().Text(), nil
}

// WatchApps 前台运行监视循环，直到收到退出信号。
func WatchApps() error {
	if err := procproxy.RequireRoot(); err != nil {
		return err
	}
	if err := writeWatchPID(os.Getpid()); err != nil {
		return err
	}
	defer os.Remove(watchPIDPath())
	ctx, stop := signalContext()
	defer stop()
	_, err := procproxy.ServeWatch(ctx, skipProtected())
	return err
}

// EnsureAppWatcher 保证有一个监视进程。已经在跑则直接返回。
func EnsureAppWatcher() (string, error) {
	if err := procproxy.RequireRoot(); err != nil {
		return "", err
	}
	if WatcherRunning() {
		return "应用监视已在运行。", nil
	}
	if systemdSystemAvailable() {
		if err := installWatchUnit(); err == nil {
			if err := systemctlSystem("enable", "--now", watchUnitName); err == nil {
				return "已安装并启动开机自启服务 " + watchUnitName + "。", nil
			}
		}
	}
	if err := startWatchDetached(); err != nil {
		return "", err
	}
	return "已在后台启动应用监视。当前没有可用的 systemd，开机后请再执行一次 partyctl proxy app add，或 partyctl proxy watch。", nil
}

// StopAppWatcher 停掉监视进程和对应的 systemd 服务。
func StopAppWatcher() error {
	if systemdSystemAvailable() {
		_ = systemctlSystem("disable", "--now", watchUnitName)
	}
	pid := readWatchPID()
	if pid > 0 && pid != os.Getpid() {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	_ = os.Remove(watchPIDPath())
	return nil
}

// WatcherRunning 判断监视进程是否还在。
func WatcherRunning() bool {
	pid := readWatchPID()
	if pid <= 0 || !processAlive(pid) {
		return false
	}
	cmd, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	text := strings.ReplaceAll(string(cmd), "\x00", " ")
	return strings.Contains(text, "proxy") && strings.Contains(text, "watch")
}

func installWatchUnit() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	body := fmt.Sprintf(`[Unit]
Description=partyctl per-app proxy watcher
After=network-online.target

[Service]
Type=simple
Environment=HOME=%s
ExecStart=%s proxy watch
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
`, config.HomeDir(), exe)
	if err := os.MkdirAll("/etc/systemd/system", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/systemd/system/"+watchUnitName, []byte(body), 0o644); err != nil {
		return err
	}
	return systemctlSystem("daemon-reload")
}

func startWatchDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	logPath := filepath.Join(config.ConfigDir(), "proxy-watch.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "proxy", "watch")
	cmd.Env = os.Environ()
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动应用监视失败: %w", err)
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if WatcherRunning() {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("应用监视没有在 3 秒内写好 pid 文件，日志: %s", logPath)
}

func systemdSystemAvailable() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func systemctlSystem(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func watchPIDPath() string {
	return filepath.Join(config.ConfigDir(), "proxy-watch.pid")
}

func writeWatchPID(pid int) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	return os.WriteFile(watchPIDPath(), []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

func readWatchPID() int {
	data, err := os.ReadFile(watchPIDPath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}
