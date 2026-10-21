package tools

import (
	"strings"
	"testing"

	"kabby/internal/providers"
)

func ToolNames(specs []providers.ToolSpec) []string {
	var out []string
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

func TestReadOnlyModeExactlyFourTools(t *testing.T) {
	specs := BuildToolSpecs(providers.ModeReadOnly)
	if len(specs) != 4 {
		t.Fatalf("expected 4 tools in ro mode, got %d: %v", len(specs), ToolNames(specs))
	}
	expected := []string{"read_file", "list_files", "search", "plan"}
	for _, name := range expected {
		found := false
		for _, s := range specs {
			if s.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected tool %q in ro mode, got %v", name, ToolNames(specs))
		}
	}
}

func TestEditModeEightTools(t *testing.T) {
	specs := BuildToolSpecs(providers.ModeEdit)
	if len(specs) != 8 {
		t.Fatalf("expected 8 tools in edit mode, got %d: %v", len(specs), ToolNames(specs))
	}
	expected := []string{"read_file", "list_files", "search", "plan", "write_file", "edit_file", "run_command", "task"}
	for _, name := range expected {
		found := false
		for _, s := range specs {
			if s.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected tool %q in edit mode, got %v", name, ToolNames(specs))
		}
	}
}

func TestBypassModeEightTools(t *testing.T) {
	specs := BuildToolSpecs(providers.ModeBypass)
	if len(specs) != 8 {
		t.Fatalf("expected 8 tools in bypass mode, got %d: %v", len(specs), ToolNames(specs))
	}
}

func TestToolSpecsHaveDescriptionsAndExamples(t *testing.T) {
	specs := BuildToolSpecs(providers.ModeEdit)
	for _, s := range specs {
		if s.Description == "" {
			t.Fatalf("tool %q has empty description", s.Name)
		}
		if !strings.Contains(s.Description, "Example") {
			t.Fatalf("tool %q description missing example call", s.Name)
		}
		if s.Parameters == nil {
			t.Fatalf("tool %q has nil parameters", s.Name)
		}
	}
}

func TestExecuteToolUnknown(t *testing.T) {
	ctx := &ToolContext{Workspace: t.TempDir()}
	_, err := ExecuteTool("nonexistent", nil, ctx, nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
