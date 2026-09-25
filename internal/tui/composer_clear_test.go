package tui

import (
	"reflect"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

type composerClearHarness struct{ state *composerClearState }

func (w composerClearHarness) CreateState() ui.State { return w.state }

type composerClearState struct {
	appState
	scroll ui.ScrollController
}

func newComposerClearState(composer string) *composerClearState {
	return &composerClearState{appState: appState{
		phase: phaseReady,
		session: protocol.SessionInfo{
			ID: "session_composer_clear", Name: "Composer clear", Model: "test/model",
		},
		composer: composer,
	}}
}

func (*composerClearState) InitState() {}
func (*composerClearState) Dispose()   {}

func (s *composerClearState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}

func (s *composerClearState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{
		Snapshot: shellSnapshot{
			Phase: s.phase, Session: s.session, Composer: s.composer,
			ComposerAttachments:         append([]stagedAttachment(nil), s.composerAttachments...),
			ComposerAnnotations:         s.composerAnnotations(),
			ComposerCursorEndGeneration: s.composerCursorEndGeneration,
			ComposerCursorOffset:        s.composerCursorOffset,
			ComposerCursorGeneration:    s.composerCursorGeneration,
			CurrentWorkspaceID:          s.workspaceID,
			Workspace:                   s.workspace.Snapshot(),
			Scroll:                      &s.scroll,
			PendingInteractions:         append([]protocol.InteractionRequest(nil), s.pendingInteractions...),
			PaletteOpen:                 s.palette.Open,
			PaletteQuery:                s.palette.Query,
			PaletteSelection:            s.palette.Selection,
			PaletteCommands:             s.palette.Contributions,
			FileMention:                 s.fileMention,
			SessionMention:              s.sessionMention,
			SessionMentions:             s.sessionMentions,
		},
		Callbacks: shellCallbacks{
			InputOwner: s.inputOwner,
			Quit:       func(ctx ui.EventContext) { ctx.Quit() },
			Dismiss:    s.dismiss,
			FocusWorkspaceComposer: func(ui.EventContext) {
				s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusComposer) })
			},
			FocusWorkspaceContent: func(ui.EventContext) {
				s.SetState(func() { s.workspace.SetFocusOwner(workspaceFocusContent) })
			},
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
			ComposerPasted: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer = value })
			},
		},
	}
}

func TestCtrlCClearsNonemptyComposerOwnedText(t *testing.T) {
	for _, draft := range []string{"draft", "   ", "first line\nsecond line"} {
		t.Run(draft, func(t *testing.T) {
			application, state := mountComposerClearHarness(t, newComposerClearState(draft))
			focusComposerClearInput(t, application)
			state.composerCursorOffset = len(draft)
			initialCursorGeneration := state.composerCursorGeneration
			initialDraftGeneration := state.composerDraftGeneration

			application.Send(composerClearCtrlC(vaxis.EventPress))
			if state.composer != "" {
				t.Fatalf("composer after Ctrl+C = %q", state.composer)
			}
			if application.ShouldQuit() {
				t.Fatal("Ctrl+C detached while clearing a nonempty composer")
			}
			if state.composerDraftGeneration != initialDraftGeneration+1 {
				t.Fatalf("draft generation = %d, want %d", state.composerDraftGeneration, initialDraftGeneration+1)
			}
			if state.composerCursorOffset != 0 || state.composerCursorGeneration != initialCursorGeneration+1 {
				t.Fatalf("cursor reset = offset:%d generation:%d, want offset:0 generation:%d", state.composerCursorOffset, state.composerCursorGeneration, initialCursorGeneration+1)
			}
		})
	}
}

func TestCtrlCEmptyComposerDetachesOnNextPressAndIgnoresHeldKeyEvents(t *testing.T) {
	application, state := mountComposerClearHarness(t, newComposerClearState("draft"))
	focusComposerClearInput(t, application)

	application.Send(composerClearCtrlC(vaxis.EventPress))
	if state.composer != "" || application.ShouldQuit() {
		t.Fatalf("first press composer=%q quit=%t", state.composer, application.ShouldQuit())
	}
	application.Send(composerClearCtrlC(vaxis.EventRepeat))
	application.Send(composerClearCtrlC(vaxis.EventRelease))
	if application.ShouldQuit() {
		t.Fatal("held-key repeat or release detached after clearing")
	}

	application.Pump(80, 24)
	application.Send(composerClearCtrlC(vaxis.EventPress))
	if !application.ShouldQuit() {
		t.Fatal("fresh Ctrl+C press did not detach with an empty composer")
	}
}

func TestComposerAcceptsTypingAfterCtrlCClear(t *testing.T) {
	application, state := mountComposerClearHarness(t, newComposerClearState("draft"))
	focusComposerClearInput(t, application)
	application.Send(composerClearCtrlC(vaxis.EventPress))
	application.Pump(80, 24)

	application.Key("x")
	if state.composer != "x" {
		t.Fatalf("composer after continued typing = %q", state.composer)
	}
	if application.ShouldQuit() {
		t.Fatal("continued typing detached the client")
	}
}

func TestCtrlCClearRemovesLocalAttachmentsPreservesAnnotationsAndClosesMentions(t *testing.T) {
	state := newComposerClearState("draft")
	attachments := []stagedAttachment{{Info: protocol.AttachmentInfo{ID: "attachment_1", Filename: "notes.txt"}, Filename: "notes.txt"}}
	annotations := []protocol.AnnotationSummary{{ID: 7, BodyPreview: "Review note"}}
	state.composerAttachments = append([]stagedAttachment(nil), attachments...)
	state.composerAttachmentIDs = []string{"attachment_legacy"}
	state.annotations = append([]protocol.AnnotationSummary(nil), annotations...)
	state.fileMention.Open = true
	state.sessionMention.Open = true
	mentionCancelled := false
	state.sessionMentionCancel = func() { mentionCancelled = true }
	state.sessionMentions.Loading = true

	application, state := mountComposerClearHarness(t, state)
	focusComposerClearInput(t, application)
	application.Send(composerClearCtrlC(vaxis.EventPress))

	if state.composer != "" {
		t.Fatalf("composer after clear = %q", state.composer)
	}
	if len(state.composerAttachments) != 0 || len(state.composerAttachmentIDs) != 0 {
		t.Fatalf("attachments survived clear: rows=%+v ids=%v", state.composerAttachments, state.composerAttachmentIDs)
	}
	if !reflect.DeepEqual(state.annotations, annotations) {
		t.Fatalf("annotations changed: %+v", state.annotations)
	}
	if state.fileMention.Open || state.sessionMention.Open || state.sessionMentions.Loading || !mentionCancelled {
		t.Fatalf("mentions survived clear: file=%t session=%t loading=%t cancelled=%t", state.fileMention.Open, state.sessionMention.Open, state.sessionMentions.Loading, mentionCancelled)
	}
}

func TestCtrlCClearsAttachmentOnlyComposerDraft(t *testing.T) {
	state := newComposerClearState("")
	state.composerAttachments = []stagedAttachment{{Info: protocol.AttachmentInfo{ID: "attachment_1"}, Filename: "notes.txt"}}
	state.composerAttachmentIDs = []string{"attachment_legacy"}
	application, state := mountComposerClearHarness(t, state)
	focusComposerClearInput(t, application)

	application.Send(composerClearCtrlC(vaxis.EventPress))
	if len(state.composerAttachments) != 0 || len(state.composerAttachmentIDs) != 0 {
		t.Fatalf("attachment-only draft survived: rows=%+v ids=%v", state.composerAttachments, state.composerAttachmentIDs)
	}
	if application.ShouldQuit() {
		t.Fatal("Ctrl+C detached while clearing an attachment-only draft")
	}
}

func TestCtrlCClearsComposerForSelectedWorkspacePane(t *testing.T) {
	state := newComposerClearState("pane draft")
	state.workspaceID = "workspace_1"
	if _, _, err := state.workspace.Open(subagentWorkspacePane("conversation_1")); err != nil {
		t.Fatal(err)
	}
	state.workspace.SetFocusOwner(workspaceFocusComposer)
	application, state := mountComposerClearHarness(t, state)
	focusComposerClearInput(t, application)

	application.Send(composerClearCtrlC(vaxis.EventPress))
	if state.composer != "" || application.ShouldQuit() {
		t.Fatalf("selected-pane composer=%q quit=%t", state.composer, application.ShouldQuit())
	}
}

func TestCtrlCDetachesWithoutClearingSelectedPaneContentDraft(t *testing.T) {
	state := newComposerClearState("content-owned draft")
	state.workspaceID = "workspace_1"
	if _, _, err := state.workspace.Open(subagentWorkspacePane("conversation_1")); err != nil {
		t.Fatal(err)
	}
	state.workspace.SetFocusOwner(workspaceFocusContent)
	application, state := mountComposerClearHarness(t, state)

	column, row := findTextCell(t, paintedRows(application, 80, 24), "Loading transcript…")
	application.Click(column, row)
	application.Pump(80, 24)
	application.Send(composerClearCtrlC(vaxis.EventPress))
	if !application.ShouldQuit() {
		t.Fatal("Ctrl+C did not detach while selected pane content owned input")
	}
	if state.composer != "content-owned draft" {
		t.Fatalf("content-owned draft changed to %q", state.composer)
	}
}

func TestCtrlCDetachesWithoutClearingDraftOwnedByOtherInput(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*composerClearState)
	}{
		{
			name: "interaction dock",
			setup: func(state *composerClearState) {
				state.pendingInteractions = []protocol.InteractionRequest{{ID: "interaction_1", Kind: protocol.InteractionInput, Title: "Answer"}}
			},
		},
		{
			name: "palette",
			setup: func(state *composerClearState) {
				state.palette.Open = true
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newComposerClearState("preserved draft")
			test.setup(state)
			application, state := mountComposerClearHarness(t, state)
			application.Send(composerClearCtrlC(vaxis.EventPress))
			if !application.ShouldQuit() {
				t.Fatal("Ctrl+C did not detach")
			}
			if state.composer != "preserved draft" {
				t.Fatalf("draft changed to %q", state.composer)
			}
		})
	}
}

func TestPastedCtrlCIsComposerTextNotClearOrDetach(t *testing.T) {
	application, state := mountComposerClearHarness(t, newComposerClearState("draft"))
	focusComposerClearInput(t, application)
	application.Send(ui.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPaste})
	if state.composer != "cdraft" {
		t.Fatalf("composer after pasted Ctrl+C = %q", state.composer)
	}
	if application.ShouldQuit() {
		t.Fatal("pasted Ctrl+C detached the client")
	}
}

func mountComposerClearHarness(t *testing.T, state *composerClearState) (*uitest.App, *composerClearState) {
	t.Helper()
	application := uitest.New(composerClearHarness{state: state})
	application.Pump(80, 24)
	return application, state
}

func focusComposerClearInput(t *testing.T, application *uitest.App) {
	t.Helper()
	// The composer ends immediately above its lower separator and footer. Click
	// the stable final editor row so this also works for whitespace and multiline
	// drafts, where no visible value or placeholder is available to locate.
	application.Click(1, 21)
	application.Pump(80, 24)
}

func composerClearCtrlC(eventType vaxis.EventType) ui.Key {
	return ui.Key{Text: "c", Keycode: 'c', Modifiers: vaxis.ModCtrl, EventType: eventType}
}
