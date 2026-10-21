package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// Paths.
const (
	CONFIG_PATH = "~/.kabby.json"
	DATA_DIR    = "~/.kabby"
)

// Config is the persistent configuration stored in ~/.kabby.json.
// Mode and Effort are stored as strings; they are converted to
// providers.Mode / providers.Effort at the boundary.
type Config struct {
	LastModel    string            `json:"lastModel,omitempty"`
	LastModelURL string            `json:"lastModelUrl,omitempty"`
	LastMode     string            `json:"lastMode,omitempty"`
	Effort       string            `json:"effort,omitempty"`
	Hosts        []SavedHost       `json:"hosts,omitempty"`
	Keys         map[string]string `json:"keys,omitempty"`

	// ProviderKeys holds API keys for hosted providers ("anthropic",
	// "openai"). Several keys per provider rotate on rate-limit, billing
	// and auth errors.
	ProviderKeys map[string][]string `json:"providerKeys,omitempty"`
	// RPM caps requests per minute per key for a hosted provider.
	RPM map[string]int `json:"rpm,omitempty"`
}

// SavedHost is a remote machine saved in ~/.kabby.json.
type SavedHost struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

var (
	// configPathOverride is set by tests via KABBY_CONFIG.
	configPathOverride string
)

// SetConfigPath overrides the config path (for tests via KABBY_CONFIG).
func SetConfigPath(path string) {
	configPathOverride = path
}

func configPath() string {
	if configPathOverride != "" {
		return configPathOverride
	}
	return expandHome(CONFIG_PATH)
}

func dataDir() string {
	return expandHome(DATA_DIR)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home + p[1:]
	}
	return p
}

// LoadConfig reads ~/.kabby.json. A saved bypass/yolo mode is silently coerced
// to edit. Missing file returns zero config (no error).
func LoadConfig() (Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	// Coerce saved bypass/yolo back to edit (never persisted).
	if cfg.LastMode == "bypass" || cfg.LastMode == "yolo" {
		cfg.LastMode = "edit"
	}
	return cfg, nil
}

// SaveConfig writes ~/.kabby.json with mode 0600. Ensures the data directory
// exists and chmods before the first key write.
func SaveConfig(cfg Config) error {
	dir := dataDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	path := configPath()
	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			return err
		}
	} else {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// UpdateConfig merges a patch into the config by re-reading from disk first
// (web sessions share the same file).
func UpdateConfig(patch func(Config) Config) (Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return Config{}, err
	}
	cfg = patch(cfg)
	if err := SaveConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// SavedKey returns the stored API key for a given base URL, or empty if none.
func SavedKey(base string) (string, bool) {
	cfg, err := LoadConfig()
	if err != nil {
		return "", false
	}
	key, ok := cfg.Keys[base]
	return key, ok
}

// SetKeys stores API keys for the given base URLs.
func SetKeys(bases []string, key string) error {
	_, err := UpdateConfig(func(cfg Config) Config {
		if cfg.Keys == nil {
			cfg.Keys = make(map[string]string)
		}
		for _, b := range bases {
			cfg.Keys[b] = key
		}
		return cfg
	})
	return err
}
