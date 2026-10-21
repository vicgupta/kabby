// Package tools provides the eight agent tools (four read-only, four
// write/exec) shared by all three surfaces.
package tools

import "kabby/internal/plan"

// ToolContext is the harness-held state handed to every tool execution.
type ToolContext struct {
	Workspace       string
	ResultCharLimit int // per-call char cap for tool results
	TaskManager     *TaskManager
	Plan            *plan.Plan
}

// ToolFunc is the signature of an individual tool implementation.
type ToolFunc func(ctx *ToolContext, args map[string]any, signal <-chan struct{}) (string, error)
