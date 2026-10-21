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

// PrepareModel selects a backend and model by partial name from the discovered
// backends. Returns the chosen model info or an error if no suitable backend
// is found.
func PrepareModel(prefs SessionPrefs, cfg config.Config, backends []providers.ModelInfo) (*providers.ModelInfo, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("%s", NoBackendsMessage())
	}

	// If a model was requested by partial name, find it.
	if prefs.Model != "" {
		for _, b := range backends {
			if strings.Contains(strings.ToLower(b.Name), strings.ToLower(prefs.Model)) {
				return &b, nil
			}
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

	// Default: pick the first tool-capable backend, else the first available.
	for _, b := range backends {
		if b.ToolsSupported {
			return &b, nil
		}
	}
	return &backends[0], nil
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
