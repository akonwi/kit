package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestSubagentsWorkspaceRendersMergedRosterWithoutCountHeader(t *testing.T) {
	t.Parallel()
	layout := &workspaceLayoutState{}
	focus := &ui.FocusNode{}
	selected := ""
	opened := ""
	canceled := ""
	dismissed := ""
	moved := 0
	closed := false
	parentAborted := false
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	view := shellView{
		Snapshot: shellSnapshot{
			Phase:         phaseReady,
			Running:       true,
			Session:       protocol.SessionInfo{Name: "Parent", Model: "test/echo", ThinkingLevel: "medium"},
			SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: layout, ActivityScroll: &ui.ScrollController{}, ActivityFocus: focus,
			SubagentDefinitions: []protocol.SubagentDefinition{{
				Name: "scout", Description: "inspects repositories", Source: protocol.SubagentSource{Kind: "user", Path: "/tmp/scout.md"},
			}},
			SubagentConversations: []protocol.SubagentConversation{{
				ID: conversationID, AgentName: "scout", Model: "test/echo", State: "running", Generation: 2,
				ActiveTaskID: "task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UpdatedAt: time.Now().Format(time.RFC3339Nano),
				Tasks: []protocol.SubagentTask{{
					ID: "task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: "running", CancellationGeneration: 3,
				}},
			}},
		},
		Callbacks: shellCallbacks{
			SelectSubagent:           func(_ ui.EventContext, name string) { selected = name },
			MoveSubagentSelection:    func(_ ui.EventContext, delta int) { moved = delta },
			OpenSubagentConversation: func(_ ui.EventContext, id string) { opened = id },
			CancelSubagentTask:       func(_ ui.EventContext, id string, _ uint64) { canceled = id },
			DismissSubagent:          func(_ ui.EventContext, id string, _ uint64) { dismissed = id },
			CloseActivity:            func(ui.EventContext) { closed = true },
			Dismiss:                  func(ui.EventContext) { parentAborted = true },
		},
	}
	application := uitest.New(view)
	application.Pump(140, 24)
	rows := paintedRows(application, 140, 24)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		glyphCircleFilled + " scout", "running " + glyphChevronRight, "inspects repositories",
		"test/echo · user · just now",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("subagent workspace missing %q:\n%s", expected, text)
		}
	}
	for _, omitted := range []string{"1 agent", "1 conversation", "Available agents", "Delegated work", "#1 running"} {
		if strings.Contains(text, omitted) {
			t.Fatalf("subagent workspace unexpectedly contains %q:\n%s", omitted, text)
		}
	}
	column, row := findTextCell(t, rows, "scout")
	application.Click(column, row)
	if selected != "scout" || opened != conversationID {
		t.Fatalf("row action = select:%q open:%q", selected, opened)
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	application.Key("c")
	application.Send(vaxis.Key{Keycode: 'd', Modifiers: vaxis.ModCtrl})
	application.Enter()
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if moved != 1 || canceled != "task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || dismissed != conversationID || opened != conversationID {
		t.Fatalf("keyboard actions = move:%d cancel:%q dismiss:%q open:%q", moved, canceled, dismissed, opened)
	}
	if !closed || parentAborted {
		t.Fatalf("escape action = closed:%t parent aborted:%t", closed, parentAborted)
	}
	closed = false
	canceled = ""
	application.Tab()
	application.Tab()
	application.Key("c")
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if canceled != "" || closed || !parentAborted {
		t.Fatalf("composer-focused actions = cancel:%q closed:%t parent aborted:%t", canceled, closed, parentAborted)
	}
	parentAborted = false
	application.Click(column, row)
	application.Key("c")
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if canceled != "task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || !closed || parentAborted {
		t.Fatalf("refocused roster actions = cancel:%q closed:%t parent aborted:%t", canceled, closed, parentAborted)
	}
}

func TestSubagentsWorkspaceGroupsAvailableDefinitionsWithoutRepeatedStatus(t *testing.T) {
	t.Parallel()
	layout := &workspaceLayoutState{}
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: layout, ActivityScroll: &ui.ScrollController{},
		SubagentDefinitions: []protocol.SubagentDefinition{
			{Name: "reviewer", Description: "Reviews code changes", Model: "test/reviewer", Source: protocol.SubagentSource{Kind: "project", Path: "/repo/.kit/agents/reviewer.md"}},
			{Name: "scout", Description: "Finds repository evidence", Source: protocol.SubagentSource{Kind: "user", Path: "/home/user/.kit/agents/scout.md"}},
		},
	}})
	application.Pump(100, 20)
	text := strings.Join(paintedRows(application, 100, 20), "\n")
	for _, expected := range []string{
		"Available", glyphCircleEmpty + " reviewer", "Reviews code changes",
		"test/reviewer · project", glyphCircleEmpty + " scout", "Finds repository evidence", "user",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("available subagent list missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "available") {
		t.Fatalf("available group repeated status on each row:\n%s", text)
	}
}

func TestSubagentsWorkspaceRendersConfiguredEmptyState(t *testing.T) {
	t.Parallel()
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: &workspaceLayoutState{}, ActivityScroll: &ui.ScrollController{},
	}})
	application.Pump(80, 18)
	text := strings.Join(paintedRows(application, 80, 18), "\n")
	for _, expected := range []string{"k i t", strings.Repeat(glyphHeavyLine, 11), "No subagents available", "Add .md files to $KIT_HOME/agents/"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("subagent empty state missing %q:\n%s", expected, text)
		}
	}
}

func TestSubagentRosterItemsSortConversationsBeforeAvailableAgents(t *testing.T) {
	t.Parallel()
	now := time.Now().Format(time.RFC3339Nano)
	items := subagentRosterItems(
		[]protocol.SubagentDefinition{
			{Name: "beta", Description: "available beta"},
			{Name: "alpha", Description: "completed alpha"},
			{Name: "gamma", Description: "running gamma"},
		},
		[]protocol.SubagentConversation{
			{ID: "subagent_1", AgentName: "alpha", State: "idle", UpdatedAt: now},
			{ID: "subagent_2", AgentName: "gamma", State: "running", UpdatedAt: now},
			{ID: "subagent_3", AgentName: "removed", State: "failed", UpdatedAt: now},
		},
	)
	got := make([]string, len(items))
	for index, item := range items {
		got[index] = item.Name + ":" + item.Status
	}
	want := []string{"gamma:running", "removed:failed", "alpha:idle", "beta:inactive"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roster order = %v, want %v", got, want)
	}
	if items[1].Description != "Previously active subagent conversation" || !items[1].HasConversation {
		t.Fatalf("removed definition conversation = %+v", items[1])
	}
}

func TestApplySubagentDiagnosticsShowsPersistentToastOncePerSessionWarning(t *testing.T) {
	t.Parallel()
	diagnostic := protocol.SubagentDiagnostic{
		Severity: "warning", Code: "subagents.invalid_frontmatter", Message: "Could not parse agent frontmatter",
		Source: protocol.SubagentSource{Kind: "project", Path: "/repo/.kit/agents/broken.md"},
	}
	state := appState{toastCancels: make(map[uint64]context.CancelFunc)}
	state.applySubagentDiagnostics("session_one", []protocol.SubagentDiagnostic{diagnostic})
	state.applySubagentDiagnostics("session_one", []protocol.SubagentDiagnostic{diagnostic})
	toasts := state.toasts.Snapshot()
	if len(toasts) != 1 {
		t.Fatalf("diagnostic toasts = %d, want 1", len(toasts))
	}
	toast := toasts[0]
	if toast.Title != "Subagent definition warning" || toast.Variant != toastWarning || !toast.Persistent {
		t.Fatalf("diagnostic toast = %+v", toast)
	}
	wantSubtitle := "Could not parse agent frontmatter " + glyphMiddleDot + " /repo/.kit/agents/broken.md"
	if toast.Subtitle != wantSubtitle {
		t.Fatalf("diagnostic toast subtitle = %q, want %q", toast.Subtitle, wantSubtitle)
	}
	state.applySubagentDiagnostics("session_two", []protocol.SubagentDiagnostic{diagnostic})
	if got := len(state.toasts.Snapshot()); got != 2 {
		t.Fatalf("cross-session diagnostic toasts = %d, want 2", got)
	}
}

func TestSubagentAsyncResponseGenerationsRejectStaleResults(t *testing.T) {
	t.Parallel()
	state := appState{
		subagentRequestGeneration: 4, subagentRosterGeneration: 8,
		subagentTranscriptLoads: make(map[string]uint64), subagentLiveLoads: make(map[string]uint64),
	}
	if !state.subagentRosterResponseCurrent(4, 8) || state.subagentRosterResponseCurrent(4, 7) || state.subagentRosterResponseCurrent(3, 8) {
		t.Fatal("roster response generation guard accepted stale state")
	}
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	first := state.advanceSubagentTranscriptLoad(conversationID)
	state.advanceSubagentTranscriptLoad(conversationID)          // close
	third := state.advanceSubagentTranscriptLoad(conversationID) // reopen
	if state.subagentTranscriptLoadCurrent(conversationID, first) || !state.subagentTranscriptLoadCurrent(conversationID, third) {
		t.Fatal("transcript load generation guard accepted a prior tab lifecycle")
	}
	firstLive := state.advanceSubagentLiveLoad(conversationID)
	state.advanceSubagentLiveLoad(conversationID)              // close
	thirdLive := state.advanceSubagentLiveLoad(conversationID) // reopen
	if state.subagentLiveLoadCurrent(conversationID, firstLive) || !state.subagentLiveLoadCurrent(conversationID, thirdLive) {
		t.Fatal("live load generation guard accepted a prior tab lifecycle")
	}
}

func TestApplySubagentResultDoesNotConsumePendingToolLink(t *testing.T) {
	t.Parallel()
	state := appState{subagentPendingAgent: "reviewer"}
	state.applySubagentResult(protocol.SubagentOperationResult{
		Conversations: []protocol.SubagentConversation{{ID: "subagent_stale", AgentName: "reviewer"}},
	})
	if state.subagentPendingAgent != "reviewer" {
		t.Fatalf("generic roster refresh consumed pending tool link: %q", state.subagentPendingAgent)
	}
}

func TestApplySubagentResultRevealsOnlyWhenRosterPositionChanges(t *testing.T) {
	t.Parallel()
	result := protocol.SubagentOperationResult{Definitions: []protocol.SubagentDefinition{{Name: "scout", Description: "Finds evidence"}}}
	state := appState{subagentsOpen: true}
	state.applySubagentResult(result)
	if !state.subagentRevealPending {
		t.Fatal("initial roster selection did not request reveal")
	}
	state.subagentRevealPending = false
	state.applySubagentResult(result)
	if state.subagentRevealPending {
		t.Fatal("unchanged roster refresh requested another reveal")
	}
	state.subagentPaneID = "subagent_open"
	result.Conversations = []protocol.SubagentConversation{{ID: "subagent_open", AgentName: "scout", State: "running"}}
	state.applySubagentResult(result)
	if state.subagentRevealPending {
		t.Fatal("hidden roster requested reveal while transcript was open")
	}
}

func TestEnsureSubagentTabFocusesExistingTab(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tabs := ensureSubagentTab(nil, conversationID)
	tabs = ensureSubagentTab(tabs, conversationID)
	if len(tabs) != 1 || tabs[0] != conversationID {
		t.Fatalf("retained tabs = %v", tabs)
	}
}

func TestSubagentTranscriptShowsLoadFailure(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	view := shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: &workspaceLayoutState{}, ActivityScroll: &ui.ScrollController{},
		SubagentPaneID: conversationID, SubagentTranscriptOrder: []string{conversationID},
		SubagentTranscriptErrors: map[string]string{conversationID: "transcript unavailable"},
	}}
	application := uitest.New(view)
	application.Pump(100, 20)
	text := strings.Join(paintedRows(application, 100, 20), "\n")
	for _, expected := range []string{"Could not load transcript", "transcript unavailable"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("transcript failure missing %q:\n%s", expected, text)
		}
	}
}

func TestSubagentFinalResponseCheckUsesLatestTaskSegment(t *testing.T) {
	t.Parallel()
	transcript := protocol.SubagentTranscript{Messages: []protocol.TranscriptMessage{
		{Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "first"}}},
		{Role: "assistant", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "first response"}}},
		{Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "follow-up"}}},
	}}
	if subagentTranscriptHasFinalResponse(transcript) {
		t.Fatal("earlier assistant response hid the latest task summary")
	}
	transcript.Messages = append(transcript.Messages, protocol.TranscriptMessage{
		Role: "assistant", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "final response"}},
	})
	if !subagentTranscriptHasFinalResponse(transcript) {
		t.Fatal("latest task final response was not detected")
	}
}

func TestReconcileSubagentTabsClosesRemotelyDismissedConversation(t *testing.T) {
	t.Parallel()
	removed := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	retained := "subagent_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	state := appState{
		subagentConversations: []protocol.SubagentConversation{{ID: retained}},
		subagentPaneID:        removed, subagentTranscriptOrder: []string{removed, retained},
		subagentTranscripts:      map[string]protocol.SubagentTranscript{removed: {ConversationID: removed}},
		subagentTranscriptErrors: map[string]string{removed: "stale"},
		subagentTranscriptLoads:  make(map[string]uint64), subagentTranscriptLoading: make(map[string]bool),
		subagentScrolls:   map[string]*ui.ScrollController{removed: {}},
		subagentLive:      map[string]protocol.SubagentLiveEventPage{removed: {}},
		subagentLiveLoads: make(map[string]uint64), subagentLiveLoading: make(map[string]bool),
		subagentScrollToEndID: removed, subagentNeedsScroll: true, subagentPendingLayout: true,
	}
	state.reconcileSubagentTabs()
	if state.subagentPaneID != "" || len(state.subagentTranscriptOrder) != 1 || state.subagentTranscriptOrder[0] != retained || state.subagentScrollToEndID != "" {
		t.Fatalf("reconciled tabs = pane:%q order:%v scroll:%q", state.subagentPaneID, state.subagentTranscriptOrder, state.subagentScrollToEndID)
	}
}

func TestSubagentTranscriptShowsCompletionSummaryWhileHistoryLoads(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: &workspaceLayoutState{}, ActivityScroll: &ui.ScrollController{},
		SubagentPaneID: conversationID, SubagentTranscriptOrder: []string{conversationID},
		SubagentConversations: []protocol.SubagentConversation{{
			ID: conversationID, AgentName: "reviewer", State: "idle", LastResultSummary: "The final review found no soundness issues.",
		}},
	}})
	application.Pump(100, 18)
	text := strings.Join(paintedRows(application, 100, 18), "\n")
	for _, expected := range []string{"The final review found no soundness issues."} {
		if !strings.Contains(text, expected) {
			t.Fatalf("completion summary missing %q:\n%s", expected, text)
		}
	}
}

func TestSubagentLiveEventsUseTranscriptWorkModel(t *testing.T) {
	t.Parallel()
	messages := subagentLiveTranscript(nil, []protocol.SubagentLiveEvent{
		{Sequence: 1, Kind: "message.thinking.delta", TurnID: "turn_1", MessageID: "assistant_1", Delta: "Inspecting carefully"},
		{Sequence: 2, Kind: "tool.planned", TurnID: "turn_1", MessageID: "assistant_1", ToolCallID: "call_1", ToolName: "read"},
		{Sequence: 3, Kind: "message.completed", TurnID: "turn_1", MessageID: "assistant_1"},
		{Sequence: 4, Kind: "tool.started", TurnID: "turn_1", ToolCallID: "call_1", ToolName: "read"},
		{Sequence: 5, Kind: "tool.completed", TurnID: "turn_1", ToolCallID: "call_1", ToolName: "read", Text: "file contents"},
	})
	if len(messages) != 2 || messages[0].Role != "assistant" || messages[0].Pending || messages[0].Thinking != "Inspecting carefully" || len(messages[0].ToolCalls) != 1 || messages[1].Role != "tool" || messages[1].ToolStatus != "Completed" {
		t.Fatalf("projected live transcript = %+v", messages)
	}
	presentation := presentTranscript(messages)
	if len(presentation.Items) != 1 || presentation.Items[0].Kind != transcriptDisplayTurnWork {
		t.Fatalf("live transcript presentation = %+v", presentation.Items)
	}
}

func TestSubagentLiveToolStateSurvivesDurableDeclarationAndScopesByTurn(t *testing.T) {
	t.Parallel()
	durable := []protocol.TranscriptMessage{{
		ID: "assistant_1", TurnID: "turn_1", Role: "assistant",
		Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentToolCall, ToolCallID: "call_1", ToolName: "read"}},
	}}
	messages := subagentLiveTranscript(durable, []protocol.SubagentLiveEvent{
		{Sequence: 1, Kind: "tool.started", TurnID: "turn_1", ToolCallID: "call_1", ToolName: "read"},
		{Sequence: 2, Kind: "tool.started", TurnID: "turn_2", ToolCallID: "call_1", ToolName: "read"},
	})
	if len(messages) != 2 || messages[0].TurnID != "turn_1" || messages[1].TurnID != "turn_2" {
		t.Fatalf("turn-scoped live tools = %+v", messages)
	}
}

func TestActivityPresentationUsesOwningSubagentTranscript(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state := appState{
		subagentConversations: []protocol.SubagentConversation{{ID: conversationID, State: "idle"}},
		subagentTranscripts: map[string]protocol.SubagentTranscript{conversationID: {
			ConversationID: conversationID,
			Messages: []protocol.TranscriptMessage{{
				ID: "child_assistant", TurnID: "child_turn", Role: "assistant",
				Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentToolCall, ToolCallID: "child_call", ToolName: "read"}},
			}},
		}},
		subagentLive: make(map[string]protocol.SubagentLiveEventPage),
	}
	main := []transcriptMessage{{ID: "main_assistant", TurnID: "main_turn", Role: "assistant", ToolCalls: []transcriptToolCall{{ID: "main_call", Name: "bash"}}}}
	child := state.activityPresentation(main, conversationID)
	if len(child.Items) != 1 || child.Items[0].TurnID != "child_turn" {
		t.Fatalf("child activity presentation = %+v", child.Items)
	}
	parent := state.activityPresentation(main, "")
	if len(parent.Items) != 1 || parent.Items[0].TurnID != "main_turn" {
		t.Fatalf("parent activity presentation = %+v", parent.Items)
	}
}

func TestSubagentTranscriptUsesSharedToolWorkPresentation(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: &workspaceLayoutState{}, ActivityScroll: &ui.ScrollController{},
		SubagentPaneID: conversationID, SubagentTranscriptOrder: []string{conversationID},
		SubagentConversations: []protocol.SubagentConversation{{ID: conversationID, AgentName: "reviewer", State: "idle"}},
		SubagentTranscripts: map[string]protocol.SubagentTranscript{conversationID: {
			ConversationID: conversationID,
			Messages: []protocol.TranscriptMessage{
				{ID: "user_1", TurnID: "turn_1", Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "Inspect README"}}},
				{ID: "assistant_1", TurnID: "turn_1", Role: "assistant", Content: []protocol.TranscriptContent{
					{Kind: protocol.TranscriptContentText, Text: "I'll inspect it."},
					{Kind: protocol.TranscriptContentToolCall, ToolCallID: "call_1", ToolName: "read", Arguments: `{"path":"README.md"}`},
				}},
				{ID: "tool_1", TurnID: "turn_1", Role: "tool", ToolCallID: "call_1", ToolName: "read", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "README contents"}}},
				{ID: "assistant_2", TurnID: "turn_1", Role: "assistant", StopReason: "stop", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "Inspection complete."}}},
			},
		}},
	}})
	application.Pump(100, 22)
	text := strings.Join(paintedRows(application, 100, 22), "\n")
	for _, expected := range []string{"Inspect README", "I'll inspect it.", "1 tool call", "Inspection complete."} {
		if !strings.Contains(text, expected) {
			t.Fatalf("shared transcript presentation missing %q:\n%s", expected, text)
		}
	}
}

func TestChildActivityPageScrollStaysWithSubagentTranscript(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	childPages, parentPages := 0, 0
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		SubagentsOpen: true, ActivitySelected: true, ActivitySourceID: "turn-work:child", ActivityConversationID: conversationID,
		WorkspaceLayout: &workspaceLayoutState{}, ActivityScroll: &ui.ScrollController{}, ActivityFocus: &ui.FocusNode{},
		SubagentPaneID: conversationID, SubagentTranscriptOrder: []string{conversationID},
		SubagentConversations: []protocol.SubagentConversation{{ID: conversationID, AgentName: "reviewer", State: "running"}},
	}, Callbacks: shellCallbacks{
		ScrollSubagentTranscript: func(_ ui.EventContext, id string, pages int) {
			if id == conversationID {
				childPages += pages
			}
		},
		ScrollActivity: func(_ ui.EventContext, pages int) { parentPages += pages },
	}})
	application.Pump(100, 20)
	application.Send(vaxis.Key{Keycode: vaxis.KeyPgDown})
	if childPages != 1 || parentPages != 0 {
		t.Fatalf("page scroll = child:%d parent:%d", childPages, parentPages)
	}
}

func TestSubagentTranscriptRequestsInitialScrollToFinalResponse(t *testing.T) {
	t.Parallel()
	state := appState{}
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state.requestSubagentScrollToEnd(conversationID)
	if state.subagentScrollToEndID != conversationID || !state.subagentNeedsScroll || !state.subagentPendingLayout {
		t.Fatalf("subagent scroll request = id:%q needs:%t pending:%t", state.subagentScrollToEndID, state.subagentNeedsScroll, state.subagentPendingLayout)
	}
}

func TestSubagentTranscriptRendersFinalResponseAtEnd(t *testing.T) {
	t.Parallel()
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := time.Now().Format(time.RFC3339Nano)
	messages := make([]protocol.TranscriptMessage, 0, 25)
	for index := 0; index < 24; index++ {
		messages = append(messages, protocol.TranscriptMessage{
			ID: fmt.Sprintf("message_%d", index), TurnID: fmt.Sprintf("turn_%d", index), Sequence: int64(index), Role: "user",
			Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: fmt.Sprintf("Earlier request %d", index)}}, CreatedAt: now,
		})
	}
	messages = append(messages, protocol.TranscriptMessage{
		ID: "message_final", TurnID: "turn_final", Sequence: 24, Role: "assistant", StopReason: "stop",
		Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "FINAL SUBAGENT RESPONSE"}}, CreatedAt: now,
	})
	scroll := &ui.ScrollController{}
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: &workspaceLayoutState{}, ActivityScroll: &ui.ScrollController{}, ActivityFocus: &ui.FocusNode{},
		SubagentPaneID: conversationID, SubagentTranscriptOrder: []string{conversationID}, SubagentScroll: scroll,
		SubagentConversations: []protocol.SubagentConversation{{ID: conversationID, AgentName: "reviewer", State: "idle"}},
		SubagentTranscripts:   map[string]protocol.SubagentTranscript{conversationID: {ConversationID: conversationID, Messages: messages}},
	}})
	application.Pump(100, 14)
	scroll.ScrollToEnd()
	application.Pump(100, 14)
	text := strings.Join(paintedRows(application, 100, 14), "\n")
	if !strings.Contains(text, "FINAL SUBAGENT RESPONSE") {
		t.Fatalf("final subagent response was not initially visible:\n%s", text)
	}
}

func TestSubagentTranscriptUsesRetainedConversationTab(t *testing.T) {
	t.Parallel()
	layout := &workspaceLayoutState{}
	scroll := &ui.ScrollController{}
	conversationID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := time.Now().Format(time.RFC3339Nano)
	back := false
	view := shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo", ThinkingLevel: "medium"},
		SubagentsOpen: true, ActivitySelected: true, WorkspaceLayout: layout, ActivityScroll: scroll, ActivityFocus: &ui.FocusNode{},
		SubagentPaneID: conversationID, SubagentTranscriptOrder: []string{conversationID},
		SubagentConversations: []protocol.SubagentConversation{{
			ID: conversationID, AgentName: "scout", Model: "test/echo", State: "completed", Generation: 1, UpdatedAt: now,
		}},
		SubagentTranscripts: map[string]protocol.SubagentTranscript{conversationID: {
			ConversationID: conversationID,
			Messages: []protocol.TranscriptMessage{
				{ID: "user_1", TurnID: "turn_1", Sequence: 0, Role: "user", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "Inspect the repository"}}, CreatedAt: now},
				{ID: "assistant_1", TurnID: "turn_1", Sequence: 1, Role: "assistant", Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: "Found the evidence"}}, StopReason: "stop", CreatedAt: now},
			},
		}},
	}, Callbacks: shellCallbacks{ShowSubagentRoster: func(ui.EventContext) { back = true }}}
	application := uitest.New(view)
	application.Pump(100, 22)
	text := strings.Join(paintedRows(application, 100, 22), "\n")
	for _, expected := range []string{"Transcript", "Subagents", "scout", "Inspect the repository", "Found the evidence", "esc back"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("retained transcript missing %q:\n%s", expected, text)
		}
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if !back {
		t.Fatal("escape did not return to the subagent roster")
	}
}
