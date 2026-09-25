package tui

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func overflowWorkspaceSnapshot(ids []string, selected int) shellSnapshot {
	panes := make([]workspacePaneDescriptor, 0, len(ids))
	conversations := make([]protocol.SubagentConversation, 0, len(ids))
	transcripts := make(map[string]protocol.SubagentTranscript, len(ids))
	focuses := make(map[string]*ui.FocusNode, len(ids))
	scrolls := make(map[string]*ui.ScrollController, len(ids))
	for index, id := range ids {
		panes = append(panes, subagentWorkspacePane(id))
		conversations = append(conversations, protocol.SubagentConversation{ID: id, AgentName: "reviewer-" + string(rune('a'+index)), State: "idle"})
		transcripts[id] = protocol.SubagentTranscript{ConversationID: id}
		focuses[id] = &ui.FocusNode{}
		scrolls[id] = &ui.ScrollController{}
	}
	selectedIdentity := workspaceAgentIdentity
	if selected >= 0 && selected < len(panes) {
		selectedIdentity, _ = workspacePaneIdentityFor(panes[selected])
	}
	return shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		Workspace: workspaceControllerSnapshot{Panes: panes, Selected: selectedIdentity}, ActivitySelected: selected >= 0,
		ActivityScroll: &ui.ScrollController{}, ActivityFocus: &ui.FocusNode{}, SubagentFocuses: focuses, SubagentScrolls: scrolls,
		SubagentConversations: conversations, SubagentTranscripts: transcripts,
	}
}

func TestWorkspaceTabStripShowsAllTabsWhenTheyFitAndLabeledOverflowWhenNarrow(t *testing.T) {
	t.Parallel()
	view := shellView{Snapshot: overflowWorkspaceSnapshot([]string{"a", "b", "c", "d"}, 2)}
	tests := []struct {
		width int
		want  string
	}{
		{width: 15, want: " revi ⋯ 3 more "},
		{width: 40, want: " Agent  reviewer-c  × ⋯ 3 more          "},
		{width: 55, want: " Agent  reviewer-a  × reviewer-c  × ⋯ 2 more           "},
		{width: 100, want: " Agent  reviewer-a  × reviewer-b  × reviewer-c  × reviewer-d  ×                                     "},
	}
	for _, test := range tests {
		application := uitest.New(view)
		application.Pump(test.width, 12)
		if got := paintedRows(application, test.width, 12)[2]; got != test.want {
			t.Fatalf("width %d tab row = %q, want %q", test.width, got, test.want)
		}
	}
}

func TestWorkspaceTabsShowVisibleAndHiddenSubagentActivity(t *testing.T) {
	t.Parallel()
	snapshot := overflowWorkspaceSnapshot([]string{"a", "b", "c", "d"}, 2)
	snapshot.SubagentConversations[0].State = "running"
	snapshot.SubagentConversations[1].State = "failed"
	snapshot.SubagentConversations[2].State = "interrupted"

	wide := uitest.New(shellView{Snapshot: snapshot})
	wide.Pump(100, 12)
	wideRow := paintedRows(wide, 100, 12)[2]
	for _, expected := range []string{"reviewer-a " + spinnerFrames[0], "reviewer-b " + glyphCross, "reviewer-c " + glyphCircleSlash} {
		if !strings.Contains(wideRow, expected) {
			t.Fatalf("wide activity row missing %q: %q", expected, wideRow)
		}
	}

	narrow := uitest.New(shellView{Snapshot: snapshot})
	narrow.Pump(40, 12)
	if got := paintedRows(narrow, 40, 12)[2]; !strings.Contains(got, "⋯ 3 more "+glyphCross) {
		t.Fatalf("hidden failed activity summary = %q", got)
	}

	snapshot.SubagentConversations[1].State = "idle"
	running := uitest.New(shellView{Snapshot: snapshot})
	running.Pump(40, 12)
	if got := paintedRows(running, 40, 12)[2]; !strings.Contains(got, "⋯ 3 more "+spinnerFrames[0]) {
		t.Fatalf("hidden running activity summary = %q", got)
	}
}

func TestWorkspaceOverflowControlOpensPanePicker(t *testing.T) {
	t.Parallel()
	opened := false
	view := shellView{
		Snapshot:  overflowWorkspaceSnapshot([]string{"a", "b", "c", "d"}, 2),
		Callbacks: shellCallbacks{OpenWorkspacePicker: func(ui.EventContext) { opened = true }},
	}
	for _, width := range []int{15, 40} {
		opened = false
		application := uitest.New(view)
		application.Pump(width, 12)
		rows := paintedRows(application, width, 12)
		column, row := findTextCell(t, rows, "⋯ 3 more")
		application.Click(column+2, row)
		if !opened {
			t.Fatalf("overflow control did not open the workspace pane picker at width %d", width)
		}
	}
}

func TestWorkspacePanePickerKeepsLateSelectionVisible(t *testing.T) {
	t.Parallel()
	ids := make([]string, 25)
	for index := range ids {
		ids[index] = string(rune('a' + index))
	}
	snapshot := overflowWorkspaceSnapshot(ids, 19)
	snapshot.WorkspacePickerOpen = true
	snapshot.WorkspacePickerSelection = 20
	scroll := &ui.ScrollController{}
	snapshot.WorkspacePickerScroll = scroll
	application := uitest.New(shellView{Snapshot: snapshot})
	application.Pump(80, 24)
	scroll.ScrollToOffset(15)
	application.Pump(80, 24)
	text := strings.Join(paintedRows(application, 80, 24), "\n")
	for _, expected := range []string{"✓ reviewer-t", "idle ×", "ctrl+d close tab"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("scrolled picker missing %q:\n%s", expected, text)
		}
	}
	selectedColumn, selectedRow := findTextCell(t, paintedRows(application, 80, 24), "✓ reviewer-t")
	selectedColumn += len([]rune("✓ "))
	idleColumn, idleRow := findTextCell(t, paintedRows(application, 80, 24), "reviewer-u")
	if application.Cell(selectedColumn, selectedRow).Style.Background == application.Cell(idleColumn, idleRow).Style.Background {
		t.Fatal("workspace picker selection does not use the standard focused-row background")
	}
}

func TestWorkspacePanePickerFiltersAndOpensCanonicalPane(t *testing.T) {
	t.Parallel()
	snapshot := overflowWorkspaceSnapshot([]string{"a", "b", "c", "d"}, 2)
	snapshot.WorkspacePickerOpen = true
	snapshot.WorkspacePickerQuery = "reviewer-b"
	selected, closed, parentDismissed := "", false, false
	application := uitest.New(shellView{Snapshot: snapshot, Callbacks: shellCallbacks{
		WorkspacePickerQuery: func(ui.EventContext, string) {},
		SelectWorkspacePane:  func(_ ui.EventContext, descriptor workspacePaneDescriptor) { selected = descriptor.ResourceID },
		CloseWorkspacePicker: func(ui.EventContext) { closed = true },
		Dismiss:              func(ui.EventContext) { parentDismissed = true },
	}})
	application.Pump(80, 24)
	text := strings.Join(paintedRows(application, 80, 24), "\n")
	for _, expected := range []string{"Open workspace tab", "reviewer-b", "idle", "ctrl+d close tab"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("workspace picker missing %q:\n%s", expected, text)
		}
	}
	application.Enter()
	if selected != "b" || !closed {
		t.Fatalf("picker activation = selected:%q closed:%t, want b and closed", selected, closed)
	}
	closed = false
	focusMoves, selectionMoves := 0, 0
	escapeApplication := uitest.New(shellView{Snapshot: snapshot, Callbacks: shellCallbacks{
		CloseWorkspacePicker:   func(ui.EventContext) { closed = true },
		MoveWorkspaceFocus:     func(ui.EventContext) { focusMoves++ },
		MoveWorkspaceSelection: func(ui.EventContext, int) { selectionMoves++ },
		Dismiss:                func(ui.EventContext) { parentDismissed = true },
	}})
	escapeApplication.Pump(80, 24)
	escapeApplication.Tab()
	if focusMoves != 0 {
		t.Fatalf("modal Tab changed workspace focus %d times", focusMoves)
	}
	escapeApplication.Send(vaxis.Key{Keycode: ']', Modifiers: vaxis.ModCtrl})
	if selectionMoves != 0 {
		t.Fatalf("modal shortcut changed workspace selection %d times", selectionMoves)
	}
	escapeApplication.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if !closed || parentDismissed {
		t.Fatalf("picker escape = closed:%t parent dismissed:%t", closed, parentDismissed)
	}

	closedPane := ""
	closeApplication := uitest.New(shellView{Snapshot: snapshot, Callbacks: shellCallbacks{
		CloseWorkspacePane: func(_ ui.EventContext, descriptor workspacePaneDescriptor) { closedPane = descriptor.ResourceID },
	}})
	closeApplication.Pump(80, 24)
	closeApplication.Send(vaxis.Key{Keycode: 'd', Modifiers: vaxis.ModCtrl})
	if closedPane != "b" {
		t.Fatalf("picker close action closed %q, want b", closedPane)
	}

	closedPane, selected, closed = "", "", false
	mouseCloseApplication := uitest.New(shellView{Snapshot: snapshot, Callbacks: shellCallbacks{
		CloseWorkspacePane:   func(_ ui.EventContext, descriptor workspacePaneDescriptor) { closedPane = descriptor.ResourceID },
		SelectWorkspacePane:  func(_ ui.EventContext, descriptor workspacePaneDescriptor) { selected = descriptor.ResourceID },
		CloseWorkspacePicker: func(ui.EventContext) { closed = true },
	}})
	mouseCloseApplication.Pump(80, 24)
	mouseRows := paintedRows(mouseCloseApplication, 80, 24)
	metadataColumn, mouseRow := findTextCell(t, mouseRows, "idle ×")
	mouseCloseApplication.Click(metadataColumn+len([]rune("idle ")), mouseRow)
	if closedPane != "b" || selected != "" || closed {
		t.Fatalf("mouse close = pane:%q selected:%q picker closed:%t", closedPane, selected, closed)
	}

	agentSnapshot := snapshot
	agentSnapshot.WorkspacePickerQuery = "Agent"
	closedPane = ""
	agentApplication := uitest.New(shellView{Snapshot: agentSnapshot, Callbacks: shellCallbacks{
		CloseWorkspacePane: func(_ ui.EventContext, descriptor workspacePaneDescriptor) { closedPane = descriptor.ResourceID },
	}})
	agentApplication.Pump(80, 24)
	agentApplication.Send(vaxis.Key{Keycode: 'd', Modifiers: vaxis.ModCtrl})
	if closedPane != "" {
		t.Fatalf("Agent picker row was closable as %q", closedPane)
	}
}
