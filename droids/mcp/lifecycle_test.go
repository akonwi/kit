package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestManagerCloseCancelsInFlightCall(t *testing.T) {
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "slow", Version: "1"}, nil)
	started := make(chan struct{})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "slow"},
		func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, any, error) {
			close(started)
			<-ctx.Done()
			return nil, nil, ctx.Err()
		})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Run(ctx, serverTransport) }()

	manager, err := NewManager(Server{
		Name: "slow",
		Transport: func(context.Context) (sdkmcp.Transport, error) {
			return clientTransport, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := manager.namespaces[0].execute(context.Background(), namespaceRequest{Action: "call", Tool: "slow"})
		callDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool call did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- manager.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Manager.Close blocked on in-flight call")
	}
	select {
	case <-callDone:
	case <-time.After(time.Second):
		t.Fatal("in-flight call was not cancelled")
	}
}

func TestNamespaceReconnectsAfterSessionCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var connections atomic.Int32
	manager, err := NewManager(Server{
		Name: "reconnect",
		Transport: func(context.Context) (sdkmcp.Transport, error) {
			connections.Add(1)
			clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
			server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "server", Version: "1"}, nil)
			sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "ping"},
				func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
					return &sdkmcp.CallToolResult{}, nil, nil
				})
			go func() { _ = server.Run(ctx, serverTransport) }()
			return clientTransport, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ns := manager.namespaces[0]
	if _, err := ns.execute(context.Background(), namespaceRequest{Action: "list"}); err != nil {
		t.Fatal(err)
	}

	ns.mu.Lock()
	first := ns.session
	ns.mu.Unlock()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		ns.mu.Lock()
		cleared := ns.session == nil
		ns.mu.Unlock()
		if cleared {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed session was not evicted")
		}
		time.Sleep(time.Millisecond)
	}

	if _, err := ns.execute(context.Background(), namespaceRequest{Action: "list"}); err != nil {
		t.Fatal(err)
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("transport factory called %d times, want 2", got)
	}
}
