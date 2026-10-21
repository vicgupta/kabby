package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// releasesAPI is the GitHub API URL of kabby's latest release. Tests point
// it at a local server.
var releasesAPI = "https://api.github.com/repos/vicgupta/kabby/releases/latest"

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// assetName is the release binary for this platform, matching install.sh.
func assetName(goos, goarch string) string {
	name := "kabby-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// newerVersion reports whether version a (e.g. "v0.8.1") is newer than b
// ("0.8.0"). Missing parts count as zero; a leading "v" is ignored.
func newerVersion(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-") // ignore pre-release suffixes
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// runUpdate replaces the running kabby with the latest release.
func runUpdate() int {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot locate the kabby binary:", err)
		return 1
	}
	if err := selfUpdate(exe, version, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// selfUpdate installs the latest release over exe if it is newer than
// current. The new binary is downloaded next to exe, checked, and renamed
// into place: a rename gives it a fresh file, which macOS requires (it
// kills a binary whose bytes were overwritten in place).
func selfUpdate(exe, current string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	rel, err := latestRelease(ctx)
	if err != nil {
		return err
	}
	if !newerVersion(rel.TagName, current) {
		fmt.Fprintf(out, "kabby %s is up to date (latest release: %s)\n", current, rel.TagName)
		return nil
	}

	name := assetName(runtime.GOOS, runtime.GOARCH)
	var url string
	var size int64
	for _, a := range rel.Assets {
		if a.Name == name {
			url, size = a.URL, a.Size
		}
	}
	if url == "" {
		return fmt.Errorf("release %s has no binary for %s/%s (%s)", rel.TagName, runtime.GOOS, runtime.GOARCH, name)
	}

	fmt.Fprintf(out, "Updating kabby %s -> %s ...\n", current, strings.TrimPrefix(rel.TagName, "v"))
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".kabby-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s (%v); rerun with permission to replace %s, or reinstall", dir, err, exe)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed into place

	written, err := download(ctx, url, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %v", name, err)
	}
	if size > 0 && written != size {
		return fmt.Errorf("download %s: got %d bytes, expected %d", name, written, size)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}

	// Run the new binary before installing it, so a broken download never
	// replaces a working kabby.
	check, err := exec.CommandContext(ctx, tmpPath, "--version").Output()
	want := strings.TrimPrefix(rel.TagName, "v")
	if err != nil || !strings.Contains(string(check), want) {
		return fmt.Errorf("downloaded binary failed its check (want version %s, got %q, err %v); nothing was changed", want, strings.TrimSpace(string(check)), err)
	}

	if err := replaceBinary(tmpPath, exe); err != nil {
		return fmt.Errorf("install %s: %v", exe, err)
	}
	fmt.Fprintf(out, "kabby %s installed to %s\n", want, exe)
	return nil
}

func latestRelease(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", releasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check for updates: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("check for updates: GitHub returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("check for updates: %v", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("check for updates: no release found")
	}
	return &rel, nil
}

func download(ctx context.Context, url string, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.Copy(w, resp.Body)
}

// replaceBinary renames src over dst. Windows cannot replace a running
// executable, so there the old one is first moved aside to dst + ".old".
func replaceBinary(src, dst string) error {
	if runtime.GOOS == "windows" {
		old := dst + ".old"
		os.Remove(old)
		if err := os.Rename(dst, old); err != nil {
			return err
		}
		if err := os.Rename(src, dst); err != nil {
			os.Rename(old, dst)
			return err
		}
		return nil
	}
	return os.Rename(src, dst)
}
