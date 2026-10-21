package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicModels are the current Claude models offered in the model list.
// Any other Claude model ID can still be named as "anthropic/<id>".
var AnthropicModels = []string{
	"claude-opus-5-5",
	"claude-fable-5-1",
	"claude-sonnet-5-5",
	"claude-haiku-5-5",
}

const (
	anthropicMaxTokens     = 64000
	anthropicContextWindow = 1000000
	anthropicFallbackBeta  = "server-side-fallback-2026-07-01"
	anthropicDefaultEffort = anthropic.OutputConfigEffortHigh
)

// anthropicFallbackModels accept server-side refusal fallback
// (`fallbacks: "default"`). Haiku has no fallback.
var anthropicFallbackModels = map[string]bool{
	"claude-fable-5-1":  true,
	"claude-opus-5-5":   true,
	"claude-opus-5":     true,
	"claude-sonnet-5-5": true,
}

// Anthropic streams chat completions from the Claude Messages API using the
// official SDK. With no configured keys it uses the SDK's own credential
// lookup: ANTHROPIC_API_KEY, then ANTHROPIC_AUTH_TOKEN, then the OAuth
// profile from `ant auth login`.
type Anthropic struct {
	baseURL string
	keys    *KeyPool
}

// NewAnthropic creates a Claude client rotating across keys. baseURL may be
// empty for the default endpoint.
func NewAnthropic(baseURL string, keys *KeyPool) *Anthropic {
	return &Anthropic{baseURL: baseURL, keys: keys}
}

func (a *Anthropic) client(key string) anthropic.Client {
	var opts []option.RequestOption
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if a.baseURL != "" {
		opts = append(opts, option.WithBaseURL(a.baseURL))
	}
	if a.keys.Len() > 1 {
		// Let a throttled key fail fast so the pool can rotate.
		opts = append(opts, option.WithMaxRetries(0))
	}
	return anthropic.NewClient(opts...)
}

// anthropicParams translates a backend-neutral request.
func anthropicParams(req ChatRequest) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: anthropicMaxTokens,
	}

	var system []string
	var toolResults []anthropic.ContentBlockParamUnion
	flushResults := func() {
		if len(toolResults) > 0 {
			// All results of one assistant turn go back in a single user
			// message, which keeps parallel tool calls working.
			params.Messages = append(params.Messages, anthropic.NewUserMessage(toolResults...))
			toolResults = nil
		}
	}
	for _, m := range req.Messages {
		if m.Role == "tool" {
			toolResults = append(toolResults, anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false))
			continue
		}
		flushResults()
		switch m.Role {
		case "system":
			system = append(system, m.Content)
		case "user":
			params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			if native, ok := m.Native.(anthropic.MessageParam); ok && m.NativeBackend == "anthropic" {
				params.Messages = append(params.Messages, native)
				continue
			}
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				args := tc.Arguments
				if args == nil {
					args = map[string]any{}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, args, tc.Name))
			}
			if len(blocks) > 0 {
				params.Messages = append(params.Messages, anthropic.NewAssistantMessage(blocks...))
			}
		}
	}
	flushResults()
	if len(system) > 0 {
		params.System = []anthropic.TextBlockParam{{Text: strings.Join(system, "\n\n")}}
	}

	for _, t := range req.Tools {
		schema, _ := t.Parameters.(map[string]any)
		tool := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: schema["properties"]},
			// Large inputs (write_file bodies) stream as generated; inputs are
			// validated before any tool runs.
			EagerInputStreaming: anthropic.Bool(true),
		}
		if required, ok := schema["required"].([]string); ok {
			tool.InputSchema.Required = required
		}
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &tool})
	}

	// Thinking stays at the model default (adaptive on current models);
	// effort is the depth control. Current models cannot turn thinking off,
	// so "off" maps to the lowest effort.
	switch req.Effort {
	case EffortOff, EffortLow:
		params.OutputConfig.Effort = anthropic.OutputConfigEffortLow
	case EffortMedium:
		params.OutputConfig.Effort = anthropic.OutputConfigEffortMedium
	case EffortHigh:
		params.OutputConfig.Effort = anthropic.OutputConfigEffortHigh
	default:
		params.OutputConfig.Effort = anthropicDefaultEffort
	}
	return params
}

// Chat streams a Claude response. Text deltas are sent as they arrive; the
// final Done result carries validated tool calls and the native message for
// replay. Key rotation happens only before the first event, so no output is
// ever duplicated.
func (a *Anthropic) Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error) {
	params := anthropicParams(req)
	var reqOpts []option.RequestOption
	if anthropicFallbackModels[req.Model] {
		reqOpts = append(reqOpts,
			option.WithHeader("anthropic-beta", anthropicFallbackBeta),
			option.WithJSONSet("fallbacks", "default"))
	}

	type opened struct {
		stream interface {
			Next() bool
			Current() anthropic.MessageStreamEventUnion
			Err() error
			Close() error
		}
		first anthropic.MessageStreamEventUnion
	}
	var open opened
	err := a.keys.Do(ctx, func(key string) error {
		client := a.client(key)
		s := client.Messages.NewStreaming(ctx, params, reqOpts...)
		if !s.Next() {
			err := s.Err()
			s.Close()
			if err == nil {
				err = ErrStreamCut
			}
			return anthropicError(err)
		}
		open = opened{stream: s, first: s.Current()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}

	results := make(chan ChatResult, 8)
	go func() {
		defer open.stream.Close()
		defer close(results)

		var msg anthropic.Message
		handle := func(ev anthropic.MessageStreamEventUnion) error {
			if err := msg.Accumulate(ev); err != nil {
				return err
			}
			if delta, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if text, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok && text.Text != "" {
					results <- ChatResult{Content: text.Text}
				}
			}
			return nil
		}
		if err := handle(open.first); err != nil {
			results <- ChatResult{Err: fmt.Errorf("anthropic stream: %v", err)}
			return
		}
		for open.stream.Next() {
			if err := handle(open.stream.Current()); err != nil {
				results <- ChatResult{Err: fmt.Errorf("anthropic stream: %v", err)}
				return
			}
		}
		if err := open.stream.Err(); err != nil {
			results <- ChatResult{Err: fmt.Errorf("anthropic stream: %w", anthropicError(err))}
			return
		}
		if msg.StopReason == "" {
			results <- ChatResult{Err: ErrStreamCut}
			return
		}
		results <- anthropicDone(msg)
	}()
	return results, nil
}

// anthropicDone builds the final result: usage, stop reason, tool calls
// (only when the model actually stopped to use tools), and the native
// message for replay.
func anthropicDone(msg anthropic.Message) ChatResult {
	done := ChatResult{
		Done:            true,
		DoneReason:      string(msg.StopReason),
		EvalCount:       int(msg.Usage.OutputTokens),
		PromptEvalCount: int(msg.Usage.InputTokens + msg.Usage.CacheReadInputTokens + msg.Usage.CacheCreationInputTokens),
		Native:          anthropicReplay(msg),
	}
	if msg.StopReason == anthropic.StopReasonRefusal {
		done.DoneReason = "refusal"
		if c := string(msg.StopDetails.Category); c != "" {
			done.DoneReason = "refusal: " + c
		}
		return done
	}
	for _, block := range msg.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		call := ToolCall{ID: tu.ID, Name: tu.Name}
		switch {
		case msg.StopReason == anthropic.StopReasonMaxTokens:
			call.ArgsError = "the reply hit the output limit before this call's arguments were complete"
		default:
			if err := parseToolArgs(string(tu.Input), &call); err != nil {
				call.ArgsError = err.Error()
			}
		}
		done.ToolCalls = append(done.ToolCalls, call)
	}
	return done
}

// anthropicReplay converts the response into the param that echoes it back
// unchanged, as preserved thinking requires. After a mid-stream server-side
// fallback, only text survives from before the last `fallback` block.
func anthropicReplay(msg anthropic.Message) anthropic.MessageParam {
	boundary := -1
	for i, block := range msg.Content {
		if block.Type == "fallback" {
			boundary = i
		}
	}
	p := anthropic.MessageParam{Role: anthropic.MessageParamRoleAssistant}
	for i, block := range msg.Content {
		if i < boundary && block.Type != "text" {
			continue
		}
		p.Content = append(p.Content, block.ToParam())
	}
	return p
}

// anthropicError turns an SDK API error into an *HTTPError so the key pool
// can classify it by status.
func anthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		body := apiErr.RawJSON()
		var parsed struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(body), &parsed) == nil && parsed.Error.Message != "" {
			body = parsed.Error.Message
		}
		return &HTTPError{StatusCode: apiErr.StatusCode, Body: body}
	}
	return err
}
