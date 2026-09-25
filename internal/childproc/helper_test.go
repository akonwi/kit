//go:build darwin || linux

package childproc

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

const helperMarker = "KIT_CHILDPROC_TEST_HELPER"

// TestChildProcessHelper is re-executed only in explicitly marked subprocesses.
func TestChildProcessHelper(t *testing.T) {
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
	case "describe":
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"cwd":         cwd,
			"args":        args[1:],
			"environment": os.Getenv("KIT_CHILDPROC_INHERITED_TEST"),
		})
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
		child := exec.Command(executable, "-test.run=^TestChildProcessHelper$", "--", "lock", args[1])
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(96)
		}
		// The test triggers either owner Stop or clean leader exit with one stdin byte.
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
	process, err := Start(ctx, Spec{
		Path: executable,
		Args: append([]string{"-test.run=^TestChildProcessHelper$", "--", mode}, args...),
		Dir:  t.TempDir(),
	})
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
