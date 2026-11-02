// cgroup v2 的创建、迁入和迁回。子进程会继承父进程的 cgroup，因此之后 fork 的进程也会被接管。
package procproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RequireRoot 在缺少权限时给出可直接执行的中文提示。
func RequireRoot() error {
	if os.Geteuid() == 0 {
		return nil
	}
	return fmt.Errorf("按进程接管代理需要 root。请使用 sudo partyctl proxy pid、sudo partyctl run 或 sudo partyctl proxy clear")
}

// CheckSupport 确认当前系统有 cgroup v2、nft 和 ip。
func CheckSupport() error {
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return fmt.Errorf("按进程接管需要 cgroup v2（未找到 /sys/fs/cgroup/cgroup.controllers）")
	}
	if _, err := execLook("nft"); err != nil {
		return fmt.Errorf("未找到 nft。请先安装 nftables，例如: sudo apt install nftables")
	}
	if _, err := execLook("ip"); err != nil {
		return fmt.Errorf("未找到 ip 命令。请先安装 iproute2")
	}
	return nil
}

func execLook(name string) (string, error) {
	return lookPath(name)
}

// ensureCgroup 创建可放入进程的叶子 cgroup。调用方需已持有 mu。
func ensureCgroup() error {
	if err := os.MkdirAll(CgroupDir, 0o755); err != nil {
		return fmt.Errorf("创建 cgroup %s 失败: %w。请确认以 root 运行，并且系统使用 cgroup v2", CgroupDir, err)
	}
	// 叶子组不能再打开控制器，否则无法写入 cgroup.procs。
	_ = os.WriteFile(filepath.Join(CgroupDir, "cgroup.subtree_control"), nil, 0o644)
	return nil
}

// ReadCgroup 返回 0:: 后面的层级路径，例如 /user.slice/foo。
func ReadCgroup(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", fmt.Errorf("进程 %d 不存在", pid)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "0::") {
			return strings.TrimPrefix(line, "0::"), nil
		}
	}
	return "", fmt.Errorf("进程 %d 不在 cgroup v2 层级里", pid)
}

// absCgroup 把 /proc 里的相对层级还原成 sysfs 路径。
func absCgroup(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "/" || rel == "." {
		return "/sys/fs/cgroup"
	}
	return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(rel, "/"))
}

func movePID(pid int, cgroupDir string) error {
	path := filepath.Join(cgroupDir, "cgroup.procs")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return fmt.Errorf("把 pid %d 移入 %s 失败: %w", pid, cgroupDir, err)
	}
	return nil
}

func cgroupMembers() []int {
	data, err := os.ReadFile(filepath.Join(CgroupDir, "cgroup.procs"))
	if err != nil {
		return nil
	}
	var out []int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err == nil && pid > 0 {
			out = append(out, pid)
		}
	}
	return out
}

func cgroupEmpty() bool {
	return len(cgroupMembers()) == 0
}

func readCmd(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " "))
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

func readPPID(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
			return n
		}
	}
	return 0
}

// descendantOf 判断 pid 是否就是 root，或是它的子孙进程。
func descendantOf(pid, root int) bool {
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] {
		if pid == root {
			return true
		}
		seen[pid] = true
		pid = readPPID(pid)
	}
	return pid == root
}

func liveDescendants(root int) []int {
	var out []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return []int{root}
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if descendantOf(pid, root) {
			out = append(out, pid)
		}
	}
	return out
}

func hasLive(st State) bool {
	for _, p := range st.Procs {
		if procExists(p.PID) {
			return true
		}
	}
	return false
}

func procExists(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}
