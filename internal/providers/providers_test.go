package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[{"name":"llama3.2:3b","size":2000000}]}`)
	}))
	defer srv.Close()

	ollama := NewOllama(srv.URL, NewTransport())
	models, err := ollama.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ID != "llama3.2:3b" {
		t.Fatalf("expected model ID llama3.2:3b, got %s", models[0].ID)
	}
	if models[0].Backend != "ollama" {
		t.Fatalf("expected backend ollama, got %s", models[0].Backend)
	}
}

func TestOllamaChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		// Stream two chunks, then done.
		fmt.Fprint(w, `{"model":"llama3","message":{"role":"assistant","content":"Hello"},"done":false}
`)
		fmt.Fprint(w, `{"model":"llama3","message":{"role":"assistant","content":" world"},"done":true,"eval_count":10}`)
	}))
	defer srv.Close()

	ollama := NewOllama(srv.URL, NewTransport())
	results, err := ollama.Chat(context.Background(), ChatRequest{
		Model:         "llama3",
		ContextWindow: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var contents []string
	var doneCount int
	for result := range results {
		if result.Err != nil {
			t.Fatalf("unexpected stream error: %v", result.Err)
		}
		contents = append(contents, result.Content)
		if result.Done {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Fatalf("expected exactly one done message, got %d", doneCount)
	}
	if strings.Join(contents, "") != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", strings.Join(contents, ""))
	}
}

func TestOllamaChatWithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"model":"llama3","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"src/main.go"}}}]},"done":false}
`)
		fmt.Fprint(w, `{"model":"llama3","message":{"role":"assistant","content":"Done"},"done":true}`)
	}))
	defer srv.Close()

	ollama := NewOllama(srv.URL, NewTransport())
	results, err := ollama.Chat(context.Background(), ChatRequest{
		Model: "llama3",
		Tools: []ToolSpec{
			{Name: "read_file", Description: "Read a file", Parameters: map[string]any{"path": "string"}},
		},
		ContextWindow: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolCalls []ToolCall
	for result := range results {
		if result.Err != nil {
			t.Fatalf("unexpected stream error: %v", result.Err)
		}
		toolCalls = append(toolCalls, result.ToolCalls...)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "read_file" {
		t.Fatalf("expected tool read_file, got %s", toolCalls[0].Name)
	}
	if toolCalls[0].Arguments["path"] != "src/main.go" {
		t.Fatalf("expected path src/main.go, got %v", toolCalls[0].Arguments["path"])
	}
}

func TestOllamaChatTruncatedStreamNoPartialExecution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send a partial tool call then cut off mid-argument.
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"model":"llama3","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"write_file","arguments":{"path":`)
		// Connection closes mid-stream.
	}))
	defer srv.Close()

	ollama := NewOllama(srv.URL, NewTransport())
	results, err := ollama.Chat(context.Background(), ChatRequest{
		Model:         "llama3",
		ContextWindow: 4096,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolCalls []ToolCall
	for result := range results {
		if result.Err != nil {
			// Decode error on truncated line is expected; no partial tool calls should be yielded.
			continue
		}
		toolCalls = append(toolCalls, result.ToolCalls...)
	}
	if len(toolCalls) != 0 {
		t.Fatalf("expected no partial tool calls from truncated stream, got %d", len(toolCalls))
	}
}

func TestOllamaProcessList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			http.Error(w, "not found", 404)
			return
		}
		fmt.Fprint(w, `{"models":[{"name":"llama3","size":2000000,"num_ctx":4096}]}`)
	}))
	defer srv.Close()

	ollama := NewOllama(srv.URL, NewTransport())
	procs, err := ollama.ProcessList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(procs))
	}
	if procs[0].NumCtx != 4096 {
		t.Fatalf("expected num_ctx 4096, got %d", procs[0].NumCtx)
	}
}

func TestLMStudioListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.Error(w, "not found", 404)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"qwen2.5-coder:3b"}]}`)
	}))
	defer srv.Close()

	lm := NewLMStudio(srv.URL, NewTransport())
	models, err := lm.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].Backend != "lmstudio" {
		t.Fatalf("expected backend lmstudio, got %s", models[0].Backend)
	}
}

func TestTransportRetriesOn5xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(500)
			fmt.Fprint(w, "temporary failure")
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	transport := NewTransport()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
	resp, err := transport.Do(req)
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("expected 'ok', got %q", string(body))
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls (2 retries + 1 success), got %d", calls)
	}
}

func TestSchedulerForegroundPriority(t *testing.T) {
	sched := NewScheduler()
	released := make(chan struct{})

	// Foreground call holds the slot.
	go func() {
		sched.RunForeground(context.Background(), "http://server1", func() error {
			close(released)
			// Hold the slot for a short time.
			time.Sleep(100 * time.Millisecond)
			return nil
		})
	}()
	<-released

	// Background call should get ErrSlotBusy while foreground holds slot.
	err := sched.RunBackground(context.Background(), "http://server1", func() error { return nil })
	if err != ErrSlotBusy {
		t.Fatalf("expected ErrSlotBusy, got %v", err)
	}

	// Wait for foreground to release, then background should succeed.
	time.Sleep(150 * time.Millisecond)
	err = sched.RunBackground(context.Background(), "http://server1", func() error { return nil })
	if err != nil {
		t.Fatalf("expected background to succeed after foreground released, got %v", err)
	}
}

func TestEffortConstants(t *testing.T) {
	if EffortOff != "off" || EffortLow != "low" || EffortMedium != "medium" || EffortHigh != "high" || EffortDefault != "" {
		t.Fatal("unexpected effort constants")
	}
}

var _ = io.ReadAll

func TestOllamaCancelIsRecognisable(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release) // runs first, so Close does not wait on the handler
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := NewOllama(srv.URL, NewTransport()).Chat(ctx, ChatRequest{Model: "m"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled chat must report context.Canceled, got %v", err)
	}
}
