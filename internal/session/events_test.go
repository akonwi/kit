package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

func TestSessionRenamedEventIsSessionScoped(t *testing.T) {
	t.Parallel()
	event := NewEvent{SessionID: "session_test", Kind: EventSessionRenamed, SessionName: "Renamed session"}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	event.TurnID, event.RunID = "turn_parent", "turn_parent"
	if err := event.Validate(); err == nil {
		t.Fatal("session rename event accepted parent turn identity")
	}
	event.TurnID, event.RunID, event.Text = "", "", "unexpected"
	if err := event.Validate(); err == nil {
		t.Fatal("session rename event accepted unrelated payload")
	}
}

func TestSubagentChangedEventIsSessionScoped(t *testing.T) {
	t.Parallel()
	event := NewEvent{
		SessionID: "session_test", Kind: EventSubagentChanged,
		SubagentConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SubagentTaskID:         "task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	event.TurnID = "turn_parent"
	if err := event.Validate(); err == nil {
		t.Fatal("subagent event accepted a parent turn identity")
	}
}

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
			projected := projectDroidEvent("session_1", "turn_1", "turn_1", test.event)
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

func TestProjectDroidEventCarriesAutomaticCompactionLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event         droids.LifecycleEvent
		kind          EventKind
		error         string
		contextTokens int
		contextWindow int
	}{
		{event: droids.LifecycleEvent{Kind: "compaction.started", Data: json.RawMessage(`{"estimated_input":100}`)}, kind: EventCompactionStarted},
		{event: droids.LifecycleEvent{Kind: "compaction.completed", Data: json.RawMessage(`{"before":100,"after":20}`)}, kind: EventCompactionCompleted},
		{event: droids.LifecycleEvent{Kind: "context.updated", Data: json.RawMessage(`{"estimated_input":20,"context_window":200}`)}, kind: EventContextUpdated, contextTokens: 20, contextWindow: 200},
		{event: droids.LifecycleEvent{Kind: "compaction.failed", Data: json.RawMessage(`{"error":"Context compaction failed"}`)}, kind: EventCompactionFailed, error: "Context compaction failed"},
		{event: droids.LifecycleEvent{Kind: "compaction.failed", Data: json.RawMessage(`{"error":"bad\n\u202eerror"}`)}, kind: EventCompactionFailed, error: "bad error"},
	}
	for _, test := range tests {
		projected := projectDroidEvent("session_1", "turn_1", "turn_1", test.event)
		if len(projected) != 1 || projected[0].Kind != test.kind || projected[0].ErrorMessage != test.error ||
			projected[0].ContextTokens != test.contextTokens || projected[0].ContextWindow != test.contextWindow {
			t.Fatalf("projected compaction event = %+v, want kind %q error %q context %d/%d", projected, test.kind, test.error, test.contextTokens, test.contextWindow)
		}
		if err := projected[0].Validate(); err != nil {
			t.Fatalf("projected compaction event validation error = %v", err)
		}
	}
}

func TestProjectDroidEventCarriesAbsoluteCumulativeUsage(t *testing.T) {
	t.Parallel()

	usage := droids.SessionUsage{Input: 10, Output: 4, CacheRead: 3, Reasoning: 2, TotalTokens: 14, Cost: droids.UsageCost{Total: 0.25}}
	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.UsageUpdated{Usage: usage})
	if len(projected) != 1 || projected[0].Kind != EventUsageUpdated || projected[0].Usage == nil {
		t.Fatalf("projected usage event = %+v", projected)
	}
	if projected[0].Usage.Input != 10 || projected[0].Usage.TotalTokens != 14 || projected[0].Usage.Cost.Total != 0.25 {
		t.Fatalf("projected usage = %+v", projected[0].Usage)
	}
}

func TestEventLogAllowsContiguousRetainedSuffix(t *testing.T) {
	t.Parallel()

	log, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	events := make([]NewEvent, 4097)
	for index := range events {
		events[index] = NewEvent{SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: EventRunStarted, Status: RunStatusRunning}
	}
	if err := log.append(events); err != nil {
		t.Fatal(err)
	}
	page := log.page(log.streamID, log.events[0].Sequence-1)
	if page.ResyncRequired || len(page.Events) == 0 || page.Events[0].Sequence != log.events[0].Sequence {
		t.Fatalf("contiguous retained page = %+v", page)
	}
}

func TestEventLogAllowsSnapshotCursorToFollowInvalidatedTail(t *testing.T) {
	t.Parallel()

	log, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	if err := log.append([]NewEvent{{SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: EventRunStarted, Status: RunStatusRunning}}); err != nil {
		t.Fatal(err)
	}
	previousStreamID := log.streamID
	log.invalidate()
	if page := log.page(previousStreamID, 1); !page.ResyncRequired {
		t.Fatalf("cursor predating invalidation did not require resynchronization: %+v", page)
	}
	if page := log.page("", 0); !page.ResyncRequired {
		t.Fatalf("unbound cursor bypassed snapshot resynchronization: %+v", page)
	}
	snapshotCursor := log.next - 1
	if page := log.page(log.streamID, snapshotCursor); page.ResyncRequired {
		t.Fatalf("snapshot cursor unexpectedly required resynchronization: %+v", page)
	}
	if err := log.append([]NewEvent{{SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: EventAssistantStarted, MessageID: "message_1"}}); err != nil {
		t.Fatal(err)
	}
	page := log.page(log.streamID, snapshotCursor)
	if page.ResyncRequired || len(page.Events) != 1 || page.Events[0].Kind != EventAssistantStarted {
		t.Fatalf("snapshot tail page = %+v", page)
	}
}

func TestEventLogRejectsDecreasingUsageWithinStream(t *testing.T) {
	t.Parallel()

	log, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	first := SessionUsage{Input: 10, TotalTokens: 10}
	if err := log.append([]NewEvent{{SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: EventUsageUpdated, Usage: &first}}); err != nil {
		t.Fatal(err)
	}
	increased := SessionUsage{Input: 20, TotalTokens: 20}
	regressed := SessionUsage{Input: 9, TotalTokens: 9}
	if err := log.append([]NewEvent{
		{SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: EventUsageUpdated, Usage: &increased},
		{SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: EventUsageUpdated, Usage: &regressed},
	}); err == nil {
		t.Fatal("event log accepted decreasing usage")
	}
	page := log.page(log.streamID, 0)
	if len(page.Events) != 1 || page.Events[0].Usage == nil || page.Events[0].Usage.Input != 10 {
		t.Fatalf("failed batch partially mutated event log: %+v", page.Events)
	}
}

func TestEventLogWaitBroadcastsAppendedEvents(t *testing.T) {
	t.Parallel()

	log, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result := make(chan EventPage, 2)
	for range 2 {
		go func() {
			page, waitErr := log.wait(ctx, log.streamID, 0)
			if waitErr == nil {
				result <- page
			}
		}()
	}
	if err := log.append([]NewEvent{{
		SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1",
		Kind: EventRunStarted, Status: RunStatusRunning,
	}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case page := <-result:
			if len(page.Events) != 1 || page.Events[0].Sequence != 1 {
				t.Fatalf("wait page = %+v", page)
			}
		case <-ctx.Done():
			t.Fatal("event append did not wake every waiter")
		}
	}
}

func TestProjectDroidEventDoesNotExposeToolArgumentStreaming(t *testing.T) {
	t.Parallel()

	for _, stream := range []droids.StreamEvent{
		droids.StreamToolCallStart{ContentIndex: 1, ID: "call_1", Name: "read"},
		droids.StreamToolCallDelta{ContentIndex: 1, Delta: `{"path":"REA`},
	} {
		projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.MessageDelta{
			MessageID: "message_1", Stream: stream,
		})
		if len(projected) != 0 {
			t.Fatalf("projected provider argument stream event = %+v, want none", projected)
		}
	}
}

func TestProjectDroidEventSuppressesMalformedPlannedToolIdentity(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.MessageDelta{
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

	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.MessageDelta{
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

	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.ToolExecutionStart{
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

	projected = projectDroidEvent("session_1", "turn_1", "turn_1", droids.ToolExecutionStart{
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

	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.ToolExecutionUpdate{
		ToolCallID: "call_1", ToolName: "read",
		Delta: droids.ToolResultDelta{Content: []droids.ResultContent{
			droids.TextContent{Text: "first"},
			droids.FileContent{Filename: "photo.png", MediaType: "image/png", URL: "data:image/png;base64,aW1hZ2U="},
		}, IsError: true},
	})
	if len(projected) != 1 {
		t.Fatalf("projected update count = %d, want 1: %+v", len(projected), projected)
	}
	event := projected[0]
	if event.Kind != EventToolUpdated || len(event.Content) != 2 || event.Content[0].Kind != TranscriptContentText || event.Content[0].Text != "first" || !event.IsError {
		t.Fatalf("text delta = %+v", event)
	}
	if event.Content[1].Kind != TranscriptContentImage || event.Content[1].Filename != "photo.png" || event.Content[1].MediaType != "image/png" {
		t.Fatalf("image delta = %+v", event)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("tool update validation error = %v", err)
	}
}

func TestProjectDroidEventPreservesAuthoritativeToolResultAndDetails(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read",
		Result: droids.ToolResult{
			Content: []droids.ResultContent{
				droids.TextContent{Text: "complete"},
				droids.FileContent{Filename: "report.txt", MediaType: "text/plain", URL: "data:text/plain;base64,eA=="},
			},
			Details: json.RawMessage(`{"lines":3}`),
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

	content := make([]droids.ResultContent, maxLiveEventContentBlocks+1)
	for index := range content {
		content[index] = droids.FileContent{MediaType: "image/png", URL: "data:image/png;base64,aQ=="}
	}
	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read", Result: droids.ToolResult{Content: content},
	})
	if len(projected) != 1 || len(projected[0].Content) != maxLiveEventContentBlocks || !projected[0].ContentTruncated {
		t.Fatalf("bounded non-text content = count %d truncated %t", len(projected[0].Content), projected[0].ContentTruncated)
	}
}

func TestProjectDroidEventOmitsInvalidToolContentMetadata(t *testing.T) {
	t.Parallel()

	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read",
		Result: droids.ToolResult{Content: []droids.ResultContent{
			droids.FileContent{MediaType: "not-a-media-type", URL: "https://example.com/image"},
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

	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.ToolExecutionEnd{
		ToolCallID: "call_1", ToolName: "read",
		Result: droids.ToolResult{Details: json.RawMessage(`{"value":"` + strings.Repeat("x", maxLiveEventDetailsBytes) + `"}`)},
	})
	if len(projected) != 1 || len(projected[0].Details) != 0 || !projected[0].DetailsOmitted {
		t.Fatalf("oversized details event = %+v", projected)
	}
}

func TestProjectDroidEventChunksLargeStreamingDeltaWithoutDataLoss(t *testing.T) {
	t.Parallel()

	delta := strings.Repeat("résumé ", maxLiveEventTextBytes/4)
	projected := projectDroidEvent("session_1", "turn_1", "turn_1", droids.MessageDelta{
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

	text, thinking := assistantPresentation(droids.AssistantMessage{Content: []droids.AssistantContent{
		droids.ThinkingContent{Thinking: "visible"},
		droids.ThinkingContent{Thinking: "secret", Redacted: true, Signature: "opaque"},
		droids.TextContent{Text: "response"},
	}})
	if thinking != "visible" || text != "response" {
		t.Fatalf("assistant presentation = thinking %q text %q", thinking, text)
	}
}

func TestRuntimeEventLogInvalidatesReplayAfterRetentionOrStreamReplacement(t *testing.T) {
	log, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	start := NewEvent{SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1", Kind: EventRunStarted, Status: RunStatusRunning}
	if err := log.append([]NewEvent{start}); err != nil {
		t.Fatal(err)
	}
	first := log.page("", 0)
	if first.ResyncRequired || len(first.Events) != 1 {
		t.Fatalf("initial page = %+v", first)
	}
	oldStream := first.StreamID
	if err := log.reset(); err != nil {
		t.Fatal(err)
	}
	if page := log.page(oldStream, 0); !page.ResyncRequired {
		t.Fatalf("replaced stream page = %+v", page)
	}
	if err := log.append([]NewEvent{start}); err != nil {
		t.Fatal(err)
	}
	updates := make([]NewEvent, 4096)
	for index := range updates {
		updates[index] = NewEvent{
			SessionID: "session_1", TurnID: "turn_1", RunID: "turn_1",
			Kind: EventAssistantTextDelta, MessageID: "message_1", ContentIndex: 0, Delta: "x",
		}
	}
	if err := log.append(updates); err != nil {
		t.Fatal(err)
	}
	if page := log.page("", 0); !page.ResyncRequired {
		t.Fatalf("retained stream page = %+v, want resync", page)
	}
}
