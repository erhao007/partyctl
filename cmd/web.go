package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/partyctl/partyctl/internal/config"
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

func init() {
	webCmd.Flags().StringVar(&webListen, "listen", "", "监听地址（默认 127.0.0.1:9800 或配置 web.listen）")
	rootCmd.AddCommand(webCmd)
}
