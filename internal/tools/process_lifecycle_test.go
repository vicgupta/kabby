package tools

import (
	"strings"
	"testing"
	"time"
)

func TestTaskStartLogsStop(t *testing.T) {
	dir := t.TempDir()
	tm := NewTaskManager()

	// Start a long-running task (sleep loop) to verify it stays running.
	id, err := tm.Start("sleep 2", dir)
	if err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}

	tasks := tm.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != id {
		t.Fatalf("expected id %q, got %q", id, tasks[0].ID)
	}
	if !tasks[0].Running {
		t.Fatal("expected task to be running")
	}

	// Stop it.
	if !tm.Stop(id) {
		t.Fatal("expected stop to return true")
	}
	tasks = tm.List()
	if len(tasks) != 1 {
		t.Fatalf("expected task still in list after stop, got %d", len(tasks))
	}
	if tasks[0].Running {
		t.Fatal("expected task not running after stop")
	}

	// Stopping again returns false.
	if tm.Stop(id) {
		t.Fatal("expected stop to return false for already-stopped task")
	}
}

func TestTaskKillAll(t *testing.T) {
	dir := t.TempDir()
	tm := NewTaskManager()

	for i := 0; i < 3; i++ {
		if _, err := tm.Start("sleep 2", dir); err != nil {
			t.Fatalf("unexpected start error: %v", err)
		}
	}
	if len(tm.List()) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tm.List()))
	}

	tm.KillAll()
	for _, task := range tm.List() {
		if task.Running {
			t.Fatalf("task %q should not be running after KillAll", task.ID)
		}
	}
}

func TestTaskOutputCapture(t *testing.T) {
	dir := t.TempDir()
	tm := NewTaskManager()

	id, err := tm.Start("echo hello-world-task; echo second-line", dir)
	if err != nil {
		t.Fatal(err)
	}

	// Wait a moment for output to be captured.
	time.Sleep(300 * time.Millisecond)

	logs, ok := tm.Logs(id)
	if !ok {
		t.Fatal("expected logs to exist")
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "hello-world-task") {
		t.Fatalf("expected captured output, got %v", logs)
	}

	tm.Stop(id)
}

func TestRunCommandTimeout(t *testing.T) {
	dir := t.TempDir()
	out, err := RunCommand("sleep 300", dir, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected 'timed out' in error, got %q", err.Error())
	}
	if out != "" {
		t.Fatalf("expected empty output on timeout, got %q", out)
	}
}

func TestRunCommandSuccess(t *testing.T) {
	dir := t.TempDir()
	out, err := RunCommand("echo hello-command", dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello-command") {
		t.Fatalf("expected output, got %q", out)
	}
}

func TestRunCommandNonzeroExit(t *testing.T) {
	dir := t.TempDir()
	_, err := RunCommand("exit 3", dir, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "exit 3") {
		t.Fatalf("expected exit code 3 in error, got %q", err.Error())
	}
}
