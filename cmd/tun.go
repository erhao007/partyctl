package cmd

import (
	"fmt"
	"os/exec"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/spf13/cobra"
)

var tunCmd = &cobra.Command{
	Use:   "tun [on|off]",
	Short: "开启/关闭 TUN（写入配置并 regenerate；提示 setcap）",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var enable bool
		switch args[0] {
		case "on":
			enable = true
		case "off":
			enable = false
		default:
			fatal(fmt.Errorf("use on|off"))
		}
		s, err := config.LoadSettings()
		fatal(err)
		s.TUNEnabled = enable
		fatal(config.SaveSettings(s))
		path, err := config.Generate()
		fatal(err)
		fmt.Printf("tun=%v, wrote %s\n", enable, path)
		bin := config.CoreBinaryPath()
		if enable {
			fmt.Printf("TUN 需要权限，可执行:\n  sudo setcap cap_net_admin,cap_net_bind_service=+ep %s\n", bin)
			fmt.Println("或依赖 systemd user unit 中的 AmbientCapabilities（需合适权限）。")
			if path, err := exec.LookPath("setcap"); err == nil {
				_ = path
				fmt.Println("尝试执行 setcap（可能需要 sudo）...")
				out, err := exec.Command("sudo", "-n", "setcap", "cap_net_admin,cap_net_bind_service=+ep", bin).CombinedOutput()
				if err != nil {
					fmt.Printf("setcap 未自动完成: %v (%s)\n请手动执行上述命令。\n", err, string(out))
				} else {
					fmt.Println("setcap ok")
				}
			}
		}
		_ = config.Validate("")
	},
}
