// Package ui provides a common print/status/warn/error sink used by all three
// surfaces (TUI, web, headless). The zero value is usable but silent; Close is
// a no-op so headless can defer ui.Close() safely.
package ui

import (
	"fmt"
	"io"
)

// UI is the shared status sink. Methods are safe for concurrent use.
type UI struct {
	out io.Writer
	err io.Writer
}

func New(out, err io.Writer) *UI {
	return &UI{out: out, err: err}
}

// Println prints a line to stdout.
func (u *UI) Println(a ...any) {
	fmt.Fprintln(u.out, a...)
}

// Status prints a status line to stdout.
func (u *UI) Status(format string, a ...any) {
	fmt.Fprintf(u.out, format+"\n", a...)
}

// Warn prints a warning to stderr.
func (u *UI) Warn(format string, a ...any) {
	fmt.Fprintf(u.err, "warn: "+format+"\n", a...)
}

// Error prints an error to stderr.
func (u *UI) Error(format string, a ...any) {
	fmt.Fprintf(u.err, "error: "+format+"\n", a...)
}

// Close is a no-op sink close, so every surface can defer Close() uniformly.
func (u *UI) Close() error { return nil }
