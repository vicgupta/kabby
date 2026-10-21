package detect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MTPLX identifies itself via /health returning JSON with both
// "mtp_enabled" and "generation_mode". It requires an API key.
const (
	MTPLXDefaultPort = 8000
)

// MTPLXSettings is the MTPLX settings file structure.
type MTPLXSettings struct {
	Port   int    `json:"port"`
	APIKey string `json:"api_key"`
	Host   string `json:"host"`
}

// MTPLXSettingsPath returns the path to MTPLX's settings file.
func MTPLXSettingsPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, "Library", "Application Support", "MTPLX", "settings.json")
}

// MTPLXPortAndKey reads the port and API key from MTPLX's settings file.
func MTPLXPortAndKey() (int, string, error) {
	settingsPath := MTPLXSettingsPath()
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return 0, "", fmt.Errorf("read mtplx settings: %v", err)
	}
	var settings MTPLXSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return 0, "", fmt.Errorf("decode mtplx settings: %v", err)
	}
	if settings.Port == 0 {
		settings.Port = MTPLXDefaultPort
	}
	key := settings.APIKey
	if key == "" {
		key = os.Getenv("MTPLX_API_KEY")
	}
	return settings.Port, key, nil
}

// IsMTPLXHealth reports whether a /health response body identifies MTPLX.
func IsMTPLXHealth(body string) bool {
	return strings.Contains(body, "mtp_enabled") && strings.Contains(body, "generation_mode")
}

var _ = fmt.Sprintf
