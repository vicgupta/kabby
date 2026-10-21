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
	"kabby/internal/sandbox"
	"kabby/internal/tools"
	"kabby/internal/util"
)

// TurnStats captures per-turn statistics for the [stats] line.
type TurnStats struct {
	ToolsCalled  int     `json:"toolsCalled"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	TokensPerSec float64 `json:"tokensPerSec"`
	DurationMs   int     `json:"durationMs"`
	Verified     bool    `json:"verified"`
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
type ChatClient = providers.ChatClient

// Observer receives a turn's progress for display. Every field is optional.
type Observer struct {
	OnText       func(text string)                      // streamed reply text
	OnToolCall   func(name string, args map[string]any) // before a tool runs
	OnToolResult func(name, result string, failed bool) // after it ran
	Approve      func(command, reason string) bool      // edit-mode reach-out; nil denies
	OnNotice     func(text string)                      // harness notices (refusals etc.)
}

const (
	// defaultMaxSteps bounds model calls per turn so a looping model stops.
	defaultMaxSteps = 40
	// planReminderAfter non-plan tool calls with unfinished plan steps, a
	// short reminder rides back on the tool result.
	planReminderAfter = 4
)

// Agent is the core read → edit → run → verify loop. History persists across
// turns, so one Agent is one conversation.
type Agent struct {
	Client     ChatClient
	CtxManager *ctxmgr.Manager
	Plan       *plan.Plan
	ToolsCtx   *tools.ToolContext
	Config     config.Config
	VerifyCmd  string
	MaxVerify  int

	Model    string           // model ID sent to the backend; defaults to Config.LastModel
	Backend  string           // backend name, tags native messages for replay
	Effort   providers.Effort // reasoning effort; "" is the backend default
	ChatOnly bool             // model has no tool support: send no tools
	// StablePrefix keeps the tool list and system prompt identical for the
	// whole conversation (all eight tools are always declared; mode is
	// enforced when a tool runs) and announces mode changes in the next user
	// message instead of editing history. Hosted Claude models require an
	// unchanged prefix to keep earlier thinking valid.
	StablePrefix bool
	MaxSteps     int
	Observer     Observer

	History []providers.ChatMessage

	lastMode       providers.Mode
	nonPlanCalls   int
	LastStats      TurnStats
	LastResult     VerificationResult
	verifyAttempts int
}

// NewAgent creates an Agent with the given dependencies. The plan p is the
// one the plan tool edits.
func NewAgent(client ChatClient, ctxManager *ctxmgr.Manager, p *plan.Plan, tc *tools.ToolContext, cfg config.Config) *Agent {
	if tc.Plan == nil {
		tc.Plan = p
	}
	return &Agent{
		Client:     client,
		CtxManager: ctxManager,
		Plan:       tc.Plan,
		ToolsCtx:   tc,
		Config:     cfg,
		MaxVerify:  6,
	}
}

// Reset clears the conversation.
func (a *Agent) Reset() {
	a.History = nil
	a.lastMode = ""
	a.nonPlanCalls = 0
}

func (a *Agent) mode() providers.Mode {
	if m := providers.Mode(a.Config.LastMode); m != "" {
		return m
	}
	return providers.ModeEdit
}

// RunTurn executes one full turn: the user prompt goes into the
// conversation, then model calls and tool executions alternate until the
// model answers without calling tools.
func (a *Agent) RunTurn(ctx context.Context, systemPrompt string, userPrompt string) (string, error) {
	start := time.Now()
	a.LastStats = TurnStats{}
	mode := a.mode()

	first := len(a.History) == 0
	switch {
	case first:
		a.History = append(a.History, providers.ChatMessage{Role: "system", Content: systemPrompt})
	case !a.StablePrefix:
		a.History[0].Content = systemPrompt
	}
	// With a stable prefix the system prompt is written for edit mode, so
	// any other mode, or a later change, is announced in the user message.
	if a.StablePrefix && ((first && mode != providers.ModeEdit) || (!first && mode != a.lastMode)) {
		userPrompt = modeNotice(mode) + "\n\n" + userPrompt
	}
	a.lastMode = mode
	a.History = append(a.History, providers.ChatMessage{Role: "user", Content: userPrompt})

	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	model := a.Model
	if model == "" {
		model = a.Config.LastModel
	}

	var finalAnswer string
	for step := 0; ; step++ {
		if step >= maxSteps {
			return "", fmt.Errorf("stopped after %d model calls without a final answer", maxSteps)
		}
		if err := a.CtxManager.ImpossibleWindow(512); err != nil {
			return "", err
		}

		var toolSpecs []providers.ToolSpec
		switch {
		case a.ChatOnly:
		case a.StablePrefix:
			toolSpecs = tools.BuildToolSpecs(providers.ModeBypass)
		default:
			toolSpecs = tools.BuildToolSpecs(mode)
		}

		results, err := a.Client.Chat(ctx, providers.ChatRequest{
			Model:         model,
			Messages:      a.History,
			Tools:         toolSpecs,
			ContextWindow: a.CtxManager.Budget().Window,
			Effort:        a.Effort,
		})
		if err != nil {
			return "", fmt.Errorf("chat: %w", err)
		}

		reply, err := a.collect(results)
		if err != nil {
			return "", err
		}
		a.History = append(a.History, providers.ChatMessage{
			Role:          "assistant",
			Content:       reply.content,
			ToolCalls:     reply.toolCalls,
			Native:        reply.native,
			NativeBackend: a.Backend,
		})

		if strings.HasPrefix(reply.doneReason, "refusal") {
			notice := "The model declined this request"
			if _, category, ok := strings.Cut(reply.doneReason, ": "); ok {
				notice += " (" + category + ")"
			}
			notice += "; any partial reply above is incomplete."
			if a.Observer.OnNotice != nil {
				a.Observer.OnNotice(notice)
			}
			finalAnswer = strings.TrimSpace(reply.content + "\n\n" + notice)
			break
		}
		if len(reply.toolCalls) == 0 {
			finalAnswer = reply.content
			break
		}

		// Every tool call gets a result, even after cancellation: a call
		// left without one makes the next request invalid.
		for _, tc := range reply.toolCalls {
			var result string
			if ctx.Err() != nil {
				result = "error: cancelled by the user before this call ran"
			} else {
				result = a.execTool(ctx, tc, mode)
			}
			a.History = append(a.History, providers.ChatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}

	duration := time.Since(start)
	a.LastStats.DurationMs = int(duration.Milliseconds())
	a.LastStats.TotalTokens = a.LastStats.InputTokens + a.LastStats.OutputTokens
	if secs := duration.Seconds(); secs > 0 {
		a.LastStats.TokensPerSec = float64(a.LastStats.OutputTokens) / secs
	}
	a.LastStats.Verified = a.LastResult.Passed
	return finalAnswer, nil
}

type reply struct {
	content    string
	toolCalls  []providers.ToolCall
	doneReason string
	native     any
}

// collect drains one streamed response. Tool calls count only if the stream
// completed: a response cut off mid-stream is an error, never a partial
// execution.
func (a *Agent) collect(results <-chan providers.ChatResult) (reply, error) {
	var r reply
	var content strings.Builder
	done := false
	for result := range results {
		if result.Err != nil {
			return r, result.Err
		}
		if result.Content != "" {
			content.WriteString(result.Content)
			if a.Observer.OnText != nil {
				a.Observer.OnText(result.Content)
			}
		}
		r.toolCalls = append(r.toolCalls, result.ToolCalls...)
		if result.PromptEvalCount > 0 {
			a.LastStats.InputTokens = result.PromptEvalCount
			a.CtxManager.UpdateUsage(ctxmgr.UsageSnapshot{InputTokens: result.PromptEvalCount})
		}
		a.LastStats.OutputTokens += result.EvalCount
		if result.Done {
			done = true
			r.doneReason = result.DoneReason
			r.native = result.Native
			break
		}
	}
	if !done {
		return r, providers.ErrStreamCut
	}
	r.content = content.String()
	return r, nil
}

// execTool runs one tool call and returns the text that goes back to the
// model. Failures are returned as text too, worded so the model can fix the
// call.
func (a *Agent) execTool(ctx context.Context, tc providers.ToolCall, mode providers.Mode) string {
	a.LastStats.ToolsCalled++
	if a.Observer.OnToolCall != nil {
		a.Observer.OnToolCall(tc.Name, tc.Arguments)
	}
	result, failed := a.runTool(ctx, tc, mode)
	if a.Observer.OnToolResult != nil {
		a.Observer.OnToolResult(tc.Name, result, failed)
	}

	if tc.Name == "plan" {
		a.nonPlanCalls = 0
	} else {
		a.nonPlanCalls++
		if a.nonPlanCalls >= planReminderAfter && a.Plan != nil && a.Plan.Exists() && a.Plan.PendingSummary() != "no pending steps" {
			result += "\n\n[plan] " + a.Plan.PendingSummary() + `. Mark progress with plan(action="done").`
			a.nonPlanCalls = 0
		}
	}
	return result
}

func (a *Agent) runTool(ctx context.Context, tc providers.ToolCall, mode providers.Mode) (string, bool) {
	if tc.ArgsError != "" {
		return fmt.Sprintf("error: %s was not run: %s. Call it again with complete JSON arguments.", tc.Name, tc.ArgsError), true
	}
	if msg := tools.ValidateArgs(tc.Name, tc.Arguments); msg != "" {
		return "error: " + msg, true
	}
	if mode == providers.ModeReadOnly && tools.IsWriteTool(tc.Name) {
		return fmt.Sprintf("error: %s is not available in read-only (ro) mode. Answer from what you can read, or ask the user to switch to edit mode.", tc.Name), true
	}
	if cmd := tools.CommandOf(tc.Name, tc.Arguments); cmd != nil && mode == providers.ModeEdit {
		if reaches, reason := sandbox.CommandReachesOut(*cmd); reaches {
			if a.Observer.Approve == nil || !a.Observer.Approve(*cmd, reason) {
				return fmt.Sprintf("error: not run, the user did not approve it (%s). Find a way that stays inside the workspace.", reason), true
			}
		}
	}

	out, err := tools.ExecuteTool(tc.Name, tc.Arguments, a.ToolsCtx, ctx.Done())
	limit := a.ToolsCtx.ResultCharLimit
	if limit <= 0 {
		limit = 10000
	}
	if err != nil {
		if out != "" {
			return util.TruncateMiddle(out+"\nerror: "+err.Error(), limit), true
		}
		return util.TruncateMiddle("error: "+err.Error(), limit), true
	}
	return util.TruncateMiddle(out, limit), false
}

// modeNotice tells the model the permission mode changed. It is appended to
// the next user message so the conversation prefix stays unchanged.
func modeNotice(mode providers.Mode) string {
	switch mode {
	case providers.ModeReadOnly:
		return "[kabby: permission mode is now ro (read-only). write_file, edit_file, run_command and task will be refused.]"
	case providers.ModeBypass:
		return "[kabby: permission mode is now bypass. All tools run without asking.]"
	default:
		return "[kabby: permission mode is now edit. All tools are available; commands that reach outside the workspace need the user's approval.]"
	}
}
