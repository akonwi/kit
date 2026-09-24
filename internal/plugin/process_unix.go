//go:build darwin || linux

package plugin

import (
	"context"
	"encoding/json"
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

// MaxStderrBytes is the retained diagnostic tail for one process. Stderr is
// continuously drained, including after the tail fills.
const MaxStderrBytes = 64 * 1024

// Process is a single supervised child process. Its owner must cancel the
// launch context or call Stop when finished. Stop performs forced teardown;
// the protocol host must attempt bounded graceful RPC shutdown beforehand.
// StartProcess itself performs no RPC initialization or contribution work.
type Process struct {
	stdin      *os.File
	stdout     *os.File
	stderr     *tailBuffer
	stop       chan struct{}
	stopOnce   sync.Once
	done       chan struct{}
	err        error // Published by closing done.
	cleanupErr error // Independent of the expected leader exit on forced teardown.
}

// StartProcess launches directly from the installation directory, inherits the
// environment, and gives the child its own process group. It owns the wait,
// stderr-drain, and supervision goroutines until Done closes. Context
// cancellation tears down the entire process group independently of callers
// waiting for completion. Session identity never comes from process-global cwd.
func StartProcess(ctx context.Context, installation Installation) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(installation.Root) {
		return nil, errors.New("plugin installation root must be absolute")
	}
	data, err := json.Marshal(installation.Manifest)
	if err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(data, nil)
	if err != nil {
		return nil, err
	}
	executable := manifest.Transport.Command
	if strings.ContainsRune(executable, filepath.Separator) && !filepath.IsAbs(executable) {
		executable = filepath.Join(installation.Root, executable)
	}
	command := exec.Command(executable, manifest.Transport.Args...)
	command.Dir = installation.Root
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
		return nil, fmt.Errorf("launch plugin %q: %w", manifest.ID, err)
	}
	stdinRead.Close()
	stdoutWrite.Close()
	stderrWrite.Close()
	process := &Process{stdin: stdinWrite, stdout: stdoutRead, stderr: &tailBuffer{limit: MaxStderrBytes}, stop: make(chan struct{}), done: make(chan struct{})}
	stderrDone := make(chan struct{})
	go func() { defer close(stderrDone); _, _ = io.Copy(process.stderr, stderrRead) }()
	exited := make(chan error, 1)
	go func() { exited <- observeExit(command.Process.Pid) }()
	go func() {
		var observeErr error
		var signalErr error
		signalGroup := func(signal syscall.Signal) {
			if err := syscall.Kill(-command.Process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
				if errors.Is(err, syscall.EPERM) && groupHasOnlyExitedProcesses(command.Process.Pid) {
					return
				}
				signalErr = errors.Join(signalErr, fmt.Errorf("signal plugin group %d with %s: %w", command.Process.Pid, signal, err))
			}
		}
		stopRequested := false
		select {
		case observeErr = <-exited:
			// Even a clean leader exit may leave children retaining pipes or side effects.
		case <-ctx.Done():
			stopRequested = true
		case <-process.stop:
			stopRequested = true
		}
		if stopRequested {
			signalGroup(syscall.SIGTERM)
			select {
			case observeErr = <-exited:
			case <-time.After(250 * time.Millisecond):
				signalGroup(syscall.SIGKILL)
				observeErr = <-exited
			}
		}
		// Do not reap until every group signal has been sent. An unreaped leader
		// pins the numeric PID/PGID, so it cannot identify an unrelated new group.
		signalGroup(syscall.SIGKILL)
		waitErr := command.Wait()
		process.stdin.Close()
		process.stdout.Close()
		// A descendant may have deliberately escaped the process group. Isolation is
		// not a sandbox; it must nevertheless not keep Kit's drain goroutine alive.
		select {
		case <-stderrDone:
		case <-time.After(100 * time.Millisecond):
		}
		stderrRead.Close()
		<-stderrDone
		if observeErr != nil {
			observeErr = fmt.Errorf("observe plugin exit: %w", observeErr)
		}
		process.cleanupErr = errors.Join(observeErr, signalErr)
		process.err = errors.Join(waitErr, process.cleanupErr)
		close(process.done)
	}()
	return process, nil
}

// Input is the plugin's stdin, exclusively owned by the protocol writer.
func (p *Process) Input() io.WriteCloser { return p.stdin }

// Output is the plugin's stdout, exclusively owned by the protocol reader.
func (p *Process) Output() io.ReadCloser { return p.stdout }

// Done closes after the leader is reaped, group teardown is attempted, and all
// owned pipes and stderr-drain goroutines are closed.
func (p *Process) Done() <-chan struct{} { return p.done }

// Wait returns the leader exit/supervision error. A nil result is still an
// unexpected crash unless shutdown was requested by the protocol host.
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

// Stderr returns a UTF-8-safe snapshot of at most MaxStderrBytes from the most
// recent diagnostic output. It is safe to call while the process is running.
func (p *Process) Stderr() string {
	value := strings.ToValidUTF8(p.stderr.snapshot(), "\uFFFD")
	if len(value) <= MaxStderrBytes {
		return value
	}
	start := len(value) - MaxStderrBytes
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
