// Package tools provides the four filesystem tools: read_file, list_files,
// write_file, edit_file. All outputs are bounded.
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kabby/internal/sandbox"
)

const (
	// TOOL_RESULT_CAP is the final cap on any single tool result (10k chars).
	TOOL_RESULT_CAP = 10000
	// DEFAULT_PAGE_LINES is the default number of lines read_file returns.
	DEFAULT_PAGE_LINES = 50
	// MAX_PAGE_LINES is the absolute max lines read_file returns in one call.
	MAX_PAGE_LINES = 200
)

// ReadFile reads a file page-by-page. Args: path, offset (line, default 0),
// limit (lines, default DEFAULT_PAGE_LINES, max MAX_PAGE_LINES).
// A continuation line is appended when more content remains.
func ReadFile(ctx *ToolContext, args map[string]any, signal <-chan struct{}) (string, error) {
	if signal != nil {
		select {
		case <-signal:
			return "", nil
		default:
		}
	}

	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("read_file: missing required argument 'path'")
	}

	abs, err := sandbox.ResolveInWorkspace(ctx.Workspace, path)
	if err != nil {
		return "", err
	}

	offset := 0
	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset = int(n)
		case int:
			offset = n
		case string:
			if parsed, perr := strconv.Atoi(n); perr == nil {
				offset = parsed
			}
		}
		if offset < 0 {
			offset = 0
		}
	}

	limit := DEFAULT_PAGE_LINES
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		case string:
			if parsed, perr := strconv.Atoi(n); perr == nil {
				limit = parsed
			}
		}
		if limit < 1 {
			limit = DEFAULT_PAGE_LINES
		}
		if limit > MAX_PAGE_LINES {
			limit = MAX_PAGE_LINES
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read_file: %s: %v", path, err)
	}

	lines := strings.Split(string(data), "\n")
	// strings.Split on "a\nb" gives ["a","b"]; on "a\nb\n" gives ["a","b",""].
	// Treat a trailing empty line from a final newline as non-existent.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	total := len(lines)
	if offset >= total {
		return fmt.Sprintf("(file %s: %d lines, nothing at offset %d)", path, total, offset), nil
	}

	end := offset + limit
	if end > total {
		end = total
	}
	page := strings.Join(lines[offset:end], "\n")

	var result strings.Builder
	result.WriteString(page)

	if end < total {
		result.WriteString(fmt.Sprintf("\n[... %d more lines; continue with offset %d ...]", total-end, end))
	}

	out := result.String()
	if len(out) > ctx.ResultCharLimit && ctx.ResultCharLimit > 0 {
		out = truncateMiddle(out, ctx.ResultCharLimit)
	}
	return out, nil
}

// ListFiles lists a directory. Args: path. Folders end with "/".
func ListFiles(ctx *ToolContext, args map[string]any, signal <-chan struct{}) (string, error) {
	if signal != nil {
		select {
		case <-signal:
			return "", nil
		default:
		}
	}

	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	abs, err := sandbox.ResolveInWorkspace(ctx.Workspace, path)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", fmt.Errorf("list_files: %s: %v", path, err)
	}

	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}

	sort.Strings(names)

	out := strings.Join(names, "\n")
	if out == "" {
		out = fmt.Sprintf("(directory %s is empty)", path)
	}
	if len(out) > ctx.ResultCharLimit && ctx.ResultCharLimit > 0 {
		out = truncateMiddle(out, ctx.ResultCharLimit)
	}
	return out, nil
}

// WriteFile creates or overwrites a file, creating parent directories.
// Args: path, content.
func WriteFile(ctx *ToolContext, args map[string]any, signal <-chan struct{}) (string, error) {
	if signal != nil {
		select {
		case <-signal:
			return "", nil
		default:
		}
	}

	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("write_file: missing required argument 'path'")
	}
	content, _ := args["content"].(string)

	abs, err := sandbox.ResolveInWorkspace(ctx.Workspace, path)
	if err != nil {
		return "", err
	}

	if err := sandbox.EnsureDir(filepath.Dir(abs)); err != nil {
		return "", fmt.Errorf("write_file: %s: %v", path, err)
	}

	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %s: %v", path, err)
	}

	return fmt.Sprintf("wrote %s (%d bytes)", path, len(content)), nil
}

// EditFile replaces an exact old_text occurrence with new_text.
// Args: path, old_text, new_text.
// On failure returns a bounded source range with guidance.
func EditFile(ctx *ToolContext, args map[string]any, signal <-chan struct{}) (string, error) {
	if signal != nil {
		select {
		case <-signal:
			return "", nil
		default:
		}
	}

	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("edit_file: missing required argument 'path'")
	}
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)

	abs, err := sandbox.ResolveInWorkspace(ctx.Workspace, path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("edit_file: %s: %v", path, err)
	}

	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return editFileNotFound(path, content, oldText)
	}
	if count > 1 {
		return "", fmt.Errorf("edit_file: %s: old_text appears %d times; make old_text more specific to target one occurrence", path, count)
	}

	replaced := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(abs, []byte(replaced), 0o644); err != nil {
		return "", fmt.Errorf("edit_file: %s: %v", path, err)
	}

	return fmt.Sprintf("edited %s (replaced %d bytes with %d bytes)", path, len(oldText), len(newText)), nil
}

// editFileNotFound returns a bounded source-range error with guidance.
func editFileNotFound(path, content, oldText string) (string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Try to find the first line of oldText to show surrounding context.
	oldLines := strings.Split(oldText, "\n")
	firstOld := oldLines[0]

	start := 0
	for i, line := range lines {
		if strings.Contains(line, firstOld) {
			start = i
			break
		}
	}

	// Show a bounded window around the candidate location.
	const contextLines = 8
	lo := start - contextLines/2
	if lo < 0 {
		lo = 0
	}
	hi := start + contextLines/2 + 1
	if hi > len(lines) {
		hi = len(lines)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("edit_file: %s: old_text not found; not executed.\n", path))
	sb.WriteString(fmt.Sprintf("Surrounding source (lines %d-%d):\n", lo+1, hi))
	for i := lo; i < hi; i++ {
		sb.WriteString(fmt.Sprintf(" %5d: %s\n", i+1, lines[i]))
	}
	sb.WriteString("Suggestion: read the file in parts (read_file with offset/limit) and retry with an exact old_text, including whitespace.\n")

	msg := sb.String()
	if len(msg) > TOOL_RESULT_CAP {
		msg = truncateMiddle(msg, TOOL_RESULT_CAP)
	}
	return "", fmt.Errorf("%s", msg)
}

// truncateMiddle keeps head + ellipsis + tail within cap runes.
func truncateMiddle(s string, cap int) string {
	runes := []rune(s)
	if len(runes) <= cap {
		return s
	}
	if cap <= 1 {
		return string(runes[:cap])
	}
	keep := cap - 1
	half := keep / 2
	tailStart := len(runes) - (keep - half)
	if tailStart < half {
		tailStart = half
	}
	return string(runes[:half]) + "…" + string(runes[tailStart:])
}
