package tools

import (
	"strings"
	"testing"
)

func TestCheckSyntaxGo(t *testing.T) {
	if err := CheckSyntax("foo.go", "package main\nfunc main() {\n}\n"); err != nil {
		t.Fatalf("expected valid Go, got %v", err)
	}
	err := CheckSyntax("foo.go", "package main\nfunc main() {\n")
	if err == nil {
		t.Fatal("expected syntax error for unclosed brace")
	}
	if !strings.Contains(err.Error(), "line") {
		t.Fatalf("expected line number in error, got %v", err)
	}
}

func TestCheckSyntaxJSON(t *testing.T) {
	if err := CheckSyntax("foo.json", `{"a": 1}`); err != nil {
		t.Fatalf("expected valid JSON, got %v", err)
	}
	err := CheckSyntax("foo.json", `{"a": 1`)
	if err == nil {
		t.Fatal("expected syntax error for unclosed JSON")
	}
}

func TestCheckSyntaxJS(t *testing.T) {
	if err := CheckSyntax("foo.js", "const x = 1;\nif (x) { console.log(x); }\n"); err != nil {
		t.Fatalf("expected valid JS, got %v", err)
	}
	err := CheckSyntax("foo.js", "const x = 1;\nif (x) { console.log(x);\n")
	if err == nil {
		t.Fatal("expected syntax error for unclosed brace")
	}
}

func TestCheckSyntaxHTML(t *testing.T) {
	if err := CheckSyntax("foo.html", "<div>hello</div>"); err != nil {
		t.Fatalf("expected valid HTML, got %v", err)
	}
	err := CheckSyntax("foo.html", "<div>hello</div><p")
	if err == nil {
		t.Fatal("expected syntax error for unclosed tag")
	}
}

func TestCheckSyntaxPython(t *testing.T) {
	if err := CheckSyntax("foo.py", "def foo():\n    return 1\n"); err != nil {
		t.Fatalf("expected valid Python, got %v", err)
	}
	err := CheckSyntax("foo.py", "def foo():\n    return 1\n(")
	if err == nil {
		t.Fatal("expected syntax error for unclosed paren")
	}
}

func TestCheckSyntaxUnsupported(t *testing.T) {
	if err := CheckSyntax("foo.txt", "anything at all"); err != nil {
		t.Fatalf("expected no check for .txt, got %v", err)
	}
}
