package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemPromptReadOnly(t *testing.T) {
	p := SystemPrompt("ro")
	if !strings.Contains(p, "read-only") {
		t.Fatalf("expected read-only mention, got %q", p)
	}
	if strings.Contains(p, "write_file") {
		t.Fatalf("should not mention write_file in ro mode, got %q", p)
	}
}

func TestSystemPromptEdit(t *testing.T) {
	p := SystemPrompt("edit")
	if !strings.Contains(p, "write_file") {
		t.Fatalf("expected write_file mention in edit mode, got %q", p)
	}
	if !strings.Contains(p, "edit_file") {
		t.Fatalf("expected edit_file mention in edit mode, got %q", p)
	}
}

func TestSystemPromptBypass(t *testing.T) {
	p := SystemPrompt("bypass")
	if !strings.Contains(p, "write_file") {
		t.Fatalf("expected write_file mention in bypass mode, got %q", p)
	}
}

func TestSystemPromptUnknownDefaultsToRO(t *testing.T) {
	p := SystemPrompt("unknown")
	if !strings.Contains(p, "read-only") {
		t.Fatalf("expected read-only default, got %q", p)
	}
}

func TestChatOnlyPrompt(t *testing.T) {
	p := ChatOnlyPrompt()
	if !strings.Contains(p, "tool") {
		t.Fatalf("expected chat-only mention of tools, got %q", p)
	}
}

func TestLoadAGENTSMDCap(t *testing.T) {
	dir := t.TempDir()
	longContent := strings.Repeat("x", 10000)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(longContent), 0o644); err != nil {
		t.Fatal(err)
	}
	content := LoadAGENTSMD(dir)
	if len(content) > AGENTSMaxChars+20 { // cap + "..." suffix
		t.Fatalf("expected AGENTS.md capped at %d, got %d", AGENTSMaxChars, len(content))
	}
	if !strings.HasSuffix(content, "... [truncated]") {
		t.Fatalf("expected truncation marker, got %q", content[len(content)-30:])
	}
}

func TestLoadAGENTSMDMissing(t *testing.T) {
	dir := t.TempDir()
	if content := LoadAGENTSMD(dir); content != "" {
		t.Fatalf("expected empty for missing AGENTS.md, got %q", content)
	}
}

func TestInjectAGENTSMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Project Notes\nDo X"), 0o644); err != nil {
		t.Fatal(err)
	}
	injected := InjectAGENTSMD("System prompt here", dir)
	if !strings.Contains(injected, "System prompt here") {
		t.Fatalf("expected system prompt preserved, got %q", injected)
	}
	if !strings.Contains(injected, "AGENTS.md:") {
		t.Fatalf("expected AGENTS.md section, got %q", injected)
	}
	if !strings.Contains(injected, "Do X") {
		t.Fatalf("expected AGENTS.md content, got %q", injected)
	}
}

func TestInjectAGENTSMDMissing(t *testing.T) {
	dir := t.TempDir()
	injected := InjectAGENTSMD("System prompt here", dir)
	if injected != "System prompt here" {
		t.Fatalf("expected unchanged prompt, got %q", injected)
	}
}
