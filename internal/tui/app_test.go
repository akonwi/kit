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

	if len(authProviderOptions) != 3 {
		t.Fatalf("provider option count = %d, want 3", len(authProviderOptions))
	}
	for index, want := range []struct {
		id     string
		method string
	}{
		{id: "openai-codex", method: "ChatGPT plan · device code"},
		{id: "anthropic", method: "API key"},
		{id: "openai", method: "API key"},
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

func TestBootstrapSessionResumesNewestUsableSession(t *testing.T) {
	t.Parallel()

	server := &fakeServer{sessions: []protocol.SessionInfo{
		{ID: "anthropic", CWD: "/repo", Model: "anthropic/claude-sonnet-4-6"},
		{ID: "codex", CWD: "/repo", Model: "openai-codex/gpt-5.6-sol"},
	}}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "medium",
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

func TestBootstrapSessionCreatesWhenNoUsableSessionExists(t *testing.T) {
	t.Parallel()

	server := &fakeServer{createdResult: protocol.SessionInfo{
		ID: "created", CWD: "/repo", Model: codexDefaultModel, ThinkingLevel: "high",
	}}
	info, bound, _, err := bootstrapSession(
		context.Background(), server, "/repo", codexDefaultModel, "high",
		func(string) bool { return false },
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

type fakeServer struct {
	sessions      []protocol.SessionInfo
	created       protocol.CreateSessionInput
	createdResult protocol.SessionInfo
}

func (s *fakeServer) CreateSession(_ context.Context, input protocol.CreateSessionInput) (protocol.SessionInfo, error) {
	s.created = input
	return s.createdResult, nil
}

func (s *fakeServer) ListSessions(context.Context, string) ([]protocol.SessionInfo, error) {
	return append([]protocol.SessionInfo(nil), s.sessions...), nil
}

func (s *fakeServer) Attach(_ context.Context, sessionID string) (sessionclient.Session, error) {
	return fakeSession{id: sessionID}, nil
}

type fakeSession struct{ id string }

func (s fakeSession) ID() string { return s.id }

func (fakeSession) Snapshot(context.Context) (protocol.SessionSnapshot, error) {
	return protocol.SessionSnapshot{}, nil
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
