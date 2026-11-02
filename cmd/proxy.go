package cmd

import (
	"github.com/partyctl/partyctl/internal/proxy"
	"github.com/spf13/cobra"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "系统代理 / 环境变量",
}

var proxyOnCmd = &cobra.Command{
	Use:   "on",
	Short: "开启 GNOME 系统代理并打印 export",
	Run: func(cmd *cobra.Command, args []string) {
		fatal(proxy.On())
	},
}

var proxyOffCmd = &cobra.Command{
	Use:   "off",
	Short: "关闭 GNOME 系统代理并打印 unset",
	Run: func(cmd *cobra.Command, args []string) {
		fatal(proxy.Off())
	},
}

var proxyEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "打印 shell 代理环境变量",
	Run: func(cmd *cobra.Command, args []string) {
		fatal(proxy.PrintEnv())
	},
}

func init() {
	proxyCmd.AddCommand(proxyOnCmd, proxyOffCmd, proxyEnvCmd)
}
