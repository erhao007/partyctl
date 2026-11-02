package cmd

import (
	"fmt"
	"time"

	"github.com/partyctl/partyctl/internal/diag"
	"github.com/spf13/cobra"
)

var diagURLs []string
var diagTimeout int

var diagCmd = &cobra.Command{
	Use:   "diag",
	Short: "检查内核、端口、节点和常用网站的代理连通性",
	Long: `对比直连和 mixed-port 代理访问常用站点，并尽量标出走过的节点和规则。
不指定 --url 时检查 Google、GitHub、api.github.com 和 YouTube。

示例:
  partyctl diag
  partyctl diag --url https://example.com --timeout 5`,
	Run: func(cmd *cobra.Command, args []string) {
		rep, err := diag.Run(diagURLs, time.Duration(diagTimeout)*time.Second)
		if rep.Summary != "" || len(rep.Probes) > 0 {
			fmt.Fprint(cmd.OutOrStdout(), rep.Text())
		}
		fatal(err)
	},
}

func init() {
	diagCmd.Flags().StringArrayVar(&diagURLs, "url", nil, "要检测的网址，可重复。指定后不再使用默认站点")
	diagCmd.Flags().IntVar(&diagTimeout, "timeout", 8, "每个请求的超时秒数")
	rootCmd.AddCommand(diagCmd)
}
