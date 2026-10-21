// Package util provides small shared helpers: ANSI color helpers and bounded
// string truncation.
package util

// ANSI color helpers. Surfaces (TUI, web, headless) may choose to ignore these
// when output is not a terminal.
const (
	Bold  = "\033[1m"
	Dim   = "\033[2m"
	Warn  = "\033[33m"
	Error = "\033[31m"
	Reset = "\033[0m"
)

// TruncateMiddle returns str shortened to at most cap runes, keeping the
// beginning and end separated by an ellipsis. If str already fits, it is
// returned unchanged. The result never exceeds cap runes.
func TruncateMiddle(str string, cap int) string {
	runes := []rune(str)
	if len(runes) <= cap {
		return str
	}
	if cap <= 0 {
		return ""
	}
	if cap == 1 {
		return "…"
	}
	keep := cap - 1 // one rune reserved for the ellipsis
	half := keep / 2
	return string(runes[:half]) + "…" + string(runes[len(runes)-(keep-half):])
}
