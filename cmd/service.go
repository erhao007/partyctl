package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

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
			fatal(service.StartDetached())
		} else {
			fatal(service.Start(startForeground))
		}
		if startWithWeb || st.Web.Enabled {
			if err := startWebDetached(st); err != nil {
				fmt.Fprintln(os.Stderr, "warning: web auto-start failed:", err)
			}
		}
	},
}

func startWebDetached(st config.Settings) error {
	exe, err := os.Executable()
	if err != nil {
		exe = "partyctl"
	}
	listen := st.Web.Listen
	if listen == "" {
		listen = "127.0.0.1:9800"
	}
	cmd := exec.Command(exe, "web", "--listen", listen)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Printf("web UI starting on http://%s (pid=%d)\n", listen, cmd.Process.Pid)
	_ = cmd.Process.Release()
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
		fmt.Print(out)
		if err != nil {
			return
		}
	},
}

func init() {
	startCmd.Flags().BoolVar(&startForeground, "foreground", false, "前台运行（不走 systemd）")
	startCmd.Flags().BoolVar(&startDetach, "detach", false, "无 systemd 时后台启动")
	startCmd.Flags().BoolVar(&startWithWeb, "with-web", false, "同时后台启动 Web UI")
	restartCmd.Flags().BoolVar(&startForeground, "foreground", false, "前台运行")
}
