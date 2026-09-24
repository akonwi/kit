package mcpruntime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectedManager adapts a real SDK client session to the manager close
// surface while exposing when its background close eventually settles.
type connectedManager struct {
	session  *sdkmcp.ClientSession
	finished chan struct{}
	calls    atomic.Int32
}

func (m *connectedManager) Close() error {
	m.calls.Add(1)
	err := m.session.Close()
	close(m.finished)
	return err
}

func TestCloseWithinDeadlineReturnsWhenConnectedServerStopsAnswering(t *testing.T) {
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	remote := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "hanging-server", Version: "0.1.0"}, nil)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var releaseOnce sync.Once
	sdkmcp.AddTool(remote, &sdkmcp.Tool{Name: "hang"},
		func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
			close(requestStarted)
			<-releaseRequest
			return &sdkmcp.CallToolResult{}, nil, nil
		})

	serverCtx, stopServer := context.WithCancel(t.Context())
	serverDone := make(chan error, 1)
	go func() { serverDone <- remote.Run(serverCtx, serverTransport) }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	connectCtx, cancelConnect := context.WithTimeout(t.Context(), time.Second)
	session, err := client.Connect(connectCtx, clientTransport, nil)
	cancelConnect()
	if err != nil {
		stopServer()
		t.Fatalf("connect fake server: %v", err)
	}
	manager := &connectedManager{session: session, finished: make(chan struct{})}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseRequest) })
		_ = session.Close()
		stopServer()
		select {
		case <-serverDone:
		case <-time.After(5 * time.Second):
			t.Error("fake server did not stop")
		}
	})

	// Make the server accept a request and then stop answering it. The SDK's
	// graceful ClientSession.Close waits for accepted calls to settle.
	callDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: "hang"})
		callDone <- err
	}()
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("fake server did not accept the hanging request")
	}

	deadline := 25 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), deadline)
	before := time.Now()
	err = closeWithinDeadline(ctx, manager)
	elapsed := time.Since(before)
	cancel()

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want deadline exceeded", err)
	}
	if !errors.Is(err, errClosePending) {
		t.Fatalf("close error = %v, want pending-close marker", err)
	}
	if elapsed < deadline {
		t.Fatalf("close returned after %v, before its %v deadline", elapsed, deadline)
	}
	// A generous upper bound catches an accidentally synchronous close without
	// making the test sensitive to a briefly busy CI worker.
	if elapsed > time.Second {
		t.Fatalf("close returned after %v, want bounded teardown", elapsed)
	}
	if got := manager.calls.Load(); got != 1 {
		t.Fatalf("close calls = %d, want exactly 1", got)
	}
	select {
	case <-manager.finished:
		t.Fatal("graceful close finished while the server was unresponsive")
	default:
	}

	// Bounded teardown abandons only the wait. Once the launcher terminates the
	// underlying server (modeled by releasing it), background cleanup settles.
	releaseOnce.Do(func() { close(releaseRequest) })
	select {
	case <-manager.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("background close did not settle after server release")
	}
	select {
	case <-callDone:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted call did not settle after server release")
	}
}

func TestCloseWithinDeadlineReturnsManagerError(t *testing.T) {
	want := errors.New("close failed")
	if err := closeWithinDeadline(t.Context(), errorCloser{err: want}); !errors.Is(err, want) {
		t.Fatalf("close error = %v, want %v", err, want)
	}
}

type errorCloser struct{ err error }

func (c errorCloser) Close() error { return c.err }
