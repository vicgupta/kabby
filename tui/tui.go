package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"kabby/internal/agent"
	"kabby/internal/config"
	kctx "kabby/internal/context"
	"kabby/internal/plan"
	"kabby/internal/providers"
	"kabby/internal/tools"
)

// TUI is the terminal user interface.
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

	raw      bool // whether raw mode is active
	oldState *term.State
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
	// Put stdin into raw mode if it's a terminal, so we can read keys
	// character by character (needed for esc, shift+tab, etc.).
	if isTerminal, ok := isTerm(t.in); ok && isTerminal {
		oldState, err := term.MakeRaw(int(fd(t.in)))
		if err != nil {
			return fmt.Errorf("failed to put terminal in raw mode: %v", err)
		}
		t.raw = true
		t.oldState = oldState
		defer term.Restore(int(fd(t.in)), oldState)
	}

	// Handle signals to restore terminal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigCh
		if t.raw && t.oldState != nil {
			term.Restore(int(fd(t.in)), t.oldState)
		}
		os.Exit(130)
	}()

	// Show startup banner (no screen clearing, just append).
	t.write(fmt.Sprintf("kabby v0.7.3 · backend: %s · mode: %s · model: %s\r\n", t.backend, t.mode, t.model))
	t.write("Type /help for commands, /exit to quit.\r\n\r\n")

	for !t.quit {
		t.prompt()
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

// prompt draws the input line.
func (t *TUI) prompt() {
	t.write("\r\n> " + t.edit.Text())
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
		t.write("\r> ")
	case key.Code == "backtab":
		// shift+tab: cycle ro → edit → bypass
		t.cycleMode()
		t.write(fmt.Sprintf("\r\nmode: %s\r\n> ", t.mode))
	case key.Code == "enter":
		t.submit()
	case key.Code == "backspace":
		t.edit.Backspace()
		if t.raw {
			t.write("\b \b")
		}
	case key.Code == "delete":
		t.edit.DeleteRune()
	case key.Code == "left":
		t.edit.MoveLeft()
		if t.raw {
			t.write("\033[D")
		}
	case key.Code == "right":
		t.edit.MoveRight()
		if t.raw {
			t.write("\033[C")
		}
	case key.Code == "home":
		t.edit.MoveHome()
	case key.Code == "end":
		t.edit.MoveEnd()
	case key.Ctrl && key.Rune == 'u':
		t.edit.Clear()
		t.write("\r> ")
	case key.Ctrl && key.Rune == 'l':
		// Ctrl+L: clear screen
		t.write("\033[2J\033[H")
	case key.Rune != 0:
		t.edit.InsertRune(key.Rune)
		if t.raw {
			t.write(string(key.Rune))
		}
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
		t.write("\r> ")
		return
	}
	t.edit.AddHistory(line)
	t.edit.Clear()
	t.write(fmt.Sprintf("\r\n> %s\r\n", line))
	if line == "/exit" || line == ":q" {
		t.quit = true
		return
	}

	// Send the prompt to the model via the agent.
	if t.agent != nil {
		t.write("…\r\n")
		req := providers.ChatRequest{
			Model:         t.model,
			Messages:      []providers.ChatMessage{{Role: "user", Content: line}},
			Tools:         nil,
			ContextWindow: t.ctxMgr.Budget().Window,
			Effort:        "default",
		}
		results, err := t.agent.Chat(context.Background(), req)
		if err != nil {
			t.write(fmt.Sprintf("error: %v\r\n", err))
			t.write("\r> ")
			return
		}
		var sb strings.Builder
		for result := range results {
			if result.Err != nil {
				t.write(fmt.Sprintf("error: %v\r\n", result.Err))
				t.write("\r> ")
				return
			}
			sb.WriteString(result.Content)
			if result.Done {
				break
			}
		}
		t.write(sb.String() + "\r\n")
	} else {
		t.write("(no model connected)\r\n")
	}
}

// RunStdin runs the TUI using os.Stdin and os.Stdout with no model connected.
func RunStdin() error {
	t := NewTUI(os.Stdin, os.Stdout, "", "", "", nil, config.Config{})
	return t.Run()
}

// isTerm reports whether r is a terminal and returns (true, true) if so,
// (false, true) if it's not a terminal, or (false, false) if we can't tell.
func isTerm(r io.Reader) (bool, bool) {
	f, ok := r.(*os.File)
	if !ok {
		return false, false
	}
	return term.IsTerminal(int(f.Fd())), true
}

// fd returns the file descriptor of an *os.File wrapped in an io.Reader.
func fd(r io.Reader) uintptr {
	if f, ok := r.(*os.File); ok {
		return f.Fd()
	}
	return 0
}
