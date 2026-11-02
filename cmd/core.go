package cmd

import (
	"fmt"

	"github.com/partyctl/partyctl/internal/core"
	"github.com/spf13/cobra"
)

var coreCmd = &cobra.Command{
	Use:   "core",
	Short: "管理 mihomo 内核",
}

var coreInstallVersion string

var coreInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "从 GitHub 下载并安装 mihomo（linux amd64/arm64 .gz）",
	Run: func(cmd *cobra.Command, args []string) {
		tag, err := core.Install(coreInstallVersion)
		fatal(err)
		fmt.Printf("installed mihomo %s -> ~/.local/share/partyctl/mihomo\n", tag)
		ver, err := core.Version()
		if err == nil {
			fmt.Println(ver)
		}
	},
}

var coreVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示已安装 mihomo 版本",
	Run: func(cmd *cobra.Command, args []string) {
		ver, err := core.Version()
		fatal(err)
		fmt.Println(ver)
	},
}

func init() {
	coreInstallCmd.Flags().StringVar(&coreInstallVersion, "version", "", "release tag（默认 latest）")
	coreCmd.AddCommand(coreInstallCmd, coreVersionCmd)
}
