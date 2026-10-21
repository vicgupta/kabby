package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// TUI is the raw-mode terminal user interface.
type TUI struct {
	in   io.Reader
	out  io.Writer
	edit *Editor
	mode string // "ro" | "edit" | "bypass"
	quit bool
}

// NewTUI creates a TUI reading from in and writing to out.
func NewTUI(in io.Reader, out io.Writer) *TUI {
	return &TUI{
		in:   in,
		out:  out,
		edit: NewEditor(),
		mode: "edit",
	}
}

// Run starts the TUI event loop. Returns when the user quits.
func (t *TUI) Run() error {
	t.write("\033[?25l")       // hide cursor
	defer t.write("\033[?25h") // show cursor on exit
	defer t.write("\033[0m")   // reset attributes

	for !t.quit {
		t.render()
		data := make([]byte, 64)
		n, err := t.in.Read(data)
		if err != nil {
			if n == 0 {
				return err
			}
			data = data[:n]
		} else {
			data = data[:n]
		}

		for len(data) > 0 {
			var key Key
			key, data, _ = ParseKey(data)
			t.handleKey(key)
		}
	}
	return nil
}

// render draws the full UI.
func (t *TUI) render() {
	t.write("\033[2J\033[H") // clear screen, move to home
	t.write(fmt.Sprintf("kabby · mode: %s\n", t.mode))
	t.write("─────────────────────────────────────────\n")
	t.write("Transcript:\n")
	t.write("  (no transcript yet)\n")
	t.write("─────────────────────────────────────────\n")
	t.write("Input: " + t.edit.Text() + "\n")
	t.write(fmt.Sprintf("Cursor: %d │ Keys: / slash menu, shift+tab mode, esc cancel, ctrl+c×2 quit\n", t.edit.CursorPos()))
}

// write writes to the output.
func (t *TUI) write(s string) {
	io.WriteString(t.out, s)
}

// handleKey processes a key event.
func (t *TUI) handleKey(key Key) {
	switch {
	case key.IsCtrlC():
		t.quit = true
	case key.Code == "escape":
		// Clear input on esc.
		t.edit.Clear()
	case key.Code == "backtab":
		// shift+tab: cycle ro → edit → bypass
		t.cycleMode()
	case key.Code == "enter":
		t.submit()
	case key.Code == "backspace":
		t.edit.Backspace()
	case key.Code == "delete":
		t.edit.DeleteRune()
	case key.Code == "left":
		t.edit.MoveLeft()
	case key.Code == "right":
		t.edit.MoveRight()
	case key.Code == "home":
		t.edit.MoveHome()
	case key.Code == "end":
		t.edit.MoveEnd()
	case key.Ctrl && key.Rune == 'u':
		t.edit.Clear()
	case key.Ctrl && key.Rune == 'l':
		// Ctrl+L: clear screen
		t.write("\033[2J\033[H")
	case key.Rune != 0:
		t.edit.InsertRune(key.Rune)
	}
}

func (t *TUI) cycleMode() {
	switch t.mode {
	case "ro":
		t.mode = "edit"
	case "edit":
		t.mode = "bypass"
	case "bypass":
		t.mode = "ro"
	}
}

func (t *TUI) submit() {
	line := strings.TrimSpace(t.edit.Text())
	if line == "" {
		return
	}
	t.edit.AddHistory(line)
	t.edit.Clear()
	t.write(fmt.Sprintf("\n> %s\n", line))
	if line == "/exit" || line == ":q" {
		t.quit = true
	}
}

// RunStdin runs the TUI using os.Stdin and os.Stdout.
func RunStdin() error {
	t := NewTUI(os.Stdin, os.Stdout)
	return t.Run()
}
