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
	"go.rockorager.dev/vaxis/ui/uitest"
)

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

func TestAutomaticCompactionEventsShowPendingAndOutcomeFeedback(t *testing.T) {
	var toasts []toastInput
	state := &appState{showToastOverride: func(toast toastInput) { toasts = append(toasts, toast) }}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 1, Kind: protocol.SessionEventCompactionStarted}})
	if state.turnActivity != "Compacting session…" {
		t.Fatalf("started compaction activity = %q", state.turnActivity)
	}
	state.applyRunEvents([]protocol.SessionEvent{
		{Sequence: 2, Kind: protocol.SessionEventCompactionCompleted},
		{Sequence: 3, Kind: protocol.SessionEventContextUpdated, ContextTokens: 20, ContextWindow: 200},
	})
	if state.turnActivity != "Working…" || state.contextTokens != 20 || state.contextWindow != 200 || len(toasts) != 1 || toasts[0].Title != "Session compacted" ||
		toasts[0].Subtitle != "Session context was compacted." || toasts[0].Variant != toastInfo {
		t.Fatalf("completed compaction activity=%q context=%d/%d toasts=%+v", state.turnActivity, state.contextTokens, state.contextWindow, toasts)
	}
	state.applyRunEvents([]protocol.SessionEvent{{Sequence: 4, Kind: protocol.SessionEventCompactionFailed, ErrorMessage: "Context compaction failed"}})
	if len(toasts) != 2 || toasts[1].Title != "Auto-compaction failed" || toasts[1].Subtitle != "Context compaction failed" || toasts[1].Variant != toastError {
		t.Fatalf("failed compaction toasts = %+v", toasts)
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

func TestActivityRevealWaitsForExpandedLayout(t *testing.T) {
	t.Parallel()

	state := appState{
		activityReveal:        activityToolKey{TurnID: "turn_1", ToolCallID: "call_1"},
		activityRevealPending: true, activityRevealPendingLayout: true,
	}
	if !state.TickFrame(time.Now()) || !state.activityRevealPending || state.activityRevealPendingLayout {
		t.Fatalf("first reveal frame = pending:%t layout:%t", state.activityRevealPending, state.activityRevealPendingLayout)
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

func TestPendingActivityFollowUsesLatestCompletedLayout(t *testing.T) {
	t.Parallel()

	calls := make([]transcriptToolCall, 24)
	for index := range calls {
		calls[index] = transcriptToolCall{
			ID: "call_" + strconv.Itoa(index), Name: "read",
			Arguments: json.RawMessage(`{"path":"README.md"}`),
		}
	}
	messages := []transcriptMessage{{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: calls}}
	state := appState{}
	layout := workspaceLayoutState{Wide: true}
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: messages, Scroll: &state.scroll,
		ActivitySourceID: "turn-work:turn_1:assistant_1", ActivityScroll: &state.activityScroll,
		ActivityList: &state.activityList, WorkspaceLayout: &layout,
	}})
	app.Pump(140, 16)
	state.activityScroll.ScrollToStart()
	state.requestActivityScroll(true)
	state.TickFrame(time.Now())
	metrics := state.activityScroll.Metrics()
	if metrics.MaxScrollOffset == 0 || metrics.ScrollOffset != metrics.MaxScrollOffset {
		t.Fatalf("pending Activity follow = %+v, want latest completed layout at end", metrics)
	}

	state.activityScroll.ScrollToStart()
	state.requestActivityScroll(true)
	state.TickFrame(time.Now())
	metrics = state.activityScroll.Metrics()
	if metrics.ScrollOffset != metrics.MaxScrollOffset {
		t.Fatalf("repeated live update starved Activity follow: %+v", metrics)
	}
}

func TestTranscriptAndActivityFollowPreserveUserScroll(t *testing.T) {
	t.Parallel()

	calls := make([]transcriptToolCall, 24)
	for index := range calls {
		calls[index] = transcriptToolCall{
			ID: "call_" + strconv.Itoa(index), Name: "read",
			Arguments: json.RawMessage(`{"path":"README.md"}`),
		}
	}
	messages := make([]transcriptMessage, 24)
	for index := range messages {
		messages[index] = transcriptMessage{
			ID: "prose_" + strconv.Itoa(index), TurnID: "turn_0", Role: "assistant", Text: "transcript row",
		}
	}
	messages = append(messages, transcriptMessage{
		ID: "assistant_1", TurnID: "turn_1", Role: "assistant", ToolCalls: calls,
	})
	state := appState{}
	layout := workspaceLayoutState{Wide: true}
	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Messages: messages, Scroll: &state.scroll,
		ActivitySourceID: "turn-work:turn_1:assistant_1", ActivityScroll: &state.activityScroll,
		ActivityList: &state.activityList, WorkspaceLayout: &layout,
	}})
	app.Pump(140, 16)
	state.scroll.ScrollToStart()
	state.activityScroll.ScrollToStart()
	if state.scroll.Metrics().MaxScrollOffset == 0 || state.activityScroll.Metrics().MaxScrollOffset == 0 {
		t.Fatal("test content did not overflow both transcript surfaces")
	}

	state.followTranscriptIfPinned()
	state.followActivityIfPinned()
	if state.needsScroll || state.activityNeedsScroll {
		t.Fatalf("follow requested while user was away from end: transcript=%t activity=%t", state.needsScroll, state.activityNeedsScroll)
	}
	if state.scroll.Metrics().ScrollOffset != 0 || state.activityScroll.Metrics().ScrollOffset != 0 {
		t.Fatalf("user scroll changed: transcript=%+v activity=%+v", state.scroll.Metrics(), state.activityScroll.Metrics())
	}

	state.scroll.ScrollToEnd()
	state.activityScroll.ScrollToEnd()
	state.followTranscriptIfPinned()
	state.followActivityIfPinned()
	if !state.needsScroll || !state.activityNeedsScroll || !state.activityScrollToEnd {
		t.Fatalf("pinned surfaces did not keep following: transcript=%t activity=%t toEnd=%t", state.needsScroll, state.activityNeedsScroll, state.activityScrollToEnd)
	}
}

func TestActivityScrollWaitsForUpdatedLayout(t *testing.T) {
	t.Parallel()

	state := appState{}
	state.requestActivityScroll(true)
	if !state.TickFrame(time.Now()) || !state.activityNeedsScroll || state.activityPendingLayout {
		t.Fatalf("first activity frame = needs:%t pending:%t", state.activityNeedsScroll, state.activityPendingLayout)
	}
	if state.TickFrame(time.Now()) || state.activityNeedsScroll {
		t.Fatalf("second activity frame = needs:%t, want settled", state.activityNeedsScroll)
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
		activitySourceID: "turn-work:turn_1:live-assistant:call_1",
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
		t.Fatalf("live Activity reconciliation = source %q expanded %+v", state.activitySourceID, state.activityExpanded)
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
	if !strings.Contains(state.status, "next turn will retry") {
		t.Fatalf("settled status = %q", state.status)
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
	if state.session.ID != target.Session.ID || state.bound.ID() != target.Session.ID || state.operation != 5 {
		t.Fatalf("installed binding session=%q bound=%q operation=%d", state.session.ID, state.bound.ID(), state.operation)
	}
	if state.sessionDrafts["session_source"] != "source draft" || state.composer != "target draft" || state.composerCursorEndGeneration != 1 {
		t.Fatalf("drafts=%+v composer=%q cursorGeneration=%d", state.sessionDrafts, state.composer, state.composerCursorEndGeneration)
	}
	if len(state.messages) != 1 || state.messages[0].ID != "target-message" || state.location != "~/other/repo" {
		t.Fatalf("target presentation messages=%+v location=%q", state.messages, state.location)
	}
	if !state.runPending || state.activeRunID != "run_target" || state.status != "esc abort · ctrl+c detach" {
		t.Fatalf("active target run pending=%t id=%q status=%q", state.runPending, state.activeRunID, state.status)
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
	return protocol.ModelCatalog{}, nil
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
	reload     func(context.Context) (protocol.ReloadSessionResult, error)
	configure  func(context.Context, protocol.ConfigureSessionInput) (protocol.ConfigureSessionResult, error)
	compact    func(context.Context, protocol.CompactSessionInput) (protocol.CompactSessionResult, error)
}

func (s fakeSession) ID() string { return s.id }

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
