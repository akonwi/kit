package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestRecoveredRunStatusClearsOnlyOnFreshEvidence(t *testing.T) {
	t.Parallel()
	state := appState{recovery: footerReconnectingActivity, liveSequence: 7, runPending: true}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 7, Kind: protocol.SessionEventUsageUpdated}})
	if state.recovery != footerReconnectingActivity {
		t.Fatalf("stale event changed recovery to %v", state.recovery)
	}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 8, Kind: protocol.SessionEventUsageUpdated}})
	if state.recovery != footerHealthy {
		t.Fatalf("fresh event left recovery at %v", state.recovery)
	}
	state.recovery = footerSyncingFinalTranscript
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 9, Kind: protocol.SessionEventUsageUpdated}})
	if state.recovery != footerSyncingFinalTranscript {
		t.Fatalf("run event cleared transcript sync: %v", state.recovery)
	}
}

func TestActiveRunAdmissionGuardPreservesDraftAndShowsInformationalToast(t *testing.T) {
	_, state, _ := mountRunAbort(t)
	state.recovery = footerReconnectingActivity
	state.composer = "another prompt"
	state.startPromptSubmission("another prompt", func(context.Context) (sessionclient.Run, error) {
		t.Fatal("submitted a prompt during an active run")
		return nil, nil
	})
	if state.recovery != footerReconnectingActivity || state.composer != "another prompt" || !state.runPending {
		t.Fatalf("active run guard: recovery=%v composer=%q pending=%t", state.recovery, state.composer, state.runPending)
	}
	if got := state.toasts.Snapshot(); len(got) != 1 || got[0].Title != "Run in progress" || got[0].Variant != toastInfo || got[0].Persistent {
		t.Fatalf("defensive admission feedback = %+v, want transient information toast", got)
	}
}

type blockingDiffPreferenceService struct {
	calls   chan bool
	release chan struct{}
}

func (s blockingDiffPreferenceService) SetWrapLines(enabled bool) error {
	s.calls <- enabled
	<-s.release
	return nil
}

func TestDiffPreferenceWritesCoalesceInOrderAndFlushBoundedly(t *testing.T) {
	service := blockingDiffPreferenceService{calls: make(chan bool, 2), release: make(chan struct{}, 2)}
	state := &appState{}
	state.enqueueDiffPreferenceWrite(service, true, nil)
	if got := <-service.calls; !got {
		t.Fatalf("first persisted value = %v, want true", got)
	}
	state.enqueueDiffPreferenceWrite(service, false, nil)
	service.release <- struct{}{}
	if got := <-service.calls; got {
		t.Fatalf("coalesced persisted value = %v, want false", got)
	}
	if state.flushDiffPreferenceWrites(10 * time.Millisecond) {
		t.Fatal("flush completed while latest preference write was blocked")
	}
	service.release <- struct{}{}
	if !state.flushDiffPreferenceWrites(time.Second) {
		t.Fatal("flush did not complete after latest preference write")
	}
}

func liveToolMessage(t *testing.T, state *appState, callID string) transcriptMessage {
	t.Helper()
	for _, message := range state.liveMessages {
		if message.Role == "tool" && message.ToolCallID == callID {
			return message
		}
	}
	t.Fatalf("live tool %q not found in %+v", callID, state.liveMessages)
	return transcriptMessage{}
}

func TestSessionRenameEventUpdatesAttachedSessionAndExplorer(t *testing.T) {
	t.Parallel()
	state := appState{
		session: protocol.SessionInfo{ID: "session_1", Name: "Before"},
		sessionExplorer: sessionExplorerController{Sessions: []sessionExplorerItem{{
			ID: "session_1", Name: "Before",
		}}},
	}
	state.applySessionMetadataEvents([]protocol.SessionEvent{{
		StreamID: "stream_1", Sequence: 3, SessionID: "session_1", Kind: protocol.SessionEventSessionRenamed, SessionName: "After",
	}})
	state.applySnapshot(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session_1", Name: "Before"}, EventStreamID: "stream_1", EventCursor: 2,
	})
	if state.session.Name != "After" || state.sessionExplorer.Sessions[0].Name != "After" || state.metadataStreamID != "stream_1" || state.metadataSequence != 3 {
		t.Fatalf("renamed state = session:%+v explorer:%+v", state.session, state.sessionExplorer.Sessions)
	}
}

func TestSessionMetadataBaselineReplacesEventStreamAuthority(t *testing.T) {
	t.Parallel()
	state := appState{
		session:          protocol.SessionInfo{ID: "session_1", Name: "Old"},
		metadataStreamID: "stream_old", metadataSequence: 9,
	}
	state.applySessionMetadataBaseline(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session_1", Name: "Authoritative"}, EventStreamID: "stream_new", EventCursor: 0,
		SubagentDefinitions: []protocol.SubagentDefinition{{Name: "demo.reviewer", Description: "Reviews"}},
	})
	if state.session.Name != "Authoritative" || state.metadataStreamID != "stream_new" || state.metadataSequence != 0 || len(state.subagentDefinitions) != 1 || state.subagentDefinitions[0].Name != "demo.reviewer" {
		t.Fatalf("metadata baseline = session:%+v stream:%q sequence:%d subagents:%+v", state.session, state.metadataStreamID, state.metadataSequence, state.subagentDefinitions)
	}
}

func TestAcceptedPromptMergesDeferredAutonomousResponse(t *testing.T) {
	t.Parallel()
	state := appState{
		operation:    7,
		messages:     []transcriptMessage{{Role: "assistant", Text: "older"}},
		liveMessages: []transcriptMessage{{Role: "user", Text: "new prompt"}},
		deferredSessionSnapshot: &protocol.SessionSnapshot{Messages: []protocol.TranscriptMessage{{
			Role: "assistant", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "autonomous response"}},
		}}},
	}
	if !state.acceptPromptAdmission(7, appTestRun{id: "run_new"}) {
		t.Fatal("prompt admission was rejected")
	}
	if state.deferredSessionSnapshot != nil || state.activeRunID != "run_new" || len(state.messages) != 1 || state.messages[0].Text != "autonomous response" || len(state.liveMessages) != 1 {
		t.Fatalf("accepted state = deferred:%v run:%q messages:%+v live:%+v", state.deferredSessionSnapshot, state.activeRunID, state.messages, state.liveMessages)
	}
}

type appTestRun struct{ id string }

func (r appTestRun) ID() string { return r.id }
func (appTestRun) Wait(context.Context) (protocol.PromptOutcome, error) {
	return protocol.PromptOutcome{}, nil
}
func (appTestRun) Abort(context.Context) error { return nil }

type recordingAbortRun struct {
	calls chan struct{}
}

func (recordingAbortRun) ID() string { return "turn_test" }
func (recordingAbortRun) Wait(context.Context) (protocol.PromptOutcome, error) {
	return protocol.PromptOutcome{}, nil
}
func (run recordingAbortRun) Abort(context.Context) error {
	run.calls <- struct{}{}
	return nil
}

func TestDismissIgnoresRepeatedRunAbort(t *testing.T) {
	calls := make(chan struct{}, 1)
	state := &appState{
		phase: phaseReady, runPending: true, runStopping: true,
		activeRun: recordingAbortRun{calls: calls}, activeRunID: "turn_test",
	}
	state.dismiss(ui.EventContext{})
	select {
	case <-calls:
		t.Fatal("repeated Escape issued another run abort")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestAttachedSnapshotReconcilesCompletedAndSuccessorRuns(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                       string
		pending                    bool
		activeRunID, snapshotRunID string
		want                       bool
	}{
		{name: "idle baseline catches completed run", want: true},
		{name: "admission keeps optimistic prompt", pending: true, want: false},
		{name: "same active run stays with current watcher", pending: true, activeRunID: "run_a", snapshotRunID: "run_a", want: false},
		{name: "successor replaces stale active run", pending: true, activeRunID: "run_a", snapshotRunID: "run_b", want: true},
		{name: "terminal snapshot settles stale active run", pending: true, activeRunID: "run_a", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldApplyAttachedSnapshot(test.pending, test.activeRunID, test.snapshotRunID); got != test.want {
				t.Fatalf("should apply = %t, want %t", got, test.want)
			}
		})
	}
}

func TestAttachedRunLifecycleFindsAutonomousRunBoundaries(t *testing.T) {
	t.Parallel()
	started, finished, status := attachedRunLifecycle([]protocol.SessionEvent{
		{Kind: protocol.SessionEventSubagentChanged},
		{Kind: protocol.SessionEventRunStarted, RunID: "run_autonomous", Status: protocol.RunStatusRunning},
		{Kind: protocol.SessionEventAssistantCompleted, RunID: "run_autonomous"},
		{Kind: protocol.SessionEventRunFinished, RunID: "run_autonomous", Status: protocol.RunStatusCompleted},
	})
	if started != "run_autonomous" || finished != "run_autonomous" || status != protocol.RunStatusCompleted {
		t.Fatalf("attached lifecycle = started:%q finished:%q status:%q", started, finished, status)
	}
}

func TestProviderRetryEventsDriveCountdownState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	state := &appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 1, Kind: protocol.SessionEventProviderRetryScheduled,
		ProviderRetry: &protocol.ProviderRetry{Count: 2, RetryAt: now.Add(3500 * time.Millisecond).Format(time.RFC3339Nano)},
	}})
	if got := state.presentedTurnActivity(now); got != "Retry 2 in 4s…" {
		t.Fatalf("scheduled retry activity = %q", got)
	}
	if got := state.presentedTurnActivity(now.Add(10 * time.Second)); got != "Retry 2 in 0s…" {
		t.Fatalf("past retry activity = %q", got)
	}
	state.setTurnActivity("Stopping…")
	state.runStopping = true
	if got := state.presentedTurnActivity(now); got != "Stopping…" {
		t.Fatalf("stopping precedence activity = %q", got)
	}
	state.runStopping = false
	state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 2, Kind: protocol.SessionEventProviderRetryStarted,
		ProviderRetry: &protocol.ProviderRetry{Count: 2},
	}})
	if state.providerRetry != nil || state.presentedTurnActivity(now) != "Working…" {
		t.Fatalf("started retry state = retry %+v activity %q", state.providerRetry, state.presentedTurnActivity(now))
	}
}

func TestProviderRetrySnapshotRestoresCountdownState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	state := &appState{}
	state.applySnapshot(protocol.SessionSnapshot{
		ActiveRunID:   "turn_test",
		ProviderRetry: &protocol.ProviderRetry{Count: 1, RetryAt: now.Add(time.Second).Format(time.RFC3339Nano)},
	})
	if got := state.presentedTurnActivity(now); got != "Retry 1 in 1s…" {
		t.Fatalf("restored retry activity = %q", got)
	}
}

func TestAutomaticCompactionEventsShowPendingAndOutcomeFeedback(t *testing.T) {
	var toasts []toastInput
	state := &appState{showToastOverride: func(toast toastInput) { toasts = append(toasts, toast) }}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 1, Kind: protocol.SessionEventCompactionStarted, CompactionID: "compact_00000000000000000000000000000001"}})
	if state.turnActivity != "Compacting session…" {
		t.Fatalf("started compaction activity = %q", state.turnActivity)
	}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 2, Kind: protocol.SessionEventCompactionCompleted, CompactionID: "compact_00000000000000000000000000000001"},
		{Sequence: 3, Kind: protocol.SessionEventContextUpdated, ContextTokens: 20, ContextWindow: 200},
	})
	if state.turnActivity != "Working…" || state.contextTokens != 20 || state.contextWindow != 200 || len(toasts) != 1 || toasts[0].Title != "Session compacted" ||
		toasts[0].Subtitle != "Session context was compacted." || toasts[0].Variant != toastInfo {
		t.Fatalf("completed compaction activity=%q context=%d/%d toasts=%+v", state.turnActivity, state.contextTokens, state.contextWindow, toasts)
	}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 4, Kind: protocol.SessionEventCompactionFailed, CompactionID: "compact_00000000000000000000000000000002", ErrorMessage: "Context compaction failed"}})
	if len(toasts) != 2 || toasts[1].Title != "Auto-compaction failed" || toasts[1].Subtitle != "Context compaction failed" || toasts[1].Variant != toastError {
		t.Fatalf("failed compaction toasts = %+v", toasts)
	}
}

func TestCompactionSnapshotAndDelayedEventsDoNotRegressNewerOperation(t *testing.T) {
	const (
		first  = "compact_00000000000000000000000000000001"
		second = "compact_00000000000000000000000000000002"
	)
	var toasts []toastInput
	state := &appState{showToastOverride: func(toast toastInput) { toasts = append(toasts, toast) }}
	state.applySnapshot(protocol.SessionSnapshot{
		ActiveRunID: "turn_test", EventStreamID: "stream_test", EventCursor: 4,
		ActiveCompaction: &protocol.ActiveCompaction{ID: first, RunID: "turn_test"},
	})
	if state.activeCompactionID != first || state.turnActivity != "Compacting session…" {
		t.Fatalf("restored compaction = %q activity=%q", state.activeCompactionID, state.turnActivity)
	}

	state.applyRunEvents([]protocol.SessionEvent{{StreamID: "stream_test", Sequence: 5, Kind: protocol.SessionEventCompactionStarted, CompactionID: second}})
	state.applySnapshot(protocol.SessionSnapshot{ActiveRunID: "turn_test", EventStreamID: "stream_test", EventCursor: 4,
		ActiveCompaction: &protocol.ActiveCompaction{ID: first, RunID: "turn_test"}})
	state.applyRunEvents([]protocol.SessionEvent{{StreamID: "stream_test", Sequence: 6, Kind: protocol.SessionEventCompactionCompleted, CompactionID: first}})
	if state.activeCompactionID != second || state.turnActivity != "Compacting session…" || len(toasts) != 0 {
		t.Fatalf("delayed state regressed: active=%q activity=%q toasts=%+v", state.activeCompactionID, state.turnActivity, toasts)
	}

	outcome := protocol.SessionEvent{StreamID: "stream_test", Kind: protocol.SessionEventCompactionCompleted, CompactionID: second}
	outcome.Sequence = 7
	state.applyRunEvents([]protocol.SessionEvent{outcome})
	outcome.Sequence = 8
	state.applyRunEvents([]protocol.SessionEvent{outcome})
	if state.activeCompactionID != "" || len(toasts) != 1 {
		t.Fatalf("duplicate outcome: active=%q toasts=%+v", state.activeCompactionID, toasts)
	}
}

func TestCompletedChangeCWDToolUpdatesSessionScopeAndRequestsToast(t *testing.T) {
	details, err := json.Marshal(cwdToolDetails{CWD: "/repo/nested", Changed: true})
	if err != nil {
		t.Fatal(err)
	}
	state := &appState{
		session: protocol.SessionInfo{CWD: "/repo"}, liveAssistant: -1,
		liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock),
	}
	changed := state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 1, Kind: protocol.SessionEventToolCompleted,
		TurnID: "turn_1", ToolCallID: "call_1", ToolName: "change_cwd", Details: details,
	}})
	if changed != "/repo/nested" || state.session.CWD != "/repo/nested" || state.location != "/repo/nested" {
		t.Fatalf("cwd completion changed=%q session=%q location=%q", changed, state.session.CWD, state.location)
	}
}

func TestSubagentFallbackToolResultRequestsEphemeralToast(t *testing.T) {
	details, err := json.Marshal(subagentToolDetails{Warning: `Subagent "reviewer" requested unavailable model "production/reviewer"; using active model "openai/gpt-5".`})
	if err != nil {
		t.Fatal(err)
	}
	state := &appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 1, Kind: protocol.SessionEventToolCompleted,
		TurnID: "turn_1", ToolCallID: "call_1", ToolName: "subagent", Details: details,
	}})
	if len(state.eventToasts) != 1 {
		t.Fatalf("subagent fallback toasts = %+v", state.eventToasts)
	}
	toast := state.eventToasts[0]
	if toast.Title != "Subagent provider unavailable" || toast.Variant != toastWarning || toast.Persistent {
		t.Fatalf("subagent fallback toast = %+v", toast)
	}
	if !strings.Contains(toast.Subtitle, "using active model") {
		t.Fatalf("subagent fallback subtitle = %q", toast.Subtitle)
	}
}

func TestTranscriptScrollWaitsForUpdatedLayout(t *testing.T) {
	t.Parallel()

	state := appState{}
	state.requestTranscriptScroll()
	if !state.TickFrame(time.Now()) || !state.needsScroll || state.scrollPendingLayout {
		t.Fatalf("first frame state = needs:%t pending:%t, want deferred scroll", state.needsScroll, state.scrollPendingLayout)
	}
	if state.TickFrame(time.Now()) || state.needsScroll {
		t.Fatalf("second frame state = needs:%t, want settled unattached scroll", state.needsScroll)
	}
}

func TestPendingTranscriptFollowUsesLatestCompletedLayout(t *testing.T) {
	t.Parallel()

	messages := make([]transcriptMessage, 24)
	for index := range messages {
		messages[index] = transcriptMessage{
			ID: "assistant_" + strconv.Itoa(index), TurnID: "turn_1", Role: "assistant",
			Text: "streamed response line " + strconv.Itoa(index),
		}
	}
	state := appState{}
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: messages, Scroll: &state.scroll,
	}})
	app.Pump(80, 16)
	state.scroll.ScrollToStart()
	state.requestTranscriptScroll()
	if !state.TickFrame(time.Now()) {
		t.Fatal("pending transcript follow did not retain its post-layout frame")
	}
	metrics := state.scroll.Metrics()
	if metrics.MaxScrollOffset == 0 || metrics.ScrollOffset != metrics.MaxScrollOffset {
		t.Fatalf("pending transcript follow = %+v, want latest completed layout at end", metrics)
	}

	state.scroll.ScrollToStart()
	state.requestTranscriptScroll()
	state.TickFrame(time.Now())
	metrics = state.scroll.Metrics()
	if metrics.ScrollOffset != metrics.MaxScrollOffset {
		t.Fatalf("repeated live update starved transcript follow: %+v", metrics)
	}
}

func TestSnapshotRefreshRetainsEarlierTranscriptMessages(t *testing.T) {
	t.Parallel()

	state := appState{messages: projectTranscript([]protocol.TranscriptMessage{
		{ID: "message_1", Sequence: 1, TurnID: "turn_1", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "earlier"}}},
		{ID: "message_2", Sequence: 2, TurnID: "turn_2", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "previous window"}}},
	})}
	state.mergeSnapshotTranscript(projectTranscript([]protocol.TranscriptMessage{
		{ID: "message_2", Sequence: 2, TurnID: "turn_2", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "refreshed overlap"}}},
		{ID: "message_3", Sequence: 3, TurnID: "turn_3", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "latest window"}}},
	}))

	if len(state.messages) != 3 {
		t.Fatalf("merged transcript count = %d, want 3", len(state.messages))
	}
	for index, want := range []string{"message_1", "message_2", "message_3"} {
		if state.messages[index].ID != want {
			t.Fatalf("merged transcript[%d] = %q, want %q", index, state.messages[index].ID, want)
		}
	}
}

func TestSnapshotRefreshDropsCachedHistoryWithoutOverlap(t *testing.T) {
	t.Parallel()

	state := appState{
		messages:                     []transcriptMessage{{ID: "message_1", Sequence: 1}, {ID: "message_2", Sequence: 2}},
		transcriptHistoryInitialized: true,
		transcriptHistoryCursor:      "1",
		transcriptHistoryHasMore:     true,
		transcriptHistoryLoading:     true,
	}
	projected := []transcriptMessage{{ID: "message_10", Sequence: 10}, {ID: "message_11", Sequence: 11}}
	if state.mergeSnapshotTranscript(projected) {
		t.Fatal("disjoint snapshot incorrectly retained cached history")
	}
	state.resetTranscriptHistoryFromSnapshot(protocol.SessionSnapshot{PreviousMessageCursor: "10", HasMoreMessages: true})
	if len(state.messages) != 2 || state.messages[0].ID != "message_10" {
		t.Fatalf("disjoint snapshot merge = %+v", state.messages)
	}
	if state.transcriptHistoryCursor != "10" || state.transcriptHistoryLoading {
		t.Fatalf("reset pagination = cursor %q loading %t", state.transcriptHistoryCursor, state.transcriptHistoryLoading)
	}
}

func TestTranscriptAnchorRestoreSuppressesFollowRequest(t *testing.T) {
	t.Parallel()

	state := appState{transcriptHistoryRestore: 1}
	state.requestTranscriptScroll()
	if state.needsScroll || state.scrollPendingLayout {
		t.Fatalf("anchor restoration queued transcript follow: needs %t pending %t", state.needsScroll, state.scrollPendingLayout)
	}
}

func TestPrependTranscriptHistoryRejectsConflictingOverlap(t *testing.T) {
	t.Parallel()

	state := appState{messages: []transcriptMessage{{ID: "message_existing", Sequence: 4}}}
	err := state.prependTranscriptHistory(protocol.TranscriptPage{Messages: []protocol.TranscriptMessage{{
		ID: "message_other", Sequence: 4, TurnID: "turn_1", Role: "user",
		Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "conflict"}},
	}}})
	if err == nil {
		t.Fatal("conflicting transcript page was accepted")
	}
	if len(state.messages) != 1 || state.messages[0].ID != "message_existing" {
		t.Fatalf("conflicting prepend mutated transcript: %+v", state.messages)
	}
}

func TestPrependTranscriptHistoryAdvancesCursor(t *testing.T) {
	t.Parallel()

	state := appState{
		messages:                 []transcriptMessage{{ID: "message_3", Sequence: 3}},
		transcriptHistoryCursor:  "3",
		transcriptHistoryHasMore: true,
	}
	err := state.prependTranscriptHistory(protocol.TranscriptPage{
		Messages: []protocol.TranscriptMessage{
			{ID: "message_1", Sequence: 1, TurnID: "turn_1", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "one"}}},
			{ID: "message_2", Sequence: 2, TurnID: "turn_2", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "two"}}},
		},
		PreviousMessageCursor: "1", HasMoreMessages: true,
	})
	if err != nil {
		t.Fatalf("prepend transcript history: %v", err)
	}
	if len(state.messages) != 3 || state.messages[0].Sequence != 1 || state.messages[2].Sequence != 3 {
		t.Fatalf("prepended transcript = %+v", state.messages)
	}
	if state.transcriptHistoryCursor != "1" || !state.transcriptHistoryHasMore {
		t.Fatalf("pagination state = cursor %q more %t", state.transcriptHistoryCursor, state.transcriptHistoryHasMore)
	}
}

func TestSnapshotPreservesExpandedActivityForStableToolCall(t *testing.T) {
	t.Parallel()

	key := activityToolKey{TurnID: "turn_1", ToolCallID: "call_1"}
	state := appState{
		activitySourceID: "turn-work:turn_1:assistant_1",
		activityExpanded: map[activityToolKey]bool{key: true},
	}
	state.applySnapshot(protocol.SessionSnapshot{Messages: []protocol.TranscriptMessage{
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Content: []protocol.TranscriptContent{{
			Kind: protocol.TranscriptContentToolCall, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`,
		}}},
		{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{
			Kind: protocol.TranscriptContentText, Text: "contents",
		}}},
	}})
	if state.activitySourceID != "turn-work:turn_1:assistant_1" || !state.activityExpanded[key] {
		t.Fatalf("stable Activity reconciliation = source %q expanded %+v", state.activitySourceID, state.activityExpanded)
	}
}

func TestSnapshotReconcilesExpandedActivityAcrossLiveSourceIdentity(t *testing.T) {
	t.Parallel()

	key := activityToolKey{TurnID: "turn_1", ToolCallID: "call_1"}
	state := appState{
		messages: []transcriptMessage{{
			ID: "live-assistant:call_1", TurnID: "turn_1", Role: "assistant",
			ToolCalls: []transcriptToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`)}},
		}},
		activitySourceID:   "turn-work:turn_1:live-assistant:call_1",
		activityExpanded:   map[activityToolKey]bool{key: true},
		inlineActivityOpen: map[string]bool{"turn-work:turn_1:live-assistant:call_1": true},
	}
	state.applySnapshot(protocol.SessionSnapshot{Messages: []protocol.TranscriptMessage{
		{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Content: []protocol.TranscriptContent{{
			Kind: protocol.TranscriptContentToolCall, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`,
		}}},
		{ID: "result_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{
			Kind: protocol.TranscriptContentText, Text: "contents",
		}}},
	}})
	if state.activitySourceID != "turn-work:turn_1:assistant_1" || !state.activityExpanded[key] {
		t.Fatalf("live Activity reconciliation = source %q expanded %+v", state.activitySourceID, state.activityExpanded)
	}
	if !state.inlineActivityOpen[state.activitySourceID] {
		t.Fatalf("reconciled Activity source is closed: %+v", state.inlineActivityOpen)
	}
}

func TestSnapshotClosesActivityWhenItsSourceDisappears(t *testing.T) {
	t.Parallel()

	state := appState{activitySourceID: "turn-work:turn_1:assistant_1", activitySelected: true}
	state.applySnapshot(protocol.SessionSnapshot{Messages: []protocol.TranscriptMessage{{
		ID: "user_1", TurnID: "turn_1", Role: "user",
		Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "hello"}},
	}}})
	if state.activitySourceID != "" || state.activitySelected {
		t.Fatalf("vanished Activity source remained open: %q selected %v", state.activitySourceID, state.activitySelected)
	}
}

func TestAssistantStartedSurfacesInitialThinkingContent(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantStarted, Thinking: "**Considering options**"},
	})
	if state.turnThinking != "**Considering options**" || len(state.liveMessages) != 1 || state.liveMessages[0].Thinking != "**Considering options**" {
		t.Fatalf("initial thinking state = content %q messages %+v", state.turnThinking, state.liveMessages)
	}
}

func TestAssistantTextDeltaDoesNotMoveTranscriptUntilCompletion(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 2, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantTextDelta, ContentIndex: 0, Delta: "partial response"},
	})
	if state.needsScroll || len(state.liveMessages) != 1 || state.liveMessages[0].Text != "partial response" || !state.liveMessages[0].Pending {
		t.Fatalf("buffered response = messages %+v needsScroll %v", state.liveMessages, state.needsScroll)
	}

	state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 3, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantCompleted,
	}})
	if !state.needsScroll || state.liveMessages[0].Pending {
		t.Fatalf("completed response = messages %+v needsScroll %v", state.liveMessages, state.needsScroll)
	}
}

func TestAssistantCompletionPreservesUnspecifiedAccumulatedChannels(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 2, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventThinkingDelta, ContentIndex: 0, Delta: "reasoning"},
		{Sequence: 3, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantTextDelta, ContentIndex: 1, Delta: "partial response"},
		{Sequence: 4, TurnID: "turn_1", MessageID: "message_1", Kind: protocol.SessionEventAssistantCompleted, Text: "final response"},
	})
	if len(state.liveMessages) != 1 || state.liveMessages[0].Text != "final response" || state.liveMessages[0].Thinking != "reasoning" {
		t.Fatalf("text-only completion = %+v", state.liveMessages)
	}

	state = appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_2", MessageID: "message_2", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 2, TurnID: "turn_2", MessageID: "message_2", Kind: protocol.SessionEventAssistantTextDelta, ContentIndex: 0, Delta: "response"},
		{Sequence: 3, TurnID: "turn_2", MessageID: "message_2", Kind: protocol.SessionEventThinkingDelta, ContentIndex: 1, Delta: "partial reasoning"},
		{Sequence: 4, TurnID: "turn_2", MessageID: "message_2", Kind: protocol.SessionEventAssistantCompleted, Thinking: "final reasoning"},
	})
	if len(state.liveMessages) != 1 || state.liveMessages[0].Text != "response" || state.liveMessages[0].Thinking != "final reasoning" {
		t.Fatalf("thinking-only completion = %+v", state.liveMessages)
	}
}

func TestToolPlanningSurvivesEmptyAssistantCompletion(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, Kind: protocol.SessionEventRunStarted},
		{Sequence: 2, Kind: protocol.SessionEventUserMessage, Text: "read it"},
		{Sequence: 3, MessageID: "message_test", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 4, MessageID: "message_test", Kind: protocol.SessionEventToolPlanned, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
		{Sequence: 5, MessageID: "message_test", Kind: protocol.SessionEventAssistantCompleted},
		{Sequence: 6, Kind: protocol.SessionEventToolStarted, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
	})
	if len(state.liveMessages) != 3 {
		t.Fatalf("live messages = %+v, want user, tool-bearing assistant, and tool result", state.liveMessages)
	}
	tool := liveToolMessage(t, &state, "call_1")
	if tool.Role != "tool" || tool.ToolName != "read" || tool.ToolArguments != `{"path":"README.md"}` || tool.ToolStatus != "Running…" {
		t.Fatalf("tool after assistant completion = %+v", tool)
	}
}

func TestToolResultDeltasAppendAndCompletionReconciles(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, Kind: protocol.SessionEventToolStarted, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
		{Sequence: 2, Kind: protocol.SessionEventToolUpdated, ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "con"}}},
		{Sequence: 3, Kind: protocol.SessionEventToolUpdated, ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "tents"}}},
	})
	if tool := liveToolMessage(t, &state, "call_1"); tool.Text != "contents" || !tool.Pending {
		t.Fatalf("streamed tool = %+v", state.liveMessages)
	}
	state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 4, Kind: protocol.SessionEventToolCompleted,
		ToolCallID: "call_1", ToolName: "read",
		Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "authoritative contents"}},
		Details: json.RawMessage(`{"lines":1}`),
	}})
	tool := liveToolMessage(t, &state, "call_1")
	if tool.Text != "authoritative contents" || tool.Pending || string(tool.ToolDetails) != `{"lines":1}` {
		t.Fatalf("completed tool = %+v", tool)
	}
}

func TestToolResultDeltaPreviewIsCumulativelyBounded(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	chunk := strings.Repeat("x", 40<<10)
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, Kind: protocol.SessionEventToolUpdated, ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: chunk}}},
		{Sequence: 2, Kind: protocol.SessionEventToolUpdated, ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: chunk}}},
	})
	tool := liveToolMessage(t, &state, "call_1")
	if !tool.ToolContentTruncated || len(tool.ToolContent) != 2 || !strings.HasSuffix(tool.Text, "… live output truncated") || len(tool.Text) > maxLiveToolPreviewBytes+64 {
		t.Fatalf("bounded tool preview = content %d text bytes %d truncated %t", len(tool.ToolContent), len(tool.Text), tool.ToolContentTruncated)
	}
}

func TestUnexecutedToolPlanSettlesWhenRunFinishes(t *testing.T) {
	t.Parallel()

	state := appState{liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock)}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, Kind: protocol.SessionEventRunStarted},
		{Sequence: 2, MessageID: "message_test", Kind: protocol.SessionEventAssistantStarted},
		{Sequence: 3, MessageID: "message_test", Kind: protocol.SessionEventToolPlanned, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
		{Sequence: 4, MessageID: "message_test", Kind: protocol.SessionEventAssistantCompleted},
		{Sequence: 5, Kind: protocol.SessionEventRunFinished},
	})
	if tool := liveToolMessage(t, &state, "call_1"); tool.Pending || tool.ToolStatus != "Not run" {
		t.Fatalf("settled tool plan = %+v", state.liveMessages)
	}
}

func TestStoppingTurnActivityTakesPrecedenceUntilRunFinishes(t *testing.T) {
	t.Parallel()

	state := appState{
		turnActivity: "Stopping…", runStopping: true, liveAssistant: -1,
		liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock),
	}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 1, MessageID: "message_test", Kind: protocol.SessionEventAssistantTextDelta, ContentIndex: 0, Delta: "late output"},
	})
	if state.turnActivity != "Stopping…" {
		t.Fatalf("activity after buffered delta = %q, want Stopping…", state.turnActivity)
	}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 2, Kind: protocol.SessionEventRunFinished}})
	if state.turnActivity != "" {
		t.Fatalf("activity after run finish = %q, want blank", state.turnActivity)
	}
}

func TestTerminalRunStatusStopsBeforeTranscriptSettlement(t *testing.T) {
	t.Parallel()

	state := appState{
		runPending: true, terminalRunActive: true, terminalRunID: "run_1",
		liveAssistant: -1, liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock),
	}
	state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 1, RunID: "run_1", Kind: protocol.SessionEventRunFinished,
	}})
	if state.terminalRunActive || !state.runPending {
		t.Fatalf("terminal settlement = active %t admission pending %t", state.terminalRunActive, state.runPending)
	}
	state.applyRunEvents([]protocol.SessionEvent{{
		Sequence: 2, RunID: "run_1", Kind: protocol.SessionEventRunStarted,
	}})
	if state.terminalRunActive {
		t.Fatal("delayed start event reactivated a settled run")
	}

	state.markTerminalRunStarted("run_2")
	state.markTerminalRunSettled("run_1")
	if !state.terminalRunActive || state.terminalRunID != "run_2" {
		t.Fatalf("stale settlement changed active run: active %t id %q", state.terminalRunActive, state.terminalRunID)
	}
}

func TestTerminalSnapshotFailureSettlesLiveTurnForContinuedInput(t *testing.T) {
	t.Parallel()

	state := appState{
		runPending: true, activeRunID: "run_test", liveAssistant: 1,
		liveTools: make(map[string]int), liveContent: make(map[int]liveContentBlock),
		liveMessages: []transcriptMessage{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "partial response", Pending: true},
		},
	}
	state.settleRunWithoutSnapshot(protocol.RunInfo{Status: protocol.RunStatusCompleted}, errors.New("snapshot too large"))
	if state.runPending || state.activeRunID != "" || len(state.liveMessages) != 0 {
		t.Fatalf("settled run state = pending %v id %q live %+v", state.runPending, state.activeRunID, state.liveMessages)
	}
	if len(state.messages) != 2 || state.messages[0].Text != "hello" || state.messages[1].Text != "partial response" || state.messages[1].Pending {
		t.Fatalf("settled transcript = %+v", state.messages)
	}
	if state.recovery != footerHealthy {
		t.Fatalf("settled recovery = %v, want healthy", state.recovery)
	}
}

func TestAuthoritativeSnapshotClearsSyncOnlyWhenTurnSettles(t *testing.T) {
	t.Parallel()
	state := &appState{recovery: footerSyncingFinalTranscript, runPending: true, activeRunID: "run_a"}
	state.applySnapshot(protocol.SessionSnapshot{ActiveRunID: "run_a"})
	if state.recovery != footerSyncingFinalTranscript {
		t.Fatalf("active-run snapshot changed recovery to %v", state.recovery)
	}
	state.applySnapshot(protocol.SessionSnapshot{})
	if state.recovery != footerHealthy || state.runPending {
		t.Fatalf("terminal snapshot left recovery=%v pending=%t", state.recovery, state.runPending)
	}
}

func TestSuccessorRunClearsCompletedTranscriptRecovery(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		nextRun string
		want    footerRecovery
	}{
		{name: "settled", want: footerHealthy},
		{name: "successor", nextRun: "run_b", want: footerHealthy},
		{name: "still same run", nextRun: "run_a", want: footerSyncingFinalTranscript},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &appState{recovery: footerSyncingFinalTranscript}
			state.settleTranscriptRecovery("run_a", test.nextRun)
			if state.recovery != test.want {
				t.Fatalf("recovery = %v, want %v", state.recovery, test.want)
			}
		})
	}
}

func TestTerminalSnapshotFailureClearsRecoveryAndShowsPersistentToast(t *testing.T) {
	application, state, _ := mountRunAbort(t)
	state.recovery = footerSyncingFinalTranscript
	state.finishRunWithoutSnapshot(protocol.RunInfo{RunID: state.activeRunID, Status: protocol.RunStatusCompleted}, errors.New("snapshot too large"))
	application.Pump(120, 36)
	if state.recovery != footerHealthy || state.runPending {
		t.Fatalf("failed refresh left recovery=%v pending=%t", state.recovery, state.runPending)
	}
	toasts := state.toasts.Snapshot()
	if len(toasts) != 1 || toasts[0].Title != "Transcript refresh failed" || toasts[0].Variant != toastError || !toasts[0].Persistent || !strings.Contains(toasts[0].Subtitle, "snapshot too large") {
		t.Fatalf("failure toast = %+v, want persistent snapshot error", toasts)
	}
}

func TestAuthSelectionDismissesToItsOpeningSurface(t *testing.T) {
	t.Parallel()

	if got := authSelectionDismissTarget(false); got != phaseAuthGate {
		t.Fatalf("first-run auth dismissal = %v, want auth gate", got)
	}
	if got := authSelectionDismissTarget(true); got != phaseReady {
		t.Fatalf("palette auth dismissal = %v, want ready conversation", got)
	}
}

func TestSupportedAuthProvidersMatchDroidsProviders(t *testing.T) {
	t.Parallel()

	if len(authProviderOptions) != 5 {
		t.Fatalf("provider option count = %d, want 5", len(authProviderOptions))
	}
	for index, want := range []struct {
		id     string
		method string
	}{
		{id: "openai-codex", method: "ChatGPT plan · device code"},
		{id: "anthropic", method: "API key"},
		{id: "openai", method: "API key"},
		{id: "opencode-go", method: "API key"},
		{id: "anthropic-oauth", method: "Pro or Max plan · browser"},
	} {
		got := authProviderOptions[index]
		if got.ID != want.id || got.Method != want.method || got.DefaultModel == "" {
			t.Fatalf("provider option %d = %#v, want id %q, method %q, and a default model", index, got, want.id, want.method)
		}
	}
	filtered := filteredAuthProviders("anth")
	if len(filtered) != 1 || filtered[0].ID != "anthropic" {
		t.Fatalf("filtered providers = %#v", filtered)
	}
	filtered = filteredAuthProviders("Claude")
	if len(filtered) != 1 || filtered[0].ID != anthropicOAuthOptionID {
		t.Fatalf("Claude filtered providers = %#v", filtered)
	}
}

func TestAPIKeyProviderSelectionSubmitsObscuredCredential(t *testing.T) {
	t.Parallel()

	calls := make(chan apiKeyLoginCall, 1)
	application := uitest.New(app{Options: Options{
		Context: context.Background(), Server: &fakeServer{}, CWD: "/repo",
		APIKeyLogin: fakeAPIKeyLogin{calls: calls},
	}})
	application.Pump(80, 24)
	application.Enter()
	application.Pump(80, 24)
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	application.Pump(80, 24)
	application.Enter()
	application.Pump(80, 24)
	for _, character := range "anthropic-secret" {
		application.Key(string(character))
		application.Pump(80, 24)
	}
	application.Enter()

	select {
	case call := <-calls:
		if call.providerID != auth.AnthropicProviderID || call.apiKey != "anthropic-secret" {
			t.Fatalf("API-key login call = %#v", call)
		}
	case <-time.After(time.Second):
		t.Fatal("API-key login was not submitted")
	}
}

func TestClaudeSubscriptionSelectionAcceptsManualCode(t *testing.T) {
	t.Parallel()

	codes := make(chan string, 1)
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application := uitest.New(app{Options: Options{
		Context: ctx, Server: &fakeServer{}, CWD: "/repo",
		BrowserLogin: fakeBrowserLogin{codes: codes, ready: ready},
	}})
	application.Pump(80, 24)
	application.Enter()
	application.Pump(80, 24)
	for range 4 {
		application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
		application.Pump(80, 24)
	}
	application.Enter()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("Claude login instructions were not delivered")
	}
	application.Pump(80, 24)
	for _, character := range "manual-code" {
		application.Key(string(character))
		application.Pump(80, 24)
	}
	application.Enter()

	select {
	case code := <-codes:
		if code != "manual-code" {
			t.Fatalf("manual code = %q", code)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatal("manual Claude authorization code was not submitted")
	}
}

func TestBashAdmissionCountsAsActiveWork(t *testing.T) {
	t.Parallel()

	state := appState{bashStarting: true}
	if !state.hasActiveWork() {
		t.Fatal("bash admission window was reported idle")
	}
}

func TestListSessionExplorerSessionsUsesGlobalDirectory(t *testing.T) {
	t.Parallel()

	requestedCWD := "not called"
	server := &fakeServer{list: func(cwd string) ([]protocol.SessionInfo, error) {
		requestedCWD = cwd
		return []protocol.SessionInfo{{ID: "session_other", CWD: "/another-repo"}}, nil
	}}
	sessions, err := listSessionExplorerSessions(context.Background(), server)
	if err != nil {
		t.Fatal(err)
	}
	if requestedCWD != "" || len(sessions) != 1 || sessions[0].ID != "session_other" {
		t.Fatalf("global session listing cwd=%q sessions=%+v", requestedCWD, sessions)
	}
}

func TestCreateSessionForSwitchUsesConfiguredDefaultsAndExactBinding(t *testing.T) {
	t.Parallel()

	target := protocol.SessionSnapshot{Session: protocol.SessionInfo{
		ID: "session_new", CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "high",
	}}
	server := &fakeServer{
		createdResult: target.Session,
		attach: func(sessionID string) (sessionclient.Session, error) {
			return fakeSession{id: sessionID, snapshot: target}, nil
		},
	}
	bound, snapshot, location, err := createSessionForSwitch(
		context.Background(), server, protocol.CreateSessionInput{
			CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "high",
		}, func(_ context.Context, cwd string) string { return "resolved:" + cwd },
	)
	if err != nil {
		t.Fatal(err)
	}
	if server.createCalls != 1 || server.created.CWD != "/repo" || server.created.Model != codexDefaultModel || server.created.ThinkingLevel != "high" || server.created.Temporary {
		t.Fatalf("create calls=%d input=%+v", server.createCalls, server.created)
	}
	if bound.ID() != target.Session.ID || snapshot.Session.ID != target.Session.ID || location != "resolved:/repo" {
		t.Fatalf("created attachment bound=%q snapshot=%q location=%q", bound.ID(), snapshot.Session.ID, location)
	}
}

func TestCreateSessionForSwitchFailureDoesNotProduceReplacement(t *testing.T) {
	t.Parallel()

	server := &fakeServer{createErr: errors.New("storage unavailable")}
	bound, snapshot, location, err := createSessionForSwitch(
		context.Background(), server,
		protocol.CreateSessionInput{CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "medium"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "create session: storage unavailable") {
		t.Fatalf("error = %v", err)
	}
	if bound != nil || snapshot.Session.ID != "" || location != "" {
		t.Fatalf("failed replacement bound=%v snapshot=%+v location=%q", bound, snapshot, location)
	}
}

func TestCreateSessionForSwitchPreservesCreatedSessionWhenAttachFails(t *testing.T) {
	t.Parallel()

	server := &fakeServer{
		createdResult: protocol.SessionInfo{ID: "session_new", CWD: "/repo", Model: codexDefaultModel},
		attachErr:     errors.New("temporarily unavailable"),
	}
	bound, _, _, err := createSessionForSwitch(
		context.Background(), server,
		protocol.CreateSessionInput{CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "medium"}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "attach session: temporarily unavailable") || bound != nil || server.createCalls != 1 {
		t.Fatalf("bound=%v creates=%d err=%v", bound, server.createCalls, err)
	}
}

func TestAttachSessionForSwitchUsesExactBindingSnapshotAndLocation(t *testing.T) {
	t.Parallel()

	target := protocol.SessionSnapshot{Session: protocol.SessionInfo{
		ID: "session_target", CWD: "/other/repo", Model: codexDefaultModel,
	}}
	server := &fakeServer{attach: func(sessionID string) (sessionclient.Session, error) {
		return fakeSession{id: sessionID, snapshot: target}, nil
	}}
	resolvedCWD := ""
	bound, snapshot, location, err := attachSessionForSwitch(
		context.Background(), server, target.Session.ID,
		func(_ context.Context, cwd string) string {
			resolvedCWD = cwd
			return "~/other/repo (feature)"
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if bound.ID() != target.Session.ID || snapshot.Session.ID != target.Session.ID || resolvedCWD != target.Session.CWD || location != "~/other/repo (feature)" {
		t.Fatalf("attachment bound=%q snapshot=%q cwd=%q location=%q", bound.ID(), snapshot.Session.ID, resolvedCWD, location)
	}
}

func TestCancelLoginDoesNotInvalidateAttachedSessionOperation(t *testing.T) {
	state := appState{operation: 7}
	state.cancelLogin()
	if state.operation != 7 || state.loginGeneration != 1 {
		t.Fatalf("cancel login operation=%d generation=%d", state.operation, state.loginGeneration)
	}
}

func TestConfigurationResultUpdatesPickerWithoutSnapshot(t *testing.T) {
	state := appState{configurationPicker: configurationPickerController{CurrentModel: "old/model", CurrentThinking: "low"}}
	state.applyConfigurationResult(protocol.SessionInfo{ID: "session_test", Model: "new/model", ThinkingLevel: "high"})
	if state.session.Model != "new/model" || state.configurationPicker.CurrentModel != "new/model" || state.configurationPicker.CurrentThinking != "high" {
		t.Fatalf("configuration result session=%+v picker=%+v", state.session, state.configurationPicker)
	}
}

func TestSessionMetadataSnapshotPreservesActivePresentation(t *testing.T) {
	state := appState{
		session:      protocol.SessionInfo{ID: "session_test", ThinkingLevel: "low"},
		messages:     []transcriptMessage{{ID: "settled", Role: "user", Text: "settled"}},
		liveMessages: []transcriptMessage{{ID: "live", Role: "assistant", Text: "streaming"}},
		runPending:   true,
	}
	state.applySessionMetadataSnapshot(protocol.SessionSnapshot{
		Session: protocol.SessionInfo{ID: "session_test", ThinkingLevel: "high"}, ContextTokens: 12, ContextWindow: 100,
	})
	if state.session.ThinkingLevel != "high" || len(state.messages) != 1 || len(state.liveMessages) != 1 || state.liveMessages[0].ID != "live" {
		t.Fatalf("metadata refresh thinking=%q messages=%d live=%+v", state.session.ThinkingLevel, len(state.messages), state.liveMessages)
	}
}

func TestInstallSessionStartsFreshAgentOnlyWorkspaceAcrossSwitchBack(t *testing.T) {
	t.Parallel()
	const oldID = "session_0123456789abcdef0123456789abcdef"
	const newID = "session_fedcba9876543210fedcba9876543210"
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	oldAttachmentContext, oldAttachmentCancel := context.WithCancel(context.Background())
	watchCanceled := false
	state := &appState{
		ctx: context.Background(), session: protocol.SessionInfo{ID: oldID, CWD: "/old"}, bound: fakeSession{id: oldID},
		attachmentCtx: oldAttachmentContext, attachmentCancel: oldAttachmentCancel,
		subagentWatchCancel: func() { watchCanceled = true },
		workspacePickerOpen: true, workspacePickerQuery: "reviewer", workspacePickerSelection: 1,
		workspacePickerRevealPending: true, workspacePickerRevealOffset: 4,
		activitySelected: true, subagentPaneID: conversationID,
		subagentRequestGeneration: 7, subagentRosterGeneration: 9, subagentRosterLoading: true, subagentRosterRefreshPending: true,
		subagentTranscripts:      map[string]protocol.SubagentTranscript{conversationID: {ConversationID: conversationID}},
		subagentTranscriptErrors: map[string]string{conversationID: "stale"},
		subagentTranscriptLoads:  map[string]uint64{conversationID: 1}, subagentTranscriptLoading: map[string]bool{conversationID: true},
		subagentTranscriptOrder: []string{conversationID},
		subagentScrolls:         map[string]*ui.ScrollController{conversationID: {}}, subagentFocuses: map[string]*ui.FocusNode{conversationID: {}},
		subagentLive:      map[string]protocol.SubagentLiveEventPage{conversationID: {}},
		subagentLiveLoads: map[string]uint64{conversationID: 1}, subagentLiveLoading: map[string]bool{conversationID: true},
		subagentScrollToEndID: conversationID, subagentNeedsScroll: true, subagentPendingLayout: true,
		inlineActivityOpen: map[string]bool{"turn": true}, activityExpanded: make(map[activityToolKey]bool),
	}
	if _, _, err := state.workspace.Open(subagentWorkspacePane(conversationID)); err != nil {
		t.Fatal(err)
	}
	state.workspace.SetFocusOwner(workspaceFocusComposer)

	assertFresh := func(label string, sessionID string) {
		t.Helper()
		if state.session.ID != sessionID || state.workspace.StripVisible() || state.workspace.SelectedIdentity() != workspaceAgentIdentity || state.workspace.FocusOwner() != workspaceFocusContent {
			t.Fatalf("%s workspace = session:%q panes:%v selected:%q focus:%v", label, state.session.ID, state.workspace.Panes(), state.workspace.SelectedIdentity(), state.workspace.FocusOwner())
		}
		if state.workspacePickerOpen || state.workspacePickerQuery != "" || state.workspacePickerSelection != 0 || state.workspacePickerRevealPending || state.workspacePickerRevealOffset != 0 || state.activitySelected || state.subagentPaneID != "" {
			t.Fatalf("%s visible state leaked: picker:%t query:%q selection:%d reveal:%t/%d activity:%t pane:%q", label, state.workspacePickerOpen, state.workspacePickerQuery, state.workspacePickerSelection, state.workspacePickerRevealPending, state.workspacePickerRevealOffset, state.activitySelected, state.subagentPaneID)
		}
		if len(state.subagentTranscriptOrder) != 0 || len(state.subagentTranscripts) != 0 || len(state.subagentScrolls) != 0 || len(state.subagentFocuses) != 0 || len(state.subagentLive) != 0 || len(state.inlineActivityOpen) != 0 || state.subagentRosterLoading || state.subagentRosterRefreshPending || state.subagentScrollToEndID != "" || state.subagentNeedsScroll || state.subagentPendingLayout {
			t.Fatalf("%s retained pane state leaked: order:%v transcripts:%d scrolls:%d focuses:%d live:%d activity:%d roster:%t/%t pending-scroll:%q/%t/%t", label, state.subagentTranscriptOrder, len(state.subagentTranscripts), len(state.subagentScrolls), len(state.subagentFocuses), len(state.subagentLive), len(state.inlineActivityOpen), state.subagentRosterLoading, state.subagentRosterRefreshPending, state.subagentScrollToEndID, state.subagentNeedsScroll, state.subagentPendingLayout)
		}
	}

	state.installSession(fakeSession{id: newID}, protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: newID, CWD: "/new"}}, "/new")
	assertFresh("new session", newID)
	if !watchCanceled {
		t.Fatal("old subagent watcher was not canceled")
	}
	select {
	case <-oldAttachmentContext.Done():
	default:
		t.Fatal("old attachment context remained active")
	}
	if state.subagentTranscriptLoadCurrent(conversationID, 1) || state.subagentLiveLoadCurrent(conversationID, 1) || state.subagentRosterResponseCurrent(7, 9) {
		t.Fatal("old pane or roster load generation remained current after session replacement")
	}

	state.installSession(fakeSession{id: oldID}, protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: oldID, CWD: "/old"}}, "/old")
	assertFresh("switched-back session", oldID)
}

func TestStalePromptAdmissionCannotAttachToSwitchedSession(t *testing.T) {
	state := appState{operation: 2, session: protocol.SessionInfo{ID: "session_target"}}
	if state.acceptPromptAdmission(1, nil) {
		t.Fatal("stale source-session prompt admission was accepted")
	}
	if state.activeRun != nil || state.activeRunID != "" || state.session.ID != "session_target" {
		t.Fatalf("stale prompt admission changed target state: active=%v id=%q session=%q", state.activeRun != nil, state.activeRunID, state.session.ID)
	}
}

func TestInstallSessionReplacesAuthoritativeBindingAndKeepsPerSessionDrafts(t *testing.T) {
	t.Parallel()

	state := appState{
		session:          protocol.SessionInfo{ID: "session_source"},
		composer:         "source draft",
		sessionDrafts:    map[string]string{"session_target": "target draft"},
		operation:        4,
		messages:         []transcriptMessage{{ID: "old", Role: "user", Text: "old"}},
		activitySourceID: "old-activity", activitySelected: true,
		activityExpanded: map[activityToolKey]bool{{TurnID: "old", ToolCallID: "tool"}: true},
		bashCollapsed:    map[string]bool{"old-bash": true},
		cwdPending:       true,
		reloadPending:    true,
	}
	state.resetAttachmentContext()
	pickerContext, pickerCancel := context.WithCancel(state.attachmentCtx)
	state.workspaceFilePickerContext = pickerContext
	state.workspaceFilePickerCancel = pickerCancel
	state.workspaceFilePicker = readyFilePickerController()
	state.workspaceFilePicker.Query = "source query"
	state.indexedFiles = indexedFileSource{SessionID: "session_source", CWD: "/source", Entries: []protocol.FileIndexEntry{{Path: "source.go"}}}
	t.Cleanup(func() { state.attachmentCancel() })
	sourceAttachment := state.attachmentCtx
	target := protocol.SessionSnapshot{
		Session:     protocol.SessionInfo{ID: "session_target", CWD: "/other/repo", Model: codexDefaultModel},
		ActiveRunID: "run_target",
		Messages: []protocol.TranscriptMessage{{
			ID: "target-message", TurnID: "target-turn", Role: "user",
			Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "target history"}},
		}},
	}
	bound := fakeSession{id: target.Session.ID, snapshot: target}
	state.installSession(bound, target, "~/other/repo")
	select {
	case <-sourceAttachment.Done():
	default:
		t.Fatal("source attachment watchers were not canceled")
	}
	select {
	case <-state.attachmentCtx.Done():
		t.Fatal("target attachment context was already canceled")
	default:
	}
	select {
	case <-pickerContext.Done():
	default:
		t.Fatal("source file-picker requests were not canceled")
	}
	if state.workspaceFilePicker.Open || state.workspaceFilePicker.Query != "" || state.workspaceFilePickerContext != nil {
		t.Fatalf("source file-picker invocation leaked into target: %+v", state.workspaceFilePicker)
	}
	if state.indexedFiles.SessionID != "" || state.indexedFiles.Entries != nil {
		t.Fatalf("source indexed files leaked into target: %+v", state.indexedFiles)
	}
	if state.session.ID != target.Session.ID || state.bound.ID() != target.Session.ID || state.operation != 5 {
		t.Fatalf("installed binding session=%q bound=%q operation=%d", state.session.ID, state.bound.ID(), state.operation)
	}
	if state.sessionDrafts["session_source"] != "source draft" || state.composer != "target draft" || state.composerCursorEndGeneration != 1 {
		t.Fatalf("drafts=%+v composer=%q cursorGeneration=%d", state.sessionDrafts, state.composer, state.composerCursorEndGeneration)
	}
	if len(state.messages) != 1 || state.messages[0].ID != "target-message" || state.location != "~/other/repo" {
		t.Fatalf("target presentation messages=%+v location=%q", state.messages, state.location)
	}
	if !state.runPending || state.activeRunID != "run_target" || state.recovery != footerHealthy {
		t.Fatalf("active target run pending=%t id=%q recovery=%v", state.runPending, state.activeRunID, state.recovery)
	}
	if state.cwdPending || state.reloadPending {
		t.Fatalf("source mutation state leaked into target: cwd=%v reload=%v", state.cwdPending, state.reloadPending)
	}
	if state.activitySourceID != "" || state.activitySelected || len(state.activityExpanded) != 0 || len(state.bashCollapsed) != 0 {
		t.Fatalf("source-local presentation leaked activity=%q selected=%t expanded=%+v bash=%+v", state.activitySourceID, state.activitySelected, state.activityExpanded, state.bashCollapsed)
	}
}

func TestPreferredStartupModelPreservesExplicitCLISelectionAfterLogin(t *testing.T) {
	t.Parallel()
	if got := preferredStartupModel("anthropic/custom", "anthropic/default"); got != "anthropic/custom" {
		t.Fatalf("explicit startup model = %q", got)
	}
	if got := preferredStartupModel("", "anthropic/default"); got != "anthropic/default" {
		t.Fatalf("fallback startup model = %q", got)
	}
}

func TestNewSessionInputPreservesActiveModelAndThinking(t *testing.T) {
	t.Parallel()
	input := newSessionInput(protocol.SessionInfo{CWD: "/repo", Model: "test/current", ThinkingLevel: "high"}, "test/default")
	if input.CWD != "/repo" || input.Model != "test/current" || input.ThinkingLevel != "high" {
		t.Fatalf("new session input = %+v", input)
	}
}

func TestResolveSessionLocationUsesAttachedSessionCWD(t *testing.T) {
	t.Parallel()
	if got := resolveSessionLocation(context.Background(), "/session-b", "/invocation-a", nil); got != "/session-b" {
		t.Fatalf("location = %q, want attached session cwd", got)
	}
	got := resolveSessionLocation(context.Background(), "/session-b", "/invocation-a", func(_ context.Context, cwd string) string {
		return "resolved:" + cwd
	})
	if got != "resolved:/session-b" {
		t.Fatalf("resolved location = %q", got)
	}
}

func TestBootstrapSessionResumesNewestUsableSession(t *testing.T) {
	t.Parallel()

	server := &fakeServer{sessions: []protocol.SessionInfo{
		{ID: "anthropic", CWD: "/repo", Model: "anthropic/claude-sonnet-4-6"},
		{ID: "codex", CWD: "/repo", Model: "openai-codex/gpt-5.6-sol"},
	}}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "medium", "", "", "", false, "", "", false, protocol.SessionInfo{},
		func(model string) bool { return modelProvider(model) == "openai-codex" },
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "codex" || bound.ID() != "codex" {
		t.Fatalf("selected session = %q / %q, want codex", info.ID, bound.ID())
	}
	if server.created.Model != "" {
		t.Fatalf("created session = %+v, want none", server.created)
	}
}

func TestBootstrapSessionAppliesExplicitModelAndThinkingFilters(t *testing.T) {
	t.Parallel()

	server := &fakeServer{sessions: []protocol.SessionInfo{
		{ID: "newer", CWD: "/repo", Model: "test/other", ThinkingLevel: "high"},
		{ID: "matching", CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "low"},
	}}
	info, _, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "low", codexDefaultModel, "low", "", false, "", "", false,
		protocol.SessionInfo{}, func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "matching" || server.createCalls != 0 {
		t.Fatalf("filtered session = %+v creates = %d", info, server.createCalls)
	}
}

func TestBootstrapSessionOpensExactShortSessionSelector(t *testing.T) {
	t.Parallel()

	const sessionID = "session_0123456789abcdef0123456789abcdef"
	server := &fakeServer{sessions: []protocol.SessionInfo{{ID: sessionID, CWD: "/other", Model: codexDefaultModel}}}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "medium", "", "", "01234567", false, "", "", false,
		protocol.SessionInfo{}, func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != sessionID || bound.ID() != sessionID || server.createCalls != 0 {
		t.Fatalf("selected session = %q / %q creates = %d", info.ID, bound.ID(), server.createCalls)
	}
}

func TestBootstrapSessionCreatesExplicitNewSessionWithoutListing(t *testing.T) {
	t.Parallel()

	listed := false
	server := &fakeServer{
		list: func(string) ([]protocol.SessionInfo, error) {
			listed = true
			return []protocol.SessionInfo{{ID: "existing", Model: codexDefaultModel}}, nil
		},
		createdResult: protocol.SessionInfo{
			ID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo",
			Model: codexDefaultModel, ThinkingLevel: "medium",
		},
	}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "medium", "", "", "", true,
		"session_0123456789abcdef0123456789abcdef", "", false, protocol.SessionInfo{}, func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if listed {
		t.Fatal("explicit new-session bootstrap listed resumable sessions")
	}
	if info.ID != "session_0123456789abcdef0123456789abcdef" || bound.ID() != info.ID {
		t.Fatalf("selected session = %q / %q, want client-selected id", info.ID, bound.ID())
	}
	if server.created.ID != "session_0123456789abcdef0123456789abcdef" || server.created.CWD != "/repo" || server.created.Model != codexDefaultModel || server.created.ThinkingLevel != "medium" {
		t.Fatalf("create input = %+v", server.created)
	}
}

func TestBootstrapSessionPassesNewSessionMetadataAndTemporaryPolicy(t *testing.T) {
	t.Parallel()

	const sessionID = "session_0123456789abcdef0123456789abcdef"
	server := &fakeServer{createdResult: protocol.SessionInfo{ID: sessionID, CWD: "/repo", Model: codexDefaultModel}}
	_, _, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "high", "", "", "", true, sessionID,
		"Temporary work", true, protocol.SessionInfo{}, func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if server.created.ID != sessionID || server.created.Name != "Temporary work" || !server.created.Temporary || server.created.ThinkingLevel != "high" {
		t.Fatalf("create input = %+v", server.created)
	}
}

func TestBootstrapSessionRetriesExplicitCreatedTargetWithoutDuplicating(t *testing.T) {
	t.Parallel()

	listed := false
	server := &fakeServer{
		list: func(string) ([]protocol.SessionInfo, error) {
			listed = true
			return nil, nil
		},
		createdResult: protocol.SessionInfo{ID: "session_0123456789abcdef0123456789abcdef", CWD: "/repo", Model: codexDefaultModel},
		attachErr:     errors.New("temporarily unavailable"),
	}
	created, _, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "medium", "", "", "", true,
		"session_0123456789abcdef0123456789abcdef", "", false, protocol.SessionInfo{}, func(string) bool { return true },
	)
	if err == nil || created.ID != "session_0123456789abcdef0123456789abcdef" || server.createCalls != 1 {
		t.Fatalf("first bootstrap info=%+v creates=%d err=%v", created, server.createCalls, err)
	}
	server.attachErr = nil
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "medium", "", "", "", false, "", "", false, created,
		func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if listed || server.createCalls != 1 || info.ID != created.ID || bound.ID() != created.ID {
		t.Fatalf("retry listed=%t creates=%d session=%q/%q", listed, server.createCalls, info.ID, bound.ID())
	}
}

func TestBootstrapSessionRejectsCreationForUnavailableProvider(t *testing.T) {
	t.Parallel()
	server := &fakeServer{}
	_, _, _, err := bootstrapSession(
		context.Background(), server, "/repo", "missing/model", "medium", "missing/model", "", "", true,
		"session_0123456789abcdef0123456789abcdef", "", false, protocol.SessionInfo{}, func(string) bool { return false },
	)
	if err == nil || server.createCalls != 0 {
		t.Fatalf("bootstrap error = %v creates = %d", err, server.createCalls)
	}
}

func TestBootstrapSessionSelectsAvailableCatalogModelWithoutDefault(t *testing.T) {
	t.Parallel()
	const selectedModel = "opencode-go/deepseek-v4-flash"
	server := &fakeServer{
		models: func() (protocol.ModelCatalog, error) {
			return protocol.ModelCatalog{Models: []protocol.ModelCapability{
				{ID: "anthropic/unavailable", Provider: "anthropic", Available: false},
				{ID: selectedModel, Provider: "opencode-go", Available: true},
			}}, nil
		},
		createdResult: protocol.SessionInfo{ID: "created", CWD: "/repo", Model: selectedModel, ThinkingLevel: "off"},
	}
	info, _, _, err := bootstrapSession(
		context.Background(), server, "/repo", "", "", "", "", "", false, "", "", false, protocol.SessionInfo{},
		func(model string) bool { return modelProvider(model) == "opencode-go" },
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "created" || server.created.Model != selectedModel || server.created.ThinkingLevel != "" {
		t.Fatalf("catalog-backed create = info:%+v input:%+v", info, server.created)
	}
}

func TestBootstrapSessionReplacesDeprecatedImplicitDefaultWithinProvider(t *testing.T) {
	t.Parallel()
	const selectedModel = "opencode-go/deepseek-v4-flash"
	server := &fakeServer{
		models: func() (protocol.ModelCatalog, error) {
			return protocol.ModelCatalog{Models: []protocol.ModelCapability{{ID: selectedModel, Provider: "opencode-go", Available: true}}}, nil
		},
		createdResult: protocol.SessionInfo{ID: "created", CWD: "/repo", Model: selectedModel, ThinkingLevel: "off"},
	}
	_, _, _, err := bootstrapSession(
		context.Background(), server, "/repo", "opencode-go/kimi-k2.7-code", "", "", "", "", false, "", "", false, protocol.SessionInfo{},
		func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if server.created.Model != selectedModel || server.created.ThinkingLevel != "" {
		t.Fatalf("deprecated implicit default create = %+v", server.created)
	}
}

func TestBootstrapSessionRejectsUnavailableExplicitModel(t *testing.T) {
	t.Parallel()
	server := &fakeServer{models: func() (protocol.ModelCatalog, error) {
		return protocol.ModelCatalog{Models: []protocol.ModelCapability{{ID: "test/current", Provider: "test", Available: true}}}, nil
	}}
	_, _, _, err := bootstrapSession(
		context.Background(), server, "/repo", "test/deprecated", "", "test/deprecated", "", "", true,
		"session_0123456789abcdef0123456789abcdef", "", false, protocol.SessionInfo{}, func(string) bool { return true },
	)
	if err == nil || server.createCalls != 0 {
		t.Fatalf("explicit unavailable model error=%v creates=%d", err, server.createCalls)
	}
}

func TestBootstrapSessionCreatesWhenNoUsableSessionExists(t *testing.T) {
	t.Parallel()

	server := &fakeServer{createdResult: protocol.SessionInfo{
		ID: "created", CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "high",
	}}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "high", "", "", "", false, "", "", false, protocol.SessionInfo{},
		func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "created" || bound.ID() != "created" {
		t.Fatalf("selected session = %q / %q, want created", info.ID, bound.ID())
	}
	if server.created.Model != codexDefaultModel || server.created.ThinkingLevel != "high" {
		t.Fatalf("create input = %+v", server.created)
	}
}

type apiKeyLoginCall struct {
	providerID string
	apiKey     string
}

type fakeAPIKeyLogin struct{ calls chan<- apiKeyLoginCall }

func (f fakeAPIKeyLogin) Login(_ context.Context, providerID, apiKey string) error {
	f.calls <- apiKeyLoginCall{providerID: providerID, apiKey: apiKey}
	return errors.New("test login stopped")
}

type fakeBrowserLogin struct {
	codes chan<- string
	ready chan<- struct{}
}

func (f fakeBrowserLogin) Login(ctx context.Context, manual <-chan string, notify func(auth.AnthropicLoginInstructions) error) error {
	if err := notify(auth.AnthropicLoginInstructions{AuthorizationURL: "https://claude.ai/oauth/authorize", RedirectURI: anthropicOAuthOptionID}); err != nil {
		return err
	}
	close(f.ready)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case code := <-manual:
		f.codes <- code
		<-ctx.Done()
		return ctx.Err()
	}
}

type fakeServer struct {
	sessions      []protocol.SessionInfo
	created       protocol.CreateSessionInput
	createdResult protocol.SessionInfo
	createCalls   int
	createErr     error
	attachErr     error
	attach        func(string) (sessionclient.Session, error)
	rename        func(string, string) (protocol.SessionInfo, error)
	deleteSession func(string) error
	list          func(string) ([]protocol.SessionInfo, error)
	models        func() (protocol.ModelCatalog, error)
}

func (s *fakeServer) CreateSession(_ context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	s.created = input
	s.createCalls++
	return s.createdResult, s.createErr
}

func (s *fakeServer) ForkSession(context.Context, string, protocol.ForkSessionInput) (protocol.SessionInfo, error) {
	panic("unexpected ForkSession")
}

func (s *fakeServer) RenameSession(_ context.Context, sessionID, name string) (protocol.SessionInfo, error) {
	if s.rename == nil {
		panic("unexpected RenameSession")
	}
	return s.rename(sessionID, name)
}

func (s *fakeServer) DeleteSession(_ context.Context, sessionID string) error {
	if s.deleteSession == nil {
		panic("unexpected DeleteSession")
	}
	return s.deleteSession(sessionID)
}

func (s *fakeServer) DisposeTemporarySession(context.Context, string) error {
	panic("unexpected DisposeTemporarySession")
}

func (s *fakeServer) ListSessions(_ context.Context, cwd string) ([]protocol.SessionInfo, error) {
	if s.list != nil {
		return s.list(cwd)
	}
	return append([]protocol.SessionInfo(nil), s.sessions...), nil
}

func (s *fakeServer) Models(context.Context) (protocol.ModelCatalog, error) {
	if s.models != nil {
		return s.models()
	}
	return protocol.ModelCatalog{Models: []protocol.ModelCapability{{
		ID: codexDefaultModel, Provider: "openai-codex", Available: true,
		ThinkingLevels: []protocol.ThinkingLevel{protocol.ThinkingOff, protocol.ThinkingMedium},
		Inputs:         []protocol.ModelInputKind{protocol.ModelInputText},
	}}}, nil
}

func (s *fakeServer) Attach(_ context.Context, sessionID string) (sessionclient.Session, error) {
	if s.attachErr != nil {
		return nil, s.attachErr
	}
	if s.attach != nil {
		return s.attach(sessionID)
	}
	return fakeSession{id: sessionID}, nil
}

type fakeSession struct {
	id         string
	snapshot   protocol.SessionSnapshot
	snapshotFn func() protocol.SessionSnapshot
	vcsStatus  func(context.Context) (protocol.SessionVCSStatus, error)
	watchVCS   func(context.Context, func(protocol.SessionVCSStatus)) error
	reload     func(context.Context) (protocol.ReloadSessionResult, error)
	configure  func(context.Context, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	compact    func(context.Context, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
}

func (s fakeSession) ID() string { return s.id }

func (fakeSession) Subagent(context.Context, protocol.SubagentOperationInput) (protocol.SubagentOperationResult, error) {
	panic("unexpected Subagent")
}

func (fakeSession) SubagentTranscript(context.Context, string) (protocol.SubagentTranscript, error) {
	panic("unexpected SubagentTranscript")
}

func (s fakeSession) Snapshot(context.Context) (protocol.SessionSnapshot, error) {
	if s.snapshotFn != nil {
		return s.snapshotFn(), nil
	}
	return s.snapshot, nil
}

func (s fakeSession) FileIndex(context.Context) (protocol.SessionFileIndex, error) {
	return protocol.SessionFileIndex{SessionID: s.id, CWD: s.snapshot.Session.CWD, Entries: []protocol.FileIndexEntry{}}, nil
}

func (s fakeSession) VCSStatus(ctx context.Context) (protocol.SessionVCSStatus, error) {
	if s.vcsStatus != nil {
		return s.vcsStatus(ctx)
	}
	return protocol.SessionVCSStatus{SessionID: s.id, CWD: s.snapshot.Session.CWD}, nil
}

func (s fakeSession) WatchVCS(ctx context.Context, receive func(protocol.SessionVCSStatus)) error {
	if s.watchVCS != nil {
		return s.watchVCS(ctx, receive)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s fakeSession) ChangeCWD(_ context.Context, target string) (protocol.SessionInfo, error) {
	return protocol.SessionInfo{ID: s.id, CWD: target, Model: "test/echo", CreatedAt: time.Now().Format(time.RFC3339Nano), UpdatedAt: time.Now().Format(time.RFC3339Nano)}, nil
}
func (s fakeSession) Reload(ctx context.Context) (protocol.ReloadSessionResult, error) {
	if s.reload != nil {
		return s.reload(ctx)
	}
	return protocol.ReloadSessionResult{}, nil
}

func (s fakeSession) Configure(ctx context.Context, input protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error) {
	if s.configure == nil {
		panic("unexpected Configure")
	}
	return s.configure(ctx, input)
}

func (s fakeSession) Compact(ctx context.Context, input protocol.CompactSessionInput) (protocol.CompactSessionResult, error) {
	if s.compact == nil {
		panic("unexpected Compact")
	}
	return s.compact(ctx, input)
}

func (fakeSession) Run(context.Context, string) (protocol.RunInfo, error) {
	return protocol.RunInfo{}, nil
}

func (fakeSession) Stream(context.Context, string) (sessionclient.EventStream, error) {
	panic("unexpected Stream")
}

func (fakeSession) Abort(context.Context, string) error { return nil }

func (fakeSession) AbortBash(context.Context, string) error { return nil }

func (fakeSession) Bash(context.Context, string) (sessionclient.BashExecution, error) {
	panic("unexpected Bash")
}

func (fakeSession) StartBash(context.Context, string, string, bool) (sessionclient.BashExecution, error) {
	panic("unexpected StartBash")
}

func (fakeSession) StartPrompt(context.Context, string) (sessionclient.Run, error) {
	panic("unexpected StartPrompt")
}

func (fakeSession) StartPromptCommand(context.Context, string, string) (sessionclient.Run, error) {
	panic("unexpected StartPromptCommand")
}
