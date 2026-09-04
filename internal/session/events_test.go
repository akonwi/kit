package session

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestProjectDroidEventPreservesExpectedLiveActivity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event droids.Event
		want  NewEvent
	}{
		{
			name: "thinking delta",
			event: droids.MessageDelta{MessageID: "message_1", Stream: droids.StreamThinkingDelta{
				ContentIndex: 0, Delta: "considering",
			}},
			want: NewEvent{Kind: EventThinkingDelta, MessageID: "message_1", ContentIndex: 0, Delta: "considering"},
		},
		{
			name: "assistant text delta",
			event: droids.MessageDelta{MessageID: "message_1", Stream: droids.StreamTextDelta{
				ContentIndex: 1, Delta: "answer",
			}},
			want: NewEvent{Kind: EventAssistantTextDelta, MessageID: "message_1", ContentIndex: 1, Delta: "answer"},
		},
		{
			name: "tool call planned",
			event: droids.MessageDelta{MessageID: "message_1", Stream: droids.StreamToolCallEnd{
				ContentIndex: 2, ToolCall: droids.ToolCall{ID: "call_1", Name: "read", Arguments: []byte(`{"path":"README.md"}`)},
			}},
			want: NewEvent{
				Kind: EventToolPlanned, MessageID: "message_1", ContentIndex: 2,
				ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projected := projectDroidEvent("session_1", "turn_1", "run_1", test.event)
			if len(projected) != 1 {
				t.Fatalf("projected event count = %d, want 1", len(projected))
			}
			got := projected[0]
			if got.Kind != test.want.Kind || got.MessageID != test.want.MessageID || got.ContentIndex != test.want.ContentIndex || got.Delta != test.want.Delta ||
				got.ToolCallID != test.want.ToolCallID || got.ToolName != test.want.ToolName || got.Arguments != test.want.Arguments {
				t.Fatalf("projected event = %+v, want activity %+v", got, test.want)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("projected event validation error = %v", err)
			}
		})
	}
}

func TestProjectDroidEventDoesNotExposeToolArgumentStreaming(t *testing.T) {
	t.Parallel()

	for _, stream := range []droids.StreamEvent{
		droids.StreamToolCallStart{ContentIndex: 1, ID: "call_1", Name: "read"},
		droids.StreamToolCallDelta{ContentIndex: 1, Delta: `{"path":"REA`},
	} {
		projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.MessageDelta{
			MessageID: "message_1", Stream: stream,
		})
		if len(projected) != 0 {
			t.Fatalf("projected provider argument stream event = %+v, want none", projected)
		}
	}
}

func TestProjectDroidEventSuppressesMalformedPlannedToolIdentity(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.MessageDelta{
		MessageID: "message_1", Stream: droids.StreamToolCallEnd{
			ContentIndex: 1, ToolCall: droids.ToolCall{Name: "read", Arguments: []byte(`{}`)},
		},
	})
	if len(projected) != 0 {
		t.Fatalf("projected malformed tool event = %+v, want none", projected)
	}
}

func TestProjectDroidEventBoundsToolArgumentsWhenPlanningCompletes(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.MessageDelta{
		MessageID: "message_1", Stream: droids.StreamToolCallEnd{
			ContentIndex: 1,
			ToolCall: droids.ToolCall{
				ID: "call_1", Name: "write",
				Arguments: []byte(`{"content":"` + strings.Repeat("x", maxPresentationToolArgumentsBytes) + `"}`),
			},
		},
	})
	if len(projected) != 1 || projected[0].Kind != EventToolPlanned || projected[0].Arguments != "" || !projected[0].ArgumentsTruncated {
		t.Fatalf("oversized planned tool call = %+v", projected)
	}
	if err := projected[0].Validate(); err != nil {
		t.Fatalf("truncated planned tool validation error = %v", err)
	}
}

func TestProjectDroidEventIncludesBoundedToolArgumentsOnStart(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionStart{
		ToolCallID: "call_1", ToolName: "read", Arguments: []byte(`{ "path": "README.md" }`),
	})
	if len(projected) != 1 {
		t.Fatalf("projected event count = %d, want 1", len(projected))
	}
	event := projected[0]
	if event.Kind != EventToolStarted || event.Arguments != `{"path":"README.md"}` || event.ArgumentsTruncated {
		t.Fatalf("projected tool start = %+v", event)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("tool start validation error = %v", err)
	}

	projected = projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionStart{
		ToolCallID: "call_2", ToolName: "write",
		Arguments: []byte(`{"content":"` + strings.Repeat("x", maxPresentationToolArgumentsBytes) + `"}`),
	})
	if len(projected) != 1 || projected[0].Arguments != "" || !projected[0].ArgumentsTruncated {
		t.Fatalf("oversized tool start = %+v", projected)
	}
	if err := projected[0].Validate(); err != nil {
		t.Fatalf("truncated tool start validation error = %v", err)
	}
}

func TestProjectDroidEventPreservesAppendOnlyToolResultDeltas(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionUpdate{
		ToolCallID: "call_1", ToolName: "read",
		Delta: droids.ToolResultDelta{Content: []droids.Content{
			droids.TextContent{Text: "first"},
			droids.ImageContent{MediaType: "image/png", URL: "data:image/png;base64,aW1hZ2U="},
		}, IsError: true},
	})
	if len(projected) != 1 {
		t.Fatalf("projected update count = %d, want 1: %+v", len(projected), projected)
	}
	event := projected[0]
	if event.Kind != EventToolUpdated || len(event.Content) != 2 || event.Content[0].Kind != TranscriptContentText || event.Content[0].Text != "first" || !event.IsError {
		t.Fatalf("text delta = %+v", event)
	}
	if event.Content[1].Kind != TranscriptContentImage || event.Content[1].MediaType != "image/png" {
		t.Fatalf("image delta = %+v", event)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("tool update validation error = %v", err)
	}
}

func TestProjectDroidEventPreservesAuthoritativeToolResultAndDetails(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read",
		Result: droids.ToolResult{
			Content: []droids.Content{
				droids.TextContent{Text: "complete"},
				droids.FileContent{Filename: "report.txt", MediaType: "text/plain", URL: "data:text/plain;base64,eA=="},
			},
			Details: map[string]any{"lines": 3},
		},
	})
	if len(projected) != 1 {
		t.Fatalf("projected completion count = %d, want 1", len(projected))
	}
	event := projected[0]
	if event.Kind != EventToolCompleted || len(event.Content) != 2 || event.Content[0].Text != "complete" || event.Content[1].Filename != "report.txt" || string(event.Details) != `{"lines":3}` || event.DetailsOmitted {
		t.Fatalf("tool completion = %+v", event)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("tool completion validation error = %v", err)
	}
}

func TestProjectDroidEventBoundsNonTextToolContent(t *testing.T) {
	t.Parallel()

	content := make([]droids.Content, maxLiveEventContentBlocks+1)
	for index := range content {
		content[index] = droids.ImageContent{MediaType: "image/png", URL: "data:image/png;base64,aQ=="}
	}
	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read", Result: droids.ToolResult{Content: content},
	})
	if len(projected) != 1 || len(projected[0].Content) != maxLiveEventContentBlocks || !projected[0].ContentTruncated {
		t.Fatalf("bounded non-text content = count %d truncated %t", len(projected[0].Content), projected[0].ContentTruncated)
	}
}

func TestProjectDroidEventOmitsInvalidToolContentMetadata(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read",
		Result: droids.ToolResult{Content: []droids.Content{
			droids.ImageContent{MediaType: "not-a-media-type", URL: "https://example.com/image"},
		}},
	})
	if len(projected) != 1 || len(projected[0].Content) != 0 || !projected[0].ContentTruncated {
		t.Fatalf("invalid content event = %+v", projected)
	}
	if err := projected[0].Validate(); err != nil {
		t.Fatalf("bounded completion validation error = %v", err)
	}
}

func TestProjectDroidEventMarksOversizedToolDetailsOmitted(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read",
		Result: droids.ToolResult{Details: map[string]any{"value": strings.Repeat("x", maxLiveEventDetailsBytes)}},
	})
	if len(projected) != 1 || len(projected[0].Details) != 0 || !projected[0].DetailsOmitted {
		t.Fatalf("oversized details event = %+v", projected)
	}
}

func TestProjectDroidEventChunksLargeStreamingDeltaWithoutDataLoss(t *testing.T) {
	t.Parallel()

	delta := strings.Repeat("résumé ", maxLiveEventTextBytes/4)
	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.MessageDelta{
		MessageID: "message_1", Stream: droids.StreamTextDelta{ContentIndex: 1, Delta: delta},
	})
	var rebuilt strings.Builder
	for _, event := range projected {
		if event.Kind != EventAssistantTextDelta || event.ContentIndex != 1 || len(event.Delta) > maxLiveEventTextBytes {
			t.Fatalf("chunk = %+v", event)
		}
		rebuilt.WriteString(event.Delta)
	}
	if rebuilt.String() != delta {
		t.Fatalf("rebuilt delta length = %d, want %d", rebuilt.Len(), len(delta))
	}
}

func TestAssistantPresentationExcludesRedactedThinking(t *testing.T) {
	t.Parallel()

	text, thinking := assistantPresentation(droids.AssistantMessage{Content: []droids.Content{
		droids.ThinkingContent{Thinking: "visible"},
		droids.ThinkingContent{Thinking: "secret", Redacted: true, Signature: "opaque"},
		droids.TextContent{Text: "response"},
	}})
	if thinking != "visible" || text != "response" {
		t.Fatalf("assistant presentation = thinking %q text %q", thinking, text)
	}
}
