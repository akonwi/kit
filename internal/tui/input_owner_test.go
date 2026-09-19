package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestInputOwnerResolverMatrix(t *testing.T) {
	t.Parallel()

	interaction := []protocol.InteractionRequest{{ID: "interaction_1"}}
	cases := []struct {
		name      string
		snapshot  shellSnapshot
		wantOwner inputOwner
		wantAdmit bool
	}{
		{name: "base", snapshot: shellSnapshot{Phase: phaseReady}, wantOwner: inputBase, wantAdmit: true},
		{name: "interaction blocks roots", snapshot: shellSnapshot{Phase: phaseReady, PendingInteractions: interaction}, wantOwner: inputInteraction},
		{name: "palette wins competing roots", snapshot: shellSnapshot{Phase: phaseReady, PaletteOpen: true, WorkspacePickerOpen: true, PendingInteractions: interaction}, wantOwner: inputPalette},
		{name: "file picker blocks roots", snapshot: shellSnapshot{Phase: phaseReady, WorkspaceFilePicker: workspaceFilePickerController{Open: true}}, wantOwner: inputFiles},
		{name: "auth blocks roots", snapshot: shellSnapshot{Phase: phaseAuthSelect}, wantOwner: inputAuth},
		{name: "explorer rename is innermost", snapshot: shellSnapshot{Phase: phaseReady, SessionExplorer: sessionExplorerSnapshot{Open: true, RenameOpen: true}}, wantOwner: inputSessionRename},
		{name: "explorer delete is innermost", snapshot: shellSnapshot{Phase: phaseReady, SessionExplorer: sessionExplorerSnapshot{Open: true, DeleteOpen: true}}, wantOwner: inputSessionDelete},
		{name: "subagent confirmation is innermost", snapshot: shellSnapshot{Phase: phaseReady, SubagentsOpen: true, SubagentDismissID: "subagent_1"}, wantOwner: inputSubagentDismiss},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snapshot.inputOwner(); got != tc.wantOwner {
				t.Fatalf("owner = %v, want %v", got, tc.wantOwner)
			}
			if got := tc.snapshot.Phase == phaseReady && tc.snapshot.inputOwner() == inputBase; got != tc.wantAdmit {
				t.Fatalf("root admission = %v, want %v", got, tc.wantAdmit)
			}
		})
	}
}

func TestRootShortcutsBlockedByOwner(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		snapshot shellSnapshot
	}{
		{name: "interaction", snapshot: shellSnapshot{Phase: phaseReady, PendingInteractions: []protocol.InteractionRequest{{ID: "i"}}}},
		{name: "palette", snapshot: shellSnapshot{Phase: phaseReady, PaletteOpen: true}},
		{name: "modal", snapshot: shellSnapshot{Phase: phaseReady, WorkspaceFilePicker: workspaceFilePickerController{Open: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			palette, picker := 0, 0
			app := uitest.New(shellView{
				Snapshot: tc.snapshot,
				Callbacks: shellCallbacks{
					OpenPalette:             func(ui.EventContext) { palette++ },
					OpenWorkspaceFilePicker: func(ui.EventContext) { picker++ },
				},
			})
			app.Pump(80, 24)
			app.Send(vaxis.Key{Text: "p", Keycode: 'p', Modifiers: vaxis.ModCtrl})
			app.Send(vaxis.Key{Text: "o", Keycode: 'o', Modifiers: vaxis.ModCtrl})
			if palette != 0 || picker != 0 {
				t.Fatalf("blocked shortcuts opened palette=%d picker=%d", palette, picker)
			}
		})
	}
}

func TestDismissUsesInnermostOwnerAndConsumesPending(t *testing.T) {
	t.Parallel()

	t.Run("rename owns dismissal before explorer", func(t *testing.T) {
		state := &appState{phase: phaseReady, sessionExplorer: sessionExplorerController{
			Open: true, RenameOpen: true, RenameSessionID: "session_1",
		}}
		if got := state.inputOwner(); got != inputSessionRename {
			t.Fatalf("owner = %v, want session rename", got)
		}
	})

	t.Run("pending rename consumes escape", func(t *testing.T) {
		state := &appState{phase: phaseReady, sessionExplorer: sessionExplorerController{
			Open: true, RenameOpen: true, RenamePending: true,
		}}
		state.dismiss(ui.EventContext{})
		if !state.sessionExplorer.RenameOpen || !state.sessionExplorer.RenamePending {
			t.Fatalf("pending rename was dismissed: %+v", state.sessionExplorer.Snapshot())
		}
	})
}

func TestCtrlCQuitsFromStandaloneChildren(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		snapshot shellSnapshot
	}{
		{name: "palette", snapshot: shellSnapshot{Phase: phaseReady, PaletteOpen: true}},
		{name: "pending interaction", snapshot: shellSnapshot{Phase: phaseReady, PendingInteractions: []protocol.InteractionRequest{{ID: "i"}}}},
		{name: "explorer child", snapshot: shellSnapshot{Phase: phaseReady, SessionExplorer: sessionExplorerSnapshot{Open: true, DeleteOpen: true}}},
		{name: "auth", snapshot: shellSnapshot{Phase: phaseAuthSelect}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := uitest.New(shellView{
				Snapshot:  tc.snapshot,
				Callbacks: shellCallbacks{Quit: func(ctx ui.EventContext) { ctx.Quit() }},
			})
			app.Pump(80, 24)
			app.Send(vaxis.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModCtrl})
			if !app.ShouldQuit() {
				t.Fatal("Ctrl+C did not quit")
			}
		})
	}
}
