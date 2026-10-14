package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/daemonctl"
)

// syncBuffer is a bytes.Buffer safe for a writer and a concurrent reader.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func TestServerRunServesInTheForegroundUntilStopped(t *testing.T) {
	home := filepath.Join(t.TempDir(), "kit")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(apphome.EnvHome, home)
	paths := apphome.FromHome(home)

	var logs syncBuffer
	exited := make(chan int, 1)
	go func() {
		exited <- executeCommand(context.Background(), []string{"server", "run", "--listen", "127.0.0.1:0"}, &bytes.Buffer{}, &logs, defaultCommandDependencies())
	}()
	manager := daemonctl.NewManager(paths)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, err := manager.Status(context.Background()); err == nil {
			break
		}
		select {
		case code := <-exited:
			t.Fatalf("server run exited with %d: %s", code, logs.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %s", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "level=INFO") {
		t.Errorf("logs = %q, want server log lines on stderr", logs.String())
	}

	code, _, stderr := runCLI("server", "run")
	if code != 1 || stderr != "kit: a server is already running for this Kit home; stop it with `kit server stop`\n" {
		t.Errorf("second run = %d, %q", code, stderr)
	}

	if code, stdout, stderr := runCLI("server", "stop"); code != 0 || stdout != "server stopped\n" {
		t.Fatalf("stop = %d, %q, %q", code, stdout, stderr)
	}
	select {
	case code := <-exited:
		if code != 0 {
			t.Errorf("server run exit code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server run did not exit after stop")
	}
}

func TestServerRunRejectsAnInvalidListenAddress(t *testing.T) {
	t.Setenv(apphome.EnvHome, filepath.Join(t.TempDir(), "kit"))
	code, _, stderr := runCLI("server", "run", "--listen", "192.168.1.10:7777")
	if code != 2 || stderr != "kit: listen address \"192.168.1.10:7777\" must use a loopback address or 0.0.0.0/::\n" {
		t.Errorf("run = %d, %q", code, stderr)
	}
}
