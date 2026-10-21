package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"kabby/internal/config"
	"kabby/internal/providers"
)

// Hosted providers. Their models are listed only when credentials exist and
// are never picked automatically: a request goes to a hosted API only when
// the user names one of its models (--model, /model) or chose it last time.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

var cloudBaseURL = map[string]string{
	ProviderAnthropic: "https://api.anthropic.com",
	ProviderOpenAI:    "https://api.openai.com/v1",
}

// cloudKeys returns the configured keys for provider: providerKeys in
// ~/.kabby.json first, then the provider's environment variable. An empty
// list for Anthropic means the SDK's own credential lookup is used.
func cloudKeys(provider string, cfg config.Config) []string {
	var keys []string
	for _, k := range cfg.ProviderKeys[provider] {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 && provider == ProviderOpenAI {
		if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// HasCloudCredentials reports whether provider has a key, token, or (for
// Anthropic) an `ant auth login` OAuth profile on this machine.
func HasCloudCredentials(provider string, cfg config.Config) bool {
	if len(cloudKeys(provider, cfg)) > 0 {
		return true
	}
	if provider != ProviderAnthropic {
		return false
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	dir := filepath.Join(home, ".config", "anthropic")
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dir = filepath.Join(xdg, "anthropic")
	}
	_, err = os.Stat(dir)
	return err == nil
}

// CloudModels lists hosted models for providers with credentials. OpenAI
// models are not listed (that would need a network call); name one as
// "openai/<model>".
func CloudModels(cfg config.Config) []providers.ModelInfo {
	if !HasCloudCredentials(ProviderAnthropic, cfg) {
		return nil
	}
	var out []providers.ModelInfo
	for _, id := range providers.AnthropicModels {
		out = append(out, CloudModel(ProviderAnthropic, id))
	}
	return out
}

// CloudModel describes a hosted model named by ID.
func CloudModel(provider, id string) providers.ModelInfo {
	window := 128000
	if provider == ProviderAnthropic {
		window = 1000000
	}
	return providers.ModelInfo{
		ID:             id,
		Name:           provider + "/" + id,
		Backend:        provider,
		URL:            cloudBaseURL[provider],
		ToolsSupported: true,
		ContextWindow:  window,
		Cloud:          true,
	}
}

// parseCloudRef splits "anthropic/<id>" or "openai/<id>".
func parseCloudRef(name string) (provider, id string, ok bool) {
	provider, id, found := strings.Cut(name, "/")
	if !found || id == "" {
		return "", "", false
	}
	provider = strings.ToLower(provider)
	if _, known := cloudBaseURL[provider]; !known {
		return "", "", false
	}
	return provider, id, true
}

var (
	poolsMu sync.Mutex
	pools   = map[string]*providers.KeyPool{}
)

// keyPool returns the process-wide key pool for provider, so cooldowns
// survive model switches.
func keyPool(provider string, cfg config.Config) *providers.KeyPool {
	poolsMu.Lock()
	defer poolsMu.Unlock()
	if p, ok := pools[provider]; ok {
		return p
	}
	p := providers.NewKeyPool(provider, cloudKeys(provider, cfg), cfg.RPM[provider])
	pools[provider] = p
	return p
}

// NewClient builds the chat client for a chosen model, local or hosted.
func NewClient(m providers.ModelInfo, cfg config.Config) providers.ChatClient {
	switch m.Backend {
	case ProviderAnthropic:
		// Empty base URL lets the SDK honor ANTHROPIC_BASE_URL.
		return providers.NewAnthropic("", keyPool(ProviderAnthropic, cfg))
	case ProviderOpenAI:
		return providers.NewOpenAI(os.Getenv("OPENAI_BASE_URL"), keyPool(ProviderOpenAI, cfg))
	case "lmstudio":
		return providers.NewLMStudio(m.URL, providers.NewTransport())
	default:
		return providers.NewOllama(m.URL, providers.NewTransport())
	}
}
