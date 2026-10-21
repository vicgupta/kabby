package session

import (
	"fmt"
	"strings"

	"kabby/internal/config"
	"kabby/internal/providers"
)

// SessionPrefs holds user-selected session preferences.
type SessionPrefs struct {
	Mode   providers.Mode
	Model  string
	CTX    int
	Effort *providers.Effort
}

// PrepareModel selects a model from the discovered backends: by partial
// name when one is requested, else the remembered model, else the first
// tool-capable local model. Hosted (Cloud) models are chosen only by name
// or by memory, never as the default. A name like "anthropic/<id>" or
// "openai/<id>" selects that hosted model even if it is not listed.
func PrepareModel(prefs SessionPrefs, cfg config.Config, backends []providers.ModelInfo) (*providers.ModelInfo, error) {
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
		if provider, id, ok := parseCloudRef(prefs.Model); ok {
			m := CloudModel(provider, id)
			return &m, nil
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
