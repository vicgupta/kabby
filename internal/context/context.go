package context

import (
	"fmt"
	"math"
)

// Budget is the computed context window budget.
type Budget struct {
	Window       int // total context window
	ReplyReserve int // tokens reserved for model reply
	Safety       int // safety margin
	InputBudget  int // tokens available for input (history + system + tools)
}

// ComputeBudget calculates the context budget from the real window.
func ComputeBudget(window int) Budget {
	if window < 1024 {
		window = 1024
	}
	replyReserve := int(math.Floor(float64(window) / 4.0))
	if replyReserve < 128 {
		replyReserve = 128
	}
	if replyReserve > 8192 {
		replyReserve = 8192
	}
	safety := int(math.Floor(float64(window) * 0.05))
	if safety > 256 {
		safety = 256
	}
	inputBudget := window - replyReserve - safety
	if inputBudget < 0 {
		inputBudget = 0
	}
	return Budget{
		Window:       window,
		ReplyReserve: replyReserve,
		Safety:       safety,
		InputBudget:  inputBudget,
	}
}

// UsageSnapshot records observed token usage after a model response.
type UsageSnapshot struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Manager tracks context usage and triggers eviction/compaction.
type Manager struct {
	budget  Budget
	used    int // estimated input tokens currently in context
	history []HistoryGroup
}

// HistoryGroup is a contiguous block of conversation history.
type HistoryGroup struct {
	ID        string
	Tokens    int
	Messages  int
	Newest    bool // protected from tier-1 eviction
	Evictable bool
}

// NewManager creates a context manager for the given window.
func NewManager(window int) *Manager {
	return &Manager{budget: ComputeBudget(window)}
}

// Budget returns the current budget.
func (m *Manager) Budget() Budget {
	return m.budget
}

// InputUsage returns the estimated input tokens currently in context.
func (m *Manager) InputUsage() int {
	return m.used
}

// InputUtilization returns the fraction of the input budget used (0.0-1.0+).
func (m *Manager) InputUtilization() float64 {
	if m.budget.InputBudget <= 0 {
		return 1.0
	}
	return float64(m.used) / float64(m.budget.InputBudget)
}

// UpdateUsage anchors usage to observed backend totals after each response.
func (m *Manager) UpdateUsage(snap UsageSnapshot) {
	// The model's total output becomes part of context; input is the
	// observed input tokens. We anchor input to the real value.
	m.used = snap.InputTokens
}

// EstimateUsage sets an estimated input token count between observations.
func (m *Manager) EstimateUsage(tokens int) {
	m.used = tokens
}

// NeedsTier1Eviction reports whether input usage exceeds 80% of budget.
func (m *Manager) NeedsTier1Eviction() bool {
	if m.budget.InputBudget <= 0 {
		return true
	}
	return m.InputUtilization() > 0.80
}

// Tier1Target returns the target usage after eviction (60% of budget).
func (m *Manager) Tier1Target() int {
	return int(math.Floor(float64(m.budget.InputBudget) * 0.60))
}

// EvictTier1 removes groups until usage is at or below the 60% target.
// The newest group is protected. Read stubs replace evicted reads of
// overwritten files.
func (m *Manager) EvictTier1() (evicted []HistoryGroup, err error) {
	target := m.Tier1Target()
	for m.used > target && len(m.history) > 0 {
		// Find the oldest evictable non-newest group.
		idx := -1
		for i := len(m.history) - 1; i >= 0; i-- {
			if !m.history[i].Newest && m.history[i].Evictable {
				idx = i
				break
			}
		}
		if idx < 0 {
			break // nothing evictable
		}
		evicted = append(evicted, m.history[idx])
		m.used -= m.history[idx].Tokens
		m.history = append(m.history[:idx], m.history[idx+1:]...)
	}
	return evicted, nil
}

// AddGroup appends a history group.
func (m *Manager) AddGroup(g HistoryGroup) {
	m.history = append(m.history, g)
	m.used += g.Tokens
}

// MarkNewest protects the most recent group from tier-1 eviction.
func (m *Manager) MarkNewest() {
	if len(m.history) == 0 {
		return
	}
	for i := range m.history {
		m.history[i].Newest = false
	}
	m.history[len(m.history)-1].Newest = true
}

// CanFitInstructions checks whether the system prompt + instructions fit
// within the input budget.
func (m *Manager) CanFitInstructions(instructionTokens int) bool {
	return instructionTokens <= m.budget.InputBudget
}

// ImpossibleWindow returns an actionable error if the window is too small
// to hold even the instructions.
func (m *Manager) ImpossibleWindow(instructionTokens int) error {
	if instructionTokens > m.budget.InputBudget {
		return fmt.Errorf(
			"context window too small: %d tokens needed for instructions but input budget is %d (window %d). "+
				"Use --ctx to increase the window or choose a smaller model.",
			instructionTokens, m.budget.InputBudget, m.budget.Window,
		)
	}
	return nil
}

// CompactionThreshold returns the usage level (80% of input budget) at which
// compaction is triggered.
func (m *Manager) CompactionThreshold() int {
	return int(math.Floor(float64(m.budget.InputBudget) * 0.80))
}

var _ = math.Max
