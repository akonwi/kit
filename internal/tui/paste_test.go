package tui

import (
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"

	"github.com/akonwi/kit/internal/protocol"
)

func TestPasteCoalescerBuffersOnePasteBetweenMarkers(t *testing.T) {
	t.Parallel()

	var coalescer pasteCoalescer
	coalescer.Begin()
	for _, key := range []ui.Key{
		{Text: "a", Keycode: 'a', EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste},
		{Text: "b", Keycode: 'b', EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyTab, EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyEsc, EventType: vaxis.EventPaste},
	} {
		if !coalescer.Append(key) {
			t.Fatalf("key %#v was not buffered", key)
		}
	}
	key, ok := coalescer.Flush()
	if !ok {
		t.Fatal("flush produced no key")
	}
	if key.Text != "a\nb\t" || key.EventType != vaxis.EventPaste {
		t.Fatalf("flushed key = %#v, want text \"a\\nb\\t\" as a paste", key)
	}
	if coalescer.Active() {
		t.Fatal("coalescer stayed active after flush")
	}
	if _, ok := coalescer.Flush(); ok {
		t.Fatal("second flush produced a key")
	}
}

func TestPasteCoalescerIgnoresKeysOutsideAPaste(t *testing.T) {
	t.Parallel()

	var coalescer pasteCoalescer
	if coalescer.Append(ui.Key{Text: "a", Keycode: 'a', EventType: vaxis.EventPaste}) {
		t.Fatal("paste key was buffered without a start marker")
	}
	coalescer.Begin()
	if coalescer.Append(ui.Key{Text: "a", Keycode: 'a'}) {
		t.Fatal("ordinary key was buffered during a paste")
	}
	if _, ok := coalescer.Flush(); ok {
		t.Fatal("empty paste produced a key")
	}
}

func TestBracketedPasteAppliesAsASingleComposerChange(t *testing.T) {
	t.Parallel()

	state := &pasteHarnessState{}
	application := uitest.New(pasteHarness{State: state})
	application.Pump(80, 24)

	application.Send(vaxis.PasteStartEvent{})
	for _, key := range []ui.Key{
		{Text: "l", Keycode: 'l', EventType: vaxis.EventPaste},
		{Text: "i", Keycode: 'i', EventType: vaxis.EventPaste},
		{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPaste},
		{Text: "x", Keycode: 'x', EventType: vaxis.EventPaste},
	} {
		application.Send(key)
	}
	application.Send(vaxis.PasteEndEvent{})
	application.Pump(80, 24)

	if state.composer != "li\nx" {
		t.Fatalf("composer = %q, want %q", state.composer, "li\nx")
	}
	if state.pastes != 1 || state.changes != 0 {
		t.Fatalf("callbacks: pastes=%d changes=%d, want exactly one paste", state.pastes, state.changes)
	}
}

func TestUnterminatedPasteFlushesBeforeTheNextKey(t *testing.T) {
	t.Parallel()

	state := &pasteHarnessState{}
	application := uitest.New(pasteHarness{State: state})
	application.Pump(80, 24)

	application.Send(vaxis.PasteStartEvent{})
	application.Send(ui.Key{Text: "h", Keycode: 'h', EventType: vaxis.EventPaste})
	application.Send(ui.Key{Text: "i", Keycode: 'i', EventType: vaxis.EventPaste})
	application.Send(ui.Key{Text: "!", Keycode: '!'})
	application.Pump(80, 24)

	if state.composer != "hi!" {
		t.Fatalf("composer = %q, want %q", state.composer, "hi!")
	}
	if state.pastes != 1 || state.changes != 1 {
		t.Fatalf("callbacks: pastes=%d changes=%d, want one paste then one typed change", state.pastes, state.changes)
	}
}

type pasteHarness struct{ State *pasteHarnessState }

func (w pasteHarness) CreateState() ui.State { return w.State }

type pasteHarnessState struct {
	ui.StateBase
	paste    pasteCoalescer
	composer string
	pastes   int
	changes  int
	scroll   ui.ScrollController
}

func (s *pasteHarnessState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	if ctx.Phase() != ui.CapturePhase {
		return ui.EventIgnored
	}
	result, _ := s.paste.Observe(ctx, event, nil)
	return result
}

func (s *pasteHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Composer: s.composer, Scroll: &s.scroll,
			Session: protocol.SessionInfo{Name: "Paste test", Model: "openai/gpt-5.3-codex"},
		},
		Callbacks: shellCallbacks{
			ComposerPasted: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer, s.pastes = value, s.pastes+1 })
			},
			ComposerChanged: func(_ ui.EventContext, value string) {
				s.SetState(func() { s.composer, s.changes = value, s.changes+1 })
			},
		},
	}
}
