package tui

import (
	"bytes"
	"strings"
	"testing"

	"kabby/internal/config"
)

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
