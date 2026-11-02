package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
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
	if _, err := config.GenerateAndValidate(); err != nil {
		return err
	}
	if foreground || !hasSystemdUser() {
		return runForeground()
	}
	if err := WriteUnit(); err != nil {
		return err
	}
	_ = systemctl("daemon-reload")
	return systemctl("start", unitName)
}

func Stop() error {
	if hasSystemdUser() {
		if err := systemctl("stop", unitName); err == nil {
			return nil
		}
	}
	return stopByPIDFileOrPkill()
}

func Restart(foreground bool) error {
	_ = Stop()
	time.Sleep(300 * time.Millisecond)
	return Start(foreground)
}

func Status() (string, error) {
	if hasSystemdUser() {
		out, err := exec.Command("systemctl", "--user", "status", unitName, "--no-pager").CombinedOutput()
		return string(out), err
	}
	bin := config.CoreBinaryPath()
	out, err := exec.Command("pgrep", "-a", "-f", bin+".*-d "+config.WorkDir()).CombinedOutput()
	if err != nil {
		return "partyctl mihomo: not running (no systemd user session; pgrep found nothing)\n", nil
	}
	return "running (no systemd):\n" + string(out), nil
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

func stopByPIDFileOrPkill() error {
	bin := config.CoreBinaryPath()
	cmd := exec.Command("pkill", "-f", bin+".*-d "+config.WorkDir())
	_ = cmd.Run()
	return nil
}

// StartDetached starts mihomo in background without systemd (fallback).
func StartDetached() error {
	if _, err := config.GenerateAndValidate(); err != nil {
		return err
	}
	bin := config.CoreBinaryPath()
	cmd := exec.Command(bin, "-d", config.WorkDir())
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("started mihomo pid=%d (detached)\n", cmd.Process.Pid)
	_ = cmd.Process.Release()
	return nil
}

func HasSystemdUser() bool { return hasSystemdUser() }


// IsRunning reports whether mihomo for the partyctl workdir appears running.
func IsRunning() bool {
	bin := config.CoreBinaryPath()
	out, err := exec.Command("pgrep", "-f", bin+".*-d "+config.WorkDir()).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}
