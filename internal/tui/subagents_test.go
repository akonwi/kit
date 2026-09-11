package tui

import (
	"context"
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
