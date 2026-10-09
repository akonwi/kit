package droids

import (
	"strings"
	"testing"
)

func TestImageContextEstimateDoesNotCountEncodedTransportAsText(t *testing.T) {
	t.Parallel()
	short := NewImageData("image/png", []byte("small"))
	large := FileContent{MediaType: "image/png", URL: "data:image/png;base64," + strings.Repeat("A", 2<<20)}
	shortEstimate := EstimateMessagesTokens([]Message{ToolResultMessage{ToolCallID: "call", ToolName: "inspect_image", Content: []ResultContent{short}}})
	largeEstimate := EstimateMessagesTokens([]Message{ToolResultMessage{ToolCallID: "call", ToolName: "inspect_image", Content: []ResultContent{large}}})
	if shortEstimate != largeEstimate {
		t.Fatalf("image estimates differ by encoded size: short=%d large=%d", shortEstimate, largeEstimate)
	}
	if largeEstimate < 12_000 || largeEstimate > 13_000 {
		t.Fatalf("bounded image estimate = %d, want approximately 12K tokens", largeEstimate)
	}
}

func TestImageContextEstimateFitsSupportedSmallContext(t *testing.T) {
	t.Parallel()
	image := NewImageData("image/png", []byte("image"))
	usage := estimateContextUsage("system", nil, Model{ContextWindow: 32_768, MaxInputTokens: 28_672}, "", 4096, []Message{
		AssistantMessage{Content: []AssistantContent{ToolCall{ID: "call", Name: "inspect_image", Arguments: []byte(`{"path":"image.png"}`)}}},
		ToolResultMessage{ToolCallID: "call", ToolName: "inspect_image", Content: []ResultContent{image}},
	}, nil)
	if !contextCanRun(usage) {
		t.Fatalf("image result should fit small context: %#v", usage)
	}
}

func TestNonImageContextEstimateStillCountsSourceBytes(t *testing.T) {
	t.Parallel()
	short := FileContent{Filename: "notes.txt", MediaType: "text/plain", URL: "data:text/plain;base64,QQ=="}
	large := short
	large.URL += strings.Repeat("A", 1024)
	shortEstimate := EstimateMessagesTokens([]Message{ToolResultMessage{ToolCallID: "call", ToolName: "read", Content: []ResultContent{short}}})
	largeEstimate := EstimateMessagesTokens([]Message{ToolResultMessage{ToolCallID: "call", ToolName: "read", Content: []ResultContent{large}}})
	if largeEstimate-shortEstimate != 512 {
		t.Fatalf("non-image estimate delta = %d, want 512", largeEstimate-shortEstimate)
	}
}
