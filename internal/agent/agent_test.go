package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kabby/internal/config"
	ctxmgr "kabby/internal/context"
	"kabby/internal/plan"
	"kabby/internal/providers"
	"kabby/internal/tools"
)

// stubClient returns canned responses without calling a real model.
// On each call it returns the next response in the list.
type stubClient struct {
	responses [][]providers.ChatResult // responses per call
	callIndex int
}

func (s *stubClient) Chat(ctx context.Context, req providers.ChatRequest) (<-chan providers.ChatResult, error) {
	idx := s.callIndex
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	s.callIndex++

	resps := s.responses[idx]
	out := make(chan providers.ChatResult, len(resps)+1)
	for _, r := range resps {
		out <- r
	}
	out <- providers.ChatResult{Done: true}
	close(out)
	return out, nil
}

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	tc := &tools.ToolContext{Workspace: t.TempDir(), ResultCharLimit: 10000}
	return NewAgent(
		&stubClient{},
		ctxmgr.NewManager(16384),
		plan.New(),
		tc,
		config.Config{LastMode: "edit"},
	)
}

func TestAgentRunTurnNoTools(t *testing.T) {
	a := newTestAgent(t)
	a.Client.(*stubClient).responses = [][]providers.ChatResult{
		{{Content: "Hello, world!"}},
	}
	answer, err := a.RunTurn(context.Background(), "You are a helper.", "Say hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "Hello, world!" {
		t.Fatalf("expected 'Hello, world!', got %q", answer)
	}
}

func TestAgentRunTurnWithTools(t *testing.T) {
	a := newTestAgent(t)
	a.Client.(*stubClient).responses = [][]providers.ChatResult{
		{
			{
				Content: "",
				ToolCalls: []providers.ToolCall{
					{Name: "read_file", Arguments: map[string]any{"path": "README.md"}},
				},
			},
		},
		{
			{Content: "I read the file."},
		},
	}
	answer, err := a.RunTurn(context.Background(), "You are a helper.", "Read the README")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stringsContains(answer, "I read the file") {
		t.Fatalf("expected final answer, got %q", answer)
	}
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// recordingClient replays scripted responses and records every request.
type recordingClient struct {
	responses [][]providers.ChatResult
	requests  []providers.ChatRequest
	noDone    bool // end streams without a Done result (a cut-off stream)
}

func (c *recordingClient) Chat(ctx context.Context, req providers.ChatRequest) (<-chan providers.ChatResult, error) {
	c.requests = append(c.requests, req)
	idx := len(c.requests) - 1
	if idx >= len(c.responses) {
		idx = len(c.responses) - 1
	}
	out := make(chan providers.ChatResult, len(c.responses[idx])+1)
	for _, r := range c.responses[idx] {
		out <- r
	}
	if !c.noDone {
		out <- providers.ChatResult{Done: true}
	}
	close(out)
	return out, nil
}

func newRecordingAgent(t *testing.T, mode string, c *recordingClient) *Agent {
	t.Helper()
	tc := &tools.ToolContext{Workspace: t.TempDir(), ResultCharLimit: 10000}
	return NewAgent(c, ctxmgr.NewManager(16384), plan.New(), tc, config.Config{LastMode: mode})
}

func call(id, name string, args map[string]any) providers.ChatResult {
	return providers.ChatResult{ToolCalls: []providers.ToolCall{{ID: id, Name: name, Arguments: args}}}
}

func TestToolResultsCarryCallIDsAndHistoryPersists(t *testing.T) {
	c := &recordingClient{responses: [][]providers.ChatResult{
		{call("c1", "list_files", map[string]any{})},
		{{Content: "done"}},
		{{Content: "second answer"}},
	}}
	a := newRecordingAgent(t, "edit", c)
	if _, err := a.RunTurn(context.Background(), "sys", "first"); err != nil {
		t.Fatal(err)
	}
	second := c.requests[1].Messages
	tool := second[len(second)-1]
	if tool.Role != "tool" || tool.ToolCallID != "c1" || tool.ToolName != "list_files" {
		t.Fatalf("tool result must reference its call, got %+v", tool)
	}
	if asst := second[len(second)-2]; asst.Role != "assistant" || len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant message must keep its tool calls, got %+v", asst)
	}

	if _, err := a.RunTurn(context.Background(), "sys", "second"); err != nil {
		t.Fatal(err)
	}
	third := c.requests[2].Messages
	if third[1].Content != "first" || third[len(third)-1].Content != "second" {
		t.Fatalf("history must persist across turns, got %d messages", len(third))
	}
}

func TestCutStreamExecutesNothing(t *testing.T) {
	dir := t.TempDir()
	c := &recordingClient{noDone: true, responses: [][]providers.ChatResult{
		{call("c1", "write_file", map[string]any{"path": "x.txt", "content": "hi"})},
	}}
	a := newRecordingAgent(t, "bypass", c)
	a.ToolsCtx.Workspace = dir
	_, err := a.RunTurn(context.Background(), "sys", "write")
	if !errors.Is(err, providers.ErrStreamCut) {
		t.Fatalf("expected ErrStreamCut, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "x.txt")); statErr == nil {
		t.Fatal("a tool call from a cut-off stream must not run")
	}
}

func TestReadOnlyModeRefusesWriteTools(t *testing.T) {
	dir := t.TempDir()
	c := &recordingClient{responses: [][]providers.ChatResult{
		{call("c1", "write_file", map[string]any{"path": "x.txt", "content": "hi"})},
		{{Content: "ok"}},
	}}
	a := newRecordingAgent(t, "ro", c)
	a.ToolsCtx.Workspace = dir
	if _, err := a.RunTurn(context.Background(), "sys", "write"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("ro mode must not write files")
	}
	msgs := c.requests[1].Messages
	if !strings.Contains(msgs[len(msgs)-1].Content, "read-only") {
		t.Fatalf("expected a read-only refusal, got %q", msgs[len(msgs)-1].Content)
	}
	if n := len(c.requests[0].Tools); n != 4 {
		t.Fatalf("ro mode should declare 4 tools to local models, got %d", n)
	}
}

func TestMalformedAndInvalidCallsAreNotRun(t *testing.T) {
	c := &recordingClient{responses: [][]providers.ChatResult{
		{{ToolCalls: []providers.ToolCall{
			{ID: "a", Name: "read_file", ArgsError: "arguments are not valid JSON"},
			{ID: "b", Name: "read_file", Arguments: map[string]any{}},
		}}},
		{{Content: "ok"}},
	}}
	a := newRecordingAgent(t, "edit", c)
	if _, err := a.RunTurn(context.Background(), "sys", "read"); err != nil {
		t.Fatal(err)
	}
	msgs := c.requests[1].Messages
	bad, missing := msgs[len(msgs)-2].Content, msgs[len(msgs)-1].Content
	if !strings.Contains(bad, "not valid JSON") || !strings.Contains(missing, `needs "path"`) {
		t.Fatalf("unexpected tool errors: %q / %q", bad, missing)
	}
}

func TestRefusalStopsTheTurn(t *testing.T) {
	c := &recordingClient{noDone: true, responses: [][]providers.ChatResult{{
		{Content: "Partial"},
		{Done: true, DoneReason: "refusal: cyber", ToolCalls: []providers.ToolCall{{ID: "x", Name: "list_files"}}},
	}}}
	a := newRecordingAgent(t, "edit", c)
	answer, err := a.RunTurn(context.Background(), "sys", "q")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "declined") || a.LastStats.ToolsCalled != 0 || len(c.requests) != 1 {
		t.Fatalf("refusal must end the turn without tools: answer %q", answer)
	}
}

func TestStablePrefixKeepsToolsAndSystemAndAnnouncesMode(t *testing.T) {
	c := &recordingClient{responses: [][]providers.ChatResult{{{Content: "a"}}}}
	a := newRecordingAgent(t, "ro", c)
	a.StablePrefix = true
	a.RunTurn(context.Background(), "sys-1", "one")
	a.Config.LastMode = "edit"
	a.RunTurn(context.Background(), "sys-2", "two")

	first, second := c.requests[0], c.requests[1]
	if len(first.Tools) != 8 || len(second.Tools) != 8 {
		t.Fatalf("stable prefix declares all 8 tools in every mode, got %d and %d", len(first.Tools), len(second.Tools))
	}
	if second.Messages[0].Content != "sys-1" {
		t.Fatalf("system prompt must not change mid-conversation, got %q", second.Messages[0].Content)
	}
	if !strings.Contains(first.Messages[1].Content, "now ro") {
		t.Fatalf("a non-edit starting mode must be announced, got %q", first.Messages[1].Content)
	}
	last := second.Messages[len(second.Messages)-1].Content
	if !strings.Contains(last, "now edit") || !strings.HasSuffix(last, "two") {
		t.Fatalf("mode change must be announced in the new user message, got %q", last)
	}
	if second.Messages[1].Content != first.Messages[1].Content {
		t.Fatal("earlier messages must not be edited")
	}
}

func TestCancelGivesEveryCallAResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &recordingClient{responses: [][]providers.ChatResult{{{ToolCalls: []providers.ToolCall{
		{ID: "a", Name: "list_files", Arguments: map[string]any{}},
		{ID: "b", Name: "list_files", Arguments: map[string]any{}},
	}}}}}
	a := newRecordingAgent(t, "edit", c)
	a.Observer.OnToolCall = func(string, map[string]any) { cancel() }
	if _, err := a.RunTurn(ctx, "sys", "go"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	var results []string
	for _, m := range a.History {
		if m.Role == "tool" {
			results = append(results, m.ToolCallID)
		}
	}
	if strings.Join(results, ",") != "a,b" {
		t.Fatalf("every tool call needs a result after cancel, got %v", results)
	}
}
