package tools

import (
	"fmt"

	"kabby/internal/providers"
)

// prop is one flat JSON Schema property.
func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// schema builds a flat JSON Schema object (no nested objects or arrays).
func schema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

// readTools and writeTools are the two tiers. Every description carries an
// example call, which small models follow far more reliably than prose.
var readTools = []providers.ToolSpec{
	{
		Name:        "read_file",
		Description: "Read a portion of a file. Example: read_file(path=\"src/main.go\", offset=0, limit=50). Returns file contents with a continuation line if more remains.",
		Parameters: schema(map[string]any{
			"path":   prop("string", "file path relative to workspace"),
			"offset": prop("integer", "starting line number (default 0)"),
			"limit":  prop("integer", "lines to read (default 50, max 200)"),
		}, "path"),
	},
	{
		Name:        "list_files",
		Description: "List a directory. Example: list_files(path=\"src\"). Folders end with '/'.",
		Parameters: schema(map[string]any{
			"path": prop("string", "directory path relative to workspace (default \".\")"),
		}),
	},
	{
		Name:        "search",
		Description: "Search for text in files. Example: search(pattern=\"func main\", path=\"src\"). Bounded results.",
		Parameters: schema(map[string]any{
			"pattern": prop("string", "text or regex to search for"),
			"path":    prop("string", "directory to search (default \".\")"),
		}, "pattern"),
	},
	{
		Name:        "plan",
		Description: "Manage the agent plan. Example: plan(action=\"set\", steps=\"write a function; add tests\"). Actions: set, done, add, show, checkpoint.",
		Parameters: schema(map[string]any{
			"action": prop("string", "set|done|add|show|checkpoint"),
			"steps":  prop("string", "newline or semicolon separated steps (set, add)"),
			"note":   prop("string", "checkpoint note, 1-1000 chars"),
			"step":   prop("integer", "step number for done (default: current step)"),
		}, "action"),
	},
}

var writeTools = []providers.ToolSpec{
	{
		Name:        "write_file",
		Description: "Create or overwrite a file. Example: write_file(path=\"src/foo.go\", content=\"package main\\n\"). Creates parent directories.",
		Parameters: schema(map[string]any{
			"path":    prop("string", "file path relative to workspace"),
			"content": prop("string", "file contents"),
		}, "path", "content"),
	},
	{
		Name:        "edit_file",
		Description: "Replace exact old_text with new_text. Example: edit_file(path=\"src/foo.go\", old_text=\"foo\", new_text=\"bar\"). Fails if old_text is not found or appears multiple times.",
		Parameters: schema(map[string]any{
			"path":     prop("string", "file path relative to workspace"),
			"old_text": prop("string", "exact text to replace"),
			"new_text": prop("string", "replacement text"),
		}, "path", "old_text", "new_text"),
	},
	{
		Name:        "run_command",
		Description: "Run a shell command inside the workspace. Example: run_command(command=\"go test ./...\"). Waits up to 120s.",
		Parameters: schema(map[string]any{
			"command": prop("string", "shell command to run"),
		}, "command"),
	},
	{
		Name:        "task",
		Description: "Start a background process. Example: task(command=\"npm run dev\"). Returns a task ID for logs/stop.",
		Parameters: schema(map[string]any{
			"command": prop("string", "shell command to run in background"),
		}, "command"),
	},
}

// BuildToolSpecs returns the four read-only tools for ro mode, plus the four
// write/exec tools for edit/bypass mode.
func BuildToolSpecs(mode providers.Mode) []providers.ToolSpec {
	specs := append([]providers.ToolSpec{}, readTools...)
	if mode == providers.ModeEdit || mode == providers.ModeBypass {
		specs = append(specs, writeTools...)
	}
	return specs
}

// IsWriteTool reports whether name writes files or runs commands, and so is
// refused in ro mode.
func IsWriteTool(name string) bool {
	for _, t := range writeTools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// ValidateArgs checks a call's arguments against the tool's schema: required
// parameters present and every value of its declared type. It returns a
// message for the model, or "" when the call is valid.
func ValidateArgs(name string, args map[string]any) string {
	var spec *providers.ToolSpec
	for _, list := range [][]providers.ToolSpec{readTools, writeTools} {
		for i := range list {
			if list[i].Name == name {
				spec = &list[i]
			}
		}
	}
	if spec == nil {
		return fmt.Sprintf("unknown tool %q", name)
	}
	sch := spec.Parameters.(map[string]any)
	for _, req := range sch["required"].([]string) {
		if _, ok := args[req]; !ok {
			return fmt.Sprintf("%s needs %q. %s", name, req, spec.Description)
		}
	}
	for key, v := range args {
		p, ok := sch["properties"].(map[string]any)[key].(map[string]any)
		if !ok {
			continue // extra arguments are ignored
		}
		switch p["type"] {
		case "string":
			if _, ok := v.(string); !ok {
				return fmt.Sprintf("%s: %q must be a string", name, key)
			}
		case "integer":
			switch n := v.(type) {
			case float64:
				if n != float64(int(n)) {
					return fmt.Sprintf("%s: %q must be a whole number", name, key)
				}
			case int, string:
				// strings like "3" are accepted by the tools themselves
			default:
				return fmt.Sprintf("%s: %q must be a number", name, key)
			}
		}
	}
	return ""
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
