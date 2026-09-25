//go:build darwin || linux

// Package childproc supervises one child process and its process group.
//
// Kit launches third-party executables for plugins and stdio MCP servers. Both
// need identical guarantees: the child gets its own process group, cancellation
// tears down the whole group rather than only the leader, stderr is drained
// into a bounded diagnostic tail, and the leader is never reaped until every
// group signal has been sent.
package childproc

import (
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
	"unicode/utf8"
)

const (
	// MaxStderrBytes is the retained diagnostic tail for one process. Stderr is
	// continuously drained, including after the tail fills.
	MaxStderrBytes = 64 * 1024

	// DefaultTerminateGrace is how long Start waits after SIGTERM before
	// escalating to SIGKILL.
	DefaultTerminateGrace = 250 * time.Millisecond

	// escapedDrainGrace bounds waiting on a descendant that deliberately left the
	// process group while holding the stderr pipe.
	escapedDrainGrace = 100 * time.Millisecond
)

// Spec describes one child process launch. The caller owns executable
// resolution and trust decisions; Spec only carries the resolved result.
type Spec struct {
	// Path is an absolute executable path, a path relative to Dir, or a bare
	// name resolved through PATH.
	Path string
	// Args are literal arguments passed without shell interpretation.
	Args []string
	// Dir is the child's absolute working directory. Session identity never
	// comes from process-global cwd, so Dir is required.
	Dir string
	// Env is the child's complete environment. A nil Env inherits the parent
	// environment; an empty non-nil Env starts the child with no variables.
	Env []string
	// TerminateGrace overrides DefaultTerminateGrace when positive.
	TerminateGrace time.Duration
	// StderrLimit overrides MaxStderrBytes when positive.
	StderrLimit int
}

// Process is a single supervised child process. Its owner must cancel the
// launch context or call Stop when finished. Stop performs forced teardown; a
// protocol host must attempt bounded graceful shutdown beforehand. Start itself
// performs no protocol initialization work.
type Process struct {
	stdin       *os.File
	stdout      *os.File
	stderr      *tailBuffer
	stderrLimit int
	stop        chan struct{}
	stopOnce    sync.Once
	done        chan struct{}
	err         error // Published by closing done.
	cleanupErr  error // Independent of the expected leader exit on forced teardown.
}

// Start launches the child in its own process group. It owns the wait,
// stderr-drain, and supervision goroutines until Done closes. Context
// cancellation tears down the entire process group independently of callers
// waiting for completion.
func Start(ctx context.Context, spec Spec) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if spec.Path == "" {
		return nil, errors.New("child process requires an executable")
	}
	if !filepath.IsAbs(spec.Dir) {
		return nil, fmt.Errorf("child process working directory must be absolute: %q", spec.Dir)
	}
	stderrLimit := spec.StderrLimit
	if stderrLimit <= 0 {
		stderrLimit = MaxStderrBytes
	}
	terminateGrace := spec.TerminateGrace
	if terminateGrace <= 0 {
		terminateGrace = DefaultTerminateGrace
	}

	command := exec.Command(spec.Path, spec.Args...)
	command.Dir = spec.Dir
	command.Env = spec.Env
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Explicit pipes avoid exec.Wait waiting forever for inherited stderr writers.
	// The supervisor owns all parent ends and bounds stderr drain after exit.
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		return nil, err
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		return nil, err
	}
	closeAll := func() {
		for _, file := range []*os.File{stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite} {
			file.Close()
		}
	}
	command.Stdin, command.Stdout, command.Stderr = stdinRead, stdoutWrite, stderrWrite
	if err := command.Start(); err != nil {
		closeAll()
		return nil, err
	}
	stdinRead.Close()
	stdoutWrite.Close()
	stderrWrite.Close()

	process := &Process{
		stdin:       stdinWrite,
		stdout:      stdoutRead,
		stderr:      &tailBuffer{limit: stderrLimit},
		stderrLimit: stderrLimit,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	stderrDone := make(chan struct{})
	go func() { defer close(stderrDone); _, _ = io.Copy(process.stderr, stderrRead) }()
	exited := make(chan error, 1)
	go func() { exited <- observeExit(command.Process.Pid) }()
	go process.supervise(ctx, command, exited, stderrRead, stderrDone, terminateGrace)
	return process, nil
}

// supervise owns teardown ordering for one child until done closes.
func (p *Process) supervise(ctx context.Context, command *exec.Cmd, exited chan error, stderrRead *os.File, stderrDone chan struct{}, terminateGrace time.Duration) {
	var observeErr error
	var signalErr error
	signalGroup := func(signal syscall.Signal) {
		if err := syscall.Kill(-command.Process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
			if errors.Is(err, syscall.EPERM) && groupHasOnlyExitedProcesses(command.Process.Pid) {
				return
			}
			signalErr = errors.Join(signalErr, fmt.Errorf("signal child group %d with %s: %w", command.Process.Pid, signal, err))
		}
	}
	stopRequested := false
	select {
	case observeErr = <-exited:
		// Even a clean leader exit may leave children retaining pipes or side effects.
	case <-ctx.Done():
		stopRequested = true
	case <-p.stop:
		stopRequested = true
	}
	if stopRequested {
		signalGroup(syscall.SIGTERM)
		select {
		case observeErr = <-exited:
		case <-time.After(terminateGrace):
			signalGroup(syscall.SIGKILL)
			observeErr = <-exited
		}
	}
	// Do not reap until every group signal has been sent. An unreaped leader
	// pins the numeric PID/PGID, so it cannot identify an unrelated new group.
	signalGroup(syscall.SIGKILL)
	waitErr := command.Wait()
	p.stdin.Close()
	p.stdout.Close()
	// A descendant may have deliberately escaped the process group. Isolation is
	// not a sandbox; it must nevertheless not keep Kit's drain goroutine alive.
	select {
	case <-stderrDone:
	case <-time.After(escapedDrainGrace):
	}
	stderrRead.Close()
	<-stderrDone
	if observeErr != nil {
		observeErr = fmt.Errorf("observe child exit: %w", observeErr)
	}
	p.cleanupErr = errors.Join(observeErr, signalErr)
	p.err = errors.Join(waitErr, p.cleanupErr)
	close(p.done)
}

// Input is the child's stdin, exclusively owned by the protocol writer.
func (p *Process) Input() io.WriteCloser { return p.stdin }

// Output is the child's stdout, exclusively owned by the protocol reader.
func (p *Process) Output() io.ReadCloser { return p.stdout }

// Done closes after the leader is reaped, group teardown is attempted, and all
// owned pipes and stderr-drain goroutines are closed.
func (p *Process) Done() <-chan struct{} { return p.done }

// Wait returns the leader exit/supervision error. A nil result is still an
// unexpected crash unless shutdown was requested by the owner.
func (p *Process) Wait(ctx context.Context) error {
	select {
	case <-p.done:
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop initiates idempotent process-group termination. A caller timing out does
// not abandon cleanup; the supervisor remains responsible for reaping the child.
func (p *Process) Stop(ctx context.Context) error {
	p.stopOnce.Do(func() { close(p.stop) })
	select {
	case <-p.done:
		return p.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stderr returns a UTF-8-safe snapshot of at most the configured limit from the
// most recent diagnostic output. It is safe to call while the process is running.
func (p *Process) Stderr() string {
	limit := p.stderrLimit
	if limit <= 0 {
		limit = MaxStderrBytes
	}
	value := strings.ToValidUTF8(p.stderr.snapshot(), "\uFFFD")
	if len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	bytes []byte
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	length := len(data)
	if len(data) >= b.limit {
		b.bytes = append(b.bytes[:0], data[len(data)-b.limit:]...)
	} else {
		overflow := len(b.bytes) + len(data) - b.limit
		if overflow > 0 {
			copy(b.bytes, b.bytes[overflow:])
			b.bytes = b.bytes[:len(b.bytes)-overflow]
		}
		b.bytes = append(b.bytes, data...)
	}
	return length, nil
}

func (b *tailBuffer) snapshot() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.bytes) }
