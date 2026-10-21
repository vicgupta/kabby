package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.8.1", "0.8.0", true},
		{"v0.10.0", "0.9.9", true},
		{"v1.0.0", "0.99.0", true},
		{"v0.8.0", "0.8.0", false},
		{"v0.7.11", "0.8.0", false},
		{"v0.8.1-rc1", "0.8.0", true},
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// fakeRelease serves a latest-release document whose binary for this
// platform is a shell script printing printed.
func fakeRelease(t *testing.T, tag, printed string, sizeOff int64) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake binaries are shell scripts")
	}
	script := fmt.Sprintf("#!/bin/sh\necho 'kabby %s'\n", printed)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"assets": []map[string]any{{
				"name":                 assetName(runtime.GOOS, runtime.GOARCH),
				"browser_download_url": srv.URL + "/bin",
				"size":                 int64(len(script)) + sizeOff,
			}},
		})
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, script)
	})
	old := releasesAPI
	releasesAPI = srv.URL + "/latest"
	t.Cleanup(func() { releasesAPI = old })
}

// installed writes a stand-in for the current kabby binary.
func installed(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "kabby")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'kabby 0.8.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func runVersion(t *testing.T, exe string) string {
	t.Helper()
	out, err := exec.Command(exe, "--version").Output()
	if err != nil {
		t.Fatalf("running %s: %v", exe, err)
	}
	return strings.TrimSpace(string(out))
}

func TestSelfUpdateInstallsNewerRelease(t *testing.T) {
	fakeRelease(t, "v0.9.0", "0.9.0", 0)
	exe := installed(t)
	before, _ := os.Stat(exe)

	var out bytes.Buffer
	if err := selfUpdate(exe, "0.8.0", &out); err != nil {
		t.Fatal(err)
	}
	if got := runVersion(t, exe); got != "kabby 0.9.0" {
		t.Fatalf("expected the new binary, got %q", got)
	}
	after, _ := os.Stat(exe)
	if os.SameFile(before, after) {
		t.Fatal("the update must be a new file renamed into place, not an in-place overwrite")
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestSelfUpdateUpToDate(t *testing.T) {
	fakeRelease(t, "v0.8.0", "0.8.0", 0)
	exe := installed(t)
	var out bytes.Buffer
	if err := selfUpdate(exe, "0.8.0", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "up to date") || runVersion(t, exe) != "kabby 0.8.0" {
		t.Fatalf("expected no change, got %q", out.String())
	}
}

func TestSelfUpdateKeepsWorkingBinaryOnBadDownload(t *testing.T) {
	for name, tc := range map[string]struct {
		printed string
		sizeOff int64
	}{
		"wrong version": {"0.0.1", 0},
		"size mismatch": {"0.9.0", 7},
	} {
		t.Run(name, func(t *testing.T) {
			fakeRelease(t, "v0.9.0", tc.printed, tc.sizeOff)
			exe := installed(t)
			if err := selfUpdate(exe, "0.8.0", &bytes.Buffer{}); err == nil {
				t.Fatal("expected the update to fail")
			}
			if got := runVersion(t, exe); got != "kabby 0.8.0" {
				t.Fatalf("a failed update must leave the old binary, got %q", got)
			}
			entries, _ := os.ReadDir(filepath.Dir(exe))
			if len(entries) != 1 {
				t.Fatalf("temporary files left behind: %v", entries)
			}
		})
	}
}
