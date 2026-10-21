package session

import (
	"fmt"
	"slices"
	"strings"

	"kabby/internal/config"
	"kabby/internal/providers"
)

// SessionPrefs holds user-selected session preferences.
type SessionPrefs struct {
	Mode     providers.Mode
	Provider string
	Model    string
	CTX      int
	Effort   *providers.Effort
}

// localProviders are the local server backends --provider accepts; the
// hosted ones are the keys of cloudBaseURL.
var localProviders = []string{"ollama", "lmstudio", "omlx", "mtplx"}

// filterProvider keeps the backends served by provider, rejecting names
// that are neither a local server nor a hosted API.
func filterProvider(provider string, backends []providers.ModelInfo) ([]providers.ModelInfo, error) {
	_, cloud := cloudBaseURL[provider]
	if !cloud && !slices.Contains(localProviders, provider) {
		return nil, fmt.Errorf("unknown provider %q (want %s, %s, or %s)",
			provider, strings.Join(localProviders, ", "), ProviderAnthropic, ProviderOpenAI)
	}
	var out []providers.ModelInfo
	for _, b := range backends {
		if b.Backend == provider {
			out = append(out, b)
		}
	}
	return out, nil
}

// PrepareModel selects a model from the discovered backends: by partial
// name when one is requested, else the remembered model, else the first
// tool-capable local model. Hosted (Cloud) models are chosen only by name
// or by memory, never as the default. A name like "anthropic/<id>" or
// "openai/<id>" selects that hosted model even if it is not listed.
//
// A provider limits every step to that backend. Naming a hosted provider is
// an explicit choice of it, so then a bare model ID resolves as
// "<provider>/<id>", and with no model Anthropic's default model is used.
func PrepareModel(prefs SessionPrefs, cfg config.Config, backends []providers.ModelInfo) (*providers.ModelInfo, error) {
	provider := strings.ToLower(strings.TrimSpace(prefs.Provider))
	_, cloudProvider := cloudBaseURL[provider]
	if provider != "" {
		var err error
		if backends, err = filterProvider(provider, backends); err != nil {
			return nil, err
		}
	}

	if prefs.Model != "" {
		want := strings.ToLower(prefs.Model)
		for _, b := range backends {
			if strings.EqualFold(b.Name, prefs.Model) || strings.EqualFold(b.ID, prefs.Model) {
				return &b, nil
			}
		}
		for _, b := range backends {
			if strings.Contains(strings.ToLower(b.Name), want) {
				return &b, nil
			}
		}
		if p, id, ok := parseCloudRef(prefs.Model); ok {
			if provider != "" && p != provider {
				return nil, fmt.Errorf("model %q is not from provider %q", prefs.Model, provider)
			}
			m := CloudModel(p, id)
			return &m, nil
		}
		if cloudProvider {
			m := CloudModel(provider, prefs.Model)
			return &m, nil
		}
		if provider != "" {
			return nil, fmt.Errorf("model %q not found among %d %s model(s)", prefs.Model, len(backends), provider)
		}
		return nil, fmt.Errorf("model %q not found among %d discovered model(s)", prefs.Model, len(backends))
	}

	// Use the last model if it matches a discovered backend (same model on
	// two machines stays apart via lastModelUrl).
	if cfg.LastModel != "" {
		for _, b := range backends {
			if b.ID == cfg.LastModel && b.URL == cfg.LastModelURL {
				return &b, nil
			}
		}
	}

	switch provider {
	case "":
	case ProviderAnthropic:
		m := CloudModel(provider, providers.AnthropicModels[0])
		return &m, nil
	case ProviderOpenAI:
		return nil, fmt.Errorf("provider openai needs a model, e.g. --model gpt-5")
	default:
		if len(backends) == 0 {
			return nil, fmt.Errorf("no %s models found; is the %s server running?", provider, provider)
		}
	}

	// Default: the first tool-capable local model, else the first local one.
	var local []providers.ModelInfo
	for _, b := range backends {
		if !b.Cloud {
			local = append(local, b)
		}
	}
	if len(local) == 0 {
		msg := NoBackendsMessage()
		if len(backends) > 0 {
			msg += fmt.Sprintf(" Or pick a hosted model, e.g. --model %s.", backends[0].Name)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	for _, b := range local {
		if b.ToolsSupported {
			return &b, nil
		}
	}
	return &local[0], nil
}

// EffortAdvice returns a human-readable note about the chosen effort level.
func EffortAdvice(chosen, setting providers.Effort) string {
	if chosen == "" || chosen == providers.EffortDefault {
		return ""
	}
	return fmt.Sprintf("effort: %s", chosen)
}

// NoBackendsMessage returns the message when no model server is discovered.
func NoBackendsMessage() string {
	return "no local model server found. Start Ollama, LM Studio, oMLX, or MTPLX, " +
		"or use /models to add a remote machine."
}

// SessionLine formats the session status line.
func SessionLine(chosen providers.ModelInfo, mode providers.Mode) string {
	return fmt.Sprintf("%s · %s · %s", chosen.Backend, chosen.ID, mode)
}
