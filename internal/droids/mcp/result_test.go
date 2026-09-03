package mcp

import (
	"testing"

	"github.com/akonwi/kit/internal/droids"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConvertCallResult(t *testing.T) {
	result, err := convertCallResult("images", "render", &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: "done"},
			&sdkmcp.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"},
		},
		StructuredContent: map[string]any{"id": "abc"},
		IsError:           true,
	}, defaultMaxResultBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("MCP error result was not preserved")
	}
	if got := result.Content[0].(droids.TextContent).Text; got != "done" {
		t.Fatalf("text = %q", got)
	}
	image := result.Content[1].(droids.ImageContent)
	if image.URL != "data:image/png;base64,AQID" || image.MediaType != "image/png" {
		t.Fatalf("unexpected image: %+v", image)
	}
}

func TestConvertCallResultRejectsOversizedContent(t *testing.T) {
	_, err := convertCallResult("data", "large", &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "too large"}},
	}, 3)
	if err == nil {
		t.Fatal("oversized result was accepted")
	}
}

func TestConvertCallResultFallsBackToStructuredContent(t *testing.T) {
	result, err := convertCallResult("data", "get", &sdkmcp.CallToolResult{
		StructuredContent: map[string]any{"ok": true},
	}, defaultMaxResultBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := textContent(t, result); got != `{"ok":true}` {
		t.Fatalf("fallback text = %q", got)
	}
}
