package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionRenamedEventValidation(t *testing.T) {
	t.Parallel()
	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		Kind: SessionEventSessionRenamed, SessionName: "Renamed session",
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	batch := SessionEventBatch{StreamID: event.StreamID, FirstSequence: 1, LastSequence: 1, Events: []SessionEvent{event}}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	event.TurnID, event.RunID = "turn_parent", "turn_parent"
	if err := event.Validate(); err == nil {
		t.Fatal("session rename event accepted parent run identity")
	}
}

func TestPeerQueryChangedEventValidation(t *testing.T) {
	t.Parallel()
	event := SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", Kind: SessionEventPeerQueryChanged, PeerRequestID: "peer_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	batch := SessionEventBatch{StreamID: event.StreamID, FirstSequence: 1, LastSequence: 1, Events: []SessionEvent{event}}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSubagentChangedEventValidation(t *testing.T) {
	t.Parallel()
	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		Kind:                   SessionEventSubagentChanged,
		SubagentConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SubagentTaskID:         "task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	batch := SessionEventBatch{StreamID: event.StreamID, FirstSequence: 1, LastSequence: 1, Events: []SessionEvent{event}}
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	event.RunID, event.TurnID = "turn_parent", "turn_parent"
	if err := event.Validate(); err == nil {
		t.Fatal("subagent event accepted parent run identity")
	}
}

func TestSessionEventBatchValidatesExpectedTurnSequence(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 9, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventRunStarted, Status: RunStatusRunning},
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 6, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventThinkingDelta, ContentIndex: 0, Delta: "plan"},
		{StreamID: "stream_test", Sequence: 7, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventAssistantTextDelta, ContentIndex: 1, Delta: "answer"},
		{StreamID: "stream_test", Sequence: 8, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventToolStarted, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
		{StreamID: "stream_test", Sequence: 9, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventRunFinished, Status: RunStatusCompleted},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventValidatesAutomaticCompactionLifecycle(t *testing.T) {
	base := SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test"}
	for _, kind := range []SessionEventKind{SessionEventCompactionStarted, SessionEventCompactionCompleted} {
		event := base
		event.Kind = kind
		event.CompactionID = "compact_00000000000000000000000000000001"
		if err := event.Validate(); err != nil {
			t.Fatalf("%s Validate() error = %v", kind, err)
		}
	}
	contextUpdated := base
	contextUpdated.Kind = SessionEventContextUpdated
	contextUpdated.ContextTokens = 20
	contextUpdated.ContextWindow = 200
	if err := contextUpdated.Validate(); err != nil {
		t.Fatalf("context update Validate() error = %v", err)
	}
	contextUpdated.ContextWindow = 0
	if err := contextUpdated.Validate(); err == nil {
		t.Fatal("context update accepted an absent context window")
	}
	failed := base
	failed.Kind = SessionEventCompactionFailed
	failed.CompactionID = "compact_00000000000000000000000000000001"
	failed.ErrorMessage = "Context compaction failed"
	if err := failed.Validate(); err != nil {
		t.Fatalf("failed compaction Validate() error = %v", err)
	}
	failed.ErrorMessage = ""
	if err := failed.Validate(); err == nil {
		t.Fatal("failed compaction accepted an empty error")
	}
	malformed := base
	malformed.Kind = SessionEventCompactionStarted
	malformed.CompactionID = "bad\nidentity"
	if err := malformed.Validate(); err == nil {
		t.Fatal("compaction accepted a malformed identity")
	}
	malformed.CompactionID = "compact_00000000000000000000000000000001"
	malformed.Text = "unrelated"
	if err := malformed.Validate(); err == nil {
		t.Fatal("compaction accepted assistant content")
	}
	malformed.Text = ""
	malformed.Kind = SessionEventRunStarted
	malformed.Status = RunStatusRunning
	malformed.CompactionID = "compact_00000000000000000000000000000001"
	if err := malformed.Validate(); err == nil {
		t.Fatal("non-compaction event accepted a compaction identity")
	}
}

func TestSessionEventValidatesProviderRetryLifecycle(t *testing.T) {
	t.Parallel()

	base := SessionEvent{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test"}
	scheduled := base
	scheduled.Kind = SessionEventProviderRetryScheduled
	scheduled.ProviderRetry = &ProviderRetry{Count: 2, RetryAt: time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)}
	if err := scheduled.Validate(); err != nil {
		t.Fatalf("past scheduled retry Validate() error = %v", err)
	}
	scheduled.ProviderRetry.RetryAt = "not-a-time"
	if err := scheduled.Validate(); err == nil {
		t.Fatal("scheduled retry accepted an invalid deadline")
	}

	started := base
	started.Kind = SessionEventProviderRetryStarted
	started.ProviderRetry = &ProviderRetry{Count: 2}
	if err := started.Validate(); err != nil {
		t.Fatalf("started retry Validate() error = %v", err)
	}
	started.ProviderRetry.RetryAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := started.Validate(); err == nil {
		t.Fatal("started retry accepted a deadline")
	}
	started.ProviderRetry.RetryAt = ""
	started.Text = "unrelated"
	if err := started.Validate(); err == nil {
		t.Fatal("started retry accepted assistant content")
	}

	unrelated := base
	unrelated.Kind = SessionEventCompactionStarted
	unrelated.CompactionID = "compact_00000000000000000000000000000001"
	unrelated.ProviderRetry = &ProviderRetry{Count: 1}
	if err := unrelated.Validate(); err == nil {
		t.Fatal("unrelated event accepted provider retry state")
	}
}

func TestSessionEventBatchValidatesAbsoluteUsageUpdates(t *testing.T) {
	t.Parallel()

	first := SessionUsage{Input: 10, TotalTokens: 10, Cost: SessionUsageCost{Total: 0.1}}
	second := SessionUsage{Input: 20, Output: 5, TotalTokens: 25, Cost: SessionUsageCost{Total: 0.2}}
	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 2, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUsageUpdated, Usage: &first},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUsageUpdated, Usage: &second},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	regressed := SessionUsage{Input: 9, TotalTokens: 9, Cost: SessionUsageCost{Total: 0.09}}
	batch.Events[1].Usage = &regressed
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted decreasing session usage")
	}
	batch = SessionEventBatch{
		StreamID: "stream_test", FirstSequence: 1, LastSequence: 2, UsageBaseline: &first,
		Events: []SessionEvent{{
			StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test",
			Kind: SessionEventUsageUpdated, Usage: &regressed,
		}},
	}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted a decrease from the preceding page baseline")
	}
}

func TestSessionEventBatchRejectsSequenceGaps(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 5, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted a sequence gap")
	}
}

func TestSessionEventToolPlanRequiresCompleteArguments(t *testing.T) {
	t.Parallel()

	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test",
		Kind: SessionEventToolPlanned, ToolCallID: "call_test", ToolName: "read",
	}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() accepted a planned tool call without arguments")
	}
	event.Arguments = `{"path":"README.md"}`
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventValidatesStructuredToolResultLifecycle(t *testing.T) {
	t.Parallel()

	update := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventToolUpdated,
		ToolCallID: "call_test", ToolName: "read",
		Content: []TranscriptContent{{Kind: TranscriptContentText, Text: "chunk"}},
	}
	if err := update.Validate(); err != nil {
		t.Fatalf("update Validate() error = %v", err)
	}
	completed := update
	completed.Kind = SessionEventToolCompleted
	completed.Content = []TranscriptContent{
		{Kind: TranscriptContentText, Text: "complete"},
		{Kind: TranscriptContentFile, Filename: "report.txt", MediaType: "text/plain"},
	}
	completed.Details = json.RawMessage(`{"lines":3}`)
	if err := completed.Validate(); err != nil {
		t.Fatalf("completed Validate() error = %v", err)
	}
	completed.Text = "flattened"
	if err := completed.Validate(); err == nil {
		t.Fatal("Validate() accepted flattened tool text")
	}
	completed.Text = ""
	completed.Content = make([]TranscriptContent, maxSessionEventContentBlocks+1)
	if err := completed.Validate(); err == nil {
		t.Fatal("Validate() accepted too many tool content blocks")
	}
}

func TestSessionEventRequiresAssistantMessageIdentity(t *testing.T) {
	t.Parallel()

	event := SessionEvent{
		StreamID: "stream_test", Sequence: 1, SessionID: "session_test",
		TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventAssistantStarted,
	}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() accepted an assistant event without a message id")
	}
}

func TestSessionEventBatchRejectsAssistantIdentityChange(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 3, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_one", Kind: SessionEventAssistantStarted},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_two", Kind: SessionEventAssistantTextDelta, Delta: "changed"},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted an assistant identity change")
	}
}

func TestSessionEventBatchPreservesAssistantIdentityAcrossRename(t *testing.T) {
	t.Parallel()
	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 3, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_one", Kind: SessionEventAssistantStarted},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", Kind: SessionEventSessionRenamed, SessionName: "Renamed"},
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_one", Kind: SessionEventAssistantTextDelta, Delta: "continued"},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventBatchScopesAssistantIdentityToRun(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 4, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 1, SessionID: "session_test", TurnID: "turn_one", RunID: "turn_one", MessageID: "message_one", Kind: SessionEventAssistantStarted},
		{StreamID: "stream_test", Sequence: 2, SessionID: "session_test", TurnID: "turn_one", RunID: "turn_one", Kind: SessionEventRunFinished, Status: RunStatusInterrupted, ErrorMessage: "daemon restarted"},
		{StreamID: "stream_test", Sequence: 3, SessionID: "session_test", TurnID: "turn_two", RunID: "turn_two", Kind: SessionEventRunStarted, Status: RunStatusRunning},
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_two", RunID: "turn_two", MessageID: "message_two", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSessionEventBatchRejectsOutOfOrderUpdates(t *testing.T) {
	t.Parallel()

	batch := SessionEventBatch{StreamID: "stream_test", FirstSequence: 1, LastSequence: 5, Events: []SessionEvent{
		{StreamID: "stream_test", Sequence: 5, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", Kind: SessionEventUserMessage, Text: "hello"},
		{StreamID: "stream_test", Sequence: 4, SessionID: "session_test", TurnID: "turn_test", RunID: "turn_test", MessageID: "message_test", Kind: SessionEventAssistantStarted},
	}}
	if err := batch.Validate(); err == nil {
		t.Fatal("Validate() accepted out-of-order updates")
	}
}
