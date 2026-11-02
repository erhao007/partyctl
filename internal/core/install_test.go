package core

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/partyctl/partyctl/internal/config"
)

func TestPickAssetNameSkipsCompatibleAndGoVariants(t *testing.T) {
	assets := []ghAsset{
		{Name: "mihomo-linux-amd64-compatible-v1.gz"},
		{Name: "mihomo-linux-amd64-go120-v1.gz"},
		{Name: "mihomo-linux-amd64-v1.gz"},
		{Name: "mihomo-linux-amd64-v1.tar.gz"},
	}
	name, err := pickAssetName(assets, "linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "mihomo-linux-amd64-v1.gz" {
		t.Fatalf("picked %s", name)
	}
	onlyFallback := []ghAsset{{Name: "mihomo-linux-arm64-compatible-v1.gz"}}
	name, err = pickAssetName(onlyFallback, "linux-arm64")
	if err != nil || name != "mihomo-linux-arm64-compatible-v1.gz" {
		t.Fatalf("fallback: %s %v", name, err)
	}
}

func TestSourcesForOrder(t *testing.T) {
	both := sourcesFor("https://mirror.example")
	if len(both) != 2 || both[0].name != "镜像" || both[1].name != "GitHub" {
		t.Fatalf("order: %+v", both)
	}
	if both[0].latest != "https://mirror.example/mihomo/releases/latest" {
		t.Fatal(both[0].latest)
	}
	if got := both[0].assetURL("v1.2.3", "mihomo-linux-amd64-v1.2.3.gz"); got != "https://mirror.example/mihomo/download/v1.2.3/mihomo-linux-amd64-v1.2.3.gz" {
		t.Fatal(got)
	}
	only := sourcesFor("")
	if len(only) != 1 || only[0].name != "GitHub" {
		t.Fatalf("github only: %+v", only)
	}
	if got := only[0].assetURL("v1.2.3", "a.gz"); got != "https://github.com/MetaCubeX/mihomo/releases/download/v1.2.3/a.gz" {
		t.Fatal(got)
	}
}

func TestInstallUsesMirrorAndSkipsGitHub(t *testing.T) {
	payload := []byte("#!/bin/sh\necho 'Mihomo Meta test'\n")
	asset := "mihomo-" + archAsset() + "-v9.9.9.gz"
	var githubHits int
	mirrorSrv := serveRelease(t, "v9.9.9", asset, gzipBytes(t, payload), &githubHits, false)
	githubSrv := serveRelease(t, "v9.9.9", asset, gzipBytes(t, payload), &githubHits, true)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withFastRetry(t)

	tag, err := installUsing("", []source{
		httptestSource("镜像", mirrorSrv.URL),
		httptestSource("GitHub", githubSrv.URL),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v9.9.9" {
		t.Fatalf("tag %s", tag)
	}
	if githubHits != 0 {
		t.Fatalf("github was contacted %d times", githubHits)
	}
	ver, err := Version()
	if err != nil || !strings.Contains(ver, "Mihomo Meta test") {
		t.Fatalf("version %q %v", ver, err)
	}
}

func TestInstallFallsBackToGitHub(t *testing.T) {
	payload := []byte("#!/bin/sh\necho fallback\n")
	asset := "mihomo-" + archAsset() + "-v8.8.8.gz"
	var mirrorHits, githubHits int
	mirrorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits++
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(mirrorSrv.Close)
	githubSrv := serveRelease(t, "v8.8.8", asset, gzipBytes(t, payload), &githubHits, true)
	t.Setenv("HOME", t.TempDir())
	withFastRetry(t)

	tag, err := installUsing("", []source{
		httptestSource("镜像", mirrorSrv.URL),
		httptestSource("GitHub", githubSrv.URL),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v8.8.8" {
		t.Fatalf("tag %s", tag)
	}
	if mirrorHits == 0 || githubHits == 0 {
		t.Fatalf("mirror hits %d github hits %d", mirrorHits, githubHits)
	}
	if _, err := os.Stat(config.CoreBinaryPath()); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRetriesThenSucceeds(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			http.Error(w, "later", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v1.0.0","assets":[]}`)
	}))
	t.Cleanup(srv.Close)
	withFastRetry(t)
	body, err := getBytes(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "v1.0.0") || hits != 3 {
		t.Fatalf("hits %d body %s", hits, body)
	}
}

func withFastRetry(t *testing.T) {
	t.Helper()
	oldWait := retryWait
	oldAttempts := downloadAttempts
	retryWait = 0
	downloadAttempts = 3
	t.Cleanup(func() {
		retryWait = oldWait
		downloadAttempts = oldAttempts
	})
}

func httptestSource(name, base string) source {
	return source{
		name:   name,
		latest: base + "/mihomo/releases/latest",
		tagURL: func(tag string) string { return base + "/mihomo/releases/tags/" + tag },
		assetURL: func(tag, file string) string {
			return base + "/mihomo/download/" + tag + "/" + file
		},
	}
}

func serveRelease(t *testing.T, tag, asset string, gz []byte, hits *int, count bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if count {
			*hits++
		}
		switch r.URL.Path {
		case "/mihomo/releases/latest":
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q}]}`, tag, asset)
		case "/mihomo/download/" + tag + "/" + asset:
			w.Write(gz)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func gzipBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
