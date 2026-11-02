// 监视新进程：优先用 proc connector，内核不投递时改为短间隔扫描。
package procproxy

import (
	"context"
	"fmt"
	"os"
	"time"
)

// ServeWatch 一直运行，直到 ctx 结束。返回实际使用的模式：connector 或 poll。
func ServeWatch(ctx context.Context, skip func(int) bool) (string, error) {
	if err := RequireRoot(); err != nil {
		return "", err
	}
	mode := "poll"
	var src *ProcSource
	if opened, err := ListenProcs(); err == nil {
		if ConnectorWorks(opened) {
			src = opened
			mode = "connector"
		} else {
			opened.Close()
		}
	}
	if src != nil {
		defer src.Close()
	}
	interval := 40 * time.Millisecond
	if mode == "connector" {
		interval = 5 * time.Second
	}
	fmt.Fprintf(os.Stderr, "%s\n", FormatWatch(mode))
	tick := time.NewTicker(interval)
	defer tick.Stop()
	maintain(skip)
	for {
		if src != nil {
			select {
			case <-ctx.Done():
				return mode, nil
			case pid, ok := <-src.Pids():
				if !ok {
					src = nil
					mode = "poll"
					tick.Reset(40 * time.Millisecond)
					continue
				}
				_ = touchPID(pid, skip)
			case <-tick.C:
				maintain(skip)
			}
			continue
		}
		select {
		case <-ctx.Done():
			return mode, nil
		case <-tick.C:
			maintain(skip)
		}
	}
}

func maintain(skip func(int) bool) {
	st := func() State {
		mu.Lock()
		defer mu.Unlock()
		return load()
	}()
	if !st.Enabled || len(st.Apps) == 0 && !hasLive(st) {
		return
	}
	if listenersReady(st.DNSPort) {
		if !RulesPresent() {
			_ = Arm()
		}
	} else if RulesPresent() {
		_ = Disarm()
	}
	_, _ = ScanApps(skip)
}

func touchPID(pid int, skip func(int) bool) error {
	if pid <= 1 || (skip != nil && skip(pid)) {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	st := load()
	app, ok := matchApps(st.Apps, pid)
	if !ok {
		return nil
	}
	if err := ensureCgroup(); err != nil {
		return err
	}
	cg, err := ReadCgroup(pid)
	if err != nil || cg == "/"+CgroupName {
		return err
	}
	if err := movePID(pid, CgroupDir); err != nil {
		return err
	}
	st = upsert(st, Proc{PID: pid, RootPID: pid, Original: cg, Cmd: readCmd(pid), Via: "app:" + app.Match})
	return save(st)
}

// WatchModeHint 用中文说明当前监视方式的局限。
func WatchModeHint(mode string) string {
	switch mode {
	case "connector":
		return "使用内核进程事件，新进程在 exec 后会尽快进入接管组。"
	default:
		return "当前内核不向本 pid 命名空间投递进程事件，已改为每 40ms 扫描。长时间运行的程序可以被接管；刚启动就立刻联网的短进程，第一个连接可能来不及被接管。"
	}
}

// FormatWatch 给命令行的一行说明。
func FormatWatch(mode string) string {
	return fmt.Sprintf("应用监视模式: %s。%s", mode, WatchModeHint(mode))
}
