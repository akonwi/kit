package tui

import (
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
	"testing"
)

func TestModalDefersDockFocusThenRestoresComposer(t *testing.T) {
	t.Parallel()
	s := &ownershipFocusHarnessState{}
	application := uitest.New(ownershipFocusHarness{s})
	application.Pump(100, 30)
	application.Send(ui.Key{Text: "draft", Keycode: 'd'})
	s.SetState(func() { s.modal = true })
	application.Pump(100, 30)
	s.SetState(func() { s.pending = true })
	application.Pump(100, 30)
	application.Send(ui.Key{Text: "query", Keycode: 'q'})
	application.Pump(100, 30)
	if s.query != "query" || s.composer != "draft" {
		t.Fatalf("modal text ownership: query=%q draft=%q", s.query, s.composer)
	}
	application.Send(ui.Key{Keycode: vaxis.KeyEsc})
	application.Pump(100, 30)
	application.Send(ui.Key{Text: "answer", Keycode: 'a'})
	application.Send(ui.Key{Keycode: vaxis.KeyEnter})
	application.Pump(100, 30)
	if s.answer != "answer" {
		t.Fatalf("restored dock answer=%q, want answer", s.answer)
	}
	application.Send(ui.Key{Text: "!", Keycode: '!'})
	application.Pump(100, 30)
	if s.composer != "draft!" {
		t.Fatalf("restored draft=%q, want draft!", s.composer)
	}
}

func TestShellFocusReturnValidatesSessionAndPaneIncarnation(t *testing.T) {
	t.Parallel()
	pane := fileWorkspacePane("workspace", "file.go")
	pane.OpenGeneration = 7
	var workspace workspaceController
	workspace.Open(pane)
	view := shellView{Snapshot: shellSnapshot{Session: protocol.SessionInfo{ID: "session"}, CurrentWorkspaceID: "workspace", Workspace: workspace.Snapshot()}}
	target := captureShellFocus(view)
	if !target.validContent(view) {
		t.Fatal("live selected pane must be a valid return target")
	}
	for _, change := range []func(*shellView){
		func(w *shellView) { w.Snapshot.Session.ID = "other" },
		func(w *shellView) { w.Snapshot.CurrentWorkspaceID = "replacement" },
		func(w *shellView) { w.Snapshot.Workspace.Selected = workspaceAgentIdentity },
		func(w *shellView) {
			w.Snapshot.Workspace.Panes = append([]workspacePaneDescriptor(nil), w.Snapshot.Workspace.Panes...)
			w.Snapshot.Workspace.Panes[0].OpenGeneration++
		},
	} {
		next := view
		change(&next)
		if target.validContent(next) {
			t.Fatal("stale return target must fall back to composer")
		}
	}
}

type ownershipFocusHarness struct{ s *ownershipFocusHarnessState }

func (w ownershipFocusHarness) CreateState() ui.State { return w.s }

type ownershipFocusHarnessState struct {
	ui.StateBase
	modal, pending          bool
	query, composer, answer string
	scroll                  ui.ScrollController
}

func (s *ownershipFocusHarnessState) Build(ui.BuildContext) ui.Widget {
	snapshot := shellSnapshot{Phase: phaseReady, Session: protocol.SessionInfo{ID: "session", Name: "Focus test"}, Scroll: &s.scroll, PaletteOpen: s.modal, PaletteQuery: s.query, Composer: s.composer}
	if s.pending {
		snapshot.PendingInteractions = []protocol.InteractionRequest{{ID: "request", Kind: protocol.InteractionInput, Title: "Answer"}}
	}
	return shellView{Snapshot: snapshot, Callbacks: shellCallbacks{
		ComposerChanged:     func(_ ui.EventContext, value string) { s.SetState(func() { s.composer = value }) },
		PaletteQueryChanged: func(_ ui.EventContext, value string) { s.SetState(func() { s.query = value }) },
		Dismiss:             func(ui.EventContext) { s.SetState(func() { s.modal = false }) },
		RespondInteraction: func(_ ui.EventContext, response protocol.InteractionResponse, done func(error)) {
			s.SetState(func() {
				if response.Value != nil {
					s.answer = *response.Value
				}
				s.pending = false
			})
			done(nil)
		},
	}}
}
