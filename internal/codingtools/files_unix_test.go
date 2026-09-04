//go:build darwin || linux

package codingtools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestFileToolsRejectFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	fifo := filepath.Join(cwd, "pipe")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	read, err := newReadTool(cwd).Execute(context.Background(), readArgs{Path: "pipe"})
	if err != nil {
		t.Fatal(err)
	}
	if !read.IsError {
		t.Fatalf("FIFO read = %+v, want tool error", read)
	}
	content := "no"
	written, err := newWriteTool(cwd).Execute(context.Background(), writeArgs{Path: "pipe", Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if !written.IsError {
		t.Fatalf("FIFO write = %+v, want tool error", written)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("FIFO operations blocked for %v", elapsed)
	}
}

func TestAtomicReplacementPreservesFilesOnCancellationAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := writeRegularFile(ctx, path, []byte("replacement"), 0o644); err == nil {
		t.Fatal("canceled replacement error = nil")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "original" {
		t.Fatalf("file after cancellation = %q, %v", body, err)
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(path, 0o440); err != nil {
			t.Fatal(err)
		}
		if err := writeRegularFile(context.Background(), path, []byte("replacement"), 0o666); err == nil {
			t.Fatal("read-only replacement error = nil")
		}
		body, err = os.ReadFile(path)
		if err != nil || string(body) != "original" {
			t.Fatalf("read-only file changed to %q, %v", body, err)
		}
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
	}

	_, version, err := readRegularFileVersion(context.Background(), path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("newer content"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := replaceRegularFile(context.Background(), path, []byte("stale edit"), 0o644, &version); err == nil {
		t.Fatal("conflicting edit error = nil")
	}
	body, err = os.ReadFile(path)
	if err != nil || string(body) != "newer content" {
		t.Fatalf("file after conflict = %q, %v", body, err)
	}
}

func TestCommandCaptureSpoolsPrivatelyAndBoundsMemory(t *testing.T) {
	capture := &commandCapture{limit: 32}
	input := strings.Repeat("x", 128)
	if _, err := capture.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	execution := capture.finish()
	if execution.Output != strings.Repeat("x", 32) || !execution.Truncated || execution.OutputPath == "" {
		t.Fatalf("capture result = %+v", execution)
	}
	defer os.RemoveAll(filepath.Dir(execution.OutputPath))
	body, err := os.ReadFile(execution.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != input {
		t.Fatalf("spooled output length = %d, want %d", len(body), len(input))
	}
	info, err := os.Stat(execution.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("spool mode = %o, want 600", info.Mode().Perm())
	}
}
