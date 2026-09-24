//go:build darwin || linux

package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
)

const helperMarker = "KIT_PLUGIN_PROCESS_TEST_HELPER"

// TestPluginProcessHelper is re-executed only in explicitly marked subprocesses.
func TestPluginProcessHelper(t *testing.T) {
	if os.Getenv(helperMarker) != "1" {
		return
	}
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 || index >= len(os.Args) {
		os.Exit(98)
	}
	args := os.Args[index:]
	switch args[0] {
	case "rpc":
		decoder := json.NewDecoder(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)
		var pending json.RawMessage
		for {
			var request rpcMessage
			if decoder.Decode(&request) != nil {
				break
			}
			if request.Method == nil {
				_ = encoder.Encode(responseMessage(pending, request.Result, nil))
				continue
			}
			switch *request.Method {
			case "initialize":
				_ = encoder.Encode(responseMessage(request.ID, json.RawMessage(`{"protocolVersion":1}`), nil))
			case "roundtrip":
				pending = request.ID
				_ = encoder.Encode(requestMessage(json.RawMessage(`"child-1"`), "host/echo", request.Params))
			default:
				_ = encoder.Encode(responseMessage(request.ID, nil, rpcError(-32601, "Method not found")))
			}
		}
	case "describe":
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"cwd": cwd, "args": args[1:], "environment": os.Getenv("KIT_PLUGIN_INHERITED_TEST")})
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "stderr":
		_, _ = os.Stderr.WriteString(strings.Repeat("x", 3*MaxStderrBytes) + "diagnostic-tail")
		_, _ = os.Stdout.WriteString("ready\n")
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "exit":
		_, _ = os.Stderr.WriteString("failure details\n")
		os.Exit(7)
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		_, _ = os.Stdout.WriteString("ready\n")
		for {
			time.Sleep(time.Hour)
		}
	case "lock":
		signal.Ignore(syscall.SIGTERM)
		file, err := os.OpenFile(args[1], os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			os.Exit(95)
		}
		if syscall.Flock(int(file.Fd()), syscall.LOCK_EX) != nil {
			os.Exit(94)
		}
		_, _ = os.Stdout.WriteString("locked\n")
		for {
			time.Sleep(time.Hour)
		}
	case "tree":
		executable, _ := os.Executable()
		child := exec.Command(executable, "-test.run=^TestPluginProcessHelper$", "--", "lock", args[1])
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(96)
		}
		// The test triggers either host Stop or clean leader exit with one stdin byte.
		var input [1]byte
		_, _ = os.Stdin.Read(input[:])
	default:
		os.Exit(97)
	}
	os.Exit(0)
}

func startHelper(t *testing.T, ctx context.Context, mode string, args ...string) *Process {
	t.Helper()
	t.Setenv(helperMarker, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process, err := StartProcess(ctx, Installation{Root: t.TempDir(), Manifest: Manifest{ManifestVersion: 1, ID: "test-plugin", Transport: StdioTransport{Type: "stdio", Command: executable, Args: append([]string{"-test.run=^TestPluginProcessHelper$", "--", mode}, args...)}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := process.Stop(ctx); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return process
}

func readReady(t *testing.T, process *Process, expected string) {
	t.Helper()
	line, err := bufio.NewReader(process.Output()).ReadString('\n')
	if err != nil || line != expected+"\n" {
		t.Fatalf("readiness = %q, %v; stderr = %q", line, err, process.Stderr())
	}
}

func TestProcessLiteralArgumentsCwdAndInheritedEnvironment(t *testing.T) {
	t.Setenv("KIT_PLUGIN_INHERITED_TEST", "inherited-value")
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

func TestProcessRelativeExecutableAndExactWorkingDirectory(t *testing.T) {
	t.Setenv(helperMarker, "1")
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(root, "plugin")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	process, err := StartProcess(ctx, Installation{Root: root, Manifest: Manifest{ManifestVersion: 1, ID: "relative", Transport: StdioTransport{Type: "stdio", Command: "./plugin", Args: []string{"-test.run=^TestPluginProcessHelper$", "--", "describe"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Stop(context.Background())
	var got struct {
		Cwd string `json:"cwd"`
	}
	if err := json.NewDecoder(process.Output()).Decode(&got); err != nil {
		t.Fatal(err)
	}
	// macOS commonly resolves /var to /private/var in getcwd.
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cwd != physical && got.Cwd != root {
		t.Fatalf("cwd = %q, want %q", got.Cwd, root)
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

func TestProcessRejectsCancelledOrInvalidLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := StartProcess(ctx, Installation{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled launch = %v", err)
	}
	valid := Installation{Root: t.TempDir(), Manifest: Manifest{ManifestVersion: 1, ID: "missing", Transport: StdioTransport{Type: "stdio", Command: "./missing"}}}
	if _, err := StartProcess(t.Context(), valid); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing executable = %v", err)
	}
	valid.Root = "relative"
	if _, err := StartProcess(t.Context(), valid); err == nil {
		t.Fatal("accepted relative installation")
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

func TestProcessRPCFullDuplex(t *testing.T) {
	process := startHelper(t, t.Context(), "rpc")
	endpoint := NewRPCEndpoint(t.Context(), process.Output(), process.Input(), RPCHandlers{Request: func(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if method != "host/echo" {
			return nil, rpcError(-32601, "Method not found")
		}
		return params, nil
	}})
	t.Cleanup(func() {
		endpoint.Close(nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = endpoint.Wait(ctx)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	initialized, err := endpoint.Call(ctx, "initialize", json.RawMessage(`{"protocolVersion":1}`))
	if err != nil || string(initialized) != `{"protocolVersion":1}` {
		t.Fatalf("initialize = %s, %v", initialized, err)
	}
	params := json.RawMessage(`{"value":"real process roundtrip"}`)
	result, err := endpoint.Call(ctx, "roundtrip", params)
	if err != nil || string(result) != string(params) {
		t.Fatalf("roundtrip = %s, %v", result, err)
	}
	endpoint.Close(nil)
	if err := process.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
