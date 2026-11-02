package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Settings is partyctl's own config (~/.config/partyctl/config.yaml).
type Settings struct {
	MixedPort          int       `yaml:"mixed-port"`
	ExternalController string    `yaml:"external-controller"`
	Secret             string    `yaml:"secret"`
	CurrentProfile     string    `yaml:"current-profile"`
	Mode               string    `yaml:"mode"`
	TUNEnabled         bool      `yaml:"tun"`
	LogLevel           string    `yaml:"log-level"`
	AllowLan           bool      `yaml:"allow-lan"`
	DelayTestURL       string    `yaml:"delay-test-url"`
	DelayTestTimeout   int       `yaml:"delay-test-timeout"`
	Overrides          []string  `yaml:"overrides"` // ordered override file basenames
	Web                WebConfig `yaml:"web"`
}

// WebConfig controls the embedded web UI.
type WebConfig struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
	Token   string `yaml:"token"` // empty => reuse Secret
}

func DefaultSettings() Settings {
	return Settings{
		MixedPort:          7890,
		ExternalController: "127.0.0.1:9090",
		Secret:             "",
		Mode:               "rule",
		LogLevel:           "info",
		DelayTestURL:       "http://www.gstatic.com/generate_204",
		DelayTestTimeout:   5000,
		Overrides:          nil,
		Web: WebConfig{
			Enabled: false,
			Listen:  "127.0.0.1:9800",
			Token:   "",
		},
	}
}

func LoadSettings() (Settings, error) {
	if err := EnsureDirs(); err != nil {
		return Settings{}, err
	}
	path := SettingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s := DefaultSettings()
			s.Secret = generateSecret()
			if err := SaveSettings(s); err != nil {
				return Settings{}, err
			}
			return s, nil
		}
		return Settings{}, err
	}
	s := DefaultSettings()
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("parse settings: %w", err)
	}
	if s.Secret == "" {
		s.Secret = generateSecret()
		_ = SaveSettings(s)
	}
	if s.MixedPort == 0 {
		s.MixedPort = 7890
	}
	if s.ExternalController == "" {
		s.ExternalController = "127.0.0.1:9090"
	}
	if s.Mode == "" {
		s.Mode = "rule"
	}
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
	if s.DelayTestURL == "" {
		s.DelayTestURL = "http://www.gstatic.com/generate_204"
	}
	if s.DelayTestTimeout == 0 {
		s.DelayTestTimeout = 5000
	}
	if s.Web.Listen == "" {
		s.Web.Listen = "127.0.0.1:9800"
	}
	return s, nil
}

func SaveSettings(s Settings) error {
	if err := EnsureDirs(); err != nil {
		return err
	}
	data, err := yaml.Marshal(&s)
	if err != nil {
		return err
	}
	return os.WriteFile(SettingsPath(), data, 0o600)
}

func generateSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ProfileMeta holds subscription metadata.
type ProfileMeta struct {
	Name                 string `yaml:"name"`
	URL                  string `yaml:"url,omitempty"`
	Updated              string `yaml:"updated,omitempty"`
	SubscriptionUserinfo string `yaml:"subscription-userinfo,omitempty"`
}

type ProfilesFile struct {
	Profiles []ProfileMeta `yaml:"profiles"`
}

func LoadProfilesMeta() (ProfilesFile, error) {
	if err := EnsureDirs(); err != nil {
		return ProfilesFile{}, err
	}
	path := ProfilesMetaPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ProfilesFile{}, nil
		}
		return ProfilesFile{}, err
	}
	var pf ProfilesFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return ProfilesFile{}, err
	}
	return pf, nil
}

func SaveProfilesMeta(pf ProfilesFile) error {
	if err := EnsureDirs(); err != nil {
		return err
	}
	data, err := yaml.Marshal(&pf)
	if err != nil {
		return err
	}
	return os.WriteFile(ProfilesMetaPath(), data, 0o644)
}

func FindProfile(pf ProfilesFile, name string) (ProfileMeta, int, bool) {
	for i, p := range pf.Profiles {
		if p.Name == name {
			return p, i, true
		}
	}
	return ProfileMeta{}, -1, false
}

// WebAuthToken returns web.token if set, otherwise the controller secret.
func WebAuthToken(s Settings) string {
	if s.Web.Token != "" {
		return s.Web.Token
	}
	return s.Secret
}

// WebURL is the browser address for the embedded UI.
func WebURL(s Settings) string {
	listen := strings.TrimSpace(s.Web.Listen)
	if listen == "" {
		listen = "127.0.0.1:9800"
	}
	if strings.Contains(listen, "://") {
		return listen
	}
	return "http://" + listen
}
