// 把按进程代理和内核启停串起来：先保证透明端口在听，再迁进程，停止内核前先撤规则。
package service

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/partyctl/partyctl/internal/procproxy"
)

// ProxyAdd 接管已有进程及其之后创建的子进程。必要时会重启内核以加载透明端口。
func ProxyAdd(pids []int) (string, error) {
	if err := procproxy.RequireRoot(); err != nil {
		return "", err
	}
	if err := procproxy.CheckSupport(); err != nil {
		return "", err
	}
	var note string
	err := withCoreLock(func() error {
		if err := procproxy.Enable(); err != nil {
			return err
		}
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
	msg, err := procproxy.TakeOver(pids, protectedPIDs())
	if err != nil {
		return "", err
	}
	return note + msg + "\n" + procproxy.Snapshot().Text(), nil
}

// ProxyRemove 撤销对某个进程的接管。接管组空了就拆掉规则，进程恢复直连。
func ProxyRemove(pid int) (string, error) {
	msg, err := procproxy.Release(pid)
	if err != nil {
		return "", err
	}
	return msg + "\n" + procproxy.Snapshot().Text(), nil
}

// ProxyList 查看当前接管了哪些进程。root 下会顺手把失效规则清掉或装回。
func ProxyList() (string, error) {
	if os.Geteuid() == 0 {
		_ = procproxy.Heal(IsRunning())
	}
	return procproxy.Snapshot().Text(), nil
}

// ProxyClear 撤掉全部规则并把进程迁回原 cgroup。内核若在跑，会重启以去掉临时透明配置。
func ProxyClear() (string, error) {
	if err := procproxy.RequireRoot(); err != nil {
		return "", err
	}
	_ = StopAppWatcher()
	msg, err := procproxy.Clear()
	if err != nil {
		return "", err
	}
	if IsRunning() {
		if rErr := RestartBackground(); rErr != nil {
			return msg, fmt.Errorf("%s\n规则已清理，但重启内核失败: %w", msg, rErr)
		}
		msg += "\n已重启内核，运行配置里的临时透明代理字段已去掉（TUN 与系统代理未改）。"
	}
	return msg, nil
}

// RunProxied 带代理启动一个程序，并等到它退出。
func RunProxied(argv []string) error {
	if err := procproxy.RequireRoot(); err != nil {
		return err
	}
	if err := procproxy.CheckSupport(); err != nil {
		return err
	}
	if len(argv) == 0 {
		return fmt.Errorf("用法: sudo partyctl run -- <命令> [参数...]")
	}
	err := withCoreLock(func() error {
		if err := procproxy.Enable(); err != nil {
			return err
		}
		if _, _, err := ensureProcCoreLocked(); err != nil {
			return err
		}
		return procproxy.WaitAndArm()
	})
	if err != nil {
		return err
	}
	return procproxy.Run(argv)
}

// ensureProcCoreLocked 让当前内核带上透明代理端口。调用方需已持有内核锁。
func ensureProcCoreLocked() (restarted, freshly bool, err error) {
	if IsRunning() && procproxy.PortsReady() {
		return false, false, nil
	}
	if IsRunning() {
		if err := stopLocked(); err != nil {
			return false, false, err
		}
		if err := startLocked(true); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	if err := startLocked(true); err != nil {
		return false, false, err
	}
	return false, true, nil
}

func skipProtected() func(int) bool {
	var mu sync.Mutex
	var set map[int]bool
	var at time.Time
	return func(pid int) bool {
		mu.Lock()
		defer mu.Unlock()
		if set == nil || time.Since(at) > time.Second {
			set = protectedPIDs()
			at = time.Now()
		}
		return set[pid]
	}
}

func protectedPIDs() map[int]bool {
	m := map[int]bool{os.Getpid(): true}
	for _, c := range ListOurCores() {
		m[c.PID] = true
	}
	return m
}
