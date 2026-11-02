// port 查看并修改 mihomo 的 mixed-port 与外部控制器地址。
package cmd

import (
	"fmt"
	"net"
	"strconv"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/spf13/cobra"
)

var portMixed int
var portController string

var portCmd = &cobra.Command{
	Use:   "port",
	Short: "查看 mixed-port 与外部控制器端口",
	Run: func(cmd *cobra.Command, args []string) {
		st, err := config.LoadSettings()
		fatal(err)
		fmt.Fprintf(cmd.OutOrStdout(), "mixed-port: %d\n", st.MixedPort)
		fmt.Fprintf(cmd.OutOrStdout(), "external-controller: %s\n", st.ExternalController)
		fmt.Fprintln(cmd.OutOrStdout(), "修改: partyctl port set --mixed <端口> --controller <地址:端口>")
	},
}

var portSetCmd = &cobra.Command{
	Use:   "set",
	Short: "修改 mixed-port 和/或外部控制器地址",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if !cmd.Flags().Changed("mixed") && !cmd.Flags().Changed("controller") {
			fatal(fmt.Errorf("请指定 --mixed 或 --controller"))
		}
		st, err := config.LoadSettings()
		fatal(err)
		if cmd.Flags().Changed("mixed") {
			if portMixed < 1 || portMixed > 65535 {
				fatal(fmt.Errorf("mixed-port 必须在 1 到 65535 之间"))
			}
			st.MixedPort = portMixed
		}
		if cmd.Flags().Changed("controller") {
			host, port, err := net.SplitHostPort(portController)
			if err != nil || host == "" || port == "" {
				fatal(fmt.Errorf("controller 需要 地址:端口，例如 127.0.0.1:9091"))
			}
			if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
				fatal(fmt.Errorf("控制器端口无效: %s", port))
			}
			st.ExternalController = net.JoinHostPort(host, port)
		}
		fatal(config.SaveSettings(st))
		if st.CurrentProfile != "" {
			if _, err := config.Generate(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "设置已保存，但重新生成配置失败: %v\n", err)
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "mixed-port: %d\n", st.MixedPort)
		fmt.Fprintf(cmd.OutOrStdout(), "external-controller: %s\n", st.ExternalController)
		fmt.Fprintln(cmd.OutOrStdout(), "若内核正在运行，请执行 partyctl restart 使新端口生效。")
	},
}

func init() {
	portSetCmd.Flags().IntVar(&portMixed, "mixed", 0, "mixed 代理端口")
	portSetCmd.Flags().StringVar(&portController, "controller", "", "外部控制器地址，例如 127.0.0.1:9091")
	portCmd.AddCommand(portSetCmd)
	rootCmd.AddCommand(portCmd)
}
