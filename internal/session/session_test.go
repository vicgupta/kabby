package session

import (
	"strings"
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

func TestPrepareModelNeverAutoPicksCloud(t *testing.T) {
	cloud := CloudModel(ProviderAnthropic, "claude-opus-5-5")
	local := providers.ModelInfo{ID: "qwen", Name: "qwen", Backend: "ollama", URL: "http://a", ToolsSupported: true}

	got, err := PrepareModel(SessionPrefs{}, config.Config{}, []providers.ModelInfo{cloud, local})
	if err != nil || got.ID != "qwen" {
		t.Fatalf("default must be the local model, got %v, %v", got, err)
	}
	if _, err := PrepareModel(SessionPrefs{}, config.Config{}, []providers.ModelInfo{cloud}); err == nil ||
		!strings.Contains(err.Error(), "--model anthropic/claude-opus-5-5") {
		t.Fatalf("with only hosted models, expect a hint to name one, got %v", err)
	}
	got, err = PrepareModel(SessionPrefs{Model: "opus"}, config.Config{}, []providers.ModelInfo{local, cloud})
	if err != nil || !got.Cloud {
		t.Fatalf("a hosted model is chosen when named, got %v, %v", got, err)
	}
}

func TestPrepareModelAcceptsProviderRefs(t *testing.T) {
	got, err := PrepareModel(SessionPrefs{Model: "openai/gpt-x"}, config.Config{}, nil)
	if err != nil || got.Backend != ProviderOpenAI || got.ID != "gpt-x" || !got.Cloud {
		t.Fatalf("openai/<id> should resolve without a listing, got %+v, %v", got, err)
	}
	if _, err := PrepareModel(SessionPrefs{Model: "mystery/x"}, config.Config{}, nil); err == nil {
		t.Fatal("unknown provider prefixes must not resolve")
	}
}

func TestPrepareModelProvider(t *testing.T) {
	ollama := providers.ModelInfo{ID: "qwen", Name: "qwen", Backend: "ollama", URL: "http://a", ToolsSupported: true}
	lms := providers.ModelInfo{ID: "qwen-lms", Name: "qwen-lms", Backend: "lmstudio", URL: "http://b", ToolsSupported: true}
	cloud := CloudModel(ProviderAnthropic, "claude-opus-5-5")
	all := []providers.ModelInfo{ollama, lms, cloud}

	got, err := PrepareModel(SessionPrefs{Provider: "LMStudio"}, config.Config{}, all)
	if err != nil || got.ID != "qwen-lms" {
		t.Fatalf("provider should pick its first model, got %v, %v", got, err)
	}
	got, err = PrepareModel(SessionPrefs{Provider: "lmstudio", Model: "qwen"}, config.Config{}, all)
	if err != nil || got.ID != "qwen-lms" {
		t.Fatalf("model search should stay inside the provider, got %v, %v", got, err)
	}
	got, err = PrepareModel(SessionPrefs{Provider: "lmstudio"}, config.Config{LastModel: "qwen", LastModelURL: "http://a"}, all)
	if err != nil || got.ID != "qwen-lms" {
		t.Fatalf("a remembered model from another provider must be ignored, got %v, %v", got, err)
	}
	if _, err := PrepareModel(SessionPrefs{Provider: "omlx"}, config.Config{}, all); err == nil ||
		!strings.Contains(err.Error(), "no omlx models") {
		t.Fatalf("a provider with no models should say so, got %v", err)
	}
	if _, err := PrepareModel(SessionPrefs{Provider: "mystery"}, config.Config{}, all); err == nil ||
		!strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("unknown providers must be rejected, got %v", err)
	}
}

func TestPrepareModelCloudProvider(t *testing.T) {
	local := providers.ModelInfo{ID: "qwen", Name: "qwen", Backend: "ollama", URL: "http://a", ToolsSupported: true}

	got, err := PrepareModel(SessionPrefs{Provider: "anthropic"}, config.Config{}, []providers.ModelInfo{local})
	if err != nil || got.Backend != ProviderAnthropic || got.ID != providers.AnthropicModels[0] {
		t.Fatalf("anthropic with no model should use the default Claude model, got %v, %v", got, err)
	}
	got, err = PrepareModel(SessionPrefs{Provider: "anthropic", Model: "claude-haiku-5-5"}, config.Config{}, nil)
	if err != nil || got.Backend != ProviderAnthropic || got.ID != "claude-haiku-5-5" {
		t.Fatalf("a bare ID should resolve under a hosted provider, got %v, %v", got, err)
	}
	got, err = PrepareModel(SessionPrefs{Provider: "openai", Model: "gpt-x"}, config.Config{}, nil)
	if err != nil || got.Backend != ProviderOpenAI || got.ID != "gpt-x" {
		t.Fatalf("openai with a model should resolve, got %v, %v", got, err)
	}
	if _, err := PrepareModel(SessionPrefs{Provider: "openai"}, config.Config{}, nil); err == nil {
		t.Fatal("openai without a model must ask for one")
	}
	if _, err := PrepareModel(SessionPrefs{Provider: "openai", Model: "anthropic/claude-opus-5-5"}, config.Config{}, nil); err == nil {
		t.Fatal("a model ref from another provider must be rejected")
	}
}

func TestCloudKeysPreferConfigOverEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "from-env")
	if got := cloudKeys(ProviderOpenAI, config.Config{}); len(got) != 1 || got[0] != "from-env" {
		t.Fatalf("env key expected, got %v", got)
	}
	cfg := config.Config{ProviderKeys: map[string][]string{ProviderOpenAI: {"k1", " ", "k2"}}}
	if got := cloudKeys(ProviderOpenAI, cfg); strings.Join(got, ",") != "k1,k2" {
		t.Fatalf("configured keys expected, got %v", got)
	}
}

func TestCloudModelsNeedCredentials(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := CloudModels(config.Config{}); len(got) != 0 {
		t.Fatalf("no credentials, no hosted models; got %d", len(got))
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	if got := CloudModels(config.Config{}); len(got) == 0 || !got[0].Cloud {
		t.Fatalf("expected hosted models with a key, got %v", got)
	}
}
