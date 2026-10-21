package detect

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
	"strings"
	"sync"
	"time"
)

// ProbeTimeout is the timeout for a single server probe at startup.
const ProbeTimeout = 1500 * time.Millisecond

// Candidate is a potential model server address.
type Candidate struct {
	URL    string
	Label  string // human-readable description
	Source string // how it was discovered (ollama-host, lmstudio, docker, etc.)
}

// LocalCandidates returns the list of local server addresses to probe.
// Port 8000 is only included on macOS (oMLX/MTPLX default).
func LocalCandidates() []Candidate {
	var cands []Candidate
	seen := make(map[string]bool)

	add := func(url, label, source string) {
		if seen[url] {
			return
		}
		seen[url] = true
		cands = append(cands, Candidate{URL: url, Label: label, Source: source})
	}

	// OLLAMA_HOST override.
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		add(normalizeURL(host), "OLLAMA_HOST", "env")
	}

	// Ollama defaults.
	add("http://127.0.0.1:11434", "Ollama localhost:11434", "default")
	add("http://localhost:11434", "Ollama localhost:11434", "default")

	// LM Studio: read port from http-server-config.json if available.
	if port := lmStudioPort(); port != 0 {
		add(fmt.Sprintf("http://127.0.0.1:%d", port), fmt.Sprintf("LM Studio :%d", port), "lmstudio-config")
	}
	add("http://127.0.0.1:1234", "LM Studio default 1234", "default")

	// oMLX/MTPLX default port 8000 (macOS only).
	if runtime.GOOS == "darwin" {
		add("http://127.0.0.1:8000", "oMLX/MTPLX :8000", "darwin-default")
	}

	// Docker/podman published ports.
	for _, port := range dockerPublishedPorts() {
		add(fmt.Sprintf("http://127.0.0.1:%d", port), fmt.Sprintf("container :%d", port), "docker")
	}

	// WSL / container gateway.
	if gw := defaultGateway(); gw != "" {
		add(fmt.Sprintf("http://%s:11434", gw), "WSL/container gateway:11434", "gateway")
		add(fmt.Sprintf("http://%s:1234", gw), "WSL/container gateway:1234", "gateway")
		add(fmt.Sprintf("http://host.docker.internal:11434"), "host.docker.internal:11434", "gateway")
		add(fmt.Sprintf("http://host.docker.internal:1234"), "host.docker.internal:1234", "gateway")
	}

	return cands
}

func normalizeURL(host string) string {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "http://" + host
}

func lmStudioPort() int {
	// Follow ~/.lmstudio-home-pointer if it exists.
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	ptr := filepath.Join(home, ".lmstudio-home-pointer")
	var configDir string
	if data, err := os.ReadFile(ptr); err == nil {
		configDir = strings.TrimSpace(string(data))
	}
	if configDir == "" {
		configDir = filepath.Join(home, "Library", "Application Support", "LM Studio")
	}
	cfg := filepath.Join(configDir, "http-server-config.json")
	data, err := os.ReadFile(cfg)
	if err != nil {
		return 0
	}
	var parsed struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0
	}
	if parsed.Port > 0 {
		return parsed.Port
	}
	return 0
}

func dockerPublishedPorts() []int {
	var ports []int
	for _, cmd := range [][]string{{"docker", "ps", "--format", "{{.Ports}}"}, {"podman", "ps", "--format", "{{.Ports}}"}} {
		out, err := exec.Command(cmd[0], cmd[1:]...).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Format: "0.0.0.0:11434->11434/tcp" or "11434/tcp"
			parts := strings.Split(line, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if idx := strings.Index(p, "->"); idx >= 0 {
					p = p[:idx]
				}
				if idx := strings.Index(p, ":"); idx >= 0 {
					p = p[idx+1:]
				}
				if idx := strings.Index(p, "/"); idx >= 0 {
					p = p[:idx]
				}
				p = strings.TrimSpace(p)
				if p != "" {
					ports = append(ports, mustAtoi(p))
				}
			}
		}
	}
	return ports
}

func defaultGateway() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	out, err := exec.Command("ip", "route", "get", "1.1.1.1").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "via ") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "via" && i+1 < len(fields) {
					return fields[i+1]
				}
			}
		}
	}
	return ""
}

func mustAtoi(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// ProbeResult is the result of probing a single server address.
type ProbeResult struct {
	URL     string
	Backend string   // ollama | lmstudio | omlx | mtplx | ""
	Models  []string // model names reported by the backend
	Error   error
}

// Probe checks a single server address by trying backend-identifying endpoints.
// Identifies by response, never by port.
func Probe(ctx context.Context, url string) ProbeResult {
	result := ProbeResult{URL: url}

	// Try Ollama /api/tags.
	if models, err := probeOllamaTags(ctx, url+"/api/tags"); err == nil {
		result.Backend = "ollama"
		result.Models = extractOllamaModelNames(models)
		result.Error = nil
		return result
	}

	// Try LM Studio /api/v1/models.
	if models, err := probeLMStudioModels(ctx, url+"/api/v1/models"); err == nil {
		result.Backend = "lmstudio"
		result.Models = extractLMStudioModelNames(models)
		return result
	}

	// Try oMLX /health (engine_pool) or MTPLX /health (mtp_enabled + generation_mode).
	if health, err := probeHealth(ctx, url+"/health"); err == nil {
		if strings.Contains(health, "engine_pool") {
			result.Backend = "omlx"
			return result
		}
		if strings.Contains(health, "mtp_enabled") && strings.Contains(health, "generation_mode") {
			result.Backend = "mtplx"
			return result
		}
	}

	result.Error = fmt.Errorf("no known backend responded at %s", url)
	return result
}

func probeOllamaTags(ctx context.Context, url string) ([]byte, error) {
	return getJSON(ctx, url)
}

func probeLMStudioModels(ctx context.Context, url string) ([]byte, error) {
	return getJSON(ctx, url)
}

// extractOllamaModelNames parses the /api/tags response and returns model names.
func extractOllamaModelNames(data []byte) []string {
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names
}

// extractLMStudioModelNames parses the /api/v1/models response and returns model IDs.
func extractLMStudioModelNames(data []byte) []string {
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &models); err != nil {
		return nil
	}
	names := make([]string, 0, len(models.Data))
	for _, m := range models.Data {
		if m.ID != "" {
			names = append(names, m.ID)
		}
	}
	return names
}

func probeHealth(ctx context.Context, url string) (string, error) {
	data, err := getJSON(ctx, url)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func getJSON(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: ProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return ioReadAll(resp.Body)
}

// DiscoverLocal probes all local candidates concurrently and returns results.
func DiscoverLocal(ctx context.Context) []ProbeResult {
	cands := LocalCandidates()
	results := make([]ProbeResult, len(cands))
	var wg sync.WaitGroup

	for i, c := range cands {
		wg.Add(1)
		go func(idx int, cand Candidate) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
			defer cancel()
			results[idx] = Probe(probeCtx, cand.URL)
		}(i, c)
	}
	wg.Wait()
	return results
}

// ioReadAll reads all bytes from a reader.
func ioReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
