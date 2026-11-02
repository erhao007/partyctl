package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "partyctl",
	Short: "mihomo (Clash Meta) 的轻量 Ubuntu CLI 封装，灵感来自 Clash Party",
	Long: `partyctl 管理 mihomo 内核、订阅配置、覆盖规则与系统代理。
配置目录: ~/.config/partyctl/
内核路径: ~/.local/share/partyctl/mihomo`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(coreCmd)
	rootCmd.AddCommand(subCmd)
	rootCmd.AddCommand(overrideCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(restartCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(modeCmd)
	rootCmd.AddCommand(nodeCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(tunCmd)
	rootCmd.AddCommand(versionCmd)
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
