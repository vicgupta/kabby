package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// OpenAICompat is a streaming client for OpenAI-compatible
// /chat/completions endpoints: LM Studio locally, OpenAI as a hosted API.
type OpenAICompat struct {
	backend   string // "lmstudio" | "openai"
	baseURL   string // including the version prefix, e.g. http://127.0.0.1:1234/v1
	transport *Transport
	keys      *KeyPool // nil for local servers that need no key
	// temperature is sent when non-nil. Hosted reasoning models reject
	// non-default values, so OpenAI leaves it unset.
	temperature *float64
}

// NewOpenAI creates a client for the OpenAI API at baseURL
// (default https://api.openai.com/v1) rotating across keys.
func NewOpenAI(baseURL string, keys *KeyPool) *OpenAICompat {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAICompat{
		backend:   "openai",
		baseURL:   strings.TrimRight(baseURL, "/"),
		transport: NewTransport(),
		keys:      keys,
	}
}

type oaiToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiMsg struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiReq struct {
	Model           string     `json:"model"`
	Messages        []oaiMsg   `json:"messages"`
	Tools           []toolDef  `json:"tools,omitempty"`
	Stream          bool       `json:"stream"`
	StreamOptions   *oaiStream `json:"stream_options,omitempty"`
	Temperature     *float64   `json:"temperature,omitempty"`
	ReasoningEffort string     `json:"reasoning_effort,omitempty"`
}

type oaiStream struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaiChunk struct {
	Choices []struct {
		Delta struct {
			Content   string        `json:"content"`
			ToolCalls []oaiToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenAICompat) buildRequest(req ChatRequest) oaiReq {
	payload := oaiReq{
		Model:         req.Model,
		Stream:        true,
		StreamOptions: &oaiStream{IncludeUsage: true},
		Temperature:   c.temperature,
	}
	switch {
	case req.Effort == EffortOff && c.backend == "lmstudio":
		payload.ReasoningEffort = "none"
	case req.Effort == EffortOff:
		payload.ReasoningEffort = "low"
	case req.Effort != EffortDefault && req.Effort != "default":
		payload.ReasoningEffort = string(req.Effort)
	}
	for _, m := range req.Messages {
		msg := oaiMsg{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			args, _ := json.Marshal(tc.Arguments)
			call := oaiToolCall{ID: tc.ID, Type: "function"}
			call.Function.Name = tc.Name
			call.Function.Arguments = string(args)
			msg.ToolCalls = append(msg.ToolCalls, call)
		}
		payload.Messages = append(payload.Messages, msg)
	}
	for _, t := range req.Tools {
		payload.Tools = append(payload.Tools, toolDef{
			Type:     "function",
			Function: funcDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}
	return payload
}

// post sends the request and returns the open response, or an *HTTPError
// for a non-2xx status so the key pool can classify it.
func (c *OpenAICompat) post(ctx context.Context, body []byte, key string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.transport.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(drainBody(resp.Body)))}
	}
	return resp, nil
}

// Chat streams a chat completion. Text deltas are sent as they arrive; tool
// calls are assembled from their fragments and sent only on the final Done
// result, after the server reported a finish reason.
func (c *OpenAICompat) Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, fmt.Errorf("%s marshal request: %v", c.backend, err)
	}

	var resp *http.Response
	if c.keys == nil {
		resp, err = c.post(ctx, body, "")
	} else {
		err = c.keys.Do(ctx, func(key string) error {
			if key == "" {
				return fmt.Errorf("no %s API key configured", c.backend)
			}
			r, perr := c.post(ctx, body, key)
			resp = r
			return perr
		})
	}
	if err != nil {
		return nil, fmt.Errorf("%s chat: %w", c.backend, err)
	}

	results := make(chan ChatResult, 8)
	go func() {
		defer resp.Body.Close()
		defer close(results)

		calls := map[int]*oaiToolCall{}
		var finish string
		var usage ChatResult
		sse := NewSSEReader(ctx, resp.Body)
		for {
			data, ok := sse.NextData()
			if !ok || data == "[DONE]" {
				break
			}
			var chunk oaiChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				results <- ChatResult{Err: fmt.Errorf("%s decode chunk: %v", c.backend, err)}
				return
			}
			if chunk.Error != nil {
				results <- ChatResult{Err: fmt.Errorf("%s: %s", c.backend, chunk.Error.Message)}
				return
			}
			if chunk.Usage != nil {
				usage.PromptEvalCount = chunk.Usage.PromptTokens
				usage.EvalCount = chunk.Usage.CompletionTokens
			}
			for _, ch := range chunk.Choices {
				if ch.Delta.Content != "" {
					results <- ChatResult{Content: ch.Delta.Content}
				}
				for _, frag := range ch.Delta.ToolCalls {
					tc := calls[frag.Index]
					if tc == nil {
						tc = &oaiToolCall{Index: frag.Index}
						calls[frag.Index] = tc
					}
					if frag.ID != "" {
						tc.ID = frag.ID
					}
					if frag.Function.Name != "" {
						tc.Function.Name = frag.Function.Name
					}
					tc.Function.Arguments += frag.Function.Arguments
				}
				if ch.FinishReason != nil && *ch.FinishReason != "" {
					finish = *ch.FinishReason
				}
			}
		}
		if ctx.Err() != nil {
			results <- ChatResult{Err: ctx.Err()}
			return
		}
		if finish == "" {
			results <- ChatResult{Err: ErrStreamCut}
			return
		}

		done := ChatResult{Done: true, DoneReason: finish, EvalCount: usage.EvalCount, PromptEvalCount: usage.PromptEvalCount}
		indexes := make([]int, 0, len(calls))
		for i := range calls {
			indexes = append(indexes, i)
		}
		sort.Ints(indexes)
		for n, i := range indexes {
			tc := calls[i]
			call := ToolCall{ID: tc.ID, Name: tc.Function.Name}
			if call.ID == "" {
				call.ID = fmt.Sprintf("call_%d", n+1)
			}
			if err := parseToolArgs(tc.Function.Arguments, &call); err != nil {
				call.ArgsError = err.Error()
			}
			done.ToolCalls = append(done.ToolCalls, call)
		}
		results <- done
	}()
	return results, nil
}

// parseToolArgs decodes a tool call's JSON arguments. Empty arguments mean
// no parameters.
func parseToolArgs(raw string, call *ToolCall) error {
	call.Arguments = map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &call.Arguments); err != nil {
		call.Arguments = nil
		return fmt.Errorf("arguments are not valid JSON (%v)", err)
	}
	return nil
}
