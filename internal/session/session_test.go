package session

import (
	"testing"

	"kabby/internal/config"
	"kabby/internal/providers"
)

func TestPrepareModelByPartialName(t *testing.T) {
	backends := []providers.ModelInfo{
		{ID: "llama3.2:3b", Name: "llama3.2:3b", Backend: "ollama"},
		{ID: "qwen2.5-coder:3b", Name: "qwen2.5-coder:3b", Backend: "lmstudio"},
	}
	chosen, err := PrepareModel(SessionPrefs{Model: "qwen"}, config.Config{}, backends)
	if err != nil {
		t.Fatal(err)
	}
	if chosen.ID != "qwen2.5-coder:3b" {
		t.Fatalf("expected qwen2.5-coder:3b, got %s", chosen.ID)
	}
}

func TestPrepareModelNotFound(t *testing.T) {
	backends := []providers.ModelInfo{
		{ID: "llama3.2:3b", Name: "llama3.2:3b", Backend: "ollama"},
	}
	_, err := PrepareModel(SessionPrefs{Model: "nonexistent"}, config.Config{}, backends)
	if err == nil {
		t.Fatal("expected error for not-found model")
	}
}

func TestPrepareModelLastModelMatch(t *testing.T) {
	backends := []providers.ModelInfo{
		{ID: "llama3.2:3b", Name: "llama3.2:3b", Backend: "ollama", URL: "http://127.0.0.1:11434"},
	}
	chosen, err := PrepareModel(SessionPrefs{}, config.Config{
		LastModel:    "llama3.2:3b",
		LastModelURL: "http://127.0.0.1:11434",
	}, backends)
	if err != nil {
		t.Fatal(err)
	}
	if chosen.ID != "llama3.2:3b" {
		t.Fatalf("expected llama3.2:3b, got %s", chosen.ID)
	}
}

func TestPrepareModelLastModelDifferentURL(t *testing.T) {
	// Same model ID on a different URL should NOT match, falls back to first backend.
	backends := []providers.ModelInfo{
		{ID: "llama3.2:3b", Name: "llama3.2:3b", Backend: "ollama", URL: "http://127.0.0.1:11434"},
		{ID: "other-model", Name: "other-model", Backend: "ollama", URL: "http://127.0.0.1:11434"},
	}
	chosen, err := PrepareModel(SessionPrefs{}, config.Config{
		LastModel:    "llama3.2:3b",
		LastModelURL: "http://remote:11434",
	}, backends)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chosen.ID != "llama3.2:3b" {
		t.Fatalf("expected fallback to llama3.2:3b, got %s", chosen.ID)
	}
}

func TestPrepareModelNoBackends(t *testing.T) {
	_, err := PrepareModel(SessionPrefs{}, config.Config{}, nil)
	if err == nil {
		t.Fatal("expected error for no backends")
	}
}

func TestNoBackendsMessage(t *testing.T) {
	msg := NoBackendsMessage()
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestSessionLine(t *testing.T) {
	line := SessionLine(providers.ModelInfo{
		Backend: "ollama",
		ID:      "llama3.2:3b",
	}, providers.ModeEdit)
	if !stringsContains(line, "ollama") || !stringsContains(line, "llama3.2:3b") || !stringsContains(line, "edit") {
		t.Fatalf("unexpected session line: %s", line)
	}
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
