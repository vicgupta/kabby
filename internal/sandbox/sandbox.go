// Package sandbox confines all file and command access to the workspace.
// Path resolution rejects traversal (.., absolute paths) and symlink escapes;
// resolution errors become tool-error strings, never crashes.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SandboxError is returned when a path or command would escape the workspace.
type SandboxError struct {
	Op     string
	Path   string
	Reason string
}

func (e *SandboxError) Error() string {
	return fmt.Sprintf("sandbox: %s: %s: %s", e.Op, e.Path, e.Reason)
}

// ResolveInWorkspace resolves rel against workspace, rejecting traversal and
// symlink escapes. The returned path is guaranteed to be inside workspace.
func ResolveInWorkspace(workspace, rel string) (string, error) {
	if rel == "" {
		return "", &SandboxError{Op: "resolve", Path: rel, Reason: "empty path"}
	}
	if filepath.IsAbs(rel) {
		return "", &SandboxError{Op: "resolve", Path: rel, Reason: "absolute paths are not allowed"}
	}
	cleanRel := filepath.Clean(rel)
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", &SandboxError{Op: "resolve", Path: rel, Reason: "parent-directory traversal (..) is not allowed"}
	}

	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", &SandboxError{Op: "resolve", Path: workspace, Reason: err.Error()}
	}
	// Resolve symlinks in the workspace itself so the boundary and all
	// candidates use the same real path (e.g. /tmp -> /private/tmp on macOS).
	realWorkspace, err := filepath.EvalSymlinks(workspaceAbs)
	if err != nil {
		// workspace may not exist yet; use the abs path as the boundary
		realWorkspace = workspaceAbs
	}

	// Walk each path component, resolving symlinks as we go, ensuring we never
	// leave the workspace boundary.
	parts := strings.Split(cleanRel, string(filepath.Separator))
	current := realWorkspace
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		if !under(current, realWorkspace) {
			return "", &SandboxError{Op: "resolve", Path: rel, Reason: "path escapes workspace"}
		}
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			current = resolved
			if !under(current, realWorkspace) {
				return "", &SandboxError{Op: "resolve", Path: rel, Reason: "symlink escapes workspace"}
			}
		}
	}

	return current, nil
}

// under reports whether path is dir or a descendant of dir.
func under(path, dir string) bool {
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// EnsureDir creates dir (and parents) if it does not exist, confined to the
// workspace boundary.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// CommandReachesOut classifies a command's text for reach-out patterns
// (home dir, other folders, global installs) that require approval in edit
// mode. It returns true and a human-readable reason when a reach-out is
// detected.
func CommandReachesOut(command string) (bool, string) {
	low := strings.ToLower(command)

	patterns := []struct {
		substr string
		reason string
	}{
		{"~", "references home directory (~) outside the workspace"},
		{"/home/", "references an absolute path outside the workspace"},
		{"/users/", "references an absolute path outside the workspace"},
		{"/etc/", "references a system directory outside the workspace"},
		{"/usr/local/", "references a system/global directory outside the workspace"},
		{"/opt/", "references a system directory outside the workspace"},
		{"npm install -g", "global install (npm install -g) outside the workspace"},
		{"npm install --global", "global install (npm install --global) outside the workspace"},
		{"pip install ", "package install may write outside the workspace"},
		{"sudo ", "elevated command (sudo) outside the workspace"},
		{"curl ", "network fetch (curl) outside the workspace"},
		{"wget ", "network fetch (wget) outside the workspace"},
	}

	for _, p := range patterns {
		if strings.Contains(low, p.substr) {
			return true, p.reason
		}
	}
	return false, ""
}
