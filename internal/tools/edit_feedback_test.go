package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newCtx(t *testing.T, workspace string) *ToolContext {
	t.Helper()
	return &ToolContext{Workspace: workspace, ResultCharLimit: TOOL_RESULT_CAP}
}

func writeFile(t *testing.T, workspace, rel, content string) {
	t.Helper()
	abs := filepath.Join(workspace, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEditFileSuccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "line one\nline two\nline three\n")

	ctx := newCtx(t, dir)
	out, err := EditFile(ctx, map[string]any{
		"path":     "a.txt",
		"old_text": "line two\n",
		"new_text": "line two edited\n",
		"signal":   nil,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "edited") {
		t.Fatalf("expected success message, got %q", out)
	}

	// Verify the file changed.
	readOut, err := ReadFile(ctx, map[string]any{"path": "a.txt", "offset": 0, "limit": 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readOut, "line two edited") {
		t.Fatalf("expected edited content, got %q", readOut)
	}
}

func TestEditFileNotFoundReturnsBoundedSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha\nbeta\ngamma\n")

	ctx := newCtx(t, dir)
	_, err := EditFile(ctx, map[string]any{
		"path":     "a.txt",
		"old_text": "delta\n",
		"new_text": "epsilon\n",
		"signal":   nil,
	}, nil)
	if err == nil {
		t.Fatal("expected error for not-found old_text, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not executed") {
		t.Fatalf("expected 'not executed' in error, got %q", msg)
	}
	if !strings.Contains(msg, "Surrounding source") {
		t.Fatalf("expected source range in error, got %q", msg)
	}
	if !strings.Contains(msg, "Suggestion") {
		t.Fatalf("expected guidance in error, got %q", msg)
	}
	// Bounded: should not repeat the whole file excessively.
	if strings.Count(msg, "alpha") > 3 {
		t.Fatalf("error not bounded: %q", msg)
	}
}

func TestEditFileMultipleMatchesRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "foo\nfoo\nfoo\n")

	ctx := newCtx(t, dir)
	_, err := EditFile(ctx, map[string]any{
		"path":     "a.txt",
		"old_text": "foo\n",
		"new_text": "bar\n",
		"signal":   nil,
	}, nil)
	if err == nil {
		t.Fatal("expected error for multiple matches, got nil")
	}
	if !strings.Contains(err.Error(), "appears") {
		t.Fatalf("expected ambiguity error, got %q", err.Error())
	}
}

func TestReadFilePaginates(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeFile(t, dir, "big.txt", strings.Join(lines, "\n")+"\n")

	ctx := newCtx(t, dir)
	// First page with limit 5 (continuation line appended since more remains).
	out, err := ReadFile(ctx, map[string]any{"path": "big.txt", "offset": 0, "limit": 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "line 0\nline 1\nline 2\nline 3\nline 4\n") {
		t.Fatalf("unexpected first page: %q", out)
	}
	if !strings.Contains(out, "continue with offset 5") {
		t.Fatalf("expected continuation line, got %q", out)
	}

	// Second page.
	out2, err := ReadFile(ctx, map[string]any{"path": "big.txt", "offset": 5, "limit": 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got2 := strings.Split(strings.TrimRight(out2, "\n"), "\n")
	if got2[0] != "line 5" || got2[4] != "line 9" {
		t.Fatalf("unexpected second page: %q", got2)
	}
}

func TestReadFileBeyondEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a\nb\n")

	ctx := newCtx(t, dir)
	out, err := ReadFile(ctx, map[string]any{"path": "a.txt", "offset": 100, "limit": 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nothing at offset") {
		t.Fatalf("expected out-of-range notice, got %q", out)
	}
}
