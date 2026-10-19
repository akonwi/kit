package session

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/subagent"
)

// TestProjectChildLiveEventsSatisfyWireContract pins the exact projection of
// every child runtime event the live stream accepts and checks each against
// the wire contract. A projected tool.planned event once carried the assistant
// message ID, which the contract rejects, so child event pages failed with
// HTTP 500.
func TestProjectChildLiveEventsSatisfyWireContract(t *testing.T) {
	t.Parallel()
	toolResult := []droids.ResultContent{droids.TextContent{Text: "line one"}, droids.TextContent{Text: "line two"}}
	tests := []struct {
		name  string
		event droids.Event
		want  subagent.LiveEvent
	}{
		{
			name:  "turn started",
			event: droids.LifecycleEvent{Kind: "execution.started"},
			want:  subagent.LiveEvent{Kind: "turn.started", TurnID: "turn_1"},
		},
		{
			name:  "turn settled",
			event: droids.LifecycleEvent{Kind: "execution.settled"},
			want:  subagent.LiveEvent{Kind: "turn.settled", TurnID: "turn_1"},
		},
		{
			name:  "turn interrupted",
			event: droids.LifecycleEvent{Kind: "execution.interrupted"},
			want:  subagent.LiveEvent{Kind: "turn.settled", TurnID: "turn_1"},
		},
		{
			name:  "text delta",
			event: droids.MessageDelta{MessageID: "message_1", Stream: droids.StreamTextDelta{ContentIndex: 2, Delta: "hello"}},
			want:  subagent.LiveEvent{Kind: "message.text.delta", TurnID: "turn_1", MessageID: "message_1", ContentIndex: 2, Delta: "hello"},
		},
		{
			name:  "thinking delta",
			event: droids.MessageDelta{MessageID: "message_1", Stream: droids.StreamThinkingDelta{ContentIndex: 1, Delta: "pondering"}},
			want:  subagent.LiveEvent{Kind: "message.thinking.delta", TurnID: "turn_1", MessageID: "message_1", ContentIndex: 1, Delta: "pondering"},
		},
		{
			name:  "tool call start",
			event: droids.MessageDelta{MessageID: "message_1", Stream: droids.StreamToolCallStart{ContentIndex: 3, ID: "toolu_1", Name: "read"}},
			want:  subagent.LiveEvent{Kind: "tool.planned", TurnID: "turn_1", ContentIndex: 3, ToolCallID: "toolu_1", ToolName: "read"},
		},
		{
			name:  "tool call end",
			event: droids.MessageDelta{MessageID: "message_1", Stream: droids.StreamToolCallEnd{ContentIndex: 3, ToolCall: droids.ToolCall{ID: "toolu_1", Name: "read"}}},
			want:  subagent.LiveEvent{Kind: "tool.planned", TurnID: "turn_1", ContentIndex: 3, ToolCallID: "toolu_1", ToolName: "read"},
		},
		{
			name:  "tool started",
			event: droids.ToolExecutionStart{ToolCallID: "toolu_1", ToolName: "read", Arguments: []byte(`{"path":"a"}`)},
			want:  subagent.LiveEvent{Kind: "tool.started", TurnID: "turn_1", ToolCallID: "toolu_1", ToolName: "read"},
		},
		{
			name:  "tool output delta",
			event: droids.ToolExecutionUpdate{ToolCallID: "toolu_1", ToolName: "bash", Delta: droids.ToolResultDelta{Content: toolResult, IsError: true}},
			want:  subagent.LiveEvent{Kind: "tool.output.delta", TurnID: "turn_1", ToolCallID: "toolu_1", ToolName: "bash", Text: "line one\nline two", IsError: true},
		},
		{
			name:  "tool completed",
			event: droids.ToolExecutionEnd{ToolCallID: "toolu_1", ToolName: "read", Result: droids.ToolResult{Content: toolResult}},
			want:  subagent.LiveEvent{Kind: "tool.completed", TurnID: "turn_1", ToolCallID: "toolu_1", ToolName: "read", Text: "line one\nline two"},
		},
		{
			name:  "tool failed",
			event: droids.ToolExecutionEnd{ToolCallID: "toolu_1", ToolName: "read", Result: droids.ToolResult{Content: toolResult}, IsError: true},
			want:  subagent.LiveEvent{Kind: "tool.completed", TurnID: "turn_1", ToolCallID: "toolu_1", ToolName: "read", Text: "line one\nline two", IsError: true},
		},
		{
			name: "message completed",
			event: droids.MessageEnd{Message: droids.AssistantMessage{
				ID: "message_1", Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
			}},
			want: subagent.LiveEvent{Kind: "message.completed", TurnID: "turn_1", MessageID: "message_1"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := projectChildLiveEvent(droids.EventEnvelope{TurnID: "turn_1", Event: test.event})
			if !ok || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("projected = %+v, %t; want %+v, true", got, ok, test.want)
			}
			got.Sequence = 1
			page := protocol.SubagentLiveEventPage{
				StreamID: "substream_0123456789abcdef0123456789abcdef", FirstSequence: 1, LastSequence: 1,
				Events: []protocol.SubagentLiveEvent{wireChildLiveEvent(got)},
			}
			if err := page.Validate(); err != nil {
				t.Fatalf("page Validate() = %v", err)
			}
		})
	}
}

// TestProjectChildLiveEventSkipsUnrepresentableDeltas covers deltas the wire
// contract cannot carry. Providers emit empty deltas, and one invalid event
// would fail every page that retains it.
func TestProjectChildLiveEventSkipsUnrepresentableDeltas(t *testing.T) {
	t.Parallel()
	tests := map[string]droids.MessageDelta{
		"empty text":         {MessageID: "message_1", Stream: droids.StreamTextDelta{}},
		"empty thinking":     {MessageID: "message_1", Stream: droids.StreamThinkingDelta{}},
		"anonymous text":     {Stream: droids.StreamTextDelta{Delta: "hello"}},
		"anonymous thinking": {Stream: droids.StreamThinkingDelta{Delta: "pondering"}},
		"no stream":          {},
	}
	for name, delta := range tests {
		t.Run(name, func(t *testing.T) {
			if got, ok := projectChildLiveEvent(droids.EventEnvelope{TurnID: "turn_1", Event: delta}); ok || !reflect.DeepEqual(got, subagent.LiveEvent{}) {
				t.Fatalf("projected = %+v, %t; want zero event, false", got, ok)
			}
		})
	}
}

// wireChildLiveEvent mirrors the server's field-for-field projection so the
// contract sees every field the runtime set.
func wireChildLiveEvent(event subagent.LiveEvent) protocol.SubagentLiveEvent {
	wire := protocol.SubagentLiveEvent{
		Sequence: event.Sequence, Kind: event.Kind, TurnID: event.TurnID,
		MessageID: event.MessageID, ContentIndex: event.ContentIndex,
		Delta: event.Delta, Text: event.Text, ToolCallID: event.ToolCallID,
		ToolName: event.ToolName, IsError: event.IsError,
		BoundaryID: event.BoundaryID, BoundaryKind: event.BoundaryKind, BoundarySource: event.BoundarySource,
		Details: append(json.RawMessage(nil), event.Details...),
	}
	if !event.CreatedAt.IsZero() {
		wire.CreatedAt = event.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return wire
}
