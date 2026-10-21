package tools

import (
	"fmt"

	"kabby/internal/providers"
)

// BuildToolSpecs returns the four read-only tools for ro mode, plus the four
// write/exec tools for edit/bypass mode.
func BuildToolSpecs(mode providers.Mode) []providers.ToolSpec {
	specs := []providers.ToolSpec{
		{
			Name:        "read_file",
			Description: "Read a portion of a file. Example: read_file(path=\"src/main.go\", offset=0, limit=50). Returns file contents with a continuation line if more remains.",
			Parameters: map[string]any{
				"path":   "string (required) - file path relative to workspace",
				"offset": "number (optional, default 0) - starting line number",
				"limit":  "number (optional, default 50, max 200) - lines to read",
			},
		},
		{
			Name:        "list_files",
			Description: "List a directory. Example: list_files(path=\"src\"). Folders end with '/'.",
			Parameters: map[string]any{
				"path": "string (optional, default \".\") - directory path relative to workspace",
			},
		},
		{
			Name:        "search",
			Description: "Search for text in files. Example: search(pattern=\"func main\", path=\"src\"). Bounded results.",
			Parameters: map[string]any{
				"pattern": "string (required) - text or regex to search for",
				"path":    "string (optional, default \".\") - directory to search",
			},
		},
		{
			Name:        "plan",
			Description: "Manage the agent plan. Example: plan(action=\"set\", steps=\"write a function; add tests\"). Actions: set, done, add, show, checkpoint.",
			Parameters: map[string]any{
				"action": "string (required) - set|done|add|show|checkpoint",
				"steps":  "string (optional) - newline or semicolon separated steps",
				"note":   "string (optional, 1-1000 chars) - checkpoint note",
				"step":   "number (optional) - step number for done/checkpoint",
			},
		},
	}

	if mode == providers.ModeEdit || mode == providers.ModeBypass {
		specs = append(specs, []providers.ToolSpec{
			{
				Name:        "write_file",
				Description: "Create or overwrite a file. Example: write_file(path=\"src/foo.go\", content=\"package main\\n\"). Creates parent directories.",
				Parameters: map[string]any{
					"path:":   "string (required) - file path relative to workspace",
					"content": "string (required) - file contents",
				},
			},
			{
				Name:        "edit_file",
				Description: "Replace exact old_text with new_text. Example: edit_file(path=\"src/foo.go\", old_text=\"foo\", new_text=\"bar\"). Fails if old_text is not found or appears multiple times.",
				Parameters: map[string]any{
					"path":     "string (required) - file path relative to workspace",
					"old_text": "string (required) - exact text to replace",
					"new_text": "string (required) - replacement text",
				},
			},
			{
				Name:        "run_command",
				Description: "Run a shell command inside the workspace. Example: run_command(command=\"go test ./...\"). Waits up to 120s.",
				Parameters: map[string]any{
					"command": "string (required) - shell command to run",
				},
			},
			{
				Name:        "task",
				Description: "Start a background process. Example: task(command=\"npm run dev\"). Returns a task ID for logs/stop.",
				Parameters: map[string]any{
					"command": "string (required) - shell command to run in background",
				},
			},
		}...)
	}
	return specs
}

// ExecuteTool dispatches a tool call by name. signal allows cancellation.
func ExecuteTool(name string, args map[string]any, ctx *ToolContext, signal <-chan struct{}) (string, error) {
	switch name {
	case "read_file":
		return ReadFile(ctx, args, signal)
	case "list_files":
		return ListFiles(ctx, args, signal)
	case "write_file":
		return WriteFile(ctx, args, signal)
	case "edit_file":
		return EditFile(ctx, args, signal)
	case "run_command":
		command, _ := args["command"].(string)
		return RunCommand(command, ctx.Workspace, signal)
	case "task":
		command, _ := args["command"].(string)
		if ctx.TaskManager == nil {
			return "", fmt.Errorf("task: task manager not available")
		}
		return ctx.TaskManager.Start(command, ctx.Workspace)
	case "search":
		return Search(ctx, args, signal)
	case "plan":
		return ExecutePlan(ctx, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// ExecutePlan handles the plan tool actions.
func ExecutePlan(ctx *ToolContext, args map[string]any) (string, error) {
	if ctx.Plan == nil {
		return "", fmt.Errorf("plan: plan not available")
	}
	action, _ := args["action"].(string)
	switch action {
	case "set":
		steps, _ := args["steps"].(string)
		return ctx.Plan.Set(steps), nil
	case "done":
		var stepNum *int
		if v, ok := args["step"]; ok {
			switch n := v.(type) {
			case float64:
				i := int(n)
				stepNum = &i
			case int:
				stepNum = &n
			case string:
				if parsed, perr := parseAtoiStr(v.(string)); perr == nil {
					stepNum = &parsed
				}
			}
		}
		return ctx.Plan.MarkDone(stepNum), nil
	case "add":
		text, _ := args["text"].(string)
		if text == "" {
			text, _ = args["steps"].(string)
		}
		return ctx.Plan.Add(text), nil
	case "show":
		return ctx.Plan.Show(), nil
	case "checkpoint":
		note, _ := args["note"].(string)
		return ctx.Plan.Checkpoint(note), nil
	default:
		return "", fmt.Errorf("plan: unknown action '%s'. Use set, done, add, show, or checkpoint", action)
	}
}

func parseAtoiStr(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// AfterWrite runs the post-write syntax check for supported file types.
// Returns a suffix message to append to the write/edit result, or "" if clean.
func AfterWrite(path, content string) string {
	if err := CheckSyntax(path, content); err != nil {
		return fmt.Sprintf("  %s", err)
	}
	return ""
}

// CommandOf extracts the shell command string from a tool's args, for the
// approval gate. Returns nil if the tool isn't a command tool.
func CommandOf(name string, args map[string]any) *string {
	switch name {
	case "run_command":
		if cmd, ok := args["command"].(string); ok {
			return &cmd
		}
	case "task":
		if cmd, ok := args["command"].(string); ok {
			return &cmd
		}
	}
	return nil
}
