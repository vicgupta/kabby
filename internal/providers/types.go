// Package providers defines the backend abstraction: types, transport, and
// per-backend clients (Ollama, LM Studio, oMLX, MTPLX).
package providers

import "context"

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

// DefaultContextWindow is the window assumed for a local model when the
// backend does not report one and no --ctx is given.
const DefaultContextWindow = 64 * 1024

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
	Cloud           bool // hosted API (Anthropic, OpenAI); never auto-selected
}

// ChatClient is implemented by every backend: local servers and hosted APIs.
type ChatClient interface {
	Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error)
}

// ChatRequest is a backend-neutral chat request.
type ChatRequest struct {
	Model         string
	Messages      []ChatMessage
	Tools         []ToolSpec
	ContextWindow int
	Effort        Effort
}

// ChatMessage is a backend-neutral chat message. Tool results use Role
// "tool" with ToolCallID and ToolName set.
type ChatMessage struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string

	// Native is the backend's own form of an assistant message (for example
	// the Anthropic content blocks, including thinking), replayed unchanged
	// when the same backend serves the next request. NativeBackend names the
	// backend that produced it.
	Native        any
	NativeBackend string
}

// ToolCall is a backend-neutral tool call.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	// ArgsError is set when the model's arguments could not be decoded.
	// Such a call is never executed; the error goes back to the model.
	ArgsError string
}

// ChatResult is one item of a streamed chat response. Content carries text
// deltas as they arrive. Tool calls are only safe to execute once a result
// with Done set has been received: a stream that ends without Done was cut
// off and its tool calls must be discarded.
type ChatResult struct {
	Content         string
	ToolCalls       []ToolCall
	Done            bool
	DoneReason      string
	EvalCount       int // output tokens
	PromptEvalCount int // input tokens
	Native          any // see ChatMessage.Native; set on the Done result
	Err             error
}
