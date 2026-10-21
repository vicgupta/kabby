package tools

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DefaultCommandTimeout is the 120s deadline for run_command.
const DefaultCommandTimeout = 120 * time.Second

// pickShell returns the user's shell per OS, falling back to sh.
func pickShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if runtime.GOOS == "windows" {
		// Prefer PowerShell, fall back to cmd.
		if _, err := exec.LookPath("powershell.exe"); err == nil {
			return "powershell.exe"
		}
		return "cmd.exe"
	}
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash"
	}
	return "sh"
}

// RunCommand runs a shell command with cwd=workspace and a 120s timeout.
// Pipelines preserve upstream exit codes (exit status of the last command).
// Output retains the beginning and the final error tail. Returns the captured
// stdout+stderr combined, or an error on timeout/cancellation/failure.
func RunCommand(command, workspace string, signal <-chan struct{}) (string, error) {
	if signal != nil {
		select {
		case <-signal:
			return "", fmt.Errorf("command cancelled")
		default:
		}
	}

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
		return "", fmt.Errorf("run_command: stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("run_command: stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("run_command: start: %v", err)
	}

	// Combined output capture with beginning + error-tail retention.
	var outBuf, errBuf strings.Builder
	outDone := make(chan struct{})
	errDone := make(chan struct{})

	go func() {
		io.Copy(&outBuf, stdout)
		close(outDone)
	}()
	go func() {
		io.Copy(&errBuf, stderr)
		close(errDone)
	}()

	// Wait for the process or a timeout/cancellation.
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case err := <-waitDone:
		<-outDone
		<-errDone
		if err != nil {
			// Non-zero exit: return beginning of output + error tail.
			out := truncateCommandOutput(outBuf.String(), errBuf.String(), err)
			return out, fmt.Errorf("run_command: exit %v: %s", exitCode(err), out)
		}
		return outBuf.String() + errBuf.String(), nil

	case <-time.After(DefaultCommandTimeout):
		cmd.Process.Kill()
		<-waitDone
		<-outDone
		<-errDone
		return "", fmt.Errorf("run_command: timed out after 120s")

	case <-signal:
		cmd.Process.Kill()
		<-waitDone
		return "", fmt.Errorf("command cancelled")
	}
}

// exitCode extracts the exit code from an exec.ExitError, or returns -1.
func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.ProcessState.Sys().(interface{ ExitStatus() int }); ok {
			return ws.ExitStatus()
		}
	}
	return -1
}

// truncateCommandOutput keeps the beginning of stdout and the tail of stderr,
// so the most useful diagnostic information is preserved within a bound.
func truncateCommandOutput(stdout, stderr string, execErr error) string {
	const headChars = 2000
	const tailChars = 1000

	var b strings.Builder
	if stdout != "" {
		b.WriteString("[stdout]\n")
		if len(stdout) > headChars {
			b.WriteString(stdout[:headChars])
			b.WriteString("\n... (stdout truncated) ...\n")
		} else {
			b.WriteString(stdout)
		}
	}
	if stderr != "" {
		b.WriteString("[stderr]\n")
		if len(stderr) > tailChars {
			b.WriteString("... (stderr tail) ...\n")
			b.WriteString(stderr[len(stderr)-tailChars:])
		} else {
			b.WriteString(stderr)
		}
	}
	if execErr != nil {
		b.WriteString(fmt.Sprintf("\n[exec error: %v]\n", execErr))
	}
	return b.String()
}
