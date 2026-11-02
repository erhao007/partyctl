package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/partyctl/partyctl/internal/client"
	"github.com/partyctl/partyctl/internal/config"
	"github.com/spf13/cobra"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "节点 / 策略组",
}

var nodeListCmd = &cobra.Command{
	Use:   "list [group]",
	Short: "列出策略组或组内节点",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.New()
		fatal(err)
		proxies, err := c.Proxies()
		fatal(err)
		pm, _ := proxies["proxies"].(map[string]any)
		if pm == nil {
			fatal(fmt.Errorf("unexpected /proxies response"))
		}
		if len(args) == 1 {
			g := args[0]
			info, ok := pm[g].(map[string]any)
			if !ok {
				fatal(fmt.Errorf("group/node %q not found", g))
				return
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(info)
			return
		}
		names := make([]string, 0, len(pm))
		for name, v := range pm {
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			if strings.EqualFold(typ, "Selector") || strings.EqualFold(typ, "URLTest") ||
				strings.EqualFold(typ, "Fallback") || strings.EqualFold(typ, "LoadBalance") ||
				strings.EqualFold(typ, "Relay") {
				now, _ := m["now"].(string)
				line := fmt.Sprintf("%-24s %-12s now=%s", name, typ, now)
				if fixed := client.FixedProxy(m); fixed != "" && !strings.EqualFold(typ, "Selector") {
					line += " 已固定=" + fixed
				}
				if strings.EqualFold(typ, "Selector") {
					line += " 手动"
				}
				names = append(names, line)
			}
		}
		sort.Strings(names)
		for _, line := range names {
			fmt.Println(line)
		}
	},
}

var nodeSelectCmd = &cobra.Command{
	Use:   "select <group> <node>",
	Short: "切换策略组节点（PUT /proxies/{group}）",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.New()
		fatal(err)
		fatal(c.SelectProxy(args[0], args[1]))
		fmt.Printf("%s -> %s\n", args[0], args[1])
		if proxies, err := c.Proxies(); err == nil {
			if pm, _ := proxies["proxies"].(map[string]any); pm != nil {
				if info, _ := pm[args[0]].(map[string]any); info != nil {
					typ := client.GroupType(info)
					if strings.EqualFold(typ, "URLTest") || strings.EqualFold(typ, "Fallback") {
						fmt.Printf("已固定。自动测速不再改掉这个节点。恢复自动: partyctl node auto %s\n", args[0])
					}
				}
			}
		}
	},
}

var nodeAutoCmd = &cobra.Command{
	Use:   "auto <group>",
	Short: "解除 url-test/fallback 的固定选择，恢复自动",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := client.New()
		fatal(err)
		proxies, err := c.Proxies()
		fatal(err)
		pm, _ := proxies["proxies"].(map[string]any)
		if pm == nil {
			fatal(fmt.Errorf("控制器没有返回策略组"))
		}
		info, _ := pm[args[0]].(map[string]any)
		if info == nil {
			fatal(fmt.Errorf("找不到策略组 %q", args[0]))
		}
		typ := client.GroupType(info)
		if strings.EqualFold(typ, "Selector") {
			fatal(fmt.Errorf("「%s」是手动选择组，没有自动切换。更换节点请用 partyctl node select", args[0]))
		}
		if client.FixedProxy(info) == "" {
			fmt.Printf("%s 当前没有固定节点。\n", args[0])
			return
		}
		fatal(c.UnfixProxy(args[0]))
		fmt.Printf("已恢复自动: %s\n", args[0])
	},
}

var nodeDelayCmd = &cobra.Command{
	Use:   "delay <node|group>",
	Short: "测延迟（/proxies/{n}/delay 或 /group/{g}/delay）",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		s, err := config.LoadSettings()
		fatal(err)
		c, err := client.New()
		fatal(err)
		// If /proxies shows this name as a group (has "all"), use /group/{g}/delay.
		isGroup := false
		if proxies, err := c.Proxies(); err == nil {
			if pm, _ := proxies["proxies"].(map[string]any); pm != nil {
				if info, _ := pm[name].(map[string]any); info != nil {
					if _, ok := info["all"]; ok {
						isGroup = true
					}
				}
			}
		}
		var out map[string]any
		if isGroup {
			out, err = c.GroupDelay(name, s.DelayTestURL, s.DelayTestTimeout)
		} else {
			out, err = c.ProxyDelay(name, s.DelayTestURL, s.DelayTestTimeout)
			if err != nil {
				out, err = c.GroupDelay(name, s.DelayTestURL, s.DelayTestTimeout)
			}
		}
		fatal(err)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	},
}

func init() {
	nodeCmd.AddCommand(nodeListCmd, nodeSelectCmd, nodeAutoCmd, nodeDelayCmd)
}
