// partyctl run 在接管 cgroup 里启动一个程序。
package cmd

import (
	"errors"
	"os"

	"github.com/partyctl/partyctl/internal/procproxy"
	"github.com/partyctl/partyctl/internal/service"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run -- <命令> [参数...]",
	Short: "在按进程代理里启动一个程序",
	Long: `在专用 cgroup 中启动命令，TCP/UDP（含 DNS）从一开始就走 mihomo。
其他进程不受影响，也不会打开全局 TUN。需要 root。

示例:
  sudo partyctl run -- curl -I https://example.com
  sudo partyctl run -- python3 app.py`,
	DisableFlagsInUseLine: true,
	Args:                  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		err := service.RunProxied(args)
		var exitErr *procproxy.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fatal(err)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
