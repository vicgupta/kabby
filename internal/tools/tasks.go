package tools

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ringBuffer holds the last N lines of output for a task.
type ringBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newRingBuffer(max int) *ringBuffer {
	return &ringBuffer{lines: make([]string, 0, max), max: max}
}

func (r *ringBuffer) write(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	if len(r.lines) > r.max {
		r.lines = r.lines[len(r.lines)-r.max:]
	}
}

func (r *ringBuffer) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// Task represents a background process.
type Task struct {
	ID      string
	Cmd     string
	Started time.Time
	Running bool
	buf     *ringBuffer
	cmd     *exec.Cmd
	cancel  func()
}

// Output returns the buffered output lines for this task.
func (t *Task) Output() []string {
	return t.buf.snapshot()
}

// TaskManager manages background tasks (dev servers, watchers, etc.).
// All tasks are killed on KillAll().
type TaskManager struct {
	mu    sync.Mutex
	tasks map[string]*Task
	next  int
}

func NewTaskManager() *TaskManager {
	return &TaskManager{tasks: make(map[string]*Task)}
}

// Start launches a background command in the workspace, capturing output to a
// ring buffer. Returns the task ID.
func (tm *TaskManager) Start(command, workspace string) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.next++
	id := fmt.Sprintf("task-%d", tm.next)

	shell := pickShell()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(shell), "powershell.exe") {
		cmd = exec.Command(shell, "-NoProfile", "-Command", command)
	} else {
		cmd = exec.Command(shell, "-c", command)
	}
	cmd.Dir = workspace

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("task: stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("task: stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("task: start: %v", err)
	}

	task := &Task{
		ID:      id,
		Cmd:     command,
		Started: time.Now(),
		Running: true,
		buf:     newRingBuffer(500),
		cmd:     cmd,
	}

	go func() {
		io.Copy(&lineWriter{w: task.buf}, stdout)
	}()
	go func() {
		io.Copy(&lineWriter{w: task.buf}, stderr)
	}()
	go func() {
		cmd.Wait()
		tm.mu.Lock()
		if t, ok := tm.tasks[id]; ok && t == task {
			t.Running = false
		}
		tm.mu.Unlock()
	}()

	tm.tasks[id] = task
	return id, nil
}

// List returns all tasks.
func (tm *TaskManager) List() []*Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]*Task, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		out = append(out, t)
	}
	return out
}

// Logs returns the buffered output for a task.
func (tm *TaskManager) Logs(id string) ([]string, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.tasks[id]
	if !ok {
		return nil, false
	}
	return t.Output(), true
}

// Stop stops a task by ID. Returns false if the task is unknown or already stopped.
func (tm *TaskManager) Stop(id string) bool {
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	if !ok {
		tm.mu.Unlock()
		return false
	}
	if !t.Running {
		tm.mu.Unlock()
		return false
	}
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	t.Running = false
	tm.mu.Unlock()
	return true
}

// KillAll stops all running tasks. Called on process exit and signals.
func (tm *TaskManager) KillAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for _, t := range tm.tasks {
		if t.cmd != nil && t.cmd.Process != nil {
			t.cmd.Process.Kill()
		}
		t.Running = false
	}
}

// lineWriter writes lines from an io.Reader to a ring buffer, splitting on newlines.
type lineWriter struct {
	w        *ringBuffer
	leftover string
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	data := lw.leftover + string(p)
	for {
		idx := strings.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		lw.w.write(data[:idx])
		data = data[idx+1:]
	}
	lw.leftover = data
	return len(p), nil
}
