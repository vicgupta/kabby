package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// oMLX identifies itself via /health returning JSON with "engine_pool".
// It requires an API key. Port and key come from its settings file on this
// computer; OMLX_API_KEY is a fallback.
const (
	OMLXDefaultPort = 8000
)

// OMLXSettings is the oMLX settings file structure.
type OMLXSettings struct {
	Port   int    `json:"port"`
	APIKey string `json:"api_key"`
	Host   string `json:"host"`
}

// OMLXSettingsPath returns the path to oMLX's settings file.
func OMLXSettingsPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, "Library", "Application Support", "oMLX", "settings.json")
}

// OMLXPortAndKey reads the port and API key from oMLX's settings file.
func OMLXPortAndKey() (int, string, error) {
	settingsPath := OMLXSettingsPath()
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return 0, "", fmt.Errorf("read omlx settings: %v", err)
	}
	var settings OMLXSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return 0, "", fmt.Errorf("decode omlx settings: %v", err)
	}
	if settings.Port == 0 {
		settings.Port = OMLXDefaultPort
	}
	key := settings.APIKey
	if key == "" {
		key = os.Getenv("OMLX_API_KEY")
	}
	return settings.Port, key, nil
}

// ProbeOMLX probes an oMLX server at the given baseURL with the given API key.
func ProbeOMLX(ctx context.Context, baseURL, apiKey string) (*ProbeResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: ProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := ioReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if strings.Contains(string(data), "engine_pool") {
		return &ProbeResult{URL: baseURL, Backend: "omlx"}, nil
	}
	return nil, fmt.Errorf("not an oMLX server: %s", string(data))
}
