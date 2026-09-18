package tui

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestWorkspaceControllerStartsWithAgentOnly(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	if controller.StripVisible() {
		t.Fatal("Agent-only workspace unexpectedly shows tab strip")
	}
	if got := controller.SelectedIdentity(); got != workspaceAgentIdentity {
		t.Fatalf("selected identity = %q, want %q", got, workspaceAgentIdentity)
	}
	if _, ok := controller.SelectedPane(); ok {
		t.Fatal("Agent selection unexpectedly returned a secondary pane")
	}
	if got := controller.FocusOwner(); got != workspaceFocusContent {
		t.Fatalf("focus owner = %v, want content", got)
	}
}

func TestWorkspaceControllerOpensAndDeduplicatesResourceIdentity(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	first := subagentWorkspacePane("conversation-a")
	identity, opened, err := controller.Open(first)
	if err != nil || !opened {
		t.Fatalf("open first pane = (%q, %t, %v), want opened", identity, opened, err)
	}
	if !controller.StripVisible() || controller.SelectedIdentity() != identity {
		t.Fatalf("opened state = strip:%t selected:%q", controller.StripVisible(), controller.SelectedIdentity())
	}

	controller.SetFocusOwner(workspaceFocusComposer)
	identityAgain, openedAgain, err := controller.Open(first)
	if err != nil || openedAgain || identityAgain != identity {
		t.Fatalf("open duplicate = (%q, %t, %v), want (%q, false, nil)", identityAgain, openedAgain, err, identity)
	}
	if controller.FocusOwner() != workspaceFocusContent {
		t.Fatalf("reopened pane focus owner = %v, want content", controller.FocusOwner())
	}
	if got := controller.Panes(); !reflect.DeepEqual(got, []workspacePaneDescriptor{first}) {
		t.Fatalf("panes = %#v, want one retained descriptor", got)
	}
}

func TestWorkspaceControllerPreservesAppendOrderAndSelectsExistingPane(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	for _, id := range []string{"a", "b", "c"} {
		if _, _, err := controller.Open(subagentWorkspacePane(id)); err != nil {
			t.Fatalf("open %q: %v", id, err)
		}
	}
	if !controller.Select(workspacePaneIdentity("subagent:b")) {
		t.Fatal("select existing pane returned false")
	}
	if _, _, err := controller.Open(subagentWorkspacePane("a")); err != nil {
		t.Fatalf("reopen a: %v", err)
	}
	got := controller.Panes()
	want := []workspacePaneDescriptor{subagentWorkspacePane("a"), subagentWorkspacePane("b"), subagentWorkspacePane("c")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("panes = %#v, want %#v", got, want)
	}
	if selected := controller.SelectedIdentity(); selected != workspacePaneIdentity("subagent:a") {
		t.Fatalf("selected = %q, want reopened pane", selected)
	}
}

func TestWorkspaceTabRoundTripRestoresPinnedTranscriptEnd(t *testing.T) {
	t.Parallel()
	state := &appState{transcriptVisible: true}
	if _, _, err := state.workspace.Open(workingTreeDiffWorkspacePane("workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")); err != nil {
		t.Fatal(err)
	}
	state.syncWorkspaceSelection()
	if state.transcriptVisible || !state.transcriptPinnedOnHide {
		t.Fatalf("hidden transcript state = visible:%t pinned:%t", state.transcriptVisible, state.transcriptPinnedOnHide)
	}
	state.workspace.SelectAgent()
	state.syncWorkspaceSelection()
	if !state.transcriptVisible || !state.needsScroll || !state.scrollPendingLayout {
		t.Fatalf("restored transcript state = visible:%t needsScroll:%t pendingLayout:%t", state.transcriptVisible, state.needsScroll, state.scrollPendingLayout)
	}
}

func TestWorkspaceTabRoundTripPreservesUnpinnedTranscriptPosition(t *testing.T) {
	t.Parallel()
	state := &appState{transcriptVisible: false, transcriptPinnedOnHide: false}
	state.syncTranscriptVisibility(true)
	if state.needsScroll || state.scrollPendingLayout {
		t.Fatalf("unpinned transcript requested end scroll: needs:%t pending:%t", state.needsScroll, state.scrollPendingLayout)
	}
}

func TestWorkspaceSelectionGatesSubagentRefreshToVisiblePane(t *testing.T) {
	t.Parallel()
	state := &appState{}
	for _, id := range []string{"a", "b"} {
		if _, _, err := state.workspace.Open(subagentWorkspacePane(id)); err != nil {
			t.Fatal(err)
		}
	}
	state.syncWorkspaceSelection()
	refreshRoster, conversationID := state.visibleSubagentRefreshTargets()
	if state.subagentPaneID != "b" || !state.activitySelected || refreshRoster || conversationID != "b" {
		t.Fatalf("selected refresh target = pane:%q active:%t roster:%t conversation:%q, want only b", state.subagentPaneID, state.activitySelected, refreshRoster, conversationID)
	}
	state.workspace.SelectAgent()
	state.syncWorkspaceSelection()
	refreshRoster, conversationID = state.visibleSubagentRefreshTargets()
	if state.subagentPaneID != "" || state.activitySelected || refreshRoster || conversationID != "" {
		t.Fatalf("Agent refresh target = pane:%q active:%t roster:%t conversation:%q, want none", state.subagentPaneID, state.activitySelected, refreshRoster, conversationID)
	}
	identity, err := workspacePaneIdentityFor(subagentWorkspacePane("a"))
	if err != nil || !state.workspace.Select(identity) {
		t.Fatalf("select a: identity=%q err=%v", identity, err)
	}
	state.syncWorkspaceSelection()
	refreshRoster, conversationID = state.visibleSubagentRefreshTargets()
	if state.subagentPaneID != "a" || !state.activitySelected || refreshRoster || conversationID != "a" {
		t.Fatalf("restored refresh target = pane:%q active:%t roster:%t conversation:%q, want only a", state.subagentPaneID, state.activitySelected, refreshRoster, conversationID)
	}
	state.subagentsOpen = true
	refreshRoster, conversationID = state.visibleSubagentRefreshTargets()
	if !refreshRoster || conversationID != "a" {
		t.Fatalf("modal refresh targets = roster:%t conversation:%q, want roster and a", refreshRoster, conversationID)
	}
	state.subagentRosterLoading = true
	refreshRoster, conversationID = state.visibleSubagentRefreshTargets()
	if refreshRoster || conversationID != "a" {
		t.Fatalf("loading refresh targets = roster:%t conversation:%q, want only a", refreshRoster, conversationID)
	}
}

func TestSubagentConversationChangeClearsPaneLocalActivity(t *testing.T) {
	t.Parallel()
	state := &appState{
		activitySourceID: "turn:old", activityConversationID: "old",
		inlineActivityOpen: map[string]bool{"turn:old": true}, activityCursor: activityToolKey{TurnID: "turn"},
	}
	state.clearSubagentActivityForConversationChange("new")
	if state.activitySourceID != "" || state.activityConversationID != "" || len(state.inlineActivityOpen) != 0 || state.activityCursor != (activityToolKey{}) {
		t.Fatalf("stale activity survived conversation change: source=%q conversation=%q open=%v cursor=%+v", state.activitySourceID, state.activityConversationID, state.inlineActivityOpen, state.activityCursor)
	}
	state.activitySourceID = "turn:old"
	state.activityConversationID = "old"
	state.inlineActivityOpen["turn:old"] = true
	state.activityCursor = activityToolKey{TurnID: "turn"}
	state.clearSubagentActivityForConversationChange("")
	if state.activitySourceID != "" || state.activityConversationID != "" || len(state.inlineActivityOpen) != 0 || state.activityCursor != (activityToolKey{}) {
		t.Fatalf("stale activity survived Agent selection: source=%q conversation=%q open=%v cursor=%+v", state.activitySourceID, state.activityConversationID, state.inlineActivityOpen, state.activityCursor)
	}
}

func TestWorkspaceControllerMovesSelectionInCanonicalOrderWithWrap(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	for _, id := range []string{"a", "b", "c"} {
		if _, _, err := controller.Open(subagentWorkspacePane(id)); err != nil {
			t.Fatal(err)
		}
	}
	controller.SelectAgent()
	want := []workspacePaneIdentity{"subagent:a", "subagent:b", "subagent:c", workspaceAgentIdentity, "subagent:c"}
	for index, delta := range []int{1, 1, 1, 1, -1} {
		if !controller.MoveSelection(delta) {
			t.Fatalf("move %d was rejected", delta)
		}
		if got := controller.SelectedIdentity(); got != want[index] {
			t.Fatalf("move %d selection = %q, want %q", delta, got, want[index])
		}
	}
}

func TestWorkspaceControllerCloseSelectionFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		selected  string
		closed    string
		want      workspacePaneIdentity
		wantOrder []workspacePaneDescriptor
	}{
		{name: "inactive keeps selection", selected: "a", closed: "b", want: "subagent:a", wantOrder: []workspacePaneDescriptor{subagentWorkspacePane("a"), subagentWorkspacePane("c")}},
		{name: "active selects same index", selected: "b", closed: "b", want: "subagent:c", wantOrder: []workspacePaneDescriptor{subagentWorkspacePane("a"), subagentWorkspacePane("c")}},
		{name: "active final selects preceding", selected: "c", closed: "c", want: "subagent:b", wantOrder: []workspacePaneDescriptor{subagentWorkspacePane("a"), subagentWorkspacePane("b")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var controller workspaceController
			for _, id := range []string{"a", "b", "c"} {
				_, _, _ = controller.Open(subagentWorkspacePane(id))
			}
			controller.Select(workspacePaneIdentity("subagent:" + test.selected))
			if !controller.Close(workspacePaneIdentity("subagent:" + test.closed)) {
				t.Fatal("close returned false")
			}
			if got := controller.SelectedIdentity(); got != test.want {
				t.Fatalf("selected = %q, want %q", got, test.want)
			}
			if got := controller.Panes(); !reflect.DeepEqual(got, test.wantOrder) {
				t.Fatalf("panes = %#v, want %#v", got, test.wantOrder)
			}
		})
	}
}

func TestWorkspaceControllerClosingLastPaneReturnsToAgent(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	identity, _, _ := controller.Open(subagentWorkspacePane("a"))
	if !controller.Close(identity) {
		t.Fatal("close returned false")
	}
	if controller.StripVisible() || controller.SelectedIdentity() != workspaceAgentIdentity {
		t.Fatalf("closed state = strip:%t selected:%q", controller.StripVisible(), controller.SelectedIdentity())
	}
	if controller.Close(workspaceAgentIdentity) {
		t.Fatal("Agent was closable")
	}
}

func TestWorkspaceControllerRejectsNewPaneAtLimitButReopensExisting(t *testing.T) {
	t.Parallel()
	controller := workspaceController{secondaryLimit: 2}
	for _, id := range []string{"a", "b"} {
		if _, _, err := controller.Open(subagentWorkspacePane(id)); err != nil {
			t.Fatalf("open %q: %v", id, err)
		}
	}
	if _, opened, err := controller.Open(subagentWorkspacePane("c")); !errors.Is(err, errWorkspacePaneLimit) || opened {
		t.Fatalf("open above limit = opened:%t err:%v", opened, err)
	}
	if identity, opened, err := controller.Open(subagentWorkspacePane("a")); err != nil || opened || identity != "subagent:a" {
		t.Fatalf("reopen at limit = (%q, %t, %v)", identity, opened, err)
	}
}

func TestWorkspaceControllerValidatesDescriptor(t *testing.T) {
	t.Parallel()
	tests := []workspacePaneDescriptor{
		{},
		{Kind: workspacePaneKind("future"), ResourceID: "resource"},
		{Kind: workspacePaneSubagentConversation},
	}
	for _, descriptor := range tests {
		if _, _, err := new(workspaceController).Open(descriptor); err == nil {
			t.Fatalf("Open(%#v) unexpectedly succeeded", descriptor)
		}
	}
}

func TestWorkspaceControllerMovesLogicalFocusAndResets(t *testing.T) {
	t.Parallel()
	var controller workspaceController
	if got := controller.MoveFocus(); got != workspaceFocusComposer {
		t.Fatalf("first focus move = %v, want composer", got)
	}
	if got := controller.MoveFocus(); got != workspaceFocusContent {
		t.Fatalf("second focus move = %v, want content", got)
	}
	controller.SetFocusOwner(workspaceFocusComposer)
	_, _, _ = controller.Open(subagentWorkspacePane("a"))
	controller.Reset()
	if controller.FocusOwner() != workspaceFocusContent || controller.SelectedIdentity() != workspaceAgentIdentity || len(controller.Panes()) != 0 {
		t.Fatalf("reset controller = %#v", controller)
	}
}

func TestWorkspacePickerRevealDefersLateSelectionUntilLayout(t *testing.T) {
	t.Parallel()
	state := &appState{}
	state.requestWorkspacePickerReveal(20)
	if !state.workspacePickerRevealPending || state.workspacePickerRevealOffset != 15 {
		t.Fatalf("reveal state = pending:%t offset:%d, want pending at 15", state.workspacePickerRevealPending, state.workspacePickerRevealOffset)
	}
}

func TestSubagentWorkspaceActivityContract(t *testing.T) {
	t.Parallel()
	definition := workspacePaneDefinitions[workspacePaneSubagentConversation]
	descriptor := subagentWorkspacePane("conversation")
	for _, test := range []struct {
		state string
		want  workspacePaneActivity
	}{
		{state: "queued", want: workspacePaneActivityRunning},
		{state: "running", want: workspacePaneActivityRunning},
		{state: "interrupted", want: workspacePaneActivityWarning},
		{state: "aborted", want: workspacePaneActivityWarning},
		{state: "failed", want: workspacePaneActivityError},
		{state: "idle", want: workspacePaneActivityNone},
	} {
		snapshot := shellSnapshot{SubagentConversations: []protocol.SubagentConversation{{ID: "conversation", State: test.state}}}
		if got := definition.Activity(snapshot, descriptor); got != test.want {
			t.Fatalf("state %q activity = %v, want %v", test.state, got, test.want)
		}
	}
}

func TestWorkspacePaneRegistryCoversEveryKind(t *testing.T) {
	t.Parallel()
	if len(workspacePaneDefinitions) != len(workspacePaneKinds) {
		t.Fatalf("registry definitions = %d, kinds = %d", len(workspacePaneDefinitions), len(workspacePaneKinds))
	}
	for _, kind := range workspacePaneKinds {
		definition, ok := workspacePaneDefinitions[kind]
		if !ok {
			t.Fatalf("missing definition for %q", kind)
		}
		if definition.Kind != kind || definition.Identity == nil || definition.Label == nil || definition.Available == nil || definition.Activity == nil || definition.Build == nil {
			t.Fatalf("incomplete definition for %q: %#v", kind, definition)
		}
	}
	for kind := range workspacePaneDefinitions {
		found := false
		for _, known := range workspacePaneKinds {
			found = found || kind == known
		}
		if !found {
			t.Fatalf("registry has unknown kind %q", kind)
		}
	}
}

func TestWorkspaceDescriptorIdentityIgnoresPresentationChanges(t *testing.T) {
	t.Parallel()
	descriptor := subagentWorkspacePane("conversation-a")
	identity, err := workspacePaneIdentityFor(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(identity), "subagent:conversation-a"; got != want {
		t.Fatalf("identity = %q, want %q", got, want)
	}
}
