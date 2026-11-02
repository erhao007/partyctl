package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/partyctl/partyctl/internal/config"
	"github.com/spf13/cobra"
)

var subCmd = &cobra.Command{
	Use:   "sub",
	Short: "订阅 / 配置文件管理",
}

var subAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "添加订阅并立即下载",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name, rawURL := args[0], args[1]
		fatal(config.EnsureDirs())
		meta, err := config.LoadProfilesMeta()
		fatal(err)
		if _, _, ok := config.FindProfile(meta, name); ok {
			fatal(fmt.Errorf("profile %q already exists", name))
		}
		userinfo, err := downloadSubscription(name, rawURL)
		fatal(err)
		meta.Profiles = append(meta.Profiles, config.ProfileMeta{
			Name:                 name,
			URL:                  rawURL,
			Updated:              time.Now().Format(time.RFC3339),
			SubscriptionUserinfo: userinfo,
		})
		fatal(config.SaveProfilesMeta(meta))
		fmt.Printf("added profile %q\n", name)
	},
}

var subUpdateAll bool

var subUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "更新订阅（--all 更新全部）",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		meta, err := config.LoadProfilesMeta()
		fatal(err)
		names := []string{}
		if subUpdateAll {
			for _, p := range meta.Profiles {
				names = append(names, p.Name)
			}
		} else if len(args) == 1 {
			names = []string{args[0]}
		} else {
			s, err := config.LoadSettings()
			fatal(err)
			if s.CurrentProfile == "" {
				fatal(fmt.Errorf("specify name or --all"))
			}
			names = []string{s.CurrentProfile}
		}
		for _, name := range names {
			p, idx, ok := config.FindProfile(meta, name)
			if !ok {
				fatal(fmt.Errorf("profile %q not found", name))
			}
			if p.URL == "" {
				fmt.Printf("skip %q (local, no url)\n", name)
				continue
			}
			userinfo, err := downloadSubscription(name, p.URL)
			fatal(err)
			meta.Profiles[idx].Updated = time.Now().Format(time.RFC3339)
			meta.Profiles[idx].SubscriptionUserinfo = userinfo
			fmt.Printf("updated %q\n", name)
		}
		fatal(config.SaveProfilesMeta(meta))
	},
}

var subUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "切换当前配置",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		meta, err := config.LoadProfilesMeta()
		fatal(err)
		if _, _, ok := config.FindProfile(meta, name); !ok {
			if _, err := os.Stat(config.ProfilePath(name)); err != nil {
				fatal(fmt.Errorf("profile %q not found", name))
			}
			meta.Profiles = append(meta.Profiles, config.ProfileMeta{Name: name})
			fatal(config.SaveProfilesMeta(meta))
		}
		s, err := config.LoadSettings()
		fatal(err)
		s.CurrentProfile = name
		fatal(config.SaveSettings(s))
		fmt.Printf("current profile -> %s\n", name)
	},
}

var subListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出配置",
	Run: func(cmd *cobra.Command, args []string) {
		s, err := config.LoadSettings()
		fatal(err)
		meta, err := config.LoadProfilesMeta()
		fatal(err)
		if len(meta.Profiles) == 0 {
			fmt.Println("(no profiles)")
			return
		}
		for _, p := range meta.Profiles {
			mark := " "
			if p.Name == s.CurrentProfile {
				mark = "*"
			}
			fmt.Printf("%s %-16s  updated=%s  url=%s\n", mark, p.Name, p.Updated, p.URL)
			if p.SubscriptionUserinfo != "" {
				fmt.Printf("    subscription-userinfo: %s\n", p.SubscriptionUserinfo)
			}
		}
	},
}

var subRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "删除配置",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		meta, err := config.LoadProfilesMeta()
		fatal(err)
		_, idx, ok := config.FindProfile(meta, name)
		if !ok {
			fatal(fmt.Errorf("profile %q not found", name))
		}
		meta.Profiles = append(meta.Profiles[:idx], meta.Profiles[idx+1:]...)
		fatal(config.SaveProfilesMeta(meta))
		_ = os.Remove(config.ProfilePath(name))
		s, err := config.LoadSettings()
		fatal(err)
		if s.CurrentProfile == name {
			s.CurrentProfile = ""
			fatal(config.SaveSettings(s))
		}
		fmt.Printf("removed %q\n", name)
	},
}

func downloadSubscription(name, rawURL string) (string, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "partyctl/clash.meta")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download %s: %s: %s", rawURL, resp.Status, string(b))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(config.ProfilePath(name), body, 0o644); err != nil {
		return "", err
	}
	return resp.Header.Get("Subscription-Userinfo"), nil
}

func init() {
	subUpdateCmd.Flags().BoolVar(&subUpdateAll, "all", false, "更新全部订阅")
	subCmd.AddCommand(subAddCmd, subUpdateCmd, subUseCmd, subListCmd, subRmCmd)
}
