package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Message string `json:"message" jsonschema:"message to echo"`
}

type echoOutput struct {
	Echo string `json:"echo"`
}

func newTestManager(t *testing.T, filter func(*sdkmcp.Tool) bool) (*Manager, *atomic.Int32) {
	t.Helper()
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "Echo a message"},
		func(_ context.Context, _ *sdkmcp.CallToolRequest, input echoInput) (*sdkmcp.CallToolResult, echoOutput, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "echo: " + input.Message}}}, echoOutput{Echo: input.Message}, nil
		})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "hidden", Description: "Filtered tool"},
		func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{}, nil, nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(ctx, serverTransport) }()

	var factoryCalls atomic.Int32
	manager, err := NewManager(Server{
		Name:        "Test Server",
		Description: "Test operations",
		Filter:      filter,
		Transport: func(context.Context) (sdkmcp.Transport, error) {
			factoryCalls.Add(1)
			return clientTransport, nil
		},
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
		cancel()
		<-serverDone
	})
	return manager, &factoryCalls
}

func TestManagerProgressiveDiscoveryAndCall(t *testing.T) {
	manager, factoryCalls := newTestManager(t, func(tool *sdkmcp.Tool) bool { return tool.Name != "hidden" })
	if got := factoryCalls.Load(); got != 0 {
		t.Fatalf("transport factory called eagerly: %d", got)
	}
	if got := len(manager.Tools()); got != 1 {
		t.Fatalf("Tools() returned %d tools, want 1", got)
	}
	if got := factoryCalls.Load(); got != 0 {
		t.Fatalf("Tools() connected eagerly: %d", got)
	}

	ns := manager.namespaces[0]
	listed, err := ns.execute(context.Background(), namespaceRequest{Action: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if got := textContent(t, listed); !strings.Contains(got, "echo") || strings.Contains(got, "hidden") {
		t.Fatalf("unexpected list result: %q", got)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("transport factory called %d times, want 1", got)
	}

	searched, err := ns.execute(context.Background(), namespaceRequest{Action: "search", Query: "message"})
	if err != nil {
		t.Fatal(err)
	}
	if got := textContent(t, searched); !strings.Contains(got, "echo") {
		t.Fatalf("unexpected search result: %q", got)
	}

	described, err := ns.execute(context.Background(), namespaceRequest{Action: "describe", Tool: "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if got := textContent(t, described); !strings.Contains(got, `"message"`) {
		t.Fatalf("describe omitted input schema: %q", got)
	}

	called, err := ns.execute(context.Background(), namespaceRequest{
		Action: "call",
		Tool:   "echo",
		Arguments: map[string]any{
			"message": "hello",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := textContent(t, called); got != "echo: hello" {
		t.Fatalf("call result = %q, want %q", got, "echo: hello")
	}
	if called.IsError {
		t.Fatal("successful call marked as error")
	}
	var details CallDetails
	if err := json.Unmarshal(called.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details.Namespace != "Test Server" || details.Tool != "echo" {
		t.Fatalf("unexpected call details: %#v", called.Details)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("connection was not reused; factory called %d times", got)
	}
}

func TestManagerValidationAndNamespaceNames(t *testing.T) {
	factory := func(context.Context) (sdkmcp.Transport, error) { return nil, nil }
	if _, err := NewManager(Server{Name: "", Transport: factory}); err == nil {
		t.Fatal("empty server name accepted")
	}
	if _, err := NewManager(Server{Name: "x"}); err == nil {
		t.Fatal("nil transport factory accepted")
	}
	if _, err := NewManager(
		Server{Name: "foo bar", Transport: factory},
		Server{Name: "foo@bar", Transport: factory},
	); err == nil {
		t.Fatal("colliding sanitized names accepted")
	}
	if got, err := namespaceToolName("GitHub Enterprise"); err != nil || got != "mcp_github_enterprise" {
		t.Fatalf("namespaceToolName = %q, %v", got, err)
	}
}

type observedCloseTransport struct {
	delegate sdkmcp.Transport
	started  chan struct{}
	once     sync.Once
}

func (t *observedCloseTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	connection, err := t.delegate.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &observedCloseConnection{Connection: connection, started: t.started, once: &t.once}, nil
}

type observedCloseConnection struct {
	sdkmcp.Connection
	started chan struct{}
	once    *sync.Once
}

func (c *observedCloseConnection) Close() error {
	c.once.Do(func() { close(c.started) })
	return c.Connection.Close()
}

func TestManagerClosesConnectedNamespacesConcurrently(t *testing.T) {
	const count = 2
	servers := make([]Server, 0, count)
	requestStarted := make([]chan struct{}, count)
	releaseRequest := make([]chan struct{}, count)
	releaseRequests := make([]func(), count)
	closeStarted := make([]chan struct{}, count)
	stopServers := make([]context.CancelFunc, 0, count)
	serverDone := make([]chan error, 0, count)

	for index := range count {
		clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
		remote := sdkmcp.NewServer(&sdkmcp.Implementation{Name: fmt.Sprintf("remote-%d", index), Version: "0.1.0"}, nil)
		requestStarted[index] = make(chan struct{})
		releaseRequest[index] = make(chan struct{})
		var releaseOnce sync.Once
		releaseRequests[index] = func() { releaseOnce.Do(func() { close(releaseRequest[index]) }) }
		closeStarted[index] = make(chan struct{})
		started, release := requestStarted[index], releaseRequest[index]
		sdkmcp.AddTool(remote, &sdkmcp.Tool{Name: "hang"},
			func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
				close(started)
				<-release
				return &sdkmcp.CallToolResult{}, nil, nil
			})
		serverCtx, stopServer := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- remote.Run(serverCtx, serverTransport) }()
		stopServers = append(stopServers, stopServer)
		serverDone = append(serverDone, done)
		observed := &observedCloseTransport{delegate: clientTransport, started: closeStarted[index]}
		servers = append(servers, Server{
			Name: fmt.Sprintf("remote-%d", index),
			Transport: func(context.Context) (sdkmcp.Transport, error) {
				return observed, nil
			},
		})
	}

	t.Cleanup(func() {
		for index := range count {
			releaseRequests[index]()
			stopServers[index]()
			select {
			case <-serverDone[index]:
			case <-time.After(5 * time.Second):
				t.Errorf("server %d did not stop", index)
			}
		}
	})

	manager, err := NewManager(servers...)
	if err != nil {
		t.Fatal(err)
	}
	for index, ns := range manager.namespaces {
		go func() {
			_, _ = ns.execute(t.Context(), namespaceRequest{Action: "call", Tool: "hang"})
		}()
		select {
		case <-requestStarted[index]:
		case <-time.After(5 * time.Second):
			t.Fatalf("namespace %d did not accept its call", index)
		}
	}

	closed := make(chan error, 1)
	go func() { closed <- manager.Close() }()
	for index := range count {
		select {
		case <-closeStarted[index]:
		case <-time.After(time.Second):
			t.Fatalf("namespace %d did not begin closing concurrently", index)
		}
	}
	for index := range count {
		releaseRequests[index]()
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close manager: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("manager close did not settle")
	}
}

func TestManagerLogoutClearsManagedCredentialsAndKeepsNamespaceAvailable(t *testing.T) {
	var logouts atomic.Int32
	manager, err := NewManager(Server{
		Name:      "oauth",
		Transport: func(context.Context) (sdkmcp.Transport, error) { return nil, errors.New("not connected") },
		Logout: func(context.Context) error {
			logouts.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Logout(t.Context(), "oauth"); err != nil {
		t.Fatal(err)
	}
	if got := logouts.Load(); got != 1 {
		t.Fatalf("logout calls = %d", got)
	}
	if manager.namespaces[0].closed {
		t.Fatal("logout permanently closed the namespace")
	}
}

func TestManagerLogoutIsBoundedAndDeletesCredentialsDuringHangingCall(t *testing.T) {
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	remote := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "remote", Version: "0.1.0"}, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	sdkmcp.AddTool(remote, &sdkmcp.Tool{Name: "hang"},
		func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
			close(started)
			<-release
			return &sdkmcp.CallToolResult{}, nil, nil
		})
	serverCtx, stopServer := context.WithCancel(t.Context())
	serverDone := make(chan error, 1)
	go func() { serverDone <- remote.Run(serverCtx, serverTransport) }()
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		stopServer()
		select {
		case <-serverDone:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})

	var logouts atomic.Int32
	manager, err := NewManager(Server{
		Name:      "oauth",
		Transport: func(context.Context) (sdkmcp.Transport, error) { return clientTransport, nil },
		Logout:    func(context.Context) error { logouts.Add(1); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = manager.namespaces[0].execute(t.Context(), namespaceRequest{Action: "call", Tool: "hang"})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not accept hanging call")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	before := time.Now()
	err = manager.Logout(ctx, "oauth")
	cancel()
	if elapsed := time.Since(before); elapsed > time.Second {
		t.Fatalf("logout took %v", elapsed)
	}
	if err != nil {
		t.Fatalf("logout error = %v", err)
	}
	if got := logouts.Load(); got != 1 {
		t.Fatalf("credential deletes = %d", got)
	}
	releaseOnce.Do(func() { close(release) })
}

func TestManagerLogoutRejectsUnknownAndUnmanagedNamespaces(t *testing.T) {
	manager, err := NewManager(Server{Name: "plain", Transport: func(context.Context) (sdkmcp.Transport, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Logout(t.Context(), "absent"); err == nil || !strings.Contains(err.Error(), "unknown namespace") {
		t.Fatalf("unknown logout = %v", err)
	}
	if err := manager.Logout(t.Context(), "plain"); err == nil || !strings.Contains(err.Error(), "does not use managed authentication") {
		t.Fatalf("unmanaged logout = %v", err)
	}
}

func TestManagerCloseBeforeConnection(t *testing.T) {
	var calls atomic.Int32
	manager, err := NewManager(Server{
		Name: "unused",
		Transport: func(context.Context) (sdkmcp.Transport, error) {
			calls.Add(1)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("Close connected to unused server %d times", got)
	}
	_, err = manager.namespaces[0].execute(context.Background(), namespaceRequest{Action: "list"})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("execute after Close error = %v", err)
	}
}

func textContent(t *testing.T, result droids.ToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("result has %d content blocks, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(droids.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want droids.TextContent", result.Content[0])
	}
	return text.Text
}
