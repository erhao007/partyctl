package cmd

import (
	"github.com/partyctl/partyctl/internal/proxy"
	"github.com/spf13/cobra"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "系统代理，或按进程接管",
	Long: `proxy on/off/env 管理 GNOME 系统代理和环境变量。

proxy pid / proxy clear 只让指定进程走 mihomo，不开启全局 TUN，也不修改系统代理。
这两类命令需要 root。内核被 partyctl 停止时会先撤掉接管规则，避免进程断网。`,
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
