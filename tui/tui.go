package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"kabby/internal/agent"
	"kabby/internal/config"
	kctx "kabby/internal/context"
	"kabby/internal/plan"
	"kabby/internal/providers"
	"kabby/internal/tools"
)

// TUI is the raw-mode terminal user interface.
type TUI struct {
	in   io.Reader
	out  io.Writer
	edit *Editor
	mode string // "ro" | "edit" | "bypass"
	quit bool

	model    string
	backend  string
	url      string
	agent    agent.ChatClient
	ctxMgr   *kctx.Manager
	toolsCtx *tools.ToolContext
	plan     *plan.Plan
	cfg      config.Config
}

// NewTUI creates a TUI reading from in and writing to out, connected to the
// given model/backend and agent.
func NewTUI(in io.Reader, out io.Writer, model, backend, url string, a agent.ChatClient, cfg config.Config) *TUI {
	window := 4096
	ctxMgr := kctx.NewManager(window)
	tc := &tools.ToolContext{
		Workspace:       os.Getenv("PWD"),
		ResultCharLimit: 10000,
		TaskManager:     tools.NewTaskManager(),
		Plan:            plan.New(),
	}
	return &TUI{
		in:       in,
		out:      out,
		edit:     NewEditor(),
		mode:     "edit",
		model:    model,
		backend:  backend,
		url:      url,
		agent:    a,
		ctxMgr:   ctxMgr,
		toolsCtx: tc,
		plan:     plan.New(),
		cfg:      cfg,
	}
}

// Run starts the TUI event loop. Returns when the user quits.
func (t *TUI) Run() error {
	t.write("\033[?25l")       // hide cursor
	defer t.write("\033[?25h") // show cursor on exit
	defer t.write("\033[0m")   // reset attributes

	// Show startup line.
	t.write(fmt.Sprintf("kabby · %s · %s · model: %s\n", t.backend, t.mode, t.model))
	t.write("─────────────────────────────────────────\n")

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
	t.write(fmt.Sprintf("kabby · backend: %s · mode: %s · model: %s\n", t.backend, t.mode, t.model))
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
		return
	}

	// Send the prompt to the model via the agent.
	if t.agent != nil {
		t.write("…\n")
		req := providers.ChatRequest{
			Model:         t.model,
			Messages:      []providers.ChatMessage{{Role: "user", Content: line}},
			Tools:         nil,
			ContextWindow: t.ctxMgr.Budget().Window,
			Effort:        "default",
		}
		results, err := t.agent.Chat(context.Background(), req)
		if err != nil {
			t.write(fmt.Sprintf("error: %v\n", err))
			return
		}
		var sb strings.Builder
		for result := range results {
			if result.Err != nil {
				t.write(fmt.Sprintf("error: %v\n", result.Err))
				return
			}
			sb.WriteString(result.Content)
			if result.Done {
				break
			}
		}
		t.write(sb.String() + "\n")
	} else {
		t.write("(no model connected)\n")
	}
}

// RunStdin runs the TUI using os.Stdin and os.Stdout with no model connected.
func RunStdin() error {
	t := NewTUI(os.Stdin, os.Stdout, "", "", "", nil, config.Config{})
	return t.Run()
}

var _ = time.Second
var _ = providers.ModeEdit
