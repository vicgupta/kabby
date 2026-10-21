package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kabby/internal/config"
	ctxmgr "kabby/internal/context"
	"kabby/internal/plan"
	"kabby/internal/providers"
	"kabby/internal/tools"
)

// TurnStats captures per-turn statistics for the [stats] line.
type TurnStats struct {
	ToolsCalled  int
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	TokensPerSec float64
	DurationMs   int
	Verified     bool
}

// VerificationResult tracks the outcome of an acceptance gate check.
type VerificationResult struct {
	Command            string
	Passed             bool
	Attempts           int
	LastError          string
	EquivalentFailures int
}

// ChatClient is the interface the agent needs to talk to a model backend.
type ChatClient interface {
	Chat(ctx context.Context, req ChatRequest) (<-chan ChatResult, error)
}

// ChatRequest is a generic chat request sent to the backend.
type ChatRequest struct {
	Model         string
	Messages      []ChatMessage
	Tools         []providers.ToolSpec
	ContextWindow int
	Effort        string
}

// ChatMessage is a generic chat message.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatResult is a generic streaming result from the backend.
type ChatResult struct {
	Content    string
	ToolCalls  []ToolCall
	Done       bool
	DoneReason string
	Err        error
}

// ToolCall is a generic tool call.
type ToolCall struct {
	Name      string
	Arguments map[string]any
}

// Agent is the core read → edit → run → verify loop.
type Agent struct {
	Client     ChatClient
	CtxManager *ctxmgr.Manager
	Plan       *plan.Plan
	ToolsCtx   *tools.ToolContext
	Config     config.Config
	VerifyCmd  string
	MaxVerify  int

	nonPlanCalls   int
	LastStats      TurnStats
	LastResult     VerificationResult
	verifyAttempts int
}

// NewAgent creates an Agent with the given dependencies.
func NewAgent(client ChatClient, ctxManager *ctxmgr.Manager, p *plan.Plan, tc *tools.ToolContext, cfg config.Config) *Agent {
	return &Agent{
		Client:     client,
		CtxManager: ctxManager,
		Plan:       p,
		ToolsCtx:   tc,
		Config:     cfg,
		MaxVerify:  6,
	}
}

// RunTurn executes one full turn: system + history → model → execute tools → repeat.
func (a *Agent) RunTurn(ctx context.Context, systemPrompt string, userPrompt string) (string, error) {
	start := time.Now()
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	var finalAnswer string
	for {
		// Check context budget before calling the model.
		if err := a.CtxManager.ImpossibleWindow(512); err != nil {
			return "", err
		}

		// Build tool specs based on current mode.
		mode := providers.Mode(a.Config.LastMode)
		if mode == "" {
			mode = providers.ModeEdit
		}
		toolSpecs := tools.BuildToolSpecs(mode)

		// Call the model.
		req := ChatRequest{
			Model:         a.Config.LastModel,
			Messages:      messages,
			Tools:         toolSpecs,
			ContextWindow: a.CtxManager.Budget().Window,
			Effort:        "default",
		}
		results, err := a.Client.Chat(ctx, req)
		if err != nil {
			return "", fmt.Errorf("chat: %v", err)
		}

		// Process streaming results.
		var toolCalls []ToolCall
		var content strings.Builder
		for result := range results {
			if result.Err != nil {
				return "", result.Err
			}
			content.WriteString(result.Content)
			toolCalls = append(toolCalls, result.ToolCalls...)
			if result.Done {
				break
			}
		}

		// If no tool calls, this is the final answer.
		if len(toolCalls) == 0 {
			finalAnswer = content.String()
			break
		}

		// Execute tool calls.
		for _, tc := range toolCalls {
			result, err := tools.ExecuteTool(tc.Name, tc.Arguments, a.ToolsCtx, ctx.Done())
			if err != nil {
				// Feed the error back as a tool result.
				messages = append(messages, ChatMessage{Role: "assistant", Content: ""})
				messages = append(messages, ChatMessage{Role: "tool", Content: fmt.Sprintf("error: %v", err)})
				continue
			}
			messages = append(messages, ChatMessage{Role: "assistant", Content: ""})
			messages = append(messages, ChatMessage{Role: "tool", Content: result})
		}
	}

	// Record stats.
	duration := time.Since(start)
	a.LastStats = TurnStats{
		DurationMs: int(duration.Milliseconds()),
		Verified:   a.LastResult.Passed,
	}

	return finalAnswer, nil
}
