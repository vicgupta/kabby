package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"

	"kabby/internal/agent"
	"kabby/internal/config"
	kctx "kabby/internal/context"
	"kabby/internal/plan"
	"kabby/internal/prompt"
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
	info     providers.ModelInfo
	client   agent.ChatClient
	agent    *agent.Agent
	backends []providers.ModelInfo
	ctxMgr   *kctx.Manager
	toolsCtx *tools.ToolContext
	plan     *plan.Plan
	cfg      config.Config

	raw      bool // whether raw mode is active
	oldState *term.State

	// keys carries raw input while Run's event loop is active; nil when
	// keys are fed through handleKey directly (tests), in which case turns
	// run synchronously and reach-out commands are denied.
	keys      chan []byte
	approvals chan approvalRequest

	outMu       sync.Mutex
	atLineStart bool
}

// approvalRequest asks the user, from the turn goroutine, to approve a
// command that reaches outside the workspace.
type approvalRequest struct {
	command, reason string
	reply           chan bool
}

// NewTUI creates a TUI reading from in and writing to out, connected to the
// given model/backend and chat client.
func NewTUI(in io.Reader, out io.Writer, model, backend, url string, client agent.ChatClient, cfg config.Config) *TUI {
	t := &TUI{
		in:          in,
		out:         out,
		edit:        NewEditor(),
		mode:        "edit",
		stream:      true,
		plan:        plan.New(),
		cfg:         cfg,
		atLineStart: true,
	}
	t.toolsCtx = &tools.ToolContext{
		Workspace:       os.Getenv("PWD"),
		ResultCharLimit: 10000,
		TaskManager:     tools.NewTaskManager(),
		Plan:            t.plan,
	}
	t.ctxMgr = kctx.NewManager(4096)
	t.agent = agent.NewAgent(client, t.ctxMgr, t.plan, t.toolsCtx, cfg)
	t.agent.Observer = agent.Observer{
		OnText:       t.onText,
		OnToolCall:   t.onToolCall,
		OnToolResult: t.onToolResult,
		OnNotice:     func(s string) { t.writeLine(s) },
		Approve:      t.approve,
	}
	t.SetModel(providers.ModelInfo{ID: model, Name: model, Backend: backend, URL: url, ToolsSupported: true, ContextWindow: 4096}, client)
	return t
}

// SetModel points the session at a model and its chat client. The
// conversation is kept, so a switch continues where it left off.
func (t *TUI) SetModel(m providers.ModelInfo, client agent.ChatClient) {
	t.info = m
	t.model, t.backend, t.url = m.ID, m.Backend, m.URL
	t.client = client
	window := m.ContextWindow
	if window <= 0 {
		window = 4096
	}
	t.ctxMgr = kctx.NewManager(window)
	t.agent.CtxManager = t.ctxMgr
	t.agent.Client = client
	t.agent.Model = m.ID
	t.agent.Backend = m.Backend
	t.agent.ChatOnly = !m.ToolsSupported
	t.agent.StablePrefix = t.agent.StablePrefix || m.Cloud
}

// SetMode sets the starting permission mode ("" keeps edit).
func (t *TUI) SetMode(mode string) {
	if mode != "" {
		t.mode = mode
	}
}

// SetEffort sets the reasoning effort sent with every request.
func (t *TUI) SetEffort(e providers.Effort) {
	t.agent.Effort = e
}

// SetWorkspace sets the folder the tools are confined to.
func (t *TUI) SetWorkspace(dir string) {
	t.toolsCtx.Workspace = dir
}

// SetBackends records the discovered models (local and hosted), enabling
// the /model command to switch between them.
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

	// Handle signals to restore terminal and stop background tasks.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigCh
		if t.raw && t.oldState != nil {
			term.Restore(int(fd(t.in)), t.oldState)
		}
		t.toolsCtx.TaskManager.KillAll()
		os.Exit(130)
	}()
	defer t.toolsCtx.TaskManager.KillAll()

	// A reader goroutine feeds keys so input stays live while a turn runs
	// (esc cancels it, y/n answers approval prompts).
	t.keys = make(chan []byte)
	t.approvals = make(chan approvalRequest)
	go func() {
		for {
			buf := make([]byte, 64)
			n, err := t.in.Read(buf)
			if n > 0 {
				t.keys <- buf[:n]
			}
			if err != nil {
				close(t.keys)
				return
			}
		}
	}()

	// Show startup banner (no screen clearing, just append).
	t.write(fmt.Sprintf("kabby v0.8.0 · backend: %s · mode: %s · model: %s\r\n", t.backend, t.mode, t.model))
	if t.info.Cloud {
		t.write(fmt.Sprintf("note: %s is a hosted model; prompts and file contents are sent to %s.\r\n", t.model, t.url))
	}
	t.write("Type /help for commands, /exit to quit.\r\n")

	// Print the initial prompt.
	t.write("> ")

	for !t.quit {
		data, ok := <-t.keys
		if !ok {
			return nil
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

// write writes to the output. It is safe to call from the turn goroutine.
func (t *TUI) write(s string) {
	if s == "" {
		return
	}
	t.outMu.Lock()
	defer t.outMu.Unlock()
	io.WriteString(t.out, s)
	t.atLineStart = strings.HasSuffix(s, "\n")
}

// writeLine writes s on a line of its own.
func (t *TUI) writeLine(s string) {
	t.outMu.Lock()
	start := t.atLineStart
	t.outMu.Unlock()
	if !start {
		s = "\r\n" + s
	}
	t.write(s + "\r\n")
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
		t.write("\r\n")
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

	if t.client == nil {
		t.write("(no model connected)\r\n> ")
		return
	}
	t.runTurn(line)
	t.write("> ")
}

// systemPrompt returns message[0] for the current model and mode. Hosted
// models keep one prompt for the whole conversation (mode changes are
// announced by the agent instead).
func (t *TUI) systemPrompt() string {
	var sys string
	switch {
	case !t.info.ToolsSupported:
		sys = prompt.ChatOnlyPrompt()
	case t.agent.StablePrefix:
		sys = prompt.SystemPrompt("edit")
	default:
		sys = prompt.SystemPrompt(t.mode)
	}
	return prompt.InjectAGENTSMD(sys, t.toolsCtx.Workspace)
}

type turnResult struct {
	answer string
	err    error
}

// runTurn sends one prompt through the agent loop. With the event loop
// running, the turn runs in a goroutine while keys stay live: esc or ctrl+c
// cancel it, y/n answer approval prompts.
func (t *TUI) runTurn(line string) {
	t.agent.Config.LastMode = t.mode
	if !t.stream {
		t.write("…\r\n")
	}
	sys := t.systemPrompt()

	if t.keys == nil {
		answer, err := t.agent.RunTurn(context.Background(), sys, line)
		t.finishTurn(answer, err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan turnResult, 1)
	go func() {
		answer, err := t.agent.RunTurn(ctx, sys, line)
		done <- turnResult{answer, err}
	}()

	var pending *approvalRequest
	for {
		select {
		case r := <-done:
			t.finishTurn(r.answer, r.err)
			return
		case req := <-t.approvals:
			pending = &req
			t.writeLine(fmt.Sprintf("approve? %s\r\n  %s\r\n  [y/N] ", req.reason, req.command))
		case data, ok := <-t.keys:
			if !ok {
				cancel()
				r := <-done
				t.finishTurn(r.answer, r.err)
				t.quit = true
				return
			}
			for len(data) > 0 {
				var key Key
				key, data, _ = ParseKey(data)
				if pending != nil {
					yes := key.Rune == 'y' || key.Rune == 'Y'
					if yes {
						t.write("yes\r\n")
					} else {
						t.write("no\r\n")
					}
					pending.reply <- yes
					pending = nil
					continue
				}
				if key.Code == "escape" || key.IsCtrlC() {
					cancel()
					t.writeLine("(cancelling…)")
				}
			}
		}
	}
}

// approve asks the user to allow a reach-out command. It runs on the turn
// goroutine and blocks until runTurn's loop collects the answer.
func (t *TUI) approve(command, reason string) bool {
	if t.approvals == nil {
		return false
	}
	reply := make(chan bool)
	t.approvals <- approvalRequest{command: command, reason: reason, reply: reply}
	return <-reply
}

func (t *TUI) onText(s string) {
	if t.stream {
		// Raw mode has OPOST off, so bare \n would staircase.
		t.write(strings.ReplaceAll(s, "\n", "\r\n"))
	}
}

func (t *TUI) onToolCall(name string, args map[string]any) {
	t.writeLine(dim("· " + name + " " + argSummary(args)))
}

func (t *TUI) onToolResult(name, result string, failed bool) {
	if failed {
		first, _, _ := strings.Cut(result, "\n")
		t.writeLine(dim("  " + truncate(first, 160)))
	}
}

// finishTurn prints the end of a turn: the rendered answer when streaming
// is off, any error, and the turn's stats.
func (t *TUI) finishTurn(answer string, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		t.writeLine("cancelled")
	case err != nil:
		t.writeLine(fmt.Sprintf("error: %v", err))
	case t.stream:
		if !t.atLineStart {
			t.write("\r\n")
		}
	default:
		t.write(renderMarkdown(answer) + "\r\n")
	}
	s := t.agent.LastStats
	if err == nil && (s.ToolsCalled > 0 || s.OutputTokens > 0) {
		t.write(dim(fmt.Sprintf("%d tools · %d tokens · %.1f tok/s", s.ToolsCalled, s.TotalTokens, s.TokensPerSec)) + "\r\n")
	}
}

// argSummary picks the argument that identifies a tool call.
func argSummary(args map[string]any) string {
	for _, k := range []string{"command", "path", "pattern", "action"} {
		if v, ok := args[k].(string); ok && v != "" {
			return truncate(v, 100)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func dim(s string) string { return "\033[2m" + s + "\033[0m" }

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
	case line == "/clear":
		t.agent.Reset()
		t.plan.Reset()
		t.write("conversation cleared\r\n")
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
		"  /model         show the current model and list the others",
		"  /model <name>  switch model by partial name, or anthropic/<id>, openai/<id>",
		"  /stream        toggle streaming replies (on by default); /stream on|off",
		"  /clear         start a new conversation",
		"  /exit, :q      quit",
		"",
		"Keys:",
		"  shift+tab      cycle permission mode (ro -> edit -> bypass)",
		"  esc            cancel the running turn, else clear the input",
		"  ctrl+c         cancel the running turn, else quit",
		"  ctrl+l         clear screen",
		"  ctrl+u         clear the input line",
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

// handleModelCommand shows or switches the active model. With no query it
// reports the current model and lists the others; with a query it reuses
// the --model matching (session.PrepareModel), which also accepts
// "anthropic/<id>" and "openai/<id>".
func (t *TUI) handleModelCommand(query string) string {
	if query == "" {
		if t.model == "" {
			return "(no model connected)\r\n"
		}
		out := fmt.Sprintf("model: %s (backend: %s, %s)\r\n", t.model, t.backend, t.url)
		for _, b := range t.backends {
			marker := "  "
			if b.ID == t.model && b.URL == t.url {
				marker = "* "
			}
			out += fmt.Sprintf("%s%s (%s)\r\n", marker, b.Name, b.Backend)
		}
		return out
	}

	chosen, err := session.PrepareModel(session.SessionPrefs{Model: query}, t.cfg, t.backends)
	if err != nil {
		return fmt.Sprintf("%v\r\n", err)
	}

	client := t.client
	if chosen.Backend != t.backend || chosen.URL != t.url || client == nil {
		client = session.NewClient(*chosen, t.cfg)
	}
	t.SetModel(*chosen, client)
	msg := fmt.Sprintf("switched to model: %s (backend: %s)\r\n", t.model, t.backend)
	if chosen.Cloud {
		msg += fmt.Sprintf("note: hosted model; prompts and file contents are sent to %s.\r\n", chosen.URL)
	}
	return msg
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
