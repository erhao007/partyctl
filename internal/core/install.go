package core

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/partyctl/partyctl/internal/config"
)

const releasesAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases"

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func archAsset() string {
	switch runtime.GOARCH {
	case "amd64":
		return "linux-amd64"
	case "arm64":
		return "linux-arm64"
	default:
		return "linux-" + runtime.GOARCH
	}
}

// Install downloads mihomo release matching arch into ~/.local/share/partyctl/mihomo.
func Install(version string) (string, error) {
	if err := config.EnsureDirs(); err != nil {
		return "", err
	}
	rel, err := pickRelease(version)
	if err != nil {
		return "", err
	}
	want := archAsset()
	var url, name string
	for _, a := range rel.Assets {
		n := strings.ToLower(a.Name)
		if strings.Contains(n, want) && strings.HasSuffix(n, ".gz") && !strings.Contains(n, "go") {
			// prefer mihomo-linux-amd64-*.gz not compatible variants with go version in weird places
			url = a.BrowserDownloadURL
			name = a.Name
			break
		}
	}
	if url == "" {
		return "", fmt.Errorf("no asset matching %s-*.gz in release %s", want, rel.TagName)
	}

	tmpGz := filepath.Join(config.DataDir(), name)
	if err := downloadFile(url, tmpGz); err != nil {
		return "", err
	}
	defer os.Remove(tmpGz)

	dest := config.CoreBinaryPath()
	if err := gunzipTo(tmpGz, dest); err != nil {
		return "", err
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func pickRelease(version string) (*ghRelease, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	if version == "" || version == "latest" {
		resp, err := client.Get(releasesAPI + "/latest")
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("github releases: %s: %s", resp.Status, string(body))
		}
		var rel ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			return nil, err
		}
		return &rel, nil
	}
	tag := version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	resp, err := client.Get(releasesAPI + "/tags/" + tag)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github release %s: %s: %s", tag, resp.Status, string(body))
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "partyctl")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func gunzipTo(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, gr); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, dest)
}

// Version runs mihomo -v.
func Version() (string, error) {
	bin := config.CoreBinaryPath()
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("mihomo not installed at %s", bin)
	}
	out, err := exec.Command(bin, "-v").CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
