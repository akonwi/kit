package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestSessionMentionTriggerAndLiteralMarkdown(t *testing.T) {
	for _, tc := range []struct {
		before, after string
		pasted, want  bool
	}{
		{"", "#", false, true}, {"See ", "See #", false, true}, {"c", "c#", false, false},
		{"", "# pasted", true, false}, {"", "#heading", false, false},
	} {
		var controller sessionMentionController
		if got := controller.Observe(tc.before, tc.after, tc.pasted); got != tc.want {
			t.Fatalf("Observe(%q,%q) = %t", tc.before, tc.after, got)
		}
	}
	var controller sessionMentionController
	controller.Observe("", "#", false)
	controller.Observe("#", "##", false)
	if controller.Open {
		t.Fatal("Markdown heading kept picker open")
	}
	controller.Observe("", "#", false)
	controller.Observe("#", "# ", false)
	if controller.Open {
		t.Fatal("Markdown heading space kept picker open")
	}
}

func TestSessionMentionSearchAndStableInsertion(t *testing.T) {
	entries := []protocol.SessionInfo{
		{ID: "session_alpha", Name: "Design review", CWD: "/repo/design"},
		{ID: "session_beta", Name: "Tests", CWD: "/repo/verification"},
	}
	for _, query := range []string{"Tests", "beta", "verification"} {
		controller := sessionMentionController{Query: query}
		controller.ensureSelection(entries)
		if controller.Selection != "session_beta" {
			t.Fatalf("query %q selected %q", query, controller.Selection)
		}
	}
	var controller sessionMentionController
	controller.Observe("See  next", "See # next", false)
	controller.Observe("See # next", "See #beta next", false)
	controller.ensureSelection(entries)
	entry, selected, handled := controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyEnter})
	if !selected || !handled {
		t.Fatal("Enter did not select")
	}
	text, cursor, ok := controller.Insert("See #beta next", entry)
	want := "See #[session:session_beta]  next"
	if !ok || text != want || cursor != len("See #[session:session_beta] ") || controller.Open {
		t.Fatalf("insertion = %q, %d, %t", text, cursor, ok)
	}
}

func TestSessionMentionCatalogAndStaleResponses(t *testing.T) {
	entries := mentionableSessions([]protocol.SessionInfo{{ID: "session_self"}, {ID: "session_other"}}, "session_self")
	if len(entries) != 1 || entries[0].ID != "session_other" {
		t.Fatalf("entries = %+v", entries)
	}
	state := appState{session: protocol.SessionInfo{ID: "session_self"}, sessionMention: sessionMentionController{Open: true}, sessionMentions: sessionMentionSource{generation: 3}}
	if !state.acceptsSessionMentions("session_self", 3) {
		t.Fatal("current response rejected")
	}
	for _, tc := range []struct {
		id         string
		generation uint64
	}{{"session_old", 3}, {"session_self", 2}} {
		if state.acceptsSessionMentions(tc.id, tc.generation) {
			t.Fatal("stale response accepted")
		}
	}
	state.sessionMention.Close()
	if state.acceptsSessionMentions("session_self", 3) {
		t.Fatal("response reopened dismissed picker")
	}
}

func TestSessionMentionSurfaceMouseSelection(t *testing.T) {
	controller := sessionMentionController{Open: true, Selection: "session_alpha", Anchor: 4, QueryEnd: 5}
	var selected string

	app := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Composer: "See #", SessionMention: controller,
		SessionMentions: sessionMentionSource{Entries: []protocol.SessionInfo{{ID: "session_alpha", Name: "Design review", CWD: "/repo/design"}, {ID: "session_beta", Name: "Tests", CWD: "/repo/tests"}}},
	}, Callbacks: shellCallbacks{SelectSessionMention: func(_ ui.EventContext, id string) { selected = id }}})
	app.Pump(100, 24)
	rows := paintedRows(app, 100, 24)
	for _, want := range []string{"Design review · /repo/design", "Tests · /repo/tests", "↑↓ move · enter insert · esc close"} {
		if !strings.Contains(strings.Join(rows, "\n"), want) {
			t.Fatalf("missing %q:\n%s", want, strings.Join(rows, "\n"))
		}
	}
	row := findPaintedRow(rows, "Tests · /repo/tests")
	_, col := markdownCellPosition([]string{rows[row]}, "Tests")
	app.Click(col, row)
	if selected != "session_beta" {
		t.Fatalf("mouse selected %q", selected)
	}
}

func TestSessionMentionDismissalCancelsLoadingAndModalOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	state := appState{phase: phaseReady, sessionMention: sessionMentionController{Open: true}, sessionMentionCancel: cancel, sessionMentions: sessionMentionSource{Loading: true, generation: 1}}
	state.annotationPicker.Open = true
	state.reconcileSessionMention()
	if state.sessionMention.Open || state.sessionMentions.Loading || state.sessionMentionCancel != nil || ctx.Err() != context.Canceled {
		t.Fatal("modal did not dismiss and cancel mention")
	}
	if state.acceptsSessionMentions("", 1) {
		t.Fatal("canceled result accepted")
	}
}

type sessionMentionTestRuntime struct{ callbacks chan func() }

func (r sessionMentionTestRuntime) Dispatch(fn func()) { r.callbacks <- fn }

type sessionMentionAppHarness struct{ state *sessionMentionAppState }

func (w sessionMentionAppHarness) CreateState() ui.State { return w.state }

type sessionMentionAppState struct{ appState }

func (*sessionMentionAppState) InitState() {}
func (*sessionMentionAppState) Dispose()   {}
func (s *sessionMentionAppState) Build(ui.BuildContext) ui.Widget {
	s.reconcileSessionMention()
	return shellView{Snapshot: shellSnapshot{Phase: phaseReady, Composer: s.composer, SessionMention: s.sessionMention, SessionMentions: s.sessionMentions}, Callbacks: shellCallbacks{SelectSessionMention: s.selectSessionMention}}
}

func TestSessionMentionAsyncLoadAndAppKeyboardInsertion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	current := protocol.SessionInfo{ID: "session_self", Name: "Current", CWD: "/repo"}
	target := protocol.SessionInfo{ID: "session_target", Name: "Mention target", CWD: "/other"}
	server := &fakeServer{sessions: []protocol.SessionInfo{current, target}}
	runtime := sessionMentionTestRuntime{callbacks: make(chan func(), 1)}
	state := &sessionMentionAppState{appState: appState{phase: phaseReady, session: current, attachmentCtx: ctx, composer: "#"}}
	state.sessionMention.Observe("", "#", false)
	application := uitest.New(sessionMentionAppHarness{state})
	application.Pump(100, 24)
	state.requestSessionMentions(runtime, server)
	application.Pump(100, 24)
	if !application.Contains("Loading sessions…") {
		t.Fatal("loading state missing")
	}
	select {
	case apply := <-runtime.callbacks:
		apply()
	case <-time.After(time.Second):
		t.Fatal("session list did not load")
	}
	application.Pump(100, 24)
	if len(state.sessionMentions.Entries) != 1 || state.sessionMentions.Entries[0].ID != target.ID {
		t.Fatalf("picker catalog = %+v", state.sessionMentions.Entries)
	}
	if !application.Contains("Mention target · /other") {
		t.Fatal("target row missing")
	}
	application.Enter()
	application.Pump(100, 24)
	if state.composer != "#[session:session_target] " || state.sessionMention.Open {
		t.Fatalf("composer = %q, open = %t", state.composer, state.sessionMention.Open)
	}
	// A queued result cannot reopen a dismissed picker.
	state.SetState(func() { state.sessionMention.Observe("", "#", false) })
	state.requestSessionMentions(runtime, server)
	state.SetState(state.closeSessionMention)
	select {
	case apply := <-runtime.callbacks:
		apply()
	case <-time.After(time.Second):
		t.Fatal("session list did not resolve")
	}
	if state.sessionMention.Open || state.sessionMentions.Loading {
		t.Fatal("dismissed picker was reopened")
	}
}

func TestSessionMentionAttachmentPasteDismissesPicker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("attachment text"), 0600); err != nil {
		t.Fatal(err)
	}
	state := &sessionMentionAppState{appState: appState{phase: phaseReady, composer: "#", showToastOverride: func(toastInput) {}}}
	state.sessionMention.Observe("", "#", false)
	cancelled := false
	state.sessionMentionCancel = func() { cancelled = true }
	state.sessionMentions.Loading = true
	application := uitest.New(sessionMentionAppHarness{state})
	application.Pump(100, 24)
	paths, composer, ok := attachmentPathsForComposerChange(state.composer, "# "+path)
	if !ok {
		t.Fatal("valid pasted attachment not detected")
	}
	state.stageAttachments(paths, composer)
	if state.sessionMention.Open || state.sessionMentions.Loading || !cancelled {
		t.Fatal("attachment paste did not dismiss and cancel the picker")
	}
}
