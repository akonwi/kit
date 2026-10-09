package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/akonwi/kit/droids"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// CallDetails is persisted with a remote MCP call result for UI and
// observability. Authentication credentials are never included.
type CallDetails struct {
	Namespace         string
	Tool              string
	StructuredContent any
	IsError           bool
}

func convertCallResult(namespace, tool string, result *sdkmcp.CallToolResult) (droids.ToolResult, error) {
	if result == nil {
		return droids.ToolResult{}, fmt.Errorf("MCP tool %s.%s returned no result", namespace, tool)
	}

	var structured []byte
	if result.StructuredContent != nil {
		var err error
		structured, err = json.Marshal(result.StructuredContent)
		if err != nil {
			return droids.ToolResult{}, fmt.Errorf("encode structured MCP result from %s.%s: %w", namespace, tool, err)
		}
	}

	content := make([]droids.ResultContent, 0, len(result.Content))
	for _, block := range result.Content {
		switch block := block.(type) {
		case *sdkmcp.TextContent:
			content = append(content, droids.TextContent{Text: block.Text})
		case *sdkmcp.ImageContent:
			content = append(content, droids.NewImageData(block.MIMEType, block.Data))
		default:
			// Droids providers currently support text and image tool-result blocks.
			// Preserve other MCP content (audio, resources, and links) as JSON text
			// rather than silently discarding it.
			raw, err := json.Marshal(block)
			if err != nil {
				return droids.ToolResult{}, fmt.Errorf("encode MCP content from %s.%s: %w", namespace, tool, err)
			}
			content = append(content, droids.TextContent{Text: string(raw)})
		}
	}
	if len(content) == 0 && result.StructuredContent != nil {
		content = append(content, droids.TextContent{Text: string(structured)})
	}
	if len(content) == 0 {
		content = append(content, droids.TextContent{Text: "MCP tool completed with no content."})
	}

	details, err := droids.EncodeDetails(CallDetails{
		Namespace: namespace, Tool: tool,
		StructuredContent: result.StructuredContent, IsError: result.IsError,
	})
	if err != nil {
		return droids.ToolResult{}, fmt.Errorf("encode MCP details from %s.%s: %w", namespace, tool, err)
	}
	return droids.ToolResult{Content: content, Details: details, IsError: result.IsError}, nil
}
