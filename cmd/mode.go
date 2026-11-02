package cmd

import (
	"fmt"
	"strings"

	"github.com/partyctl/partyctl/internal/client"
	"github.com/partyctl/partyctl/internal/config"
	"github.com/spf13/cobra"
)

var modeCmd = &cobra.Command{
	Use:   "mode [rule|global|direct]",
	Short: "查看或切换代理模式（PATCH /configs）",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		s, err := config.LoadSettings()
		fatal(err)
		if len(args) == 0 {
			fmt.Println(s.Mode)
			return
		}
		mode := strings.ToLower(args[0])
		switch mode {
		case "rule", "global", "direct":
		default:
			fatal(fmt.Errorf("mode must be rule|global|direct"))
		}
		c, err := client.New()
		fatal(err)
		fatal(c.PatchConfigs(map[string]any{"mode": mode}))
		s.Mode = mode
		fatal(config.SaveSettings(s))
		fmt.Printf("mode -> %s\n", mode)
	},
}
