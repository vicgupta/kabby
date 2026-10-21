// Package providers defines the backend abstraction: types, transport, and
// per-backend clients (Ollama, LM Studio, oMLX, MTPLX).
package providers

// Mode is the permission mode for tool access.
type Mode string

const (
	ModeReadOnly Mode = "ro"
	ModeEdit     Mode = "edit"
	ModeBypass   Mode = "bypass"
)

// Effort is the reasoning effort setting. Empty string means default.
type Effort string

const (
	EffortOff     Effort = "off"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortDefault Effort = ""
)

// ToolSpec describes a single tool exposed to the model.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // flat schema (no nested objects/arrays)
}

// ModelInfo describes a discovered model.
type ModelInfo struct {
	ID              string
	Name            string
	Backend         string // ollama | lmstudio | omlx | mtplx
	URL             string
	ToolsSupported  bool
	ContextWindow   int
	SupportedEffort []Effort
	DefaultEffort   Effort
}
