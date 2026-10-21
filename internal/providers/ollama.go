package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Ollama is a client for the Ollama API.
type Ollama struct {
	baseURL   string
	transport *Transport
	client    *http.Client
}

// NewOllama creates an Ollama client for the given base URL.
func NewOllama(baseURL string, transport *Transport) *Ollama {
	return &Ollama{
		baseURL:   strings.TrimRight(baseURL, "/"),
		transport: transport,
		client:    &http.Client{Timeout: TotalDeadline},
	}
}

// tagsResponse is the /api/tags response.
type tagsResponse struct {
	Models []ollamaModel `json:"models"`
}

type ollamaModel struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
	ModifiedAt string `json:"modified_at"`
}

// ListModels calls /api/tags and returns discovered models.
func (o *Ollama) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.transport.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %v", err)
	}
	defer resp.Body.Close()

	var tags tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("ollama decode tags: %v", err)
	}

	models := make([]ModelInfo, 0, len(tags.Models))
	for _, m := range tags.Models {
		models = append(models, ModelInfo{
			ID:             m.Name,
			Name:           m.Name,
			Backend:        "ollama",
			URL:            o.baseURL,
			ToolsSupported: true, // assume tool support; refined at chat time
			ContextWindow:  4096, // default; refined via /api/ps
		})
	}
	return models, nil
}

// chatRequest is the /api/chat request body.
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []chatMsg `json:"messages"`
	Tools    []toolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
	Options  chatOpts  `json:"options"`
}

type chatMsg struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

type toolDef struct {
	Type     string  `json:"type"`
	Function funcDef `json:"function"`
}

type funcDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type chatOpts struct {
	NumCtx      int     `json:"num_ctx,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
	// NoThink maps to effort=off for Ollama.
	NoThink bool `json:"-"` // handled via options map below
}

// chatResponse is one line of the /api/chat NDJSON stream.
type chatResponse struct {
	Model           string  `json:"model"`
	CreatedAt       string  `json:"created_at"`
	Message         chatMsg `json:"message"`
	Done            bool    `json:"done"`
	DoneReason      string  `json:"done_reason"`
	TotalDuration   int64   `json:"total_duration"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
}

// Chat streams a chat completion. Each line of the NDJSON stream is parsed
// into a ChatResult sent to the results channel.
func (o *Ollama) Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error) {
	payload := chatRequest{
		Model:  req.Model,
		Stream: true,
		Options: chatOpts{
			NumCtx:      req.ContextWindow,
			Temperature: 0.7,
			TopP:        0.9,
		},
	}
	if req.Effort == EffortOff {
		payload.Options.NoThink = true
	}

	for _, m := range req.Messages {
		msg := chatMsg{Role: m.Role, Content: m.Content}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, toolCall{Function: toolCallFunc{Name: tc.Name, Arguments: tc.Arguments}})
		}
		payload.Messages = append(payload.Messages, msg)
	}

	for _, t := range req.Tools {
		payload.Tools = append(payload.Tools, toolDef{
			Type: "function",
			Function: funcDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ollama marshal chat request: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/chat", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.transport.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama chat: %v", err)
	}

	results := make(chan ChatResult, 8)
	go func() {
		defer resp.Body.Close()
		defer close(results)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 4*1024*1024) // 4MB max line

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				results <- ChatResult{Err: ctx.Err()}
				return
			default:
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			var cr chatResponse
			if err := json.Unmarshal([]byte(line), &cr); err != nil {
				results <- ChatResult{Err: fmt.Errorf("ollama decode stream line: %v", err)}
				continue
			}

			result := ChatResult{
				Content:    cr.Message.Content,
				Done:       cr.Done,
				DoneReason: cr.DoneReason,
			}
			for _, tc := range cr.Message.ToolCalls {
				args := map[string]any{}
				if m, ok := tc.Function.Arguments.(map[string]any); ok {
					args = m
				}
				result.ToolCalls = append(result.ToolCalls, ToolCall{
					Name:      tc.Function.Name,
					Arguments: args,
				})
			}
			if cr.EvalCount > 0 {
				result.EvalCount = cr.EvalCount
			}
			if cr.PromptEvalCount > 0 {
				result.PromptEvalCount = cr.PromptEvalCount
			}

			results <- result
			if cr.Done {
				return
			}
		}
	}()

	return results, nil
}

// ProcessList calls /api/ps to get loaded models and context info.
func (o *Ollama) ProcessList(ctx context.Context) ([]ollamaProcess, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/ps", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.transport.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama ps: %v", err)
	}
	defer resp.Body.Close()

	var ps struct {
		Models []ollamaProcess `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		return nil, fmt.Errorf("ollama decode ps: %v", err)
	}
	return ps.Models, nil
}

type ollamaProcess struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	NumCtx int    `json:"num_ctx"`
}

// ChatRequest is a generic chat request.
type ChatRequest struct {
	Model         string
	Messages      []ChatMessage
	Tools         []ToolSpec
	ContextWindow int
	Effort        Effort
}

// ChatMessage is a generic chat message.
type ChatMessage struct {
	Role      string
	Content   string
	ToolCalls []ToolCall
}

// ToolCall is a generic tool call.
type ToolCall struct {
	Name      string
	Arguments map[string]any
}

// ChatResult is a generic chat result.
type ChatResult struct {
	Content         string
	ToolCalls       []ToolCall
	Done            bool
	DoneReason      string
	EvalCount       int
	PromptEvalCount int
	Err             error
}

type toolCallFunc struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

type toolCall struct {
	Function toolCallFunc `json:"function"`
}
