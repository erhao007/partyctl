package config

import (
	"os"
	"path/filepath"
)

func HomeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func ConfigDir() string {
	return filepath.Join(HomeDir(), ".config", "partyctl")
}

func SettingsPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

func ProfilesDir() string {
	return filepath.Join(ConfigDir(), "profiles")
}

func ProfilesMetaPath() string {
	return filepath.Join(ConfigDir(), "profiles.yaml")
}

func ProfilePath(name string) string {
	return filepath.Join(ProfilesDir(), name+".yaml")
}

func OverrideDir() string {
	return filepath.Join(ConfigDir(), "override")
}

func WorkDir() string {
	return filepath.Join(ConfigDir(), "work")
}

func WorkConfigPath() string {
	return filepath.Join(WorkDir(), "config.yaml")
}

func DataDir() string {
	return filepath.Join(HomeDir(), ".local", "share", "partyctl")
}

func CoreBinaryPath() string {
	return filepath.Join(DataDir(), "mihomo")
}

func SystemdUserDir() string {
	return filepath.Join(HomeDir(), ".config", "systemd", "user")
}

func SystemdUnitPath() string {
	return filepath.Join(SystemdUserDir(), "partyctl.service")
}

func EnsureDirs() error {
	for _, d := range []string{ConfigDir(), ProfilesDir(), OverrideDir(), WorkDir(), DataDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
