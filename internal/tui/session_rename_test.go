package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestCurrentSessionRenameControllerHandlesValidationFailureAndSuccess(t *testing.T) {
	t.Parallel()

	controller := currentSessionRenameController{}
	session := protocol.SessionInfo{ID: "session_current", Name: "Before", CWD: "/repo"}
	if !controller.Begin(session) || !controller.Open || controller.Text != "Before" || controller.Target != "Before" {
		t.Fatalf("begin rename = %+v", controller)
	}
	if controller.Begin(session) {
		t.Fatal("opened a second rename operation")
	}
	controller.SetText("  After  ")
	generation, sessionID, name, started := controller.BeginSave()
	if !started || sessionID != session.ID || name != "After" || !controller.Pending {
		t.Fatalf("begin save generation=%d id=%q name=%q controller=%+v", generation, sessionID, name, controller)
	}
	if controller.Resolve(generation-1, protocol.SessionInfo{}, errors.New("stale")) {
		t.Fatal("stale rename result was accepted")
	}
	cursorGeneration := controller.CursorEnd
	if !controller.Resolve(generation, protocol.SessionInfo{}, errors.New("offline")) || !controller.Open || controller.Pending || controller.Error != "offline" || controller.CursorEnd == cursorGeneration {
		t.Fatalf("rename failure = %+v", controller)
	}
	if !controller.HandleEditorKey(ui.Key{Keycode: vaxis.KeyLeft}) || !controller.HandleEditorKey(ui.Key{Text: "?", Keycode: '?'}) || controller.Text != "  After ? " {
		t.Fatalf("post-failure pre-frame input = %+v", controller)
	}
	controller.SetText("After")
	generation, _, _, started = controller.BeginSave()
	renamed := protocol.SessionInfo{ID: session.ID, Name: "After", CWD: session.CWD}
	if !started || !controller.Resolve(generation, renamed, nil) || controller.Open || controller.Pending {
		t.Fatalf("rename success = %+v", controller)
	}
}

func TestCurrentSessionRenameControllerResetInvalidatesStaleResult(t *testing.T) {
	t.Parallel()

	controller := currentSessionRenameController{}
	controller.Begin(protocol.SessionInfo{ID: "session_old", Name: "Old"})
	oldGeneration, _, _, _ := controller.BeginSave()
	controller.Reset()
	controller.Begin(protocol.SessionInfo{ID: "session_new", Name: "New"})
	newGeneration, _, _, _ := controller.BeginSave()
	if oldGeneration == newGeneration {
		t.Fatalf("reset reused generation %d", oldGeneration)
	}
	if controller.Resolve(oldGeneration, protocol.SessionInfo{ID: "session_old", Name: "Old"}, nil) {
		t.Fatal("stale rename result was accepted after session reset")
	}
}

func TestCurrentSessionRenameControllerCarriesPreFrameCursorIntoField(t *testing.T) {
	t.Parallel()

	controller := currentSessionRenameController{}
	controller.Begin(protocol.SessionInfo{ID: "session_current", Name: "Before"})
	if !controller.HandleEditorKey(ui.Key{Keycode: vaxis.KeyLeft}) || controller.Snapshot().CursorOffset != 5 {
		t.Fatalf("pre-frame cursor = %+v snapshot=%+v", controller, controller.Snapshot())
	}
	controller.TickFrame()
	if controller.Snapshot().CursorOffset != 5 || controller.HandleEditorKey(ui.Key{Text: "?", Keycode: '?'}) {
		t.Fatalf("mounted field cursor handoff = %+v snapshot=%+v", controller, controller.Snapshot())
	}
}

func TestCurrentSessionRenameControllerAppliesRapidPreFrameSelectionEdit(t *testing.T) {
	t.Parallel()

	controller := currentSessionRenameController{}
	controller.Begin(protocol.SessionInfo{ID: "session_current", Name: "Before"})
	if !controller.HandleEditorKey(ui.Key{Text: "a", Keycode: 'a', Modifiers: vaxis.ModCtrl}) ||
		!controller.HandleEditorKey(ui.Key{Text: "X", Keycode: 'X'}) || controller.Text != "X" {
		t.Fatalf("rapid pre-frame selection replacement = %+v", controller)
	}
	controller.TickFrame()
	if controller.HandleEditorKey(ui.Key{Text: "?", Keycode: '?'}) {
		t.Fatal("capture remained active after the field mounted")
	}
}

func TestCurrentSessionRenameControllerCapturesEditorInputUntilFirstFrame(t *testing.T) {
	t.Parallel()

	controller := currentSessionRenameController{}
	controller.Begin(protocol.SessionInfo{ID: "session_current", Name: "Before"})
	if !controller.HandleEditorKey(ui.Key{Keycode: vaxis.KeyLeft}) || !controller.HandleEditorKey(ui.Key{Text: "!", Keycode: '!'}) || controller.Text != "Befor!e" {
		t.Fatalf("pre-frame cursor input = %+v", controller)
	}
	if !controller.HandleEditorKey(ui.Key{Keycode: vaxis.KeyBackspace}) || controller.Text != "Before" {
		t.Fatalf("pre-frame backspace = %+v", controller)
	}
	controller.HandleEditorKey(ui.Key{Keycode: vaxis.KeyEnd})
	if !controller.HandleEditorKey(ui.Key{Text: " renamed\n", EventType: vaxis.EventPaste}) || controller.Text != "Before renamed " {
		t.Fatalf("pre-frame paste = %+v", controller)
	}
	controller.TickFrame()
	if controller.HandleEditorKey(ui.Key{Text: "?", Keycode: '?'}) || controller.Text != "Before renamed " {
		t.Fatalf("post-frame input bypassed mounted field: %+v", controller)
	}
}

func TestCurrentSessionRenamePresentationUsesStandaloneDialog(t *testing.T) {
	t.Parallel()

	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Scroll: &ui.ScrollController{},
		Session:       protocol.SessionInfo{ID: "session_current", Name: "Before", Model: "test/echo"},
		SessionRename: sessionRenameSnapshot{Open: true, Target: "Before", Text: "After"},
	}})
	application.Pump(80, 24)
	application.Pump(80, 24)
	text := application.Text()
	for _, expected := range []string{"Rename session", "Before", "After", "enter save · esc cancel"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("rename dialog missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "Session Explorer") {
		t.Fatalf("attached rename opened the session explorer:\n%s", text)
	}
}

func TestCurrentSessionRenameControllerCancelsEmptyName(t *testing.T) {
	t.Parallel()

	controller := currentSessionRenameController{}
	controller.Begin(protocol.SessionInfo{ID: "session_current", Name: "Before"})
	controller.SetText("   ")
	if _, _, _, started := controller.BeginSave(); started || controller.Open {
		t.Fatalf("empty rename was not cancelled: %+v", controller)
	}
}
