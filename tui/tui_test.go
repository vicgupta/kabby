package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"kabby/internal/config"
	"kabby/internal/providers"
)

// panicClient fails the test if the agent is ever invoked -- used to prove
// that recognized slash commands are handled locally instead of being
// forwarded to the model.
type panicClient struct{ t *testing.T }

func (p *panicClient) Chat(ctx context.Context, req providers.ChatRequest) (<-chan providers.ChatResult, error) {
	p.t.Fatalf("agent.Chat called with %q; slash commands must not reach the model", req.Messages[len(req.Messages)-1].Content)
	return nil, nil
}

func TestEditorInsertAndDelete(t *testing.T) {
	e := NewEditor()
	e.InsertRune('h')
	e.InsertRune('i')
	if e.Text() != "hi" {
		t.Fatalf("expected 'hi', got %q", e.Text())
	}
	e.MoveLeft()
	e.InsertRune('a')
	if e.Text() != "hai" {
		t.Fatalf("expected 'hai', got %q", e.Text())
	}
	e.MoveRight()
	e.DeleteRune()
	if e.Text() != "hai" {
		t.Fatalf("expected 'hai', got %q", e.Text())
	}
}

func TestEditorBackspace(t *testing.T) {
	e := NewEditor()
	e.InsertRune('a')
	e.InsertRune('b')
	e.InsertRune('c')
	e.MoveLeft()  // cursor before 'c'
	e.Backspace() // delete 'b'
	if e.Text() != "ac" {
		t.Fatalf("expected 'ac', got %q", e.Text())
	}
}

func TestEditorWordMovement(t *testing.T) {
	e := NewEditor()
	e.InsertRune('h')
	e.InsertRune('e')
	e.InsertRune('l')
	e.InsertRune('l')
	e.InsertRune('o')
	e.InsertRune(' ')
	e.InsertRune('w')
	e.InsertRune('o')
	e.InsertRune('r')
	e.InsertRune('l')
	e.InsertRune('d')
	e.MoveHome()
	e.WordRight()
	if e.CursorPos() != 6 {
		t.Fatalf("expected cursor at 6, got %d", e.CursorPos())
	}
	e.WordLeft()
	if e.CursorPos() != 0 {
		t.Fatalf("expected cursor at 0, got %d", e.CursorPos())
	}
}

func TestEditorHistory(t *testing.T) {
	e := NewEditor()
	e.AddHistory("first command")
	e.AddHistory("second command")
	e.InsertRune('t')
	e.InsertRune('e')
	e.InsertRune('s')
	e.InsertRune('t')
	prev := e.HistoryUp()
	if prev != "second command" {
		t.Fatalf("expected 'second command', got %q", prev)
	}
	prev = e.HistoryUp()
	if prev != "first command" {
		t.Fatalf("expected 'first command', got %q", prev)
	}
	next := e.HistoryDown()
	if next != "second command" {
		t.Fatalf("expected 'second command', got %q", next)
	}
}

func TestEditorWrappedLines(t *testing.T) {
	e := NewEditor()
	e.InsertRune('h')
	e.InsertRune('e')
	e.InsertRune('l')
	e.InsertRune('l')
	e.InsertRune('o')
	e.InsertRune(' ')
	e.InsertRune('w')
	e.InsertRune('o')
	e.InsertRune('r')
	e.InsertRune('l')
	e.InsertRune('d')
	lines := e.WrappedLines(5)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "hello" {
		t.Fatalf("expected 'hello', got %q", lines[0])
	}
	if lines[1] != "world" {
		t.Fatalf("expected 'world', got %q", lines[1])
	}
}

func TestKeyParse(t *testing.T) {
	key, rest, ok := ParseKey([]byte{0x03}) // Ctrl+C
	if !ok {
		t.Fatal("expected ok")
	}
	if !key.IsCtrlC() {
		t.Fatal("expected Ctrl+C")
	}
	if len(rest) != 0 {
		t.Fatal("expected no rest")
	}

	key, rest, ok = ParseKey([]byte{0x1b, '[', 'A'}) // Up arrow
	if !ok {
		t.Fatal("expected ok")
	}
	if key.Code != "up" {
		t.Fatalf("expected 'up', got %q", key.Code)
	}

	key, rest, ok = ParseKey([]byte{0x1b, '[', 'Z'}) // Shift+Tab
	if !ok {
		t.Fatal("expected ok")
	}
	if key.Code != "backtab" {
		t.Fatalf("expected 'backtab', got %q", key.Code)
	}

	key, rest, ok = ParseKey([]byte{'h'}) // printable
	if !ok {
		t.Fatal("expected ok")
	}
	if key.Rune != 'h' {
		t.Fatalf("expected 'h', got %c", key.Rune)
	}

	// 0x7f (DEL) is the backspace key in raw mode. It must NOT be treated
	// as a printable rune.
	key, rest, ok = ParseKey([]byte{0x7f})
	if !ok {
		t.Fatal("expected ok")
	}
	if key.Code != "backspace" {
		t.Fatalf("expected 'backspace' for 0x7f, got code=%q rune=%c", key.Code, key.Rune)
	}
	if len(rest) != 0 {
		t.Fatal("expected no rest")
	}

	// 0x08 (BS) is also backspace.
	key, _, ok = ParseKey([]byte{0x08})
	if !ok || key.Code != "backspace" {
		t.Fatalf("expected 'backspace' for 0x08, got code=%q ok=%v", key.Code, ok)
	}
}

func TestTUIModeCycle(t *testing.T) {
	in := bytes.NewReader(nil)
	out := &bytes.Buffer{}
	tui := NewTUI(in, out, "", "", "", nil, config.Config{})
	if tui.mode != "edit" {
		t.Fatalf("expected initial mode 'edit', got %s", tui.mode)
	}
	tui.handleKey(Key{Code: "backtab", Shift: true})
	if tui.mode != "bypass" {
		t.Fatalf("expected 'bypass', got %s", tui.mode)
	}
	tui.handleKey(Key{Code: "backtab", Shift: true})
	if tui.mode != "ro" {
		t.Fatalf("expected 'ro', got %s", tui.mode)
	}
	tui.handleKey(Key{Code: "backtab", Shift: true})
	if tui.mode != "edit" {
		t.Fatalf("expected 'edit', got %s", tui.mode)
	}
}

func TestTUIEnterSubmit(t *testing.T) {
	in := bytes.NewReader([]byte("hello\n"))
	out := &bytes.Buffer{}
	tui := NewTUI(in, out, "", "", "", nil, config.Config{})

	// Simulate key events directly instead of reading from stdin.
	tui.edit.InsertRune('h')
	tui.edit.InsertRune('e')
	tui.edit.InsertRune('l')
	tui.edit.InsertRune('l')
	tui.edit.InsertRune('o')
	tui.handleKey(Key{Code: "enter"})

	output := out.String()
	if !strings.Contains(output, "hello") {
		t.Fatalf("expected 'hello' in output, got %q", output)
	}
	if tui.edit.Text() != "" {
		t.Fatal("expected input cleared after submit")
	}
}

func TestTUIEscClear(t *testing.T) {
	in := bytes.NewReader(nil)
	out := &bytes.Buffer{}
	tui := NewTUI(in, out, "", "", "", nil, config.Config{})
	tui.edit.InsertRune('a')
	tui.edit.InsertRune('b')
	tui.handleKey(Key{Code: "escape"})
	if tui.edit.Text() != "" {
		t.Fatal("expected input cleared on esc")
	}
}

func TestTUIQuitCtrlC(t *testing.T) {
	in := bytes.NewReader(nil)
	out := &bytes.Buffer{}
	tui := NewTUI(in, out, "", "", "", nil, config.Config{})
	tui.handleKey(Key{Ctrl: true, Rune: 'c'})
	if !tui.quit {
		t.Fatal("expected quit on Ctrl+C")
	}
}

var _ = strings.TrimSpace

// TestRenderMarkdownUsesCRLF guards against the raw-mode "staircase" bug:
// the terminal runs with OPOST disabled, so every line break in a
// multi-line reply must be \r\n or the cursor never returns to column 0.
func TestRenderMarkdownUsesCRLF(t *testing.T) {
	md := "## Commands\n\n- `/help` shows this message\n- `/model` switches model\n\nSome **bold** text.\n"
	out := renderMarkdown(md)
	for _, line := range strings.Split(out, "\n") {
		if line != "" && !strings.HasSuffix(line, "\r") {
			t.Fatalf("line %q is not \\r-terminated; it will staircase in raw mode: %q", line, out)
		}
	}
}

func TestStripInlineLeavesLiteralTextAlone(t *testing.T) {
	cases := map[string]string{
		"2 * 3 = 6":                              "2 * 3 = 6",
		"my_variable_name should stay untouched": "my_variable_name should stay untouched",
		"**bold**":                               "bold",
		"this is *important* text":               "this is important text",
		"the _word_ is nice":                     "the word is nice",
		"use `go test ./...` to run tests":       "use go test ./... to run tests",
	}
	for in, want := range cases {
		if got := stripInline(in); got != want {
			t.Errorf("stripInline(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlashHelpIsHandledLocally(t *testing.T) {
	in := bytes.NewReader(nil)
	out := &bytes.Buffer{}
	tu := NewTUI(in, out, "", "", "", &panicClient{t: t}, config.Config{})
	for _, r := range "/help" {
		tu.edit.InsertRune(r)
	}
	tu.handleKey(Key{Code: "enter"})
	if !strings.Contains(out.String(), "Commands:") {
		t.Fatalf("expected /help output to list commands, got %q", out.String())
	}
}

func TestSlashModelShowsCurrent(t *testing.T) {
	in := bytes.NewReader(nil)
	out := &bytes.Buffer{}
	tu := NewTUI(in, out, "demo-model", "ollama", "http://x", &panicClient{t: t}, config.Config{})
	for _, r := range "/model" {
		tu.edit.InsertRune(r)
	}
	tu.handleKey(Key{Code: "enter"})
	if !strings.Contains(out.String(), "demo-model") {
		t.Fatalf("expected /model to report the current model, got %q", out.String())
	}
}

func TestSlashModelSwitchesByPartialName(t *testing.T) {
	in := bytes.NewReader(nil)
	out := &bytes.Buffer{}
	tu := NewTUI(in, out, "qwen3.5", "ollama", "http://a", &panicClient{t: t}, config.Config{})
	tu.SetBackends([]providers.ModelInfo{
		{ID: "qwen3.5", Name: "qwen3.5", Backend: "ollama", URL: "http://a"},
		{ID: "gemma4:12b-mlx", Name: "gemma4:12b-mlx", Backend: "ollama", URL: "http://a"},
	})
	for _, r := range "/model gemma4" {
		tu.edit.InsertRune(r)
	}
	tu.handleKey(Key{Code: "enter"})
	if !strings.Contains(out.String(), "switched to model: gemma4:12b-mlx") {
		t.Fatalf("expected /model gemma4 to switch models, got %q", out.String())
	}
	if tu.model != "gemma4:12b-mlx" {
		t.Fatalf("expected tui.model updated, got %q", tu.model)
	}
}

func TestUnknownSlashCommandDoesNotReachModel(t *testing.T) {
	in := bytes.NewReader(nil)
	out := &bytes.Buffer{}
	tu := NewTUI(in, out, "", "", "", &panicClient{t: t}, config.Config{})
	for _, r := range "/bogus" {
		tu.edit.InsertRune(r)
	}
	tu.handleKey(Key{Code: "enter"})
	if !strings.Contains(out.String(), "unknown command") {
		t.Fatalf("expected an unknown-command reply, got %q", out.String())
	}
}

// chunkClient replies with a fixed sequence of streamed chunks.
type chunkClient struct {
	chunks []string
}

func (c *chunkClient) Chat(ctx context.Context, req providers.ChatRequest) (<-chan providers.ChatResult, error) {
	ch := make(chan providers.ChatResult)
	go func() {
		defer close(ch)
		for i, s := range c.chunks {
			ch <- providers.ChatResult{Content: s, Done: i == len(c.chunks)-1}
		}
	}()
	return ch, nil
}

func submitLine(tu *TUI, line string) {
	for _, r := range line {
		tu.edit.InsertRune(r)
	}
	tu.handleKey(Key{Code: "enter"})
}

func TestStreamingIsOnByDefault(t *testing.T) {
	out := &bytes.Buffer{}
	tu := NewTUI(bytes.NewReader(nil), out, "m", "ollama", "http://x", &chunkClient{chunks: []string{"Hel", "lo\nworld"}}, config.Config{})
	if !tu.stream {
		t.Fatal("expected streaming on by default")
	}
	submitLine(tu, "hi")
	if !strings.Contains(out.String(), "Hello\r\nworld") {
		t.Fatalf("expected streamed chunks with CRLF line breaks, got %q", out.String())
	}
}

func TestSlashStreamToggles(t *testing.T) {
	out := &bytes.Buffer{}
	tu := NewTUI(bytes.NewReader(nil), out, "", "", "", &panicClient{t: t}, config.Config{})
	submitLine(tu, "/stream")
	if tu.stream || !strings.Contains(out.String(), "streaming: off") {
		t.Fatalf("expected /stream to turn streaming off, got %q", out.String())
	}
	submitLine(tu, "/stream")
	if !tu.stream || !strings.Contains(out.String(), "streaming: on") {
		t.Fatalf("expected second /stream to turn streaming on, got %q", out.String())
	}
	submitLine(tu, "/stream off")
	if tu.stream {
		t.Fatal("expected /stream off to turn streaming off")
	}
	submitLine(tu, "/stream off")
	if tu.stream {
		t.Fatal("expected /stream off to be idempotent")
	}
	submitLine(tu, "/stream on")
	if !tu.stream {
		t.Fatal("expected /stream on to turn streaming on")
	}
	submitLine(tu, "/stream maybe")
	if !strings.Contains(out.String(), "usage: /stream [on|off]") {
		t.Fatalf("expected usage for a bad argument, got %q", out.String())
	}
}

func TestStreamOffRendersMarkdownAfterReply(t *testing.T) {
	out := &bytes.Buffer{}
	tu := NewTUI(bytes.NewReader(nil), out, "m", "ollama", "http://x", &chunkClient{chunks: []string{"some **bo", "ld** text"}}, config.Config{})
	tu.stream = false
	submitLine(tu, "hi")
	if !strings.Contains(out.String(), "some bold text") {
		t.Fatalf("expected rendered markdown when streaming is off, got %q", out.String())
	}
}

// toolThenAnswerClient asks for one list_files call, then answers.
type toolThenAnswerClient struct{ calls int }

func (c *toolThenAnswerClient) Chat(ctx context.Context, req providers.ChatRequest) (<-chan providers.ChatResult, error) {
	c.calls++
	ch := make(chan providers.ChatResult, 2)
	if c.calls == 1 {
		ch <- providers.ChatResult{Done: true, ToolCalls: []providers.ToolCall{{ID: "c1", Name: "list_files", Arguments: map[string]any{"path": "."}}}}
	} else {
		ch <- providers.ChatResult{Content: "all done", Done: true, EvalCount: 3}
	}
	close(ch)
	return ch, nil
}

func TestTUIRunsToolsThroughTheAgentLoop(t *testing.T) {
	out := &bytes.Buffer{}
	client := &toolThenAnswerClient{}
	tu := NewTUI(bytes.NewReader(nil), out, "m", "ollama", "http://x", client, config.Config{})
	tu.SetWorkspace(t.TempDir())
	submitLine(tu, "look around")
	got := out.String()
	if client.calls != 2 {
		t.Fatalf("expected a tool round-trip (2 model calls), got %d", client.calls)
	}
	if !strings.Contains(got, "· list_files .") || !strings.Contains(got, "all done") {
		t.Fatalf("expected the tool line and the answer, got %q", got)
	}
	if !strings.Contains(got, "1 tools") {
		t.Fatalf("expected a stats line, got %q", got)
	}
}

func TestSlashClearResetsConversation(t *testing.T) {
	out := &bytes.Buffer{}
	tu := NewTUI(bytes.NewReader(nil), out, "m", "ollama", "http://x", &chunkClient{chunks: []string{"hi"}}, config.Config{})
	submitLine(tu, "hello")
	if len(tu.agent.History) == 0 {
		t.Fatal("expected history after a turn")
	}
	submitLine(tu, "/clear")
	if len(tu.agent.History) != 0 {
		t.Fatal("expected /clear to reset the conversation")
	}
}
