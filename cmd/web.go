package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/partyctl/partyctl/internal/service"
	"github.com/partyctl/partyctl/internal/web"
	"github.com/spf13/cobra"
)

var webListen string

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "启动嵌入式 Web 管理界面",
	Run: func(cmd *cobra.Command, args []string) {
		st, err := config.LoadSettings()
		fatal(err)
		listen := webListen
		if listen == "" {
			listen = st.Web.Listen
		}
		srv, err := web.New(listen, config.WebAuthToken(st))
		fatal(err)
		go func() {
			if err := srv.ListenAndServe(); err != nil {
				fmt.Fprintln(os.Stderr, "web server:", err)
				os.Exit(1)
			}
		}()
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		_ = srv.Shutdown(cmd.Context())
	},
}

var webTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "打印 Web 登录令牌",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		st, err := config.LoadSettings()
		fatal(err)
		fmt.Fprintln(cmd.OutOrStdout(), config.WebAuthToken(st))
	},
}

var webURLCmd = &cobra.Command{
	Use:   "url",
	Short: "打印 Web 界面地址",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		st, err := config.LoadSettings()
		fatal(err)
		fmt.Fprintln(cmd.OutOrStdout(), config.WebURL(st))
	},
}

var webLANCmd = &cobra.Command{
	Use:   "lan",
	Short: "开启或关闭 Web 的局域网访问",
}

var webLANOnCmd = &cobra.Command{
	Use:   "on",
	Short: "监听 0.0.0.0 并重启 Web（只在可信网络使用）",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.WebLAN(true)
		if msg != "" {
			fmt.Fprintln(cmd.OutOrStdout(), msg)
		}
		fatal(err)
	},
}

var webLANOffCmd = &cobra.Command{
	Use:   "off",
	Short: "恢复只监听 127.0.0.1 并重启 Web",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.WebLAN(false)
		if msg != "" {
			fmt.Fprintln(cmd.OutOrStdout(), msg)
		}
		fatal(err)
	},
}

var webEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "安装并启动 Web（systemd 开机/登录自启，否则后台运行）",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		res, err := service.EnableWeb()
		if res.Message != "" {
			fmt.Fprint(cmd.OutOrStdout(), res.Message)
		}
		fatal(err)
	},
}

func init() {
	webCmd.Flags().StringVar(&webListen, "listen", "", "监听地址（默认 127.0.0.1:9800 或配置 web.listen）")
	webLANCmd.AddCommand(webLANOnCmd, webLANOffCmd)
	webCmd.AddCommand(webTokenCmd, webURLCmd, webEnableCmd, webLANCmd)
	rootCmd.AddCommand(webCmd)
}
