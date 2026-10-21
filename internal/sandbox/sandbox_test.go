package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resolvedDir(t *testing.T, dir string) string {
	t.Helper()
	rd, err := filepath.EvalSymlinks(dir)
	if err != nil {
		rd = dir
	}
	return rd
}

func TestResolveInWorkspaceNormal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveInWorkspace(dir, "a.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base := resolvedDir(t, dir)
	if !strings.HasPrefix(got, base+string(filepath.Separator)) {
		t.Fatalf("got %q, not under %q", got, base)
	}
	if filepath.Base(got) != "a.txt" {
		t.Fatalf("got %q, base should be a.txt", got)
	}
}

func TestResolveInWorkspaceSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveInWorkspace(dir, "sub/deep/x.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base := resolvedDir(t, dir)
	if !strings.HasPrefix(got, base+string(filepath.Separator)) {
		t.Fatalf("got %q, not under %q", got, base)
	}
	if filepath.Base(got) != "x.txt" {
		t.Fatalf("got %q, base should be x.txt", got)
	}
}

func TestResolveInWorkspaceDot(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveInWorkspace(dir, "./a.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	base := resolvedDir(t, dir)
	if !strings.HasPrefix(got, base+string(filepath.Separator)) {
		t.Fatalf("got %q, not under %q", got, base)
	}
	if filepath.Base(got) != "a.txt" {
		t.Fatalf("got %q, base should be a.txt", got)
	}
}

func TestResolveRejectsParentTraversal(t *testing.T) {
	dir := t.TempDir()
	// Create a sibling outside the workspace
	sibling := filepath.Join(dir, "..", "outside.txt")
	if err := os.WriteFile(sibling, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInWorkspace(dir, "../outside.txt")
	if err == nil {
		t.Fatal("expected error for parent traversal, got nil")
	}
	if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveRejectsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveInWorkspace(dir, "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "target.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveInWorkspace(dir, "escape")
	if err == nil {
		t.Fatal("expected error for symlink escape, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") && !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveRejectsEmptyPath(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveInWorkspace(dir, "")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestSandboxErrorImplementsError(t *testing.T) {
	e := &SandboxError{Op: "resolve", Path: "x", Reason: "boom"}
	if e.Error() == "" {
		t.Fatal("Error() should not be empty")
	}
}
