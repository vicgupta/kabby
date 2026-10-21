package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// drain collects a stream: concatenated text, tool calls from Done results,
// how many Done results arrived, and the first error.
func drain(t *testing.T, ch <-chan ChatResult) (text string, calls []ToolCall, dones int, last ChatResult, err error) {
	t.Helper()
	var sb strings.Builder
	for r := range ch {
		if r.Err != nil && err == nil {
			err = r.Err
		}
		sb.WriteString(r.Content)
		calls = append(calls, r.ToolCalls...)
		if r.Done {
			dones++
			last = r
		}
	}
	return sb.String(), calls, dones, last, err
}

func sse(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, l := range lines {
		fmt.Fprintf(w, "data: %s\n\n", l)
	}
}

func TestLMStudioStreamsTextAndAssemblesToolCalls(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		sse(w,
			`{"choices":[{"delta":{"content":"Let me look."}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"main.go\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":9}}`,
			`[DONE]`)
	}))
	defer srv.Close()

	ch, err := NewLMStudio(srv.URL, NewTransport()).Chat(context.Background(), ChatRequest{
		Model:    "qwen",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Tools:    []ToolSpec{{Name: "read_file", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, dones, last, err := drain(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("expected /v1/chat/completions, got %s", gotPath)
	}
	if tools, _ := gotBody["tools"].([]any); len(tools) != 1 {
		t.Fatalf("expected tools to be sent, body %v", gotBody)
	}
	if text != "Let me look." || dones != 1 {
		t.Fatalf("text %q dones %d", text, dones)
	}
	if len(calls) != 1 || calls[0].ID != "call_a" || calls[0].Arguments["path"] != "main.go" {
		t.Fatalf("unexpected calls %+v", calls)
	}
	if last.PromptEvalCount != 120 || last.EvalCount != 9 {
		t.Fatalf("usage not reported: %+v", last)
	}
}

func TestOpenAICompatCutStreamYieldsNoToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"write_file","arguments":"{\"path\":"}}]}}]}`)
	}))
	defer srv.Close()

	ch, err := NewLMStudio(srv.URL, NewTransport()).Chat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	_, calls, dones, _, err := drain(t, ch)
	if !errors.Is(err, ErrStreamCut) || len(calls) != 0 || dones != 0 {
		t.Fatalf("want ErrStreamCut and no calls, got err=%v calls=%d dones=%d", err, len(calls), dones)
	}
}

func TestOpenAICompatMalformedArgumentsAreFlagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"read_file","arguments":"{path: main.go"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`)
	}))
	defer srv.Close()

	ch, _ := NewLMStudio(srv.URL, NewTransport()).Chat(context.Background(), ChatRequest{Model: "m"})
	_, calls, _, _, err := drain(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].ArgsError == "" || calls[0].Arguments != nil {
		t.Fatalf("expected a flagged call, got %+v", calls)
	}
}

func TestOpenAIRotatesKeyOnRateLimit(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Header.Get("Authorization") == "Bearer key-1" {
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"message":"rate limit reached"}}`)
			return
		}
		sse(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `[DONE]`)
	}))
	defer srv.Close()

	pool := NewKeyPool("openai", []string{"key-1", "key-2"}, 0)
	ch, err := NewOpenAI(srv.URL, pool).Chat(context.Background(), ChatRequest{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	text, _, _, _, err := drain(t, ch)
	if err != nil || text != "ok" {
		t.Fatalf("text %q err %v", text, err)
	}
	if len(seen) != 2 || seen[1] != "Bearer key-2" {
		t.Fatalf("expected rotation to key-2, saw %v", seen)
	}

	// key-1 is cooling down, so the next request starts with key-2.
	seen = nil
	ch, _ = NewOpenAI(srv.URL, pool).Chat(context.Background(), ChatRequest{Model: "gpt"})
	drain(t, ch)
	if len(seen) != 1 || seen[0] != "Bearer key-2" {
		t.Fatalf("expected cooled key to be skipped, saw %v", seen)
	}
}

func TestKeyPoolDoesNotRotateOnBadRequest(t *testing.T) {
	pool := NewKeyPool("openai", []string{"a", "b"}, 0)
	calls := 0
	err := pool.Do(context.Background(), func(key string) error {
		calls++
		return &HTTPError{StatusCode: 400, Body: "messages: invalid"}
	})
	if err == nil || calls != 1 {
		t.Fatalf("a 400 must not rotate keys: calls=%d err=%v", calls, err)
	}
}

func TestClassifyKeyFault(t *testing.T) {
	cases := []struct {
		err  error
		want keyFault
	}{
		{&HTTPError{StatusCode: 429}, faultLimited},
		{&HTTPError{StatusCode: 529}, faultLimited},
		{&HTTPError{StatusCode: 401}, faultAuth},
		{&HTTPError{StatusCode: 402}, faultBilling},
		{&HTTPError{StatusCode: 400, Body: "prompt is too long"}, faultNone},
		{&HTTPError{StatusCode: 500}, faultNone},
		{errors.New("Overloaded"), faultLimited},
		{context.DeadlineExceeded, faultNone},
		{context.Canceled, faultNone},
	}
	for _, c := range cases {
		if got := classifyKeyFault(c.err); got != c.want {
			t.Errorf("classifyKeyFault(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestKeyPoolBenchesAndRecovers(t *testing.T) {
	now := time.Unix(0, 0)
	pool := NewKeyPool("x", []string{"a", "b"}, 0)
	pool.now = func() time.Time { return now }

	// a is throttled: the request moves to b, and a sits out.
	var tried []string
	pool.Do(context.Background(), func(k string) error {
		tried = append(tried, k)
		if k == "a" {
			return &HTTPError{StatusCode: 429}
		}
		return nil
	})
	tried = nil
	pool.Do(context.Background(), func(k string) error { tried = append(tried, k); return nil })
	if strings.Join(tried, ",") != "b" {
		t.Fatalf("a benched key must be skipped, tried %v", tried)
	}

	// After its bench time a is back in play.
	now = now.Add(31 * time.Second)
	tried = nil
	pool.Do(context.Background(), func(k string) error { tried = append(tried, k); return nil })
	pool.Do(context.Background(), func(k string) error { tried = append(tried, k); return nil })
	if !strings.Contains(strings.Join(tried, ","), "a") {
		t.Fatalf("a should be tried again after its bench, tried %v", tried)
	}

	if bench(faultLimited, 1) != 30*time.Second || bench(faultLimited, 20) != 30*time.Minute || bench(faultBilling, 1) != time.Hour {
		t.Fatal("unexpected bench schedule")
	}
}

func TestKeyPoolPacesRequestsPerKey(t *testing.T) {
	pool := NewKeyPool("x", []string{"a"}, 600) // one request per 100ms
	start := time.Now()
	for i := 0; i < 3; i++ {
		pool.Do(context.Background(), func(string) error { return nil })
	}
	if elapsed := time.Since(start); elapsed < 180*time.Millisecond {
		t.Fatalf("3 requests at 600 rpm should take >= 200ms, took %v", elapsed)
	}
}

// anthropicServer streams a canned Messages API response and records each
// request's headers and body.
func anthropicServer(t *testing.T, status func(key string) int, events []string) (*httptest.Server, *[]http.Header, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var headers []http.Header
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		headers = append(headers, r.Header.Clone())
		bodies = append(bodies, body)
		mu.Unlock()
		if code := status(r.Header.Get("X-Api-Key")); code != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			var probe struct {
				Type string `json:"type"`
			}
			json.Unmarshal([]byte(e), &probe)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", probe.Type, e)
		}
	}))
	return srv, &headers, &bodies
}

var toolUseEvents = []string{
	`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":50,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Reading "}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"it."}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"main.go\"}"}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`,
	`{"type":"message_stop"}`,
}

func TestAnthropicStreamsTextAndToolUse(t *testing.T) {
	srv, headers, bodies := anthropicServer(t, func(string) int { return 200 }, toolUseEvents)
	defer srv.Close()

	pool := NewKeyPool("anthropic", []string{"k1"}, 0)
	ch, err := NewAnthropic(srv.URL, pool).Chat(context.Background(), ChatRequest{
		Model: "claude-opus-5-5",
		Messages: []ChatMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "read main.go"},
		},
		Tools: []ToolSpec{{Name: "read_file", Description: "read", Parameters: map[string]any{
			"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, calls, dones, last, err := drain(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Reading it." || dones != 1 {
		t.Fatalf("text %q dones %d", text, dones)
	}
	if len(calls) != 1 || calls[0].ID != "toolu_1" || calls[0].Arguments["path"] != "main.go" {
		t.Fatalf("unexpected calls %+v", calls)
	}
	if _, ok := last.Native.(anthropic.MessageParam); !ok {
		t.Fatalf("expected a native message for replay, got %T", last.Native)
	}

	body := (*bodies)[0]
	if body["fallbacks"] != "default" || !strings.Contains((*headers)[0].Get("Anthropic-Beta"), anthropicFallbackBeta) {
		t.Fatalf("expected server-side fallback on claude-opus-5-5, body %v", body["fallbacks"])
	}
	if cfg, _ := body["output_config"].(map[string]any); cfg["effort"] != "high" {
		t.Fatalf("expected explicit effort, got %v", body["output_config"])
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	if tool["eager_input_streaming"] != true {
		t.Fatalf("expected eager_input_streaming on tools, got %v", tool)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("thinking must be left at the model default, got %v", body["thinking"])
	}
}

func TestAnthropicHaikuSendsNoFallback(t *testing.T) {
	srv, _, bodies := anthropicServer(t, func(string) int { return 200 }, toolUseEvents)
	defer srv.Close()
	ch, err := NewAnthropic(srv.URL, NewKeyPool("anthropic", []string{"k"}, 0)).Chat(context.Background(), ChatRequest{
		Model: "claude-haiku-5-5", Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ch)
	if _, ok := (*bodies)[0]["fallbacks"]; ok {
		t.Fatal("claude-haiku-5-5 has no server-side fallback")
	}
}

func TestAnthropicRotatesKeyOnRateLimit(t *testing.T) {
	srv, headers, _ := anthropicServer(t, func(key string) int {
		if key == "k1" {
			return 429
		}
		return 200
	}, toolUseEvents)
	defer srv.Close()

	pool := NewKeyPool("anthropic", []string{"k1", "k2"}, 0)
	ch, err := NewAnthropic(srv.URL, pool).Chat(context.Background(), ChatRequest{
		Model: "claude-opus-5-5", Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := drain(t, ch); err != nil {
		t.Fatal(err)
	}
	if len(*headers) != 2 || (*headers)[1].Get("X-Api-Key") != "k2" {
		t.Fatalf("expected a retry on k2, got %d requests", len(*headers))
	}
}

func TestAnthropicRefusalRunsNoTools(t *testing.T) {
	events := []string{
		toolUseEvents[0],
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"}},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	}
	srv, _, _ := anthropicServer(t, func(string) int { return 200 }, events)
	defer srv.Close()
	ch, _ := NewAnthropic(srv.URL, NewKeyPool("anthropic", []string{"k"}, 0)).Chat(context.Background(), ChatRequest{
		Model: "claude-opus-5-5", Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	_, calls, _, last, err := drain(t, ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || !strings.HasPrefix(last.DoneReason, "refusal") {
		t.Fatalf("refusal must carry no tool calls: reason %q calls %d", last.DoneReason, len(calls))
	}
}

func TestAnthropicGroupsToolResultsAndReplaysNative(t *testing.T) {
	native := anthropic.NewAssistantMessage(anthropic.NewTextBlock("from native"))
	params := anthropicParams(ChatRequest{
		Model: "claude-opus-5-5",
		Messages: []ChatMessage{
			{Role: "user", Content: "go"},
			{Role: "assistant", Native: native, NativeBackend: "anthropic", ToolCalls: []ToolCall{{ID: "a"}, {ID: "b"}}},
			{Role: "tool", ToolCallID: "a", Content: "ra"},
			{Role: "tool", ToolCallID: "b", Content: "rb"},
			{Role: "assistant", Content: "local reply", NativeBackend: "ollama"},
		},
	})
	if len(params.Messages) != 4 {
		t.Fatalf("expected user, assistant, one grouped tool-result user message, assistant; got %d", len(params.Messages))
	}
	if got := len(params.Messages[2].Content); got != 2 {
		t.Fatalf("parallel tool results must share one user message, got %d blocks", got)
	}
	if params.Messages[1].Content[0].OfText.Text != "from native" {
		t.Fatal("assistant turn from the same backend must be replayed unchanged")
	}
}

func TestAnthropicReplayDropsNonTextBeforeFallback(t *testing.T) {
	var msg anthropic.Message
	raw := `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1},"content":[
		{"type":"thinking","thinking":"","signature":"s1"},
		{"type":"text","text":"partial "},
		{"type":"tool_use","id":"t1","name":"read_file","input":{}},
		{"type":"fallback"},
		{"type":"thinking","thinking":"","signature":"s2"},
		{"type":"text","text":"rest"}]}`
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	p := anthropicReplay(msg)
	var kinds []string
	for _, b := range p.Content {
		switch {
		case b.OfText != nil:
			kinds = append(kinds, "text")
		case b.OfThinking != nil:
			kinds = append(kinds, "thinking")
		case b.OfToolUse != nil:
			kinds = append(kinds, "tool_use")
		default:
			kinds = append(kinds, "other")
		}
	}
	if got := strings.Join(kinds, ","); got != "text,other,thinking,text" {
		t.Fatalf("replay after fallback = %s, want text,other,thinking,text", got)
	}
}
