// Package plan implements the harness-held checklist. The plan never enters the
// model transcript; the model sees only a bounded re-injection via ModelView.
package plan

import (
	"fmt"
	"strings"
)

// maxSteps is the hard cap on plan steps.
const maxSteps = 20

// maxCheckpointChars is the hard cap on a checkpoint note.
const maxCheckpointChars = 1000

// PlanStep is a single step in the harness-held checklist.
type PlanStep struct {
	Text string
	Done bool
	Note string // ≤1000 chars, replaces the current step's note
}

// Plan is the harness-held checklist. It survives compaction and is never fed
// to the model as raw tokens.
type Plan struct {
	steps []PlanStep
}

// New creates an empty Plan.
func New() *Plan {
	return &Plan{}
}

// Exists reports whether the plan has any steps.
func (p *Plan) Exists() bool {
	return len(p.steps) > 0
}

// DoneCount returns the number of completed steps.
func (p *Plan) DoneCount() int {
	n := 0
	for _, s := range p.steps {
		if s.Done {
			n++
		}
	}
	return n
}

// CurrentIndex returns the index of the first incomplete step, or len(steps) if
// all steps are done.
func (p *Plan) CurrentIndex() int {
	for i, s := range p.steps {
		if !s.Done {
			return i
		}
	}
	return len(p.steps)
}

// Reset clears all steps.
func (p *Plan) Reset() {
	p.steps = nil
}

// Set replaces the plan with steps parsed from stepsText. Steps may be separated
// by newlines or semicolons; bullets and numbering are stripped. At most maxSteps
// steps are kept. Returns a self-teaching confirmation string.
func (p *Plan) Set(stepsText string) string {
	raw := splitSteps(stepsText)
	steps := make([]PlanStep, 0, len(raw))
	for _, r := range raw {
		r = stripBullet(r)
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if len(steps) >= maxSteps {
			break
		}
		steps = append(steps, PlanStep{Text: r})
	}
	p.steps = steps

	if len(steps) == 0 {
		return "Plan cleared. Example: /plan set create the feature; add tests; run go test"
	}
	current := p.steps[0].Text
	if len(steps) > maxSteps {
		steps = steps[:maxSteps]
	}
	return fmt.Sprintf("Plan set (%d steps). Current: 1. %s  Example: /plan done", len(steps), current)
}

// MarkDone marks a step done. If stepNumber is nil (or 0), the current (first
// incomplete) step is marked done. Returns a reply restating the next step.
func (p *Plan) MarkDone(stepNumber *int) string {
	if len(p.steps) == 0 {
		return "Plan is empty. Set one first: /plan set write a function; test it"
	}

	idx := p.CurrentIndex()
	if stepNumber != nil && *stepNumber > 0 && *stepNumber <= len(p.steps) {
		idx = *stepNumber - 1
	}
	if idx < 0 || idx >= len(p.steps) {
		shown := idx + 1
		if stepNumber != nil {
			shown = *stepNumber
		}
		return fmt.Sprintf("No step %d. Plan has %d steps. Example: /plan done 2", shown, len(p.steps))
	}

	p.steps[idx].Done = true

	nextIdx := p.CurrentIndex()
	if nextIdx >= len(p.steps) {
		return fmt.Sprintf("Step %d done. Plan complete: %s.", idx+1, p.steps[idx].Text)
	}
	return fmt.Sprintf("Step %d done. Next: %d. %s", idx+1, nextIdx+1, p.steps[nextIdx].Text)
}

// Add appends a new step to the end of the plan.
func (p *Plan) Add(text string) string {
	text = strings.TrimSpace(stripBullet(text))
	if text == "" {
		return "Cannot add an empty step. Example: /plan add run the linter"
	}
	if len(p.steps) >= maxSteps {
		return fmt.Sprintf("Plan already has %d steps (max %d). Finish or remove some first.", len(p.steps), maxSteps)
	}
	p.steps = append(p.steps, PlanStep{Text: text})
	return fmt.Sprintf("Added step %d: %s", len(p.steps), text)
}

// Show returns a readable rendering of the plan with done/pending markers.
func (p *Plan) Show() string {
	if len(p.steps) == 0 {
		return "Plan is empty. Example: /plan set explore the codebase; add tests"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Plan (%d/%d done):\n", p.DoneCount(), len(p.steps)))
	for i, s := range p.steps {
		marker := "[ ]"
		if s.Done {
			marker = "[x]"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s %s\n", i+1, marker, s.Text))
		if s.Note != "" {
			sb.WriteString(fmt.Sprintf("      note: %s\n", s.Note))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Checkpoint replaces the current step's note with text (1–1000 chars).
func (p *Plan) Checkpoint(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Checkpoint note must not be empty. Example: /plan checkpoint waiting for API response"
	}
	if len(text) > maxCheckpointChars {
		return fmt.Sprintf("Checkpoint note too long (%d chars, max %d). Shorten it.", len(text), maxCheckpointChars)
	}
	idx := p.CurrentIndex()
	if idx >= len(p.steps) {
		return "No current step to checkpoint. Set a plan first: /plan set do X; do Y"
	}
	p.steps[idx].Note = text
	return fmt.Sprintf("Checkpoint saved on step %d: %s", idx+1, text)
}

// ModelView returns the bounded re-injection the model sees: the current step
// and pending steps, free of raw plan tokens.
func (p *Plan) ModelView() string {
	if !p.Exists() {
		return ""
	}
	idx := p.CurrentIndex()
	if idx >= len(p.steps) {
		return "Plan: all steps complete."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Current step: %d. %s", idx+1, p.steps[idx].Text))
	if p.steps[idx].Note != "" {
		sb.WriteString(fmt.Sprintf(" (note: %s)", p.steps[idx].Note))
	}
	pending := p.pendingSteps()
	if len(pending) > 0 {
		sb.WriteString("\nPending: ")
		for i, t := range pending {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(t)
		}
	}
	return sb.String()
}

// CompactLine returns a single-line summary for compaction. The bool is false
// when the plan is empty.
func (p *Plan) CompactLine() (string, bool) {
	if !p.Exists() {
		return "", false
	}
	idx := p.CurrentIndex()
	var parts []string
	for i, s := range p.steps {
		if s.Done {
			parts = append(parts, fmt.Sprintf("[x] %s", s.Text))
		} else if i == idx {
			parts = append(parts, fmt.Sprintf("[>] %s", s.Text))
		} else {
			parts = append(parts, fmt.Sprintf("[ ] %s", s.Text))
		}
	}
	return strings.Join(parts, " ; "), true
}

// PendingSummary returns a short summary of unfinished steps, starting with
// the current step.
func (p *Plan) PendingSummary() string {
	idx := p.CurrentIndex()
	if idx >= len(p.steps) {
		return "no pending steps"
	}
	var unfinished []string
	for i := idx; i < len(p.steps); i++ {
		if !p.steps[i].Done {
			unfinished = append(unfinished, p.steps[i].Text)
		}
	}
	if len(unfinished) == 0 {
		return "no pending steps"
	}
	if len(unfinished) == 1 {
		return fmt.Sprintf("1 pending: %s", unfinished[0])
	}
	return fmt.Sprintf("%d pending: %s; %s", len(unfinished), unfinished[0], unfinished[1])
}

// pendingSteps returns the text of all incomplete steps after the current one.
func (p *Plan) pendingSteps() []string {
	idx := p.CurrentIndex()
	var out []string
	for i := idx + 1; i < len(p.steps); i++ {
		if !p.steps[i].Done {
			out = append(out, p.steps[i].Text)
		}
	}
	return out
}

// splitSteps splits on newlines or semicolons.
func splitSteps(text string) []string {
	// Normalize semicolons to newlines, then split.
	normalized := strings.ReplaceAll(text, ";", "\n")
	return strings.Split(normalized, "\n")
}

// stripBullet removes leading bullets/numbering (e.g. "- ", "1. ", "• ").
func stripBullet(s string) string {
	s = strings.TrimSpace(s)
	for {
		orig := s
		runes := []rune(s)
		if len(runes) >= 2 && runes[0] == '-' && runes[1] == ' ' {
			s = strings.TrimSpace(string(runes[2:]))
			continue
		}
		if len(runes) >= 2 && runes[0] == '•' && runes[1] == ' ' {
			s = strings.TrimSpace(string(runes[2:]))
			continue
		}
		if len(runes) >= 2 && runes[0] == '*' && runes[1] == ' ' {
			s = strings.TrimSpace(string(runes[2:]))
			continue
		}
		// Numbered: "1. ", "12. ", etc.
		if len(runes) >= 2 && runes[0] >= '0' && runes[0] <= '9' {
			i := 1
			for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
				i++
			}
			if i < len(runes) && runes[i] == '.' && (i+1 == len(runes) || runes[i+1] == ' ') {
				s = strings.TrimSpace(string(runes[i+1:]))
				continue
			}
		}
		if s == orig {
			break
		}
	}
	return s
}
