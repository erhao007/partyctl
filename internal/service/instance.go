// 保证同一配置目录只有一个 partyctl 内核进程。
// 识别 Web、systemd 和 --detach 拉起的 mihomo，不碰其他目录里的 mihomo。
package service

import (
	"errors"
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

// AlreadyRunningError 表示本配置目录已经有内核在跑。
type AlreadyRunningError struct {
	PIDs []int
}

func (e *AlreadyRunningError) Error() string {
	return fmt.Sprintf("内核已在运行（pid %s）。如需重启请执行 partyctl restart", joinPIDs(e.PIDs))
}

// IsAlreadyRunning 判断错误是不是“已经在运行”。
func IsAlreadyRunning(err error) bool {
	var ar *AlreadyRunningError
	return errors.As(err, &ar)
}

// CoreProc 是一个使用本配置目录或本内核路径的 mihomo。
type CoreProc struct {
	PID int
	Cmd string
}

// ListOurCores 扫描正在运行、属于本 partyctl 配置目录或内核路径的 mihomo。
func ListOurCores() []CoreProc {
	bin := config.CoreBinaryPath()
	work := config.WorkDir()
	var out []CoreProc
	seen := map[int]bool{}
	for _, p := range listProcs() {
		exe, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(p.pid), "exe"))
		cwd, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(p.pid), "cwd"))
		if !isOurCore(exe, cwd, p.args, bin, work) {
			continue
		}
		seen[p.pid] = true
		out = append(out, CoreProc{PID: p.pid, Cmd: strings.Join(p.args, " ")})
	}
	if pid := readPIDFile(); pid > 0 && !seen[pid] && processAlive(pid) {
		exe, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
		cwd, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
		cmd, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
		args := splitCmdline(cmd)
		if isOurCore(exe, cwd, args, bin, work) {
			out = append(out, CoreProc{PID: pid, Cmd: strings.Join(args, " ")})
		}
	}
	return out
}

// ReconcileCore 在升级时把已运行的内核收成一个，并让它重新执行当前二进制。没有在跑则不动。
func ReconcileCore() (string, error) {
	var msg string
	err := withCoreLock(func() error {
		procs := ListOurCores()
		if len(procs) == 0 {
			msg = "内核未在运行，未启动新进程。\n"
			return nil
		}
		pids := make([]int, len(procs))
		for i, p := range procs {
			pids[i] = p.PID
		}
		msg = fmt.Sprintf("发现 %d 个 partyctl 内核进程（pid %s），停止后重新启动一个。\n", len(procs), joinPIDs(pids))
		if err := stopLocked(); err != nil {
			return err
		}
		if err := startLocked(true); err != nil {
			return err
		}
		msg += "已重新启动内核。\n"
		return nil
	})
	return msg, err
}

func rejectIfRunning() error {
	procs := ListOurCores()
	if len(procs) == 0 {
		return nil
	}
	pids := make([]int, len(procs))
	for i, p := range procs {
		pids[i] = p.PID
	}
	return &AlreadyRunningError{PIDs: pids}
}

func startLocked(detach bool) error {
	if err := rejectIfRunning(); err != nil {
		return err
	}
	if _, err := config.GenerateAndValidate(); err != nil {
		return err
	}
	if err := EnsurePortsFree(); err != nil {
		return err
	}
	if detach || !userSystemdEnabled() {
		if err := spawnDetached(); err != nil {
			return err
		}
	} else {
		if err := WriteUnit(); err != nil {
			return err
		}
		_ = systemctl("daemon-reload")
		if err := systemctl("start", unitName); err != nil {
			return err
		}
		if procs := ListOurCores(); len(procs) > 0 {
			_ = writePIDFile(procs[0].PID)
		}
	}
	if procproxy.Enabled() {
		return procproxy.WaitAndArm()
	}
	return nil
}

func spawnDetached() error {
	bin := config.CoreBinaryPath()
	cmd := exec.Command(bin, "-d", config.WorkDir())
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	_ = writePIDFile(pid)
	fmt.Printf("started mihomo pid=%d (detached)\n", pid)
	_ = cmd.Process.Release()
	return nil
}

func stopLocked() error {
	if err := procproxy.Disarm(); err != nil {
		return fmt.Errorf("已取消停止内核：撤掉按进程代理规则失败，否则被接管的进程会断网。%w", err)
	}
	if userSystemdEnabled() {
		_ = systemctl("stop", unitName)
	}
	for _, p := range ListOurCores() {
		_ = syscall.Kill(p.PID, syscall.SIGTERM)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(ListOurCores()) > 0 {
		time.Sleep(100 * time.Millisecond)
	}
	for _, p := range ListOurCores() {
		_ = syscall.Kill(p.PID, syscall.SIGKILL)
	}
	time.Sleep(100 * time.Millisecond)
	_ = os.Remove(corePIDPath())
	return nil
}

func withCoreLock(fn func() error) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	f, err := os.OpenFile(coreLockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func coreLockPath() string { return filepath.Join(config.ConfigDir(), "mihomo.lock") }
func corePIDPath() string  { return filepath.Join(config.ConfigDir(), "mihomo.pid") }

func writePIDFile(pid int) error {
	return os.WriteFile(corePIDPath(), []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

func readPIDFile() int {
	data, err := os.ReadFile(corePIDPath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func joinPIDs(pids []int) string {
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ", ")
}

// isOurCore 判断进程是不是使用本内核文件或本配置目录的 mihomo。
func isOurCore(exe, cwd string, args []string, bin, work string) bool {
	if !looksLikeMihomo(exe, args) {
		return false
	}
	if sameFile(exe, bin) {
		return true
	}
	if sameFile(cwd, work) {
		return true
	}
	return dirInArgs(args, work) || isCoreArgs(args, bin, work)
}

func looksLikeMihomo(exe string, args []string) bool {
	if filepath.Base(stripDeleted(exe)) == "mihomo" {
		return true
	}
	for _, a := range args {
		if filepath.Base(stripDeleted(a)) == "mihomo" {
			return true
		}
	}
	return false
}

func dirInArgs(args []string, work string) bool {
	for _, a := range args {
		if sameFile(a, work) {
			return true
		}
	}
	return false
}

func sameFile(a, b string) bool {
	a, b = stripDeleted(strings.TrimSpace(a)), stripDeleted(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && ra == rb
}

func stripDeleted(s string) string {
	return strings.TrimSuffix(s, " (deleted)")
}

func splitCmdline(data []byte) []string {
	raw := strings.TrimRight(string(data), "\x00")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "\x00")
	var args []string
	for _, p := range parts {
		if p != "" {
			args = append(args, p)
		}
	}
	return args
}
