package cmd

import (
	"fmt"
	"os"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/service"
	"github.com/spf13/cobra"
)

var startForeground bool
var startDetach bool
var startWithWeb bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "启动 mihomo（优先 systemd --user；否则可用 --foreground / --detach）",
	Run: func(cmd *cobra.Command, args []string) {
		st, err := config.LoadSettings()
		fatal(err)
		if startDetach && !startForeground {
			err = service.StartDetached()
		} else {
			err = service.Start(startForeground)
		}
		if service.IsAlreadyRunning(err) {
			fmt.Fprintln(cmd.OutOrStdout(), err.Error())
			err = nil
		}
		fatal(err)
		if startWithWeb || st.Web.Enabled {
			if err := startWebDetached(st); err != nil {
				fmt.Fprintln(os.Stderr, "warning: web auto-start failed:", err)
			}
		}
	},
}

func startWebDetached(st config.Settings) error {
	listen := st.Web.Listen
	if listen == "" {
		listen = "127.0.0.1:9800"
	}
	pid, err := service.StartWebDetached(listen)
	if err != nil {
		return err
	}
	fmt.Printf("Web: %s (pid=%d)\n", config.WebURL(st), pid)
	fmt.Println("登录令牌: partyctl web token")
	return nil
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "停止 mihomo",
	Run: func(cmd *cobra.Command, args []string) {
		fatal(service.Stop())
		fmt.Println("stopped")
	},
}

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "重启 mihomo",
	Run: func(cmd *cobra.Command, args []string) {
		fatal(service.Restart(startForeground))
		fmt.Println("restarted")
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "查看运行状态",
	Run: func(cmd *cobra.Command, args []string) {
		out, err := service.Status()
		if out != "" {
			fmt.Fprint(cmd.OutOrStdout(), out)
		}
		fatal(err)
	},
}

func init() {
	startCmd.Flags().BoolVar(&startForeground, "foreground", false, "前台运行（不走 systemd）")
	startCmd.Flags().BoolVar(&startDetach, "detach", false, "无 systemd 时后台启动")
	startCmd.Flags().BoolVar(&startWithWeb, "with-web", false, "同时后台启动 Web UI")
	restartCmd.Flags().BoolVar(&startForeground, "foreground", false, "前台运行")
}
