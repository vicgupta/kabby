package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissingFile(t *testing.T) {
	SetConfigPath(filepath.Join(t.TempDir(), "nonexistent.json"))
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if cfg.LastMode != "" {
		t.Fatalf("expected empty mode, got %s", cfg.LastMode)
	}
}

func TestLoadConfigBypassCoercedToEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kabby.json")
	if err := os.WriteFile(path, []byte(`{"lastMode":"bypass"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	SetConfigPath(path)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastMode != "edit" {
		t.Fatalf("expected bypass coerced to edit, got %s", cfg.LastMode)
	}
}

func TestLoadConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kabby.json")
	SetConfigPath(path)

	cfg := Config{
		LastModel:    "llama3",
		LastModelURL: "http://127.0.0.1:11434",
		LastMode:     "edit",
		Hosts:        []SavedHost{{Address: "192.168.1.50", Name: "office"}},
		Keys:         map[string]string{"http://192.168.1.50": "secret-key"},
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastModel != "llama3" {
		t.Fatalf("expected LastModel llama3, got %s", loaded.LastModel)
	}
	if loaded.LastModelURL != "http://127.0.0.1:11434" {
		t.Fatalf("expected LastModelURL, got %s", loaded.LastModelURL)
	}
	if loaded.LastMode != "edit" {
		t.Fatalf("expected ModeEdit, got %s", loaded.LastMode)
	}
	if len(loaded.Hosts) != 1 || loaded.Hosts[0].Address != "192.168.1.50" {
		t.Fatalf("unexpected hosts: %+v", loaded.Hosts)
	}
	if loaded.Keys["http://192.168.1.50"] != "secret-key" {
		t.Fatalf("expected key preserved")
	}
}

func TestConfigFileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kabby.json")
	SetConfigPath(path)

	cfg := Config{Keys: map[string]string{"http://example.com": "k"}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Fatalf("expected config mode 0600, got %o", mode)
	}
}

func TestUpdateConfigMergesByReReading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kabby.json")
	SetConfigPath(path)

	// Write initial config.
	initial := Config{LastModel: "model-a"}
	if err := SaveConfig(initial); err != nil {
		t.Fatal(err)
	}

	// Update only the mode to bypass.
	_, err := UpdateConfig(func(cfg Config) Config {
		cfg.LastMode = "bypass"
		return cfg
	})
	if err != nil {
		t.Fatal(err)
	}

	// Loading again should coerce bypass back to edit.
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastModel != "model-a" {
		t.Fatalf("expected LastModel preserved through merge, got %s", loaded.LastModel)
	}
	if loaded.LastMode != "edit" {
		t.Fatalf("expected bypass coerced to edit on load, got %s", loaded.LastMode)
	}
}

func TestSavedKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kabby.json")
	SetConfigPath(path)

	if key, _ := SavedKey("http://example.com"); key != "" {
		t.Fatalf("expected no key initially, got %s", key)
	}

	if err := SetKeys([]string{"http://example.com"}, "my-key"); err != nil {
		t.Fatal(err)
	}

	key, ok := SavedKey("http://example.com")
	if !ok || key != "my-key" {
		t.Fatalf("expected key my-key, got %q ok=%v", key, ok)
	}
}

func TestExpandHome(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	result := expandHome("~/.kabby.json")
	expected := home + "/.kabby.json"
	if result != expected {
		t.Fatalf("expected %s, got %s", expected, result)
	}
}
