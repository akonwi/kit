//go:build darwin || linux

package mcpruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/mcpconfig"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect builds one namespace from configuration and connects a real MCP
// client to it, exercising the whole projection and transport path.
func connect(t *testing.T, lifetime context.Context, server mcpconfig.Server, cwd string) *sdkmcp.ClientSession {
	t.Helper()
	launcher, err := NewLauncher(lifetime)
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(cwd, []mcpconfig.Server{server})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(servers))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	transport, err := servers[0].Transport(ctx)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func stdioServer(t *testing.T) mcpconfig.Server {
	t.Helper()
	command, args := helperExecutable(t)
	t.Setenv(helperMode, "serve")
	return mcpconfig.Server{
		Name:      "helper",
		Transport: mcpconfig.TransportStdio,
		Command:   command,
		Args:      args,
	}
}

func TestStdioServerListsAndCallsTools(t *testing.T) {
	session := connect(t, t.Context(), stdioServer(t), t.TempDir())

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "probe" {
		t.Fatalf("tools = %#v, want one tool named probe", tools.Tools)
	}
	if got := probe(t, ctx, session, ""); got != helperReplyOK {
		t.Fatalf("probe = %q, want %q", got, helperReplyOK)
	}
}

// The agent core cancels the context it passes to a transport factory once the
// originating tool call settles. A server bound to that context would not
// survive the very call that started it.
func TestStdioServerOutlivesTheCallThatStartedIt(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{stdioServer(t)})
	if err != nil {
		t.Fatal(err)
	}

	callCtx, cancelCall := context.WithCancel(t.Context())
	transport, err := servers[0].Transport(callCtx)
	if err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
	session, err := client.Connect(callCtx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	cancelCall()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if got := probe(t, ctx, session, ""); got != helperReplyOK {
		t.Fatalf("probe after call cancellation = %q, want %q", got, helperReplyOK)
	}
}

func TestStdioClosingSessionTerminatesServerProcess(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "server.lock")
	t.Setenv(helperLock, lockPath)
	session := connect(t, t.Context(), stdioServer(t), t.TempDir())

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if got := probe(t, ctx, session, ""); got != helperReplyOK {
		t.Fatalf("probe = %q", got)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	requireLockReleased(t, ctx, lockPath)
}

func TestStdioRuntimeShutdownTerminatesServerProcess(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "server.lock")
	t.Setenv(helperLock, lockPath)
	lifetime, shutdown := context.WithCancel(t.Context())
	session := connect(t, lifetime, stdioServer(t), t.TempDir())

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if got := probe(t, ctx, session, ""); got != helperReplyOK {
		t.Fatalf("probe = %q", got)
	}
	shutdown()
	requireLockReleased(t, ctx, lockPath)
}

func TestStdioConnectFailureReportsServerStderr(t *testing.T) {
	server := stdioServer(t)
	t.Setenv(helperMode, "fail")
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{server})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	transport, err := servers[0].Transport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.1.0"}, nil)
	_, err = client.Connect(ctx, transport, nil)
	if err == nil {
		t.Fatal("expected a connect failure")
	}
	if !strings.Contains(err.Error(), "missing API token") {
		t.Fatalf("err = %v, want the server's stderr tail", err)
	}
}

func TestStdioMissingExecutableIsReported(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{{
		Name:      "absent",
		Transport: mcpconfig.TransportStdio,
		Command:   filepath.Join(t.TempDir(), "absent-server"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := servers[0].Transport(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Connect(t.Context())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), `launch MCP server "absent"`) {
		t.Fatalf("err = %v, want the server name", err)
	}
}

// requireLockReleased waits for the helper's exclusive lock to become
// available, which only happens once its process has exited.
func requireLockReleased(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			return
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("server process survived teardown")
		case <-time.After(time.Millisecond):
		}
	}
}
