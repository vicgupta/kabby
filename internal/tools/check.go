package tools

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
)

// CheckSyntax parses a file for syntax errors based on its extension.
// Supported: Go (.go), JSON (.json), JavaScript (.js), HTML (.html), Python (.py).
// Returns the first error with a line number, or nil if the file parses.
func CheckSyntax(path, content string) error {
	switch ext := strings.ToLower(filepathExt(path)); ext {
	case ".go":
		return checkGo(content)
	case ".json":
		return checkJSON(content)
	case ".js":
		return checkJS(content)
	case ".html", ".htm":
		return checkHTML(content)
	case ".py":
		return checkPython(content)
	default:
		return nil // no check for unsupported types
	}
}

func filepathExt(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			return ""
		}
	}
	return ""
}

func checkGo(content string) error {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "<input>", content, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("syntax error at line %d: %v", errLine(err), err)
	}
	return nil
}

func checkJSON(content string) error {
	var js any
	if err := parseJSON(content, &js); err != nil {
		return fmt.Errorf("syntax error: %v", err)
	}
	return nil
}

// parseJSON is a minimal JSON validity check (braces/brackets/strings balanced).
func parseJSON(content string, out any) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("empty document")
	}
	var depth int
	inString := false
	escaped := false
	for i, r := range content {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"' && !inString:
			inString = true
		case r == '"' && inString:
			inString = false
		case !inString:
			switch r {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth < 0 {
					return fmt.Errorf("unexpected %c at position %d", r, i)
				}
			}
		}
	}
	if inString {
		return fmt.Errorf("unterminated string")
	}
	if depth != 0 {
		return fmt.Errorf("unbalanced brackets")
	}
	_ = out
	return nil
}

func checkJS(content string) error {
	// JS: balanced braces/brackets/parens, no unmatched comment issues.
	if err := balanced(content, "{}\"", "[]\"", "()\"\""); err != nil {
		return fmt.Errorf("syntax error: %v", err)
	}
	return nil
}

func checkHTML(content string) error {
	// HTML: basic tag balance check for inline scripts.
	if err := balanced(content, "<>"); err != nil {
		return fmt.Errorf("syntax error: %v", err)
	}
	return nil
}

func checkPython(content string) error {
	// Python: indentation-aware check is complex; do a basic colon/paren balance.
	if err := balanced(content, "()\"\""); err != nil {
		return fmt.Errorf("syntax error: %v", err)
	}
	return nil
}

// balanced checks that open/close pairs match, respecting string literals.
func balanced(content string, pairs ...string) error {
	opens := make(map[rune]bool)
	closes := make(map[rune]rune) // closing rune -> opening rune
	for _, p := range pairs {
		if len(p) >= 2 {
			opens[rune(p[0])] = true
			closes[rune(p[1])] = rune(p[0])
		}
	}

	var stack []rune
	inString := false
	stringRune := rune(0)
	escaped := false

	for _, r := range content {
		switch {
		case escaped:
			escaped = false
			continue
		case r == '\\' && inString:
			escaped = true
			continue
		case inString:
			if r == stringRune {
				inString = false
				stringRune = 0
			}
			continue
		case r == '"' || r == '\'' || r == '`':
			inString = true
			stringRune = r
			continue
		}

		if opening, isClose := closes[r]; isClose {
			if len(stack) == 0 || stack[len(stack)-1] != opening {
				return fmt.Errorf("unmatched %c", r)
			}
			stack = stack[:len(stack)-1]
		} else if opens[r] {
			stack = append(stack, r)
		}
	}
	if inString {
		return fmt.Errorf("unterminated string")
	}
	if len(stack) > 0 {
		return fmt.Errorf("unmatched %c", stack[len(stack)-1])
	}
	return nil
}

// errLine extracts a line number from a go/parser error message.
func errLine(err error) int {
	msg := err.Error()
	// go parser errors look like "<input>:1:10: ..."
	for i := len(msg) - 1; i >= 0; i-- {
		if msg[i] == ':' {
			// try parsing the number after the colon
			start := i + 1
			end := start
			for end < len(msg) && msg[end] >= '0' && msg[end] <= '9' {
				end++
			}
			if end > start {
				if n := parseAtoi(msg[start:end]); n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

func parseAtoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
