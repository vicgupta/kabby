// Package prompt builds the system prompt per mode and injects AGENTS.md.
package prompt

import (
	"os"
	"path/filepath"
)

// AGENTSMaxChars is the cap on AGENTS.md content injected into message[0].
const AGENTSMaxChars = 8000

// SystemPrompt returns the short system prompt for the given mode.
func SystemPrompt(mode string) string {
	switch mode {
	case "edit", "bypass":
		return editSystemPrompt()
	case "ro":
		return readOnlySystemPrompt()
	default:
		return readOnlySystemPrompt()
	}
}

func readOnlySystemPrompt() string {
	return `You are a helpful coding agent working in the user's local project.

You have read-only access to files. Use read_file, list_files, search, and plan to understand the codebase. You cannot write files or run commands.

Keep answers concise. Use tools when you need information about the project.`
}

func editSystemPrompt() string {
	return `You are a helpful coding agent working in the user's local project.

You can read and edit files and run commands inside the workspace. Use read_file, list_files, search, plan, write_file, edit_file, run_command, and task.

Prefer small, targeted edits. After writing code, run project checks (build, test) when available. Report results clearly and concisely.`
}

// ChatOnlyPrompt returns the system prompt for models without tool support.
func ChatOnlyPrompt() string {
	return `You are a helpful coding assistant. You do not have tool access, so answer directly based on your knowledge.`
}

// LoadAGENTSMD reads AGENTS.md from the workspace root, capped at AGENTSMaxChars.
func LoadAGENTSMD(workspace string) string {
	path := filepath.Join(workspace, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > AGENTSMaxChars {
		content = content[:AGENTSMaxChars] + "\n... [truncated]"
	}
	return content
}

// InjectAGENTSMD prepends the AGENTS.md content (capped) after the system
// prompt text, separated by a blank line.
func InjectAGENTSMD(systemPrompt, workspace string) string {
	agents := LoadAGENTSMD(workspace)
	if agents == "" {
		return systemPrompt
	}
	return systemPrompt + "\n\n---\nAGENTS.md:\n" + agents + "\n---"
}

// Ensure filepath is referenced (used in LoadAGENTSMD).
