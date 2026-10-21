package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kabby/internal/sandbox"
)

// Search searches for a pattern in files under the workspace. Results are
// bounded (max 50 matches, max 5 per file).
func Search(ctx *ToolContext, args map[string]any, signal <-chan struct{}) (string, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return "", fmt.Errorf("search: missing required argument 'pattern'")
	}
	searchPath, _ := args["path"].(string)
	if searchPath == "" {
		searchPath = "."
	}

	abs, err := sandbox.ResolveInWorkspace(ctx.Workspace, searchPath)
	if err != nil {
		return "", err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("search: invalid pattern %q: %v", pattern, err)
	}

	var results []string
	matchCount := 0
	const maxMatches = 50
	const maxPerFile = 5

	err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if signal != nil {
			select {
			case <-signal:
				return fmt.Errorf("cancelled")
			default:
			}
		}
		if info.IsDir() {
			return nil
		}
		if matchCount >= maxMatches {
			return fmt.Errorf("max matches reached")
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(ctx.Workspace, path)
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		lineNum := 0
		fileMatches := 0
		for scanner.Scan() {
			lineNum++
			if re.MatchString(scanner.Text()) {
				if fileMatches >= maxPerFile {
					continue
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", rel, lineNum, truncateLine(scanner.Text(), 120)))
				matchCount++
				fileMatches++
				if matchCount >= maxMatches {
					break
				}
			}
		}
		if matchCount >= maxMatches {
			return fmt.Errorf("max matches reached")
		}
		return nil
	})

	if len(results) == 0 {
		return fmt.Sprintf("search: no matches for %q in %s", pattern, searchPath), nil
	}

	out := fmt.Sprintf("search: %d match(es) for %q:\n", len(results), pattern)
	out += strings.Join(results, "\n")
	if err != nil && err.Error() == "max matches reached" {
		out += "\n[... results bounded at 50 matches ...]"
	}
	return out, nil
}

func truncateLine(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
