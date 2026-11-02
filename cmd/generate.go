package cmd

import (
	"fmt"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/spf13/cobra"
)

var generateSkipValidate bool

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "生成 work/config.yaml（profile → overrides → 强制基础设置）并校验",
	Run: func(cmd *cobra.Command, args []string) {
		path, err := config.Generate()
		fatal(err)
		fmt.Printf("wrote %s\n", path)
		if generateSkipValidate {
			return
		}
		if err := config.Validate(""); err != nil {
			fatal(err)
		}
		fmt.Println("validation: ok")
	},
}

func init() {
	generateCmd.Flags().BoolVar(&generateSkipValidate, "skip-validate", false, "跳过 mihomo -t")
}
