package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/spf13/cobra"
)

var overrideCmd = &cobra.Command{
	Use:   "override",
	Short: "YAML 覆盖配置（支持 ! / +key / key+ 语义）",
}

var overrideAddCmd = &cobra.Command{
	Use:   "add <file>",
	Short: "添加覆盖文件到 override/ 并加入生成链",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		src := args[0]
		fatal(config.EnsureDirs())
		data, err := os.ReadFile(src)
		fatal(err)
		base := filepath.Base(src)
		if !strings.HasSuffix(base, ".yaml") && !strings.HasSuffix(base, ".yml") {
			base += ".yaml"
		}
		dest := filepath.Join(config.OverrideDir(), base)
		fatal(os.WriteFile(dest, data, 0o644))
		s, err := config.LoadSettings()
		fatal(err)
		for _, o := range s.Overrides {
			if o == base {
				fmt.Printf("override %s already registered\n", base)
				return
			}
		}
		s.Overrides = append(s.Overrides, base)
		fatal(config.SaveSettings(s))
		fmt.Printf("added override %s\n", dest)
	},
}

var overrideListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出覆盖文件",
	Run: func(cmd *cobra.Command, args []string) {
		s, err := config.LoadSettings()
		fatal(err)
		if len(s.Overrides) == 0 {
			fmt.Println("(no overrides)")
			return
		}
		for i, o := range s.Overrides {
			fmt.Printf("%d. %s\n", i+1, o)
		}
	},
}

var overrideRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "移除覆盖文件",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			name += ".yaml"
		}
		s, err := config.LoadSettings()
		fatal(err)
		found := false
		next := s.Overrides[:0]
		for _, o := range s.Overrides {
			if o == name {
				found = true
				continue
			}
			next = append(next, o)
		}
		if !found {
			fatal(fmt.Errorf("override %q not found", name))
		}
		s.Overrides = next
		fatal(config.SaveSettings(s))
		_ = os.Remove(filepath.Join(config.OverrideDir(), name))
		fmt.Printf("removed override %s\n", name)
	},
}

func init() {
	overrideCmd.AddCommand(overrideAddCmd, overrideListCmd, overrideRmCmd)
}
