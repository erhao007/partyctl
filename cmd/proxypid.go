// 按进程接管：只把指定 pid 及其之后的子进程送进 mihomo。
package cmd

import (
	"fmt"
	"strconv"

	"github.com/partyctl/partyctl/internal/service"
	"github.com/spf13/cobra"
)

var proxyPidCmd = &cobra.Command{
	Use:   "pid [pid...]",
	Short: "接管已有进程及其之后创建的子进程",
	Long: `把已有进程移入专用 cgroup。之后它 fork 出来的子进程也会走 mihomo。
其他进程保持直连。需要 root。

示例:
  sudo partyctl proxy pid 1234 1235
  sudo partyctl proxy pid list
  sudo partyctl proxy pid rm 1234`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			_ = cmd.Help()
			fatal(fmt.Errorf("请指定至少一个 pid"))
		}
		pids := make([]int, 0, len(args))
		for _, arg := range args {
			pid, err := strconv.Atoi(arg)
			if err != nil || pid <= 0 {
				fatal(fmt.Errorf("无效的 pid: %s", arg))
			}
			pids = append(pids, pid)
		}
		msg, err := service.ProxyAdd(pids)
		if msg != "" {
			fmt.Fprint(cmd.OutOrStdout(), msg)
			if msg[len(msg)-1] != '\n' {
				fmt.Fprintln(cmd.OutOrStdout())
			}
		}
		fatal(err)
	},
}

var proxyPidListCmd = &cobra.Command{
	Use:   "list",
	Short: "查看当前接管了哪些进程",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.ProxyList()
		if msg != "" {
			fmt.Fprint(cmd.OutOrStdout(), msg)
		}
		fatal(err)
	},
}

var proxyPidRmCmd = &cobra.Command{
	Use:   "rm <pid>",
	Short: "撤销对某个进程的接管",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pid, err := strconv.Atoi(args[0])
		if err != nil || pid <= 0 {
			fatal(fmt.Errorf("无效的 pid: %s", args[0]))
		}
		msg, err := service.ProxyRemove(pid)
		if msg != "" {
			fmt.Fprint(cmd.OutOrStdout(), msg)
			if msg[len(msg)-1] != '\n' {
				fmt.Fprintln(cmd.OutOrStdout())
			}
		}
		fatal(err)
	},
}

var proxyClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "一键清理全部按进程代理规则",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.ProxyClear()
		if msg != "" {
			fmt.Fprintln(cmd.OutOrStdout(), msg)
		}
		fatal(err)
	},
}

var proxyAppCmd = &cobra.Command{
	Use:   "app",
	Short: "按程序名或可执行路径持久接管",
	Long: `应用重启后 pid 会变。按名称或路径记下规则后，新启动的匹配进程会自动进入接管组。
需要 root。有 systemd 时会安装开机自启服务。

示例:
  sudo partyctl proxy app add firefox
  sudo partyctl proxy app add /usr/bin/curl
  sudo partyctl proxy app list
  sudo partyctl proxy app rm firefox`,
}

var proxyAppAddCmd = &cobra.Command{
	Use:   "add <名称|路径>",
	Short: "增加一条按应用接管",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.ProxyAppAdd(args[0])
		if msg != "" {
			fmt.Fprint(cmd.OutOrStdout(), msg)
		}
		fatal(err)
	},
}

var proxyAppRmCmd = &cobra.Command{
	Use:   "rm <名称|路径>",
	Short: "取消一条按应用接管",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.ProxyAppRemove(args[0])
		if msg != "" {
			fmt.Fprint(cmd.OutOrStdout(), msg)
		}
		fatal(err)
	},
}

var proxyAppListCmd = &cobra.Command{
	Use:   "list",
	Short: "查看按应用接管",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.ProxyAppList()
		if msg != "" {
			fmt.Fprint(cmd.OutOrStdout(), msg)
		}
		fatal(err)
	},
}

var proxyWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "前台运行按应用接管监视",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fatal(service.WatchApps())
	},
}

func init() {
	proxyPidCmd.AddCommand(proxyPidListCmd, proxyPidRmCmd)
	proxyAppCmd.AddCommand(proxyAppAddCmd, proxyAppRmCmd, proxyAppListCmd)
	proxyCmd.AddCommand(proxyPidCmd, proxyAppCmd, proxyClearCmd, proxyWatchCmd)
}
