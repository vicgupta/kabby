package plan

import (
	"fmt"
	"strings"
	"testing"
)

func TestSetNewlineList(t *testing.T) {
	p := New()
	msg := p.Set("create the file\nwrite the content\nrun the tests")
	if !p.Exists() {
		t.Fatal("plan should exist after set")
	}
	if p.DoneCount() != 0 {
		t.Fatalf("expected 0 done, got %d", p.DoneCount())
	}
	if p.CurrentIndex() != 0 {
		t.Fatalf("expected current index 0, got %d", p.CurrentIndex())
	}
	if !strings.Contains(msg, "3 steps") {
		t.Fatalf("expected '3 steps' in message, got %q", msg)
	}
	if !strings.Contains(msg, "create the file") {
		t.Fatalf("expected first step in message, got %q", msg)
	}
}

func TestSetSemicolonList(t *testing.T) {
	p := New()
	msg := p.Set("step one; step two; step three")
	if !p.Exists() {
		t.Fatal("plan should exist after set")
	}
	if p.DoneCount() != 0 || len(p.steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(p.steps))
	}
	if !strings.Contains(msg, "3 steps") {
		t.Fatalf("expected '3 steps' in message, got %q", msg)
	}
}

func TestSetStripsBulletsAndNumbering(t *testing.T) {
	p := New()
	p.Set("- first\n1. second\n• third\n4. fourth")
	if len(p.steps) != 4 {
		t.Fatalf("expected 4 steps, got %d: %+v", len(p.steps), p.steps)
	}
	expected := []string{"first", "second", "third", "fourth"}
	for i, e := range expected {
		if p.steps[i].Text != e {
			t.Fatalf("step %d = %q, want %q", i, p.steps[i].Text, e)
		}
	}
}

func TestSetEmptyClears(t *testing.T) {
	p := New()
	p.Set("a; b")
	msg := p.Set("")
	if p.Exists() {
		t.Fatal("plan should be cleared after empty set")
	}
	if !strings.Contains(msg, "cleared") {
		t.Fatalf("expected 'cleared' message, got %q", msg)
	}
}

func TestSetCapsAt20(t *testing.T) {
	p := New()
	var b strings.Builder
	for i := 0; i < 25; i++ {
		b.WriteString(fmt.Sprintf("step %d;", i))
	}
	p.Set(b.String())
	if len(p.steps) != 20 {
		t.Fatalf("expected 20 steps (cap), got %d", len(p.steps))
	}
}

func TestMarkDoneNoArgsAdvancesCurrent(t *testing.T) {
	p := New()
	p.Set("one; two; three")
	if p.CurrentIndex() != 0 {
		t.Fatalf("expected current index 0, got %d", p.CurrentIndex())
	}

	msg := p.MarkDone(nil)
	if !strings.Contains(msg, "Next: 2") {
		t.Fatalf("expected restated next step, got %q", msg)
	}
	if p.DoneCount() != 1 {
		t.Fatalf("expected 1 done, got %d", p.DoneCount())
	}
	if p.CurrentIndex() != 1 {
		t.Fatalf("expected current index 1, got %d", p.CurrentIndex())
	}
}

func TestMarkDoneWithIndex(t *testing.T) {
	p := New()
	p.Set("one; two; three")
	two := 2
	msg := p.MarkDone(&two)
	if !strings.Contains(msg, "Step 2 done") {
		t.Fatalf("expected 'Step 2 done', got %q", msg)
	}
	if p.DoneCount() != 1 || !p.steps[1].Done {
		t.Fatal("expected step 2 marked done")
	}
}

func TestMarkDoneAllComplete(t *testing.T) {
	p := New()
	p.Set("only one")
	msg := p.MarkDone(nil)
	if !strings.Contains(msg, "Plan complete") {
		t.Fatalf("expected completion message, got %q", msg)
	}
	if p.DoneCount() != 1 {
		t.Fatalf("expected 1 done, got %d", p.DoneCount())
	}
	if p.CurrentIndex() != 1 {
		t.Fatalf("expected current index 1 (past end), got %d", p.CurrentIndex())
	}
	// Calling done again on a complete plan is a no-op with a clear message.
	msg2 := p.MarkDone(nil)
	if !strings.Contains(msg2, "No step 2") {
		t.Fatalf("expected 'No step 2' message, got %q", msg2)
	}
}

func TestMarkDoneEmptyPlan(t *testing.T) {
	p := New()
	msg := p.MarkDone(nil)
	if !strings.Contains(msg, "empty") {
		t.Fatalf("expected 'empty' message, got %q", msg)
	}
}

func TestAdd(t *testing.T) {
	p := New()
	p.Set("first")
	msg := p.Add("second")
	if !strings.Contains(msg, "Added step 2") {
		t.Fatalf("expected 'Added step 2', got %q", msg)
	}
	if len(p.steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(p.steps))
	}
}

func TestAddEmptyRejected(t *testing.T) {
	p := New()
	msg := p.Add("")
	if !strings.Contains(msg, "empty") {
		t.Fatalf("expected 'empty' rejection, got %q", msg)
	}
}

func TestAddAtCapRejected(t *testing.T) {
	p := New()
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString(fmt.Sprintf("step %d;", i))
	}
	p.Set(b.String())
	msg := p.Add("extra")
	if !strings.Contains(msg, "already has 20 steps") {
		t.Fatalf("expected cap rejection, got %q", msg)
	}
}

func TestCheckpoint(t *testing.T) {
	p := New()
	p.Set("one; two")
	msg := p.Checkpoint("waiting for reply")
	if !strings.Contains(msg, "Checkpoint saved on step 1") {
		t.Fatalf("expected checkpoint confirmation, got %q", msg)
	}
	if p.steps[0].Note != "waiting for reply" {
		t.Fatalf("expected note set, got %q", p.steps[0].Note)
	}
}

func TestCheckpointEmptyRejected(t *testing.T) {
	p := New()
	p.Set("one")
	msg := p.Checkpoint("")
	if !strings.Contains(msg, "must not be empty") {
		t.Fatalf("expected empty rejection, got %q", msg)
	}
}

func TestCheckpointTooLongRejected(t *testing.T) {
	p := New()
	p.Set("one")
	long := strings.Repeat("x", 1001)
	msg := p.Checkpoint(long)
	if !strings.Contains(msg, "too long") {
		t.Fatalf("expected length rejection, got %q", msg)
	}
}

func TestCheckpointMaxLengthAccepted(t *testing.T) {
	p := New()
	p.Set("one")
	exact := strings.Repeat("x", 1000)
	msg := p.Checkpoint(exact)
	if !strings.Contains(msg, "Checkpoint saved") {
		t.Fatalf("expected acceptance at 1000 chars, got %q", msg)
	}
}

func TestModelView(t *testing.T) {
	p := New()
	p.Set("one; two; three")
	p.MarkDone(nil)
	view := p.ModelView()
	if !strings.Contains(view, "Current step: 2. two") {
		t.Fatalf("expected current step in view, got %q", view)
	}
	if !strings.Contains(view, "three") {
		t.Fatalf("expected pending step in view, got %q", view)
	}
}

func TestModelViewEmpty(t *testing.T) {
	p := New()
	if p.ModelView() != "" {
		t.Fatal("expected empty view for empty plan")
	}
}

func TestCompactLine(t *testing.T) {
	p := New()
	p.Set("one; two; three")
	p.MarkDone(nil)
	line, ok := p.CompactLine()
	if !ok {
		t.Fatal("expected non-empty compact line")
	}
	if !strings.Contains(line, "[x] one") {
		t.Fatalf("expected [x] one, got %q", line)
	}
	if !strings.Contains(line, "[>] two") {
		t.Fatalf("expected [>] two, got %q", line)
	}
}

func TestCompactLineEmpty(t *testing.T) {
	p := New()
	if _, ok := p.CompactLine(); ok {
		t.Fatal("expected false for empty plan")
	}
}

func TestPendingSummary(t *testing.T) {
	p := New()
	p.Set("one; two; three")
	p.MarkDone(nil)
	sum := p.PendingSummary()
	if !strings.Contains(sum, "two") || !strings.Contains(sum, "three") {
		t.Fatalf("expected pending steps in summary, got %q", sum)
	}
}

func TestShow(t *testing.T) {
	p := New()
	p.Set("one; two")
	out := p.Show()
	if !strings.Contains(out, "[ ] one") {
		t.Fatalf("expected pending marker, got %q", out)
	}
	p.MarkDone(nil)
	out = p.Show()
	if !strings.Contains(out, "[x] one") {
		t.Fatalf("expected done marker, got %q", out)
	}
}
