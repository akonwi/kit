package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestInteractionDockClickThenImmediateInputDoesNotLeakToComposer(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	state.composer = "draft"
	state.pendingInteractions = []protocol.InteractionRequest{{
		ID: "interaction_1", Kind: protocol.InteractionConfirm, Title: "Continue deployment?",
	}}
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(100, 30)

	column, row := findTextCell(t, paintedRows(application, 100, 30), "Continue deployment?")
	application.Click(column, row)
	application.Key("x")
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "pasted", Keycode: 'p', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})

	if state.composer != "draft" {
		t.Fatalf("dock input leaked to composer: %q", state.composer)
	}
	if state.interactionResponses != 0 {
		t.Fatalf("dock click/input submitted interaction: %d responses", state.interactionResponses)
	}
}

func TestInteractionInputBackgroundSelectionDiscardsImmediateInputUntilNextFrame(t *testing.T) {
	t.Parallel()

	state := &backgroundAgentInteractionState{appState: appState{phase: phaseReady, session: protocol.SessionInfo{ID: "session_test", Name: "Ownership test", Model: "test/model"}}}
	state.composer = "draft"
	state.messages = []transcriptMessage{{Role: "assistant", Text: "background agent content"}}
	state.pendingInteractions = []protocol.InteractionRequest{{
		ID: "interaction_input", Kind: protocol.InteractionInput, Title: "Provide deployment name",
	}}
	application := uitest.New(backgroundAgentInteractionHarness{state: state})
	application.Pump(100, 30)

	column, row := findTextCell(t, paintedRows(application, 100, 30), "background agent content")
	// SelectionArea captures this pointer target synchronously. The framework
	// keeps that target fixed for the event, so root input ownership discards
	// the following coalesced input rather than redirecting it.
	application.Click(column, row)
	application.Key("x")
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "visible", Keycode: 'v', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})
	application.Enter()

	if state.composer != "draft" {
		t.Fatalf("background click/input leaked to composer: %q", state.composer)
	}
	if state.interactionResponses != 0 {
		t.Fatalf("stale immediate input responded before next frame: %d", state.interactionResponses)
	}

	// Owner changes discard the coalesced pre-frame input. A fresh frame may
	// then deliver interaction input normally.
	application.Pump(100, 30)
	application.Key("answer")
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: " fresh", Keycode: 'f', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})
	application.Enter()
	if state.interactionResponses != 1 {
		t.Fatalf("interaction response count after fresh frame = %d, want 1", state.interactionResponses)
	}
	if state.composer != "draft" {
		t.Fatalf("fresh interaction input leaked to composer: %q", state.composer)
	}
}

func TestInteractionArrivalBlocksStalePreFramePointerTarget(t *testing.T) {
	t.Parallel()

	state := newInputOwnerTransitionState()
	state.composer = "draft"
	application := uitest.New(inputOwnerTransitionHarness{state: state})
	application.Pump(100, 30)
	column, row := findTextCell(t, paintedRows(application, 100, 30), "draft")

	state.SetState(func() {
		state.pendingInteractions = []protocol.InteractionRequest{{
			ID: "interaction_arrived", Kind: protocol.InteractionConfirm, Title: "Answer now?",
		}}
	})
	application.Click(column, row)
	application.Key("x")
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "stale", Keycode: 's', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})

	if state.composer != "draft" {
		t.Fatalf("stale pre-frame pointer mutated composer: %q", state.composer)
	}
	if state.interactionResponses != 0 {
		t.Fatalf("stale pointer submitted interaction: %d responses", state.interactionResponses)
	}
}

func TestHiddenPaneDoesNotReceiveComposerInput(t *testing.T) {
	t.Parallel()

	state := &retainedEditorTransitionState{hidden: "hidden-before", composer: "draft"}
	application := uitest.New(retainedEditorTransitionHarness{state: state})
	application.Pump(100, 10)
	hiddenBefore := state.hidden
	application.Key("x")
	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "visible", Keycode: 'v', EventType: vaxis.EventPaste})
	application.Send(vaxis.PasteEndEvent{})
	application.Pump(100, 10)

	if state.hidden != hiddenBefore {
		t.Fatalf("inactive retained editor changed: before=%q after=%q", hiddenBefore, state.hidden)
	}
	if state.composer != "xvisibledraft" {
		t.Fatalf("visible composer = %q, want %q", state.composer, "xvisibledraft")
	}
}

type retainedEditorTransitionHarness struct {
	state *retainedEditorTransitionState
}

func (h retainedEditorTransitionHarness) CreateState() ui.State { return h.state }

type retainedEditorTransitionState struct {
	ui.StateBase
	hidden, composer string
}

func (s *retainedEditorTransitionState) Build(ui.BuildContext) ui.Widget {
	return ui.Column(
		ui.FocusScope{AutoFocus: true, Child: ui.TextField{
			Value: s.composer, OnChanged: func(_ ui.EventContext, value string) { s.composer = value },
		}},
		retainedWorkspacePaneStack{Panes: []ui.Widget{
			retainedWorkspacePane{Identity: workspacePaneIdentity("hidden"), Child: ui.TextField{
				Value: s.hidden, OnChanged: func(_ ui.EventContext, value string) { s.hidden = value },
			}},
		}},
	)
}

type backgroundAgentInteractionHarness struct {
	state *backgroundAgentInteractionState
}

func (h backgroundAgentInteractionHarness) CreateState() ui.State { return h.state }

type backgroundAgentInteractionState struct {
	appState
	scroll               ui.ScrollController
	interactionResponses int
}

func (s *backgroundAgentInteractionState) InitState() {}
func (s *backgroundAgentInteractionState) Dispose()   {}

func (s *backgroundAgentInteractionState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}

func (s *backgroundAgentInteractionState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{
		Snapshot: shellSnapshot{
			Phase: s.phase, Composer: s.composer, Messages: append([]transcriptMessage(nil), s.messages...),
			Scroll: &s.scroll, Session: s.session, PendingInteractions: append([]protocol.InteractionRequest(nil), s.pendingInteractions...),
		},
		Callbacks: shellCallbacks{
			InputOwner:      s.inputOwner,
			Quit:            func(ctx ui.EventContext) { ctx.Quit() },
			Dismiss:         s.dismiss,
			ComposerChanged: func(_ ui.EventContext, value string) { s.SetState(func() { s.composer = value }) },
			ComposerPasted:  func(_ ui.EventContext, value string) { s.SetState(func() { s.composer = value }) },
			RespondInteraction: func(_ ui.EventContext, _ protocol.InteractionResponse, complete func(error)) {
				s.interactionResponses++
				complete(nil)
			},
		},
	}
}
