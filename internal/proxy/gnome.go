package proxy

import (
	"fmt"
	"os/exec"
	"strconv"

	"github.com/partyctl/partyctl/internal/config"
)

func EnvExports(port int) string {
	if port == 0 {
		port = 7890
	}
	p := strconv.Itoa(port)
	host := "127.0.0.1"
	proxy := "http://" + host + ":" + p
	return fmt.Sprintf(`export http_proxy=%s
export https_proxy=%s
export HTTP_PROXY=%s
export HTTPS_PROXY=%s
export all_proxy=socks5://%s:%s
export ALL_PROXY=socks5://%s:%s
export no_proxy=localhost,127.0.0.1,::1
export NO_PROXY=localhost,127.0.0.1,::1
`, proxy, proxy, proxy, proxy, host, p, host, p)
}

func EnvUnset() string {
	return `unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY no_proxy NO_PROXY
`
}

func On() error {
	s, err := config.LoadSettings()
	if err != nil {
		return err
	}
	port := strconv.Itoa(s.MixedPort)
	host := "127.0.0.1"
	cmds := [][]string{
		{"gsettings", "set", "org.gnome.system.proxy", "mode", "manual"},
		{"gsettings", "set", "org.gnome.system.proxy.http", "host", host},
		{"gsettings", "set", "org.gnome.system.proxy.http", "port", port},
		{"gsettings", "set", "org.gnome.system.proxy.https", "host", host},
		{"gsettings", "set", "org.gnome.system.proxy.https", "port", port},
		{"gsettings", "set", "org.gnome.system.proxy.socks", "host", host},
		{"gsettings", "set", "org.gnome.system.proxy.socks", "port", port},
	}
	for _, c := range cmds {
		if err := runOptional(c...); err != nil {
			return err
		}
	}
	fmt.Print(EnvExports(s.MixedPort))
	return nil
}

func Off() error {
	_ = runOptional("gsettings", "set", "org.gnome.system.proxy", "mode", "none")
	fmt.Print(EnvUnset())
	return nil
}

func PrintEnv() error {
	s, err := config.LoadSettings()
	if err != nil {
		return err
	}
	fmt.Print(EnvExports(s.MixedPort))
	return nil
}

func runOptional(args ...string) error {
	if _, err := exec.LookPath(args[0]); err != nil {
		fmt.Printf("# skip (not found): %v\n", args)
		return nil
	}
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %v (%s)", args, err, string(out))
	}
	return nil
}
