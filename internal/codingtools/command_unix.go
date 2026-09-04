//go:build darwin || linux

package codingtools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	commandKillGrace       = 5 * time.Second
	commandPipeWait        = 250 * time.Millisecond
	commandCaptureLifetime = 24 * time.Hour
	maxSpooledOutput       = 64 << 20
)

var (
	commandCleanupMu   sync.Mutex
	commandLastCleanup time.Time
)

type commandExecution struct {
	Output       string
	ExitCode     *int
	Truncated    bool
	OutputPath   string
	OutputCapped bool
	TimedOut     bool
}

type commandCapture struct {
	mu          sync.Mutex
	head        bytes.Buffer
	limit       int
	spool       *os.File
	spoolPath   string
	spooled     int64
	truncated   bool
	capped      bool
	spoolFailed bool
}

func (c *commandCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.head.Len()+len(data) > c.limit {
		c.truncated = true
	}
	if c.spool == nil && !c.spoolFailed && c.truncated {
		directory, err := os.MkdirTemp("", "kit-bash-")
		if err == nil {
			if chmodErr := os.Chmod(directory, 0o700); chmodErr != nil {
				err = chmodErr
			}
			if err == nil {
				c.spoolPath = filepath.Join(directory, "output.txt")
				c.spool, err = os.OpenFile(c.spoolPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			}
		}
		if err != nil {
			c.spoolFailed = true
			c.spoolPath = ""
			if directory != "" {
				_ = os.RemoveAll(directory)
			}
		} else {
			c.writeSpool(c.head.Bytes())
		}
	}

	if c.spool != nil {
		c.writeSpool(data)
	}
	if remaining := c.limit - c.head.Len(); remaining > 0 {
		c.head.Write(data[:min(remaining, len(data))])
	}
	return len(data), nil
}

func (c *commandCapture) writeSpool(data []byte) {
	if c.spool == nil || c.capped || len(data) == 0 {
		return
	}
	remaining := int64(maxSpooledOutput) - c.spooled
	if remaining <= 0 {
		c.capped = true
		return
	}
	if int64(len(data)) > remaining {
		data = data[:remaining]
		c.capped = true
	}
	written, err := c.spool.Write(data)
	c.spooled += int64(written)
	if err != nil {
		c.spoolFailed = true
		_ = c.spool.Close()
		c.spool = nil
	}
}

func (c *commandCapture) finish() commandExecution {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.spool != nil {
		_ = c.spool.Sync()
		_ = c.spool.Close()
		c.spool = nil
	}
	return commandExecution{
		Output:       normalizeText(c.head.String()),
		Truncated:    c.truncated,
		OutputPath:   c.spoolPath,
		OutputCapped: c.capped || c.spoolFailed,
	}
}

func runCommand(ctx context.Context, shell, command, cwd string, timeout time.Duration, outputLimit int) (commandExecution, error) {
	maybeCleanOldCommandCaptures()
	if err := ctx.Err(); err != nil {
		return commandExecution{}, err
	}
	capture := &commandCapture{limit: outputLimit}
	cmd := exec.Command(shell, "-c", command)
	cmd.Dir = cwd
	cmd.Stdout = capture
	cmd.Stderr = capture
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = commandPipeWait
	if err := cmd.Start(); err != nil {
		return commandExecution{}, fmt.Errorf("start command: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var waitErr error
	timedOut := false
	canceled := false
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		canceled = true
		waitErr = terminateCommand(cmd.Process.Pid, done)
	case <-timer.C:
		timedOut = true
		waitErr = terminateCommand(cmd.Process.Pid, done)
	}

	execution := capture.finish()
	execution.TimedOut = timedOut
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		if code >= 0 {
			execution.ExitCode = &code
		}
	}
	if canceled {
		if execution.OutputPath != "" {
			_ = os.RemoveAll(filepath.Dir(execution.OutputPath))
		}
		return execution, ctx.Err()
	}
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		terminateRemainingProcessGroup(cmd.Process.Pid)
		return execution, fmt.Errorf("wait for command output: %w", waitErr)
	}
	var exitError *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitError) && !timedOut {
		return execution, fmt.Errorf("wait for command: %w", waitErr)
	}
	return execution, nil
}

func maybeCleanOldCommandCaptures() {
	commandCleanupMu.Lock()
	if time.Since(commandLastCleanup) < time.Hour {
		commandCleanupMu.Unlock()
		return
	}
	commandLastCleanup = time.Now()
	commandCleanupMu.Unlock()

	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-commandCaptureLifetime)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "kit-bash-") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(os.TempDir(), entry.Name()))
		}
	}
}

func terminateRemainingProcessGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.NewTimer(commandKillGrace)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
				return
			}
		case <-deadline.C:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return
		}
	}
}

func terminateCommand(pid int, done <-chan error) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	timer := time.NewTimer(commandKillGrace)
	defer timer.Stop()
	select {
	case err := <-done:
		terminateRemainingProcessGroup(pid)
		return err
	case <-timer.C:
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		err := <-done
		terminateRemainingProcessGroup(pid)
		return err
	}
}

var _ io.Writer = (*commandCapture)(nil)
