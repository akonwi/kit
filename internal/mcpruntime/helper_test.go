//go:build darwin || linux

package mcpruntime

import (
	"context"
	"os"
	"syscall"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	helperMarker  = "KIT_MCPRUNTIME_TEST_HELPER"
	helperMode    = "KIT_MCPRUNTIME_MODE"
	helperLock    = "KIT_MCPRUNTIME_LOCK"
	helperProbe   = "KIT_MCPRUNTIME_ENV_PROBE"
	helperReplyOK = "ok"
)

// TestMCPServerHelper is re-executed as a real stdio MCP server only in
// explicitly marked subprocesses.
func TestMCPServerHelper(t *testing.T) {
	if os.Getenv(helperMarker) != "1" {
		return
	}
	switch os.Getenv(helperMode) {
	case "fail":
		os.Stderr.WriteString("configuration error: missing API token\n")
		os.Exit(3)
	case "silent-exit":
		os.Exit(4)
	}
	// Holding an exclusive lock for the process lifetime lets a test observe
	// termination without depending on reaping schedules or platform ps output.
	if path := os.Getenv(helperLock); path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil || syscall.Flock(int(file.Fd()), syscall.LOCK_EX) != nil {
			os.Exit(94)
		}
	}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "helper", Version: "0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "probe",
		Description: "Report the server's observed environment.",
	}, func(ctx context.Context, request *sdkmcp.CallToolRequest, input struct {
		Field string `json:"field"`
	}) (*sdkmcp.CallToolResult, any, error) {
		reply := helperReplyOK
		switch input.Field {
		case "env":
			reply = os.Getenv(helperProbe)
		case "cwd":
			reply, _ = os.Getwd()
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: reply}}}, nil, nil
	})
	if err := server.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil {
		os.Exit(5)
	}
	os.Exit(0)
}

// helperExecutable returns the command and arguments that re-execute this test
// binary as a stdio MCP server.
func helperExecutable(t *testing.T) (string, []string) {
	t.Helper()
	t.Setenv(helperMarker, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return executable, []string{"-test.run=^TestMCPServerHelper$"}
}

// probe calls the helper's tool and returns its single text reply.
func probe(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, field string) string {
	t.Helper()
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "probe",
		Arguments: map[string]any{"field": field},
	})
	if err != nil {
		t.Fatalf("call probe: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v, want one item", result.Content)
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want *sdkmcp.TextContent", result.Content[0])
	}
	return text.Text
}
