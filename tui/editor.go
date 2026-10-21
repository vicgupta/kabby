package tui

import (
	"strings"
)

// Editor is a raw-mode line editor with word wrapping and history.
type Editor struct {
	buf     strings.Builder
	pos     int // cursor position (rune index)
	history []string
	histIdx int
}

// NewEditor creates an empty editor.
func NewEditor() *Editor {
	return &Editor{}
}

// InsertRune inserts a rune at the cursor position.
func (e *Editor) InsertRune(r rune) {
	e.insertRuneAt(r, e.pos)
	e.pos++
}

func (e *Editor) insertRuneAt(r rune, pos int) {
	runes := e.runes()
	if pos > len(runes) {
		pos = len(runes)
	}
	e.buf.Reset()
	e.buf.WriteString(string(runes[:pos]))
	e.buf.WriteRune(r)
	e.buf.WriteString(string(runes[pos:]))
}

// DeleteRune deletes the rune at the cursor position.
func (e *Editor) DeleteRune() {
	runes := e.runes()
	if e.pos >= len(runes) {
		return
	}
	e.buf.Reset()
	e.buf.WriteString(string(runes[:e.pos]))
	e.buf.WriteString(string(runes[e.pos+1:]))
}

// Backspace deletes the rune before the cursor. Returns true if a rune was deleted.
func (e *Editor) Backspace() bool {
	if e.pos <= 0 {
		return false
	}
	e.pos--
	e.DeleteRune()
	return true
}

// MoveLeft moves the cursor left.
func (e *Editor) MoveLeft() {
	if e.pos > 0 {
		e.pos--
	}
}

// MoveRight moves the cursor right.
func (e *Editor) MoveRight() {
	runes := e.runes()
	if e.pos < len(runes) {
		e.pos++
	}
}

// MoveHome moves the cursor to the start.
func (e *Editor) MoveHome() {
	e.pos = 0
}

// MoveEnd moves the cursor to the end.
func (e *Editor) MoveEnd() {
	e.pos = len(e.runes())
}

// Text returns the current buffer contents.
func (e *Editor) Text() string {
	return e.buf.String()
}

// Runes returns the current buffer as runes.
func (e *Editor) runes() []rune {
	return []rune(e.buf.String())
}

// Clear clears the buffer.
func (e *Editor) Clear() {
	e.buf.Reset()
	e.pos = 0
}

// WordLeft moves the cursor to the start of the previous word.
func (e *Editor) WordLeft() {
	runes := e.runes()
	i := e.pos
	for i > 0 && runes[i-1] == ' ' {
		i--
	}
	for i > 0 && runes[i-1] != ' ' {
		i--
	}
	e.pos = i
}

// WordRight moves the cursor to the start of the next word.
func (e *Editor) WordRight() {
	runes := e.runes()
	i := e.pos
	// Skip remainder of current word.
	for i < len(runes) && runes[i] != ' ' {
		i++
	}
	// Skip spaces.
	for i < len(runes) && runes[i] == ' ' {
		i++
	}
	e.pos = i
}

// WrappedLines returns the text wrapped to the given width.
func (e *Editor) WrappedLines(width int) []string {
	text := e.Text()
	if width <= 0 {
		return []string{text}
	}
	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) <= width {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = w
		}
	}
	lines = append(lines, line)
	return lines
}

// HistoryUp cycles to the previous history item.
func (e *Editor) HistoryUp() string {
	if len(e.history) == 0 {
		return e.Text()
	}
	if e.histIdx <= 0 {
		e.histIdx = len(e.history)
	}
	e.histIdx--
	return e.history[e.histIdx]
}

// HistoryDown cycles to the next history item.
func (e *Editor) HistoryDown() string {
	if len(e.history) == 0 {
		return e.Text()
	}
	e.histIdx++
	if e.histIdx >= len(e.history) {
		e.histIdx = len(e.history)
		return ""
	}
	return e.history[e.histIdx]
}

// AddHistory adds a line to history.
func (e *Editor) AddHistory(line string) {
	if line == "" {
		return
	}
	e.history = append(e.history, line)
	e.histIdx = len(e.history)
}

// CursorPos returns the cursor position.
func (e *Editor) CursorPos() int {
	return e.pos
}

// SetCursorPos sets the cursor position.
func (e *Editor) SetCursorPos(pos int) {
	runes := e.runes()
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	e.pos = pos
}
