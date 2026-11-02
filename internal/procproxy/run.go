// partyctl run：在进程执行任何网络操作之前就把它放进接管 cgroup。
package procproxy

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Run 在接管 cgroup 里前台执行命令，并把标准输入输出接回去。
func Run(argv []string) error {
	if err := RequireRoot(); err != nil {
		return err
	}
	if len(argv) == 0 {
		return fmt.Errorf("用法: sudo partyctl run -- <命令> [参数...]")
	}
	mu.Lock()
	if err := ensureCgroup(); err != nil {
		mu.Unlock()
		return err
	}
	parentCG, _ := ReadCgroup(os.Getpid())
	mu.Unlock()

	dir, err := os.Open(CgroupDir)
	if err != nil {
		return fmt.Errorf("打开接管 cgroup 失败: %w", err)
	}
	defer dir.Close()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    int(dir.Fd()),
		Pdeathsig:   syscall.SIGTERM,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动命令失败: %w", err)
	}
	fmt.Fprintf(os.Stderr, "已在按进程代理中启动 pid %d\n", cmd.Process.Pid)
	rememberRun(cmd.Process.Pid, parentCG, argv)
	waitErr := cmd.Wait()
	finishRun(cmd.Process.Pid, parentCG)
	if waitErr != nil {
		if exit, ok := waitErr.(*exec.ExitError); ok {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok {
				code := status.ExitStatus()
				if status.Signaled() {
					code = 128 + int(status.Signal())
				}
				return &ExitError{Code: code}
			}
		}
		return waitErr
	}
	return nil
}

// ExitError 是被接管命令的退出码，调用方应原样退出，不要当成 partyctl 自己的失败。
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("命令退出码 %d", e.Code)
}

func rememberRun(pid int, original string, argv []string) {
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
	st = upsert(st, Proc{PID: pid, RootPID: pid, Original: original, Cmd: stringsJoin(argv)})
	_ = save(st)
}

func finishRun(pid int, original string) {
	mu.Lock()
	defer mu.Unlock()
	st := load()
	old := append([]Proc(nil), st.Procs...)
	kept := make([]Proc, 0, len(old))
	for _, p := range old {
		if p.PID == pid || !procExists(p.PID) {
			continue
		}
		kept = append(kept, p)
	}
	st.Procs = kept
	for _, m := range cgroupMembers() {
		if !procExists(m) {
			continue
		}
		if _, ok := findProc(st, m); ok {
			continue
		}
		st = upsert(st, Proc{PID: m, RootPID: pid, Original: original, Cmd: readCmd(m)})
	}
	_ = save(st)
	if cgroupEmpty() && len(st.Apps) == 0 {
		_ = disarmLocked()
	}
}

func stringsJoin(argv []string) string {
	out := ""
	for i, a := range argv {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
