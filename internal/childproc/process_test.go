//go:build darwin || linux

package childproc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

func readReady(t *testing.T, process *Process, expected string) {
	t.Helper()
	line, err := bufio.NewReader(process.Output()).ReadString('\n')
	if err != nil || line != expected+"\n" {
		t.Fatalf("readiness = %q, %v; stderr = %q", line, err, process.Stderr())
	}
}

func TestProcessLiteralArgumentsCwdAndInheritedEnvironment(t *testing.T) {
	t.Setenv("KIT_CHILDPROC_INHERITED_TEST", "inherited-value")
	args := []string{"$(touch should-not-exist)", "*.go", "~", "$HOME", "two words", ""}
	process := startHelper(t, t.Context(), "describe", args...)
	var got struct {
		Cwd         string   `json:"cwd"`
		Args        []string `json:"args"`
		Environment string   `json:"environment"`
	}
	if err := json.NewDecoder(process.Output()).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got.Cwd) || !reflect.DeepEqual(got.Args, args) || got.Environment != "inherited-value" {
		t.Fatalf("child context = %#v", got)
	}
	// Compare the actual child directory to the installed executable's independently
	// supplied working directory in the relative-command test below.
	if err := process.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDrainsBoundedStderr(t *testing.T) {
	process := startHelper(t, t.Context(), "stderr")
	readReady(t, process, "ready")
	if err := process.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("x", MaxStderrBytes-len("diagnostic-tail")) + "diagnostic-tail"
	if got := process.Stderr(); got != want {
		t.Fatalf("stderr tail length = %d, want %d; suffix = %v", len(got), len(want), strings.HasSuffix(got, "diagnostic-tail"))
	}
}

func TestProcessStderrReplacementRemainsBounded(t *testing.T) {
	process := &Process{stderr: &tailBuffer{limit: MaxStderrBytes}}
	input := bytes.Repeat([]byte{0xff}, MaxStderrBytes)
	if _, err := process.stderr.Write(input); err != nil {
		t.Fatal(err)
	}
	stderr := process.Stderr()
	if len(stderr) > MaxStderrBytes || !utf8.ValidString(stderr) {
		t.Fatalf("stderr bytes = %d, valid = %v", len(stderr), utf8.ValidString(stderr))
	}
}

func TestProcessUnexpectedExitRetainsDiagnostics(t *testing.T) {
	process := startHelper(t, t.Context(), "exit")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var exit *exec.ExitError
	if err := process.Wait(ctx); !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit = %v", err)
	}
	if got := process.Stderr(); got != "failure details\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestProcessCancellationEscalatesAndStopIsConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	process := startHelper(t, ctx, "ignore-term")
	readReady(t, process, "ready")
	cancel()
	wait, cancelWait := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelWait()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := process.Stop(wait); err != nil {
				t.Errorf("stop = %v", err)
			}
			_ = process.Stderr()
		})
	}
	wg.Wait()
	var exit *exec.ExitError
	if err := process.Wait(wait); !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL exit, got %v", err)
	}
}

func TestProcessOwnerCancellationWithoutStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	process := startHelper(t, ctx, "ignore-term")
	readReady(t, process, "ready")
	cancel()
	wait, cancelWait := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelWait()
	select {
	case <-process.Done():
	case <-wait.Done():
		t.Fatal("owner cancellation did not supervise cleanup")
	}
}

func TestProcessStopCallerCancellationDoesNotAbandonCleanup(t *testing.T) {
	process := startHelper(t, t.Context(), "ignore-term")
	readReady(t, process, "ready")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := process.Stop(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop = %v", err)
	}
	wait, cancelWait := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelWait()
	select {
	case <-process.Done():
	case <-wait.Done():
		t.Fatal("cleanup was abandoned")
	}
}

func TestProcessTerminatesGroupAfterStopOrLeaderExit(t *testing.T) {
	for _, cleanExit := range []bool{false, true} {
		t.Run(fmt.Sprintf("clean_exit_%t", cleanExit), func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "descendant.lock")
			process := startHelper(t, t.Context(), "tree", lockPath)
			readReady(t, process, "locked")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if cleanExit {
				if _, err := process.Input().Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
				if err := process.Wait(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := process.Stop(ctx); err != nil {
					t.Fatal(err)
				}
			}
			file, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			// Killing an orphan can briefly leave a zombie. A released kernel lock tests
			// termination without relying on init's reaping schedule or platform ps text.
			for {
				err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
				if err == nil {
					break
				}
				if !errors.Is(err, syscall.EWOULDBLOCK) {
					t.Fatal(err)
				}
				select {
				case <-ctx.Done():
					t.Fatal("descendant survived group cleanup")
				case <-time.After(time.Millisecond):
				}
			}
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		})
	}
}

func TestTailBuffer(t *testing.T) {
	buffer := tailBuffer{limit: 8}
	for _, step := range []struct{ input, want string }{{"ab", "ab"}, {"cdefghi", "bcdefghi"}, {"0123456789", "23456789"}, {"z", "3456789z"}} {
		n, err := buffer.Write([]byte(step.input))
		if n != len(step.input) || err != nil || buffer.snapshot() != step.want {
			t.Fatalf("write %q = %q (%d, %v)", step.input, buffer.snapshot(), n, err)
		}
	}
}
