package session

import (
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

func TestProjectTranscriptMessagePreservesStructuredAssistantContent(t *testing.T) {
	t.Parallel()

	record := MessageRecord{
		ID: "message_1", TurnID: "turn_1", Sequence: 3,
		Role: "assistant", CreatedAt: time.Unix(1, 0).UTC(),
	}
	message := droids.AssistantMessage{
		Content: []droids.Content{
			droids.TextContent{Text: "I will inspect it."},
			droids.ToolCall{ID: "call_1", Name: "read", Arguments: []byte(`{ "z": 2, "a": 1 }`)},
			droids.ThinkingContent{Thinking: "visible reasoning"},
			droids.ThinkingContent{Thinking: "hidden reasoning", Redacted: true, Signature: "opaque"},
			droids.TextContent{Text: "Inspection complete."},
		},
		StopReason:   droids.StopReasonAborted,
		ErrorMessage: "stopped by user",
	}

	got, err := projectTranscriptMessage(record, message)
	if err != nil {
		t.Fatalf("projectTranscriptMessage() error = %v", err)
	}
	if got.ID != record.ID || got.TurnID != record.TurnID || got.Sequence != record.Sequence || got.Role != record.Role {
		t.Fatalf("message identity = %+v", got)
	}
	if got.StopReason != "aborted" || got.ErrorMessage != "stopped by user" || !got.IsError {
		t.Fatalf("assistant terminal metadata = %+v", got)
	}
	if len(got.Content) != 4 {
		t.Fatalf("content block count = %d, want 4: %+v", len(got.Content), got.Content)
	}
	if got.Content[0].Kind != TranscriptContentText || got.Content[0].Text != "I will inspect it." {
		t.Fatalf("first content = %+v", got.Content[0])
	}
	call := got.Content[1]
	if call.Kind != TranscriptContentToolCall || call.ToolCallID != "call_1" || call.ToolName != "read" || string(call.Arguments) != `{"a":1,"z":2}` {
		t.Fatalf("tool call = %+v", call)
	}
	if got.Content[2].Kind != TranscriptContentThinking || got.Content[2].Text != "visible reasoning" {
		t.Fatalf("thinking content = %+v", got.Content[2])
	}
	if got.Content[3].Kind != TranscriptContentText || got.Content[3].Text != "Inspection complete." {
		t.Fatalf("last content = %+v", got.Content[3])
	}
}

func TestProjectTranscriptMessagePreservesToolResultIdentityAndDetails(t *testing.T) {
	t.Parallel()

	got, err := projectTranscriptMessage(MessageRecord{
		ID: "message_2", TurnID: "turn_1", Sequence: 4,
		Role: "tool", CreatedAt: time.Unix(2, 0).UTC(),
	}, droids.ToolResultMessage{
		ToolCallID: "call_1", ToolName: "read",
		Content: []droids.Content{droids.TextContent{Text: "file contents"}},
		Details: map[string]any{"lines": 1, "path": "README.md"}, IsError: true,
	})
	if err != nil {
		t.Fatalf("projectTranscriptMessage() error = %v", err)
	}
	if got.ToolCallID != "call_1" || got.ToolName != "read" || !got.IsError {
		t.Fatalf("tool result metadata = %+v", got)
	}
	if string(got.Details) != `{"lines":1,"path":"README.md"}` {
		t.Fatalf("tool result details = %s", got.Details)
	}
	if len(got.Content) != 1 || got.Content[0].Kind != TranscriptContentText || got.Content[0].Text != "file contents" {
		t.Fatalf("tool result content = %+v", got.Content)
	}
}

func TestProjectTranscriptMessageBoundsOversizedToolArguments(t *testing.T) {
	t.Parallel()

	got, err := projectTranscriptMessage(MessageRecord{Role: "assistant"}, droids.AssistantMessage{
		Content: []droids.Content{
			droids.ToolCall{
				ID: "call_1", Name: "write",
				Arguments: []byte(`{"content":"` + strings.Repeat("x", maxPresentationToolArgumentsBytes) + `"}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("projectTranscriptMessage() error = %v", err)
	}
	if len(got.Content) != 1 || got.Content[0].Arguments != "" || !got.Content[0].ArgumentsTruncated {
		t.Fatalf("oversized arguments = %+v", got.Content)
	}
}

func TestProjectTranscriptMessagePreservesPersistedToolDetailNumbers(t *testing.T) {
	t.Parallel()

	const details = `{"large":9007199254740993}`
	got, err := projectTranscriptMessage(MessageRecord{
		Role: "tool", PayloadJSON: []byte(`{"details":` + details + `}`),
	}, droids.ToolResultMessage{ToolCallID: "call_1", ToolName: "read", Details: map[string]any{"large": float64(9007199254740993)}})
	if err != nil {
		t.Fatalf("projectTranscriptMessage() error = %v", err)
	}
	if string(got.Details) != details {
		t.Fatalf("projected details = %s, want %s", got.Details, details)
	}
}

func TestProjectTranscriptMessagePreservesMalformedToolArgumentsForDiagnostics(t *testing.T) {
	t.Parallel()

	got, err := projectTranscriptMessage(MessageRecord{Role: "assistant"}, droids.AssistantMessage{
		Content: []droids.Content{
			droids.ToolCall{ID: "call_1", Name: "read", Arguments: []byte(`{"path":`)},
		},
	})
	if err != nil {
		t.Fatalf("projectTranscriptMessage() error = %v", err)
	}
	if len(got.Content) != 1 || got.Content[0].Arguments != `{"path":` {
		t.Fatalf("malformed diagnostic arguments = %+v", got.Content)
	}
}
