package cmd

import (
	"fmt"
	"strings"

	"github.com/partyctl/partyctl/internal/core"
	"github.com/partyctl/partyctl/internal/mirror"
	"github.com/partyctl/partyctl/internal/service"
	"github.com/spf13/cobra"
)

var coreCmd = &cobra.Command{
	Use:   "core",
	Short: "管理 mihomo 内核",
}

var coreInstallVersion string
var coreInstallIfMissing bool
var coreInstallMirror string

var coreInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "下载并安装 mihomo（默认经镜像，失败回退 GitHub）",
	Run: func(cmd *cobra.Command, args []string) {
		out := cmd.OutOrStdout()
		if coreInstallIfMissing {
			if ver, err := core.Version(); err == nil && strings.TrimSpace(ver) != "" {
				line := ver
				if i := strings.IndexByte(line, '\n'); i >= 0 {
					line = line[:i]
				}
				fmt.Fprintf(out, "内核已安装，跳过下载: %s\n", strings.TrimSpace(line))
				fmt.Fprintln(out, "路径: ~/.local/share/partyctl/mihomo")
				fmt.Fprintln(out, "如需更新内核请执行: partyctl core install")
				return
			}
		}
		base := mirror.Base()
		if cmd.Flags().Changed("mirror") {
			base = mirror.Normalize(coreInstallMirror)
		}
		tag, err := core.InstallWith(coreInstallVersion, base)
		fatal(err)
		fmt.Fprintf(out, "installed mihomo %s -> ~/.local/share/partyctl/mihomo\n", tag)
		ver, err := core.Version()
		if err == nil {
			fmt.Fprintln(out, ver)
		}
	},
}

var coreReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "若内核已在运行，收成一个进程并重新启动；未运行则不动",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		msg, err := service.ReconcileCore()
		if msg != "" {
			fmt.Fprint(cmd.OutOrStdout(), msg)
		}
		fatal(err)
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
	coreInstallCmd.Flags().BoolVar(&coreInstallIfMissing, "if-missing", false, "内核已能运行时跳过下载")
	coreInstallCmd.Flags().StringVar(&coreInstallMirror, "mirror", "", "内核镜像原点（默认 $PARTYCTL_MIRROR 或 https://get.251819788.xyz；off 表示只从 GitHub 下载）")
	coreCmd.AddCommand(coreInstallCmd, coreVersionCmd, coreReconcileCmd)
}
