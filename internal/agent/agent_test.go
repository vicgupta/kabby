package agent

import (
	"context"
	"testing"

	"kabby/internal/config"
	ctxmgr "kabby/internal/context"
	"kabby/internal/plan"
	"kabby/internal/tools"
)

// stubClient returns canned responses without calling a real model.
// On each call it returns the next response in the list.
type stubClient struct {
	responses [][]ChatResult // responses per call
	callIndex int
}

func (s *stubClient) Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error) {
	idx := s.callIndex
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	s.callIndex++

	resps := s.responses[idx]
	out := make(chan ChatResult, len(resps)+1)
	for _, r := range resps {
		out <- r
	}
	out <- ChatResult{Done: true}
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
	a.Client.(*stubClient).responses = [][]ChatResult{
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
	a.Client.(*stubClient).responses = [][]ChatResult{
		{
			{
				Content: "",
				ToolCalls: []ToolCall{
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
