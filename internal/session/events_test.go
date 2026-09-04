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
			event: droids.MessageDelta{Stream: droids.StreamThinkingDelta{
				ContentIndex: 0, Delta: "considering",
			}},
			want: NewEvent{Kind: EventThinkingDelta, ContentIndex: 0, Delta: "considering"},
		},
		{
			name: "assistant text delta",
			event: droids.MessageDelta{Stream: droids.StreamTextDelta{
				ContentIndex: 1, Delta: "answer",
			}},
			want: NewEvent{Kind: EventAssistantTextDelta, ContentIndex: 1, Delta: "answer"},
		},
		{
			name: "tool call planned",
			event: droids.MessageDelta{Stream: droids.StreamToolCallStart{
				ContentIndex: 2, ID: "call_1", Name: "read",
			}},
			want: NewEvent{Kind: EventToolPlanned, ContentIndex: 2, ToolCallID: "call_1", ToolName: "read"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projected := projectDroidEvent("session_1", "turn_1", "run_1", test.event)
			if len(projected) != 1 {
				t.Fatalf("projected event count = %d, want 1", len(projected))
			}
			got := projected[0]
			if got.Kind != test.want.Kind || got.ContentIndex != test.want.ContentIndex || got.Delta != test.want.Delta ||
				got.ToolCallID != test.want.ToolCallID || got.ToolName != test.want.ToolName {
				t.Fatalf("projected event = %+v, want activity %+v", got, test.want)
			}
		})
	}
}

func TestProjectDroidEventSuppressesMalformedPlannedToolIdentity(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.MessageDelta{
		Stream: droids.StreamToolCallStart{ContentIndex: 1, Name: "read"},
	})
	if len(projected) != 0 {
		t.Fatalf("projected malformed tool event = %+v, want none", projected)
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

func TestProjectDroidEventChunksLargeStreamingDeltaWithoutDataLoss(t *testing.T) {
	t.Parallel()

	delta := strings.Repeat("résumé ", maxLiveEventTextBytes/4)
	projected := projectDroidEvent("session_1", "turn_1", "run_1", droids.MessageDelta{
		Stream: droids.StreamTextDelta{ContentIndex: 1, Delta: delta},
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
