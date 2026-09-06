package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

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
