package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type switchRetainedPaneIntent struct{}

func (switchRetainedPaneIntent) IntentType() ui.IntentType { return "test.workspace.switch" }

type retainedPaneHarness struct{ State *retainedPaneHarnessState }

func (w retainedPaneHarness) CreateState() ui.State { return w.State }

type retainedPaneHarnessState struct {
	ui.StateBase
	second bool
}

func (s *retainedPaneHarnessState) Build(ui.BuildContext) ui.Widget {
	active := workspacePaneIdentity("first")
	if s.second {
		active = "second"
	}
	panes := []ui.Widget{
		retainedWorkspacePane{Identity: "first", Active: active == "first", Child: retainedPaneProbe{Name: "first"}},
		retainedWorkspacePane{Identity: "second", Active: active == "second", Child: retainedPaneProbe{Name: "second"}},
	}
	return ui.Actions{Bindings: map[ui.IntentType]ui.ActionFunc{
		switchRetainedPaneIntent{}.IntentType(): func(ui.EventContext, ui.Intent) ui.EventResult {
			s.SetState(func() { s.second = !s.second })
			return ui.EventHandled
		},
	}, Child: keyShortcuts{Bindings: ui.ShortcutMap{"s": switchRetainedPaneIntent{}}, Child: retainedWorkspacePaneStack{Panes: panes}}}
}

type retainedPaneProbe struct{ Name string }

func (retainedPaneProbe) CreateState() ui.State { return &retainedPaneProbeState{} }

type retainedPaneProbeState struct {
	ui.StateBase
	count int
}

func (s *retainedPaneProbeState) Build(ui.BuildContext) ui.Widget {
	probe := s.Widget().(retainedPaneProbe)
	return mouseActivator{
		OnPressed: func(ui.EventContext) { s.SetState(func() { s.count++ }) },
		Child:     ui.Text{Value: fmt.Sprintf("%s:%d", probe.Name, s.count)},
	}
}

func TestRetainedWorkspacePaneStackPreservesPaneStateAcrossSelection(t *testing.T) {
	t.Parallel()
	state := &retainedPaneHarnessState{}
	application := uitest.New(retainedPaneHarness{State: state})
	application.Pump(30, 4)
	if got := strings.TrimSpace(application.Text()); got != "first:0" {
		t.Fatalf("initial pane = %q, want first:0", got)
	}
	application.Click(1, 0)
	application.Pump(30, 4)
	if got := strings.TrimSpace(application.Text()); got != "first:1" {
		t.Fatalf("updated first pane = %q, want first:1", got)
	}
	application.Key("s")
	application.Pump(30, 4)
	if got := strings.TrimSpace(application.Text()); got != "second:0" {
		t.Fatalf("selected second pane = %q, want second:0", got)
	}
	application.Click(1, 0)
	application.Pump(12, 3)
	if got := strings.TrimSpace(application.Text()); got != "second:1" {
		t.Fatalf("resized second pane = %q, want retained second:1", got)
	}
	application.Key("s")
	application.Pump(12, 3)
	if got := strings.TrimSpace(application.Text()); got != "first:1" {
		t.Fatalf("restored narrow first pane = %q, want retained first:1", got)
	}
	application.Pump(30, 4)
	if got := strings.TrimSpace(application.Text()); got != "first:1" {
		t.Fatalf("restored wide first pane = %q, want retained first:1", got)
	}
	application.Key("s")
	application.Pump(30, 4)
	if got := strings.TrimSpace(application.Text()); got != "second:1" {
		t.Fatalf("restored resized second pane = %q, want retained second:1", got)
	}
}

type disposeRetainedPanesIntent struct{}

func (disposeRetainedPanesIntent) IntentType() ui.IntentType { return "test.workspace.dispose" }

type retainedPaneDisposalHarness struct{ State *retainedPaneDisposalState }

func (w retainedPaneDisposalHarness) CreateState() ui.State { return w.State }

type retainedPaneDisposalState struct {
	ui.StateBase
	removed  bool
	disposed int
}

func (s *retainedPaneDisposalState) Build(ui.BuildContext) ui.Widget {
	panes := []ui.Widget(nil)
	if !s.removed {
		panes = []ui.Widget{
			retainedWorkspacePane{Identity: "first", Active: true, Child: retainedPaneDisposalProbe{OnDispose: func() { s.disposed++ }}},
			retainedWorkspacePane{Identity: "second", Child: retainedPaneDisposalProbe{OnDispose: func() { s.disposed++ }}},
		}
	}
	return ui.Actions{Bindings: map[ui.IntentType]ui.ActionFunc{
		disposeRetainedPanesIntent{}.IntentType(): func(ui.EventContext, ui.Intent) ui.EventResult {
			s.SetState(func() { s.removed = true })
			return ui.EventHandled
		},
	}, Child: keyShortcuts{Bindings: ui.ShortcutMap{"x": disposeRetainedPanesIntent{}}, Child: retainedWorkspacePaneStack{Panes: panes}}}
}

type retainedPaneDisposalProbe struct{ OnDispose func() }

func (retainedPaneDisposalProbe) CreateState() ui.State { return &retainedPaneDisposalProbeState{} }

type retainedPaneDisposalProbeState struct {
	ui.StateBase
	onDispose func()
}

func (s *retainedPaneDisposalProbeState) Build(ui.BuildContext) ui.Widget {
	s.onDispose = s.Widget().(retainedPaneDisposalProbe).OnDispose
	return ui.Text{Value: "mounted"}
}

func (s *retainedPaneDisposalProbeState) Dispose() {
	if s.onDispose != nil {
		s.onDispose()
	}
}

func TestRetainedWorkspacePaneStackDisposesRemovedPanes(t *testing.T) {
	t.Parallel()
	state := &retainedPaneDisposalState{}
	application := uitest.New(retainedPaneDisposalHarness{State: state})
	application.Pump(30, 4)
	application.Key("x")
	application.Pump(30, 4)
	if state.disposed != 2 {
		t.Fatalf("disposed pane states = %d, want 2", state.disposed)
	}
}

type switchScrollablePaneIntent struct{}

func (switchScrollablePaneIntent) IntentType() ui.IntentType { return "test.workspace.switch-scroll" }

type scrollRetentionHarness struct{ State *scrollRetentionState }

func (w scrollRetentionHarness) CreateState() ui.State { return w.State }

type scrollRetentionState struct {
	ui.StateBase
	selected workspacePaneIdentity
	firstID  string
	secondID string
	first    *ui.ScrollController
	second   *ui.ScrollController
	focuses  map[string]*ui.FocusNode
	messages []protocol.TranscriptMessage
}

func (s *scrollRetentionState) Build(ui.BuildContext) ui.Widget {
	workspace := workspaceControllerSnapshot{
		Panes: []workspacePaneDescriptor{subagentWorkspacePane(s.firstID), subagentWorkspacePane(s.secondID)}, Selected: s.selected,
	}
	view := shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		Workspace: workspace, ActivitySelected: true, ActivityScroll: &ui.ScrollController{}, ActivityFocus: &ui.FocusNode{},
		SubagentFocuses: s.focuses, SubagentScrolls: map[string]*ui.ScrollController{s.firstID: s.first, s.secondID: s.second},
		SubagentConversations: []protocol.SubagentConversation{
			{ID: s.firstID, AgentName: "first", State: "idle"}, {ID: s.secondID, AgentName: "second", State: "idle"},
		},
		SubagentTranscripts: map[string]protocol.SubagentTranscript{
			s.firstID: {ConversationID: s.firstID, Messages: s.messages}, s.secondID: {ConversationID: s.secondID, Messages: s.messages},
		},
	}}
	return ui.Actions{Bindings: map[ui.IntentType]ui.ActionFunc{
		switchScrollablePaneIntent{}.IntentType(): func(ui.EventContext, ui.Intent) ui.EventResult {
			s.SetState(func() {
				if s.selected == workspacePaneIdentity("subagent:"+s.firstID) {
					s.selected = workspacePaneIdentity("subagent:" + s.secondID)
				} else {
					s.selected = workspacePaneIdentity("subagent:" + s.firstID)
				}
			})
			return ui.EventHandled
		},
	}, Child: keyShortcuts{Bindings: ui.ShortcutMap{"s": switchScrollablePaneIntent{}}, Child: view}}
}

func TestSubagentPaneRetainsScrollAcrossSelectionAndResize(t *testing.T) {
	t.Parallel()
	firstID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secondID := "subagent_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	messages := make([]protocol.TranscriptMessage, 30)
	for index := range messages {
		messages[index] = protocol.TranscriptMessage{
			ID: fmt.Sprintf("message_%d", index), TurnID: fmt.Sprintf("turn_%d", index), Sequence: int64(index), Role: "user",
			Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: fmt.Sprintf("Request %d", index)}},
		}
	}
	firstScroll, secondScroll := &ui.ScrollController{}, &ui.ScrollController{}
	state := &scrollRetentionState{
		selected: workspacePaneIdentity("subagent:" + firstID), firstID: firstID, secondID: secondID,
		first: firstScroll, second: secondScroll, focuses: map[string]*ui.FocusNode{firstID: {}, secondID: {}}, messages: messages,
	}
	application := uitest.New(scrollRetentionHarness{State: state})
	application.Pump(100, 20)
	firstScroll.ScrollToOffset(8)
	application.Pump(100, 20)
	retainedOffset := firstScroll.Metrics().ScrollOffset
	if retainedOffset == 0 {
		t.Fatal("first subagent transcript did not scroll")
	}
	application.Key("s")
	application.Pump(40, 12)
	application.Pump(120, 24)
	application.Key("s")
	application.Pump(100, 20)
	if got := firstScroll.Metrics().ScrollOffset; got != retainedOffset {
		t.Fatalf("restored scroll offset = %d, want %d", got, retainedOffset)
	}
}

func TestHiddenWorkspacePanesDoNotParticipateInFocusTraversal(t *testing.T) {
	t.Parallel()
	firstID := "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secondID := "subagent_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	secondIdentity, err := workspacePaneIdentityFor(subagentWorkspacePane(secondID))
	if err != nil {
		t.Fatal(err)
	}
	firstFocus, secondFocus := &ui.FocusNode{}, &ui.FocusNode{}
	focusMoves := 0
	selectionMoves := []int{}
	canceledTasks := []string{}
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Parent", Model: "test/echo"},
		Workspace: workspaceControllerSnapshot{
			Panes: []workspacePaneDescriptor{subagentWorkspacePane(firstID), subagentWorkspacePane(secondID)}, Selected: secondIdentity,
		},
		ActivitySelected: true, ActivityScroll: &ui.ScrollController{}, ActivityFocus: &ui.FocusNode{},
		SubagentFocuses: map[string]*ui.FocusNode{firstID: firstFocus, secondID: secondFocus},
		SubagentScrolls: map[string]*ui.ScrollController{firstID: {}, secondID: {}},
		SubagentConversations: []protocol.SubagentConversation{
			{ID: firstID, AgentName: "first", State: "running", ActiveTaskID: "first-task", Tasks: []protocol.SubagentTask{{ID: "first-task", State: "running"}}},
			{ID: secondID, AgentName: "second", State: "running", ActiveTaskID: "second-task", Tasks: []protocol.SubagentTask{{ID: "second-task", State: "running"}}},
		},
		SubagentTranscripts: map[string]protocol.SubagentTranscript{
			firstID: {ConversationID: firstID}, secondID: {ConversationID: secondID},
		},
	}, Callbacks: shellCallbacks{
		MoveWorkspaceFocus:     func(ui.EventContext) { focusMoves++ },
		MoveWorkspaceSelection: func(_ ui.EventContext, delta int) { selectionMoves = append(selectionMoves, delta) },
		CancelSubagentTask:     func(_ ui.EventContext, taskID string, _ uint64) { canceledTasks = append(canceledTasks, taskID) },
	}})
	application.Pump(100, 18)
	if firstFocus.HasFocus() || !secondFocus.HasFocus() {
		t.Fatalf("initial focus = first:%t second:%t, want selected second", firstFocus.HasFocus(), secondFocus.HasFocus())
	}
	application.Key("c")
	if !reflect.DeepEqual(canceledTasks, []string{"second-task"}) {
		t.Fatalf("visible-pane keyboard actions = %v, want only second-task", canceledTasks)
	}
	application.Tab()
	if firstFocus.HasFocus() || secondFocus.HasFocus() {
		t.Fatalf("composer traversal focused retained pane = first:%t second:%t", firstFocus.HasFocus(), secondFocus.HasFocus())
	}
	application.ShiftTab()
	if focusMoves != 2 {
		t.Fatalf("logical workspace focus moves = %d, want 2", focusMoves)
	}
	if firstFocus.HasFocus() || !secondFocus.HasFocus() {
		t.Fatalf("reverse traversal = first:%t second:%t, want selected second", firstFocus.HasFocus(), secondFocus.HasFocus())
	}
	application.Send(vaxis.Key{Keycode: ']', Modifiers: vaxis.ModCtrl})
	application.Send(vaxis.Key{Keycode: '[', Modifiers: vaxis.ModCtrl})
	if !reflect.DeepEqual(selectionMoves, []int{1, -1}) {
		t.Fatalf("workspace selection moves = %v, want next then previous", selectionMoves)
	}
}
