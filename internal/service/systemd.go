package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/partyctl/partyctl/internal/config"
)

const unitName = "partyctl.service"

func unitContent() string {
	bin := config.CoreBinaryPath()
	work := config.WorkDir()
	return fmt.Sprintf(`[Unit]
Description=partyctl mihomo (Clash Meta)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s -d %s
Restart=on-failure
RestartSec=2
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE

[Install]
WantedBy=default.target
`, bin, work)
}

func WriteUnit() error {
	if err := os.MkdirAll(config.SystemdUserDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(config.SystemdUnitPath(), []byte(unitContent()), 0o644)
}

func hasSystemdUser() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	// Check if user systemd is available
	out, err := exec.Command("systemctl", "--user", "is-system-running").CombinedOutput()
	_ = out
	if err != nil {
		// still might work for start/stop even if "offline"
		if _, err2 := os.Stat("/run/user/" + fmt.Sprint(os.Getuid()) + "/systemd/private"); err2 != nil {
			return false
		}
	}
	return true
}

func systemctl(args ...string) error {
	full := append([]string{"--user"}, args...)
	cmd := exec.Command("systemctl", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return nil
}

func Start(foreground bool) error {
	if foreground {
		if err := withCoreLock(func() error {
			if err := rejectIfRunning(); err != nil {
				return err
			}
			if _, err := config.GenerateAndValidate(); err != nil {
				return err
			}
			return EnsurePortsFree()
		}); err != nil {
			return err
		}
		return runForeground()
	}
	return withCoreLock(func() error { return startLocked(false) })
}

func Stop() error {
	return withCoreLock(stopLocked)
}

// StartBackground 启动内核并立即返回，不会占住当前终端。
// 有用户级 systemd 时交给它管理，否则脱离终端在后台运行。
func StartBackground() error {
	err := Start(false)
	if IsAlreadyRunning(err) {
		return nil
	}
	return err
}

// RestartBackground 停止内核后再用 StartBackground 拉起。
func RestartBackground() error {
	_ = Stop()
	time.Sleep(300 * time.Millisecond)
	return StartBackground()
}

// userSystemdEnabled 判断是否该用用户级 systemd 管理内核。
// PARTYCTL_FORCE_DETACH=1 时始终走后台（测试隔离，避免误操作系统服务）。
func userSystemdEnabled() bool {
	if os.Getenv("PARTYCTL_FORCE_DETACH") == "1" {
		return false
	}
	return hasSystemdUser()
}

func Restart(foreground bool) error {
	_ = Stop()
	time.Sleep(300 * time.Millisecond)
	return Start(foreground)
}

func runForeground() error {
	bin := config.CoreBinaryPath()
	cmd := exec.Command(bin, "-d", config.WorkDir())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	fmt.Fprintf(os.Stderr, "starting mihomo in foreground: %s -d %s\n", bin, config.WorkDir())
	return cmd.Run()
}

// StartDetached starts mihomo in background without systemd (fallback).
// 若本配置目录已有内核在跑，则不再启动第二个。
func StartDetached() error {
	return withCoreLock(func() error { return startLocked(true) })
}

func HasSystemdUser() bool { return hasSystemdUser() }

// IsRunning reports whether mihomo for the partyctl workdir appears running.
func IsRunning() bool {
	return len(ListOurCores()) > 0
}
