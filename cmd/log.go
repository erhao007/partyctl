package cmd

import (
	"os"

	"github.com/partyctl/partyctl/internal/client"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "流式输出内核日志（GET /logs）",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.New()
		fatal(err)
		fatal(c.StreamLogs(os.Stdout))
	},
}
