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
	"kabby/internal/session"
	"kabby/internal/tools"
)

// TUI is the terminal user interface.
type TUI struct {
	in     io.Reader
	out    io.Writer
	edit   *Editor
	mode   string // "ro" | "edit" | "bypass"
	stream bool   // print reply tokens as they arrive (toggle with /stream)
	quit   bool

	model    string
	backend  string
	url      string
	agent    agent.ChatClient
	backends []providers.ModelInfo
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
		stream:   true,
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

// SetBackends records the locally discovered model backends, enabling the
// /model command to switch between them.
func (t *TUI) SetBackends(backends []providers.ModelInfo) {
	t.backends = backends
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
	t.write(fmt.Sprintf("kabby v0.7.11 · backend: %s · mode: %s · model: %s\r\n", t.backend, t.mode, t.model))
	t.write("Type /help for commands, /exit to quit.\r\n")

	// Print the initial prompt.
	t.write("> ")

	for !t.quit {
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

// redrawLine clears the current line and re-prints the prompt + text.
// Used after backspace/delete so the display stays in sync.
func (t *TUI) redrawLine() {
	t.write("\r\033[K> " + t.edit.Text())
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
		if t.edit.Backspace() {
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
	if t.raw {
		t.write("\r\n> ")
	} else {
		t.write(fmt.Sprintf("\r\n> %s\r\n", line))
	}
	if line == ":q" {
		t.quit = true
		return
	}
	if strings.HasPrefix(line, "/") {
		t.handleCommand(line)
		return
	}

	// Send the prompt to the model via the agent.
	if t.agent != nil {
		if !t.stream {
			t.write("…\r\n")
		}
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
				t.write(fmt.Sprintf("\r\nerror: %v\r\n", result.Err))
				t.write("\r> ")
				return
			}
			sb.WriteString(result.Content)
			if t.stream {
				// Raw mode has OPOST off, so bare \n would staircase.
				t.write(strings.ReplaceAll(result.Content, "\n", "\r\n"))
			}
			if result.Done {
				break
			}
		}
		if t.stream {
			t.write("\r\n")
		} else {
			t.write(renderMarkdown(sb.String()) + "\r\n")
		}
	} else {
		t.write("(no model connected)\r\n")
	}
	t.write("> ")
}

// handleCommand dispatches a slash command (the line has already been
// confirmed to start with "/"). Unrecognized commands get an explicit
// "unknown command" reply instead of silently being sent to the model.
func (t *TUI) handleCommand(line string) {
	switch {
	case line == "/exit":
		t.quit = true
		return
	case line == "/help":
		t.write(helpText())
	case line == "/model" || strings.HasPrefix(line, "/model "):
		query := strings.TrimSpace(strings.TrimPrefix(line, "/model"))
		t.write(t.handleModelCommand(query))
	case line == "/stream" || strings.HasPrefix(line, "/stream "):
		arg := strings.TrimSpace(strings.TrimPrefix(line, "/stream"))
		t.write(t.handleStreamCommand(arg))
	default:
		t.write(fmt.Sprintf("unknown command: %s (try /help)\r\n", line))
	}
	t.write("> ")
}

// helpText lists the TUI's slash commands and keybindings.
func helpText() string {
	lines := []string{
		"Commands:",
		"  /help          show this message",
		"  /model         show the current model",
		"  /model <name>  switch to a discovered model by partial name match",
		"  /stream        toggle streaming replies (on by default); /stream on|off",
		"  /exit, :q      quit",
		"",
		"Keys:",
		"  shift+tab      cycle permission mode (ro -> edit -> bypass)",
		"  ctrl+c         quit",
		"  ctrl+l         clear screen",
		"  ctrl+u         clear the input line",
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// handleModelCommand shows or switches the active model. With no query it
// reports the current model; with a query it reuses the same partial-name
// matching as the --model flag (session.PrepareModel) against the locally
// discovered backends.
func (t *TUI) handleModelCommand(query string) string {
	if query == "" {
		if t.model == "" {
			return "(no model connected)\r\n"
		}
		return fmt.Sprintf("model: %s (backend: %s, %s)\r\n", t.model, t.backend, t.url)
	}

	chosen, err := session.PrepareModel(session.SessionPrefs{Model: query}, t.cfg, t.backends)
	if err != nil {
		return fmt.Sprintf("%v\r\n", err)
	}

	if chosen.Backend != t.backend || chosen.URL != t.url {
		t.agent = clientForBackend(chosen.Backend, chosen.URL)
	}
	t.model = chosen.ID
	t.backend = chosen.Backend
	t.url = chosen.URL
	return fmt.Sprintf("switched to model: %s (backend: %s)\r\n", t.model, t.backend)
}

// handleStreamCommand toggles streaming with no argument, or sets it
// explicitly with "on"/"off". Streaming prints tokens as they arrive; off
// waits for the full reply and renders its markdown.
func (t *TUI) handleStreamCommand(arg string) string {
	switch arg {
	case "":
		t.stream = !t.stream
	case "on":
		t.stream = true
	case "off":
		t.stream = false
	default:
		return "usage: /stream [on|off]\r\n"
	}
	if t.stream {
		return "streaming: on\r\n"
	}
	return "streaming: off\r\n"
}

// clientForBackend builds the chat client for a discovered backend.
func clientForBackend(backend, url string) agent.ChatClient {
	switch backend {
	case "lmstudio":
		return providers.NewLMStudio(url, providers.NewTransport())
	default:
		return providers.NewOllama(url, providers.NewTransport())
	}
}

// renderMarkdown converts basic markdown to plain text suitable for a terminal.
func renderMarkdown(md string) string {
	lines := strings.Split(md, "\n")
	var out []string
	inCodeBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Code block fences.
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			out = append(out, line)
			continue
		}
		// Horizontal rule.
		if strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "***" {
			out = append(out, "-----------------------------------------")
			continue
		}
		// Headers.
		if strings.HasPrefix(trimmed, "# ") {
			out = append(out, strings.TrimPrefix(trimmed, "# "))
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, strings.TrimPrefix(trimmed, "## "))
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			out = append(out, strings.TrimPrefix(trimmed, "### "))
			continue
		}
		if strings.HasPrefix(trimmed, "#### ") {
			out = append(out, strings.TrimPrefix(trimmed, "#### "))
			continue
		}
		// Bullet lists (check before stripping inline markers so * is preserved).
		if strings.HasPrefix(trimmed, "- ") {
			out = append(out, "  * "+stripInline(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		if strings.HasPrefix(trimmed, "* ") {
			out = append(out, "  * "+stripInline(strings.TrimPrefix(trimmed, "* ")))
			continue
		}
		// Strip inline formatting markers.
		out = append(out, stripInline(line))
	}
	// The TUI runs the terminal in raw mode, which disables the output
	// post-processing (OPOST) that would otherwise translate \n to \r\n.
	// A bare \n here only moves the cursor down a row without returning
	// it to column 0, so every line of a multi-line reply must be joined
	// with \r\n or it staircases to the right.
	return strings.Join(out, "\r\n")
}

// stripInline removes markdown emphasis/code markers from a line of text.
// Only matched delimiter pairs with markdown-style flanking are stripped,
// so literal text that merely contains the delimiter characters -- math
// like "2 * 3", identifiers like "my_variable_name" -- passes through
// unchanged instead of being mangled.
func stripInline(s string) string {
	s = stripDelimPairs(s, "`", false)
	s = stripDelimPairs(s, "**", false)
	s = stripDelimPairs(s, "__", true)
	s = stripDelimPairs(s, "*", false)
	s = stripDelimPairs(s, "_", true)
	return s
}

// isWordByte reports whether b is a letter, digit, or underscore.
func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// stripDelimPairs removes matched pairs of delim from s, keeping the
// enclosed content. A delimiter can only open/close when the character
// just inside it isn't whitespace (so "2 * 3" and "** bold **" are left
// alone). When wordBoundary is set (the underscore emphasis rule), the
// character just outside the delimiter additionally must not be a word
// character, so "my_variable_name" is never treated as emphasis.
func stripDelimPairs(s string, delim string, wordBoundary bool) string {
	var out strings.Builder
	n := len(s)
	dl := len(delim)
	i := 0
	for i < n {
		if i+dl <= n && s[i:i+dl] == delim {
			insideOK := i+dl < n && s[i+dl] != ' ' && s[i+dl] != '\t'
			outsideOK := !wordBoundary || i == 0 || !isWordByte(s[i-1])
			if insideOK && outsideOK {
				if closeIdx := findClose(s, delim, i+dl, wordBoundary); closeIdx != -1 {
					out.WriteString(s[i+dl : closeIdx])
					i = closeIdx + dl
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// findClose finds the next valid closing delimiter at or after from,
// returning its index, or -1 if none is found.
func findClose(s, delim string, from int, wordBoundary bool) int {
	n := len(s)
	dl := len(delim)
	for j := from; j+dl <= n; j++ {
		if s[j:j+dl] != delim {
			continue
		}
		insideOK := s[j-1] != ' ' && s[j-1] != '\t'
		outsideOK := !wordBoundary || j+dl == n || !isWordByte(s[j+dl])
		if insideOK && outsideOK {
			return j
		}
	}
	return -1
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
