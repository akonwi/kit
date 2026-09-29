package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// inlineMenuHarness mounts the real application shell and input routing, so
// keys reach the composer and the inline pickers exactly as in the app.
type inlineMenuHarness struct{ state *inlineMenuState }

func (w inlineMenuHarness) CreateState() ui.State { return w.state }

type inlineMenuState struct{ appState }

func (*inlineMenuState) InitState() {}
func (*inlineMenuState) Dispose()   {}
func (s *inlineMenuState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}

const inlineMenuWidth, inlineMenuHeight = 80, 24

// inlineMenuApp drives the harness through a real frame runner whose
// dispatched callbacks run on the test goroutine at the next paint, as they
// run on the UI thread in the app.
type inlineMenuApp struct {
	t       *testing.T
	app     *ui.App
	runner  *ui.Runner
	backend *inlineMenuBackend
	now     time.Time
}

type inlineMenuBackend struct {
	toastAnimationBackend
	mu     sync.Mutex
	queued []func()
}

func (b *inlineMenuBackend) Dispatch(callback func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queued = append(b.queued, callback)
}

func (b *inlineMenuBackend) drain() []func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	queued := b.queued
	b.queued = nil
	return queued
}

// Pump runs dispatched callbacks, then builds, lays out, and paints a frame.
func (a *inlineMenuApp) Pump() {
	a.t.Helper()
	for _, callback := range a.backend.drain() {
		callback()
	}
	a.app.RequestFrame()
	a.now = a.now.Add(time.Second / 60)
	if err := a.runner.HandleFrame(a.now); err != nil {
		a.t.Fatal(err)
	}
}

func (a *inlineMenuApp) Send(event ui.Event) { a.app.Send(event) }
func (a *inlineMenuApp) Key(text string) {
	a.Send(vaxis.Key{Text: text, Keycode: []rune(text)[0]})
}
func (a *inlineMenuApp) Enter() { a.Send(vaxis.Key{Keycode: vaxis.KeyEnter}) }
func (a *inlineMenuApp) Click(x, y int) {
	a.Send(vaxis.Mouse{Col: x, Row: y, Button: vaxis.MouseLeftButton, EventType: vaxis.EventPress})
}

// mountInlineMenu mounts a ready session; setup prepares the state first.
func mountInlineMenu(t *testing.T, setup func(*appState)) (*inlineMenuApp, *inlineMenuState) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	harness := &inlineMenuState{}
	state := &harness.appState
	state.ctx, state.attachmentCtx, state.phase = ctx, ctx, phaseReady
	state.session = protocol.SessionInfo{ID: "session_menu", Name: "Menus", Model: "test/model"}
	state.showToastOverride = func(toastInput) {}
	if setup != nil {
		setup(state)
	}
	backend := &inlineMenuBackend{toastAnimationBackend: toastAnimationBackend{
		events: make(chan ui.Event), size: ui.Size{Width: inlineMenuWidth, Height: inlineMenuHeight},
	}}
	app := ui.NewApp(inlineMenuHarness{state: harness})
	runner := ui.NewRunner(app, backend, ui.NewFrameScheduler(time.Second/60))
	application := &inlineMenuApp{t: t, app: app, runner: runner, backend: backend, now: time.Now()}
	runner.Start(application.now)
	application.Pump()
	return application, harness
}

// rows paints a frame and returns its rows.
func (s *inlineMenuState) rows(application *inlineMenuApp) []string {
	application.Pump()
	return toastPainterRows(application.backend.painter)
}

// typeText types into the focused composer without painting between keys.
func typeText(application *inlineMenuApp, text string) {
	for _, character := range text {
		application.Key(string(character))
	}
}

func sendKey(application *inlineMenuApp, keycode rune) {
	application.Send(vaxis.Key{Keycode: keycode})
}

func TestInlineMenuHarnessTypesIntoTheComposer(t *testing.T) {
	application, state := mountInlineMenu(t, nil)
	typeText(application, "hello")
	state.rows(application)
	if state.composer != "hello" {
		t.Fatalf("composer = %q, want typed text", state.composer)
	}
	_ = strings.Join
}

func fileMentionTestIndex() indexedFileSource {
	return indexedFileSource{Entries: []protocol.FileIndexEntry{
		{Path: "docs/", IsDir: true},
		{Path: "internal/tui/app.go"},
		{Path: "internal/tui/shell.go"},
		{Path: "README.md"},
	}}
}

// inlineMenuBox frames list rows (62 cells wide) in the 64-column inline
// picker drawn at 80 columns, with the shared footer.
func inlineMenuBox(list ...string) []string {
	rule := strings.Repeat("─", 62)
	box := []string{"┌" + rule + "┐"}
	for _, row := range list {
		box = append(box, "│"+row+strings.Repeat(" ", max(0, 62-len([]rune(row))))+"│")
	}
	return append(box,
		"├"+rule+"┤",
		"│ ↑↓ move · enter insert · esc close                           │",
		"└"+rule+"┘",
	)
}

// assertInlineMenuAnchor asserts the picker's left edge sits on the column
// of the composer character it was opened at, and its bottom border on the
// composer separator directly above a one-line composer.
func assertInlineMenuAnchor(t *testing.T, rows []string, column int) {
	t.Helper()
	assertInlinePickerOrigin(t, rows, column, inlineMenuHeight-4)
}

func TestFileMentionInlinePickerFollowsTheComposer(t *testing.T) {
	application, state := mountInlineMenu(t, func(state *appState) { state.indexedFiles = fileMentionTestIndex() })
	typeText(application, "see @")
	rows := state.rows(application)
	assertInlinePickerRows(t, rows, inlineMenuBox(
		"▌docs/",
		" internal/tui/app.go",
		" internal/tui/shell.go",
		" README.md",
	))
	// The picker rests on the composer line at the "@" in "see @".
	assertInlineMenuAnchor(t, rows, len(" see "))

	// Typing into the composer filters and highlights the first match; the
	// bottom edge stays put while the picker shrinks.
	typeText(application, "tui")
	rows = state.rows(application)
	assertInlinePickerRows(t, rows, inlineMenuBox("▌internal/tui/app.go", " internal/tui/shell.go"))
	assertInlineMenuAnchor(t, rows, len(" see "))

	// Up and Down wrap.
	for _, step := range []struct {
		key  rune
		want string
	}{{vaxis.KeyUp, "internal/tui/shell.go"}, {vaxis.KeyDown, "internal/tui/app.go"}, {vaxis.KeyDown, "internal/tui/shell.go"}} {
		sendKey(application, step.key)
		state.rows(application)
		if state.fileMention.Selection != step.want {
			t.Fatalf("selection = %q, want %q", state.fileMention.Selection, step.want)
		}
	}

	// Backspace and text reach the composer and change the query.
	sendKey(application, vaxis.KeyBackspace)
	typeText(application, "i")
	state.rows(application)
	if state.composer != "see @tui" || state.fileMention.Query != "tui" {
		t.Fatalf("composer = %q query = %q, want both edited through the composer", state.composer, state.fileMention.Query)
	}

	// Enter inserts the highlighted path, kept through the edits, and closes
	// the picker.
	application.Enter()
	state.rows(application)
	if state.composer != "see @internal/tui/shell.go " || state.fileMention.Open {
		t.Fatalf("composer = %q open = %t, want the inserted mention", state.composer, state.fileMention.Open)
	}
}

func TestFileMentionInlinePickerClickEscapeAndPrePaintKeys(t *testing.T) {
	application, state := mountInlineMenu(t, func(state *appState) { state.indexedFiles = fileMentionTestIndex() })
	typeText(application, "@")
	rows := state.rows(application)
	column, row := findTextCell(t, rows, "README.md")
	application.Click(column, row)
	state.rows(application)
	if state.composer != "@README.md " || state.fileMention.Open {
		t.Fatalf("clicked composer = %q open = %t", state.composer, state.fileMention.Open)
	}

	// Escape closes the picker and leaves the composer text as it is.
	typeText(application, "@doc")
	state.rows(application)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	rows = state.rows(application)
	if state.composer != "@README.md @doc" || state.fileMention.Open {
		t.Fatalf("escaped composer = %q open = %t", state.composer, state.fileMention.Open)
	}
	if got := rows[inlineMenuHeight-3]; strings.TrimRight(got, " ") != " @README.md @doc" {
		t.Fatalf("composer row = %q", got)
	}

	// Left reaches the composer: the cursor moves, so typed text lands
	// before the last character.
	sendKey(application, vaxis.KeyLeft)
	typeText(application, "X")
	state.rows(application)
	if state.composer != "@README.md @doXc" {
		t.Fatalf("composer after Left = %q", state.composer)
	}

	// Keys typed before the picker first paints are applied: the trigger,
	// the query, a move, and Enter all arrive in one frame.
	application, state = mountInlineMenu(t, func(state *appState) { state.indexedFiles = fileMentionTestIndex() })
	typeText(application, "@tui")
	sendKey(application, vaxis.KeyDown)
	application.Enter()
	state.rows(application)
	if state.composer != "@internal/tui/shell.go " || state.fileMention.Open {
		t.Fatalf("pre-paint composer = %q open = %t", state.composer, state.fileMention.Open)
	}
}

// mountSessionMention mounts the shell with "#" typed and the session list
// loaded, since the list request belongs to the full application.
func mountSessionMention(t *testing.T) (*inlineMenuApp, *inlineMenuState) {
	t.Helper()
	ago := func(duration time.Duration) string { return time.Now().Add(-duration).UTC().Format(time.RFC3339) }
	return mountInlineMenu(t, func(state *appState) {
		state.composer, state.composerCursorEndGeneration = "#", 1
		state.sessionMentions.Entries = []protocol.SessionInfo{
			{ID: "session_design", Name: "Design review", CWD: "/repo/design", UpdatedAt: ago(2 * time.Hour)},
			{ID: "session_tests", Name: "Tests", CWD: "/repo/verification", UpdatedAt: ago(3 * 24 * time.Hour)},
			{ID: "session_unnamed", CWD: "/srv", UpdatedAt: ago(10 * time.Minute)},
		}
		state.sessionMention.Observe("", "#", false)
	})
}

func TestSessionMentionInlinePickerFollowsTheComposer(t *testing.T) {
	application, state := mountSessionMention(t)
	rows := state.rows(application)
	assertInlinePickerRows(t, rows, inlineMenuBox(
		"▌Design review    /repo/design                         2h ago",
		" Tests            /repo/verification                   3d ago",
		" Unnamed session  /srv                                10m ago",
	))
	assertInlineMenuAnchor(t, rows, 1)

	// The working directory and ID are matched too.
	typeText(application, "verif")
	rows = state.rows(application)
	assertInlinePickerRows(t, rows, inlineMenuBox("▌Tests            /repo/verification                   3d ago"))
	sendKey(application, vaxis.KeyBackspace)
	sendKey(application, vaxis.KeyBackspace)
	sendKey(application, vaxis.KeyBackspace)
	sendKey(application, vaxis.KeyBackspace)
	sendKey(application, vaxis.KeyBackspace)
	state.rows(application)
	if state.composer != "#" || state.sessionMention.Query != "" {
		t.Fatalf("composer = %q query = %q after deleting the query", state.composer, state.sessionMention.Query)
	}

	// The highlight stayed on Tests; Up passes the top and wraps to the last
	// session, and Down wraps back.
	for _, step := range []struct {
		key  rune
		want string
	}{{vaxis.KeyUp, "session_design"}, {vaxis.KeyUp, "session_unnamed"}, {vaxis.KeyDown, "session_design"}, {vaxis.KeyDown, "session_tests"}} {
		sendKey(application, step.key)
		state.rows(application)
		if state.sessionMention.Selection != step.want {
			t.Fatalf("selection = %q, want %q", state.sessionMention.Selection, step.want)
		}
	}
	application.Enter()
	state.rows(application)
	if state.composer != "#[session:session_tests] " || state.sessionMention.Open {
		t.Fatalf("composer = %q open = %t, want the inserted reference", state.composer, state.sessionMention.Open)
	}
}

func TestSessionMentionInlinePickerClickEscapeAndPrePaintKeys(t *testing.T) {
	application, state := mountSessionMention(t)
	rows := state.rows(application)
	column, row := findTextCell(t, rows, "Unnamed session")
	application.Click(column, row)
	state.rows(application)
	if state.composer != "#[session:session_unnamed] " {
		t.Fatalf("clicked composer = %q", state.composer)
	}

	application, state = mountSessionMention(t)
	typeText(application, "tes")
	state.rows(application)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	state.rows(application)
	if state.composer != "#tes" || state.sessionMention.Open {
		t.Fatalf("escaped composer = %q open = %t", state.composer, state.sessionMention.Open)
	}

	// Text, a move, and Enter typed in one frame are all applied.
	application, state = mountSessionMention(t)
	typeText(application, "re")
	sendKey(application, vaxis.KeyDown)
	application.Enter()
	state.rows(application)
	if state.composer != "#[session:session_tests] " {
		t.Fatalf("pre-paint composer = %q", state.composer)
	}
}

// messageHistorySession pages newest-first user prompts.
type messageHistorySession struct {
	fakeSession
	prompts []string
}

func (s *messageHistorySession) MessagePage(context.Context, protocol.MessagePageQuery) (protocol.MessagePage, error) {
	messages := make([]protocol.TranscriptMessage, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		messages = append(messages, protocol.TranscriptMessage{
			ID: "message_" + prompt, Role: "user",
			Content: []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: prompt}},
		})
	}
	return protocol.MessagePage{Messages: messages}, nil
}

// openMessageHistory presses Up on the empty composer and waits for the
// history load to open the picker.
func openMessageHistory(t *testing.T, prompts ...string) (*inlineMenuApp, *inlineMenuState) {
	t.Helper()
	return openMessageHistoryTyping(t, "", prompts...)
}

// openMessageHistoryTyping types text right after Up, while history loads.
func openMessageHistoryTyping(t *testing.T, text string, prompts ...string) (*inlineMenuApp, *inlineMenuState) {
	t.Helper()
	application, state := mountInlineMenu(t, func(state *appState) {
		state.bound = &messageHistorySession{fakeSession: fakeSession{id: "session_menu"}, prompts: prompts}
	})
	sendKey(application, vaxis.KeyUp)
	typeText(application, text)
	deadline := time.Now().Add(time.Second)
	for !state.messageHistory.Open && time.Now().Before(deadline) {
		state.rows(application)
		time.Sleep(time.Millisecond)
	}
	if !state.messageHistory.Open {
		t.Fatal("message history did not open")
	}
	return application, state
}

func TestMessageHistoryInlinePickerFiltersByTheComposer(t *testing.T) {
	application, state := openMessageHistory(t, "git status", "deploy staging", "git log")
	rows := state.rows(application)
	// Oldest first, so the newest prompt sits nearest the composer and is
	// highlighted; there is no title or search field.
	assertInlinePickerRows(t, rows, inlineMenuBox(" git log", " deploy staging", "▌git status"))
	assertInlineMenuAnchor(t, rows, 1)

	// Typing edits the composer and filters, keeping chronological order and
	// highlighting the newest match.
	typeText(application, "git")
	rows = state.rows(application)
	if state.composer != "git" {
		t.Fatalf("composer = %q, want the typed query", state.composer)
	}
	assertInlinePickerRows(t, rows, inlineMenuBox(" git log", "▌git status"))
	assertInlineMenuAnchor(t, rows, 1)

	// Down wraps from the newest to the oldest match; Up wraps back.
	for _, step := range []struct {
		key  rune
		want string
	}{{vaxis.KeyDown, "message_git log"}, {vaxis.KeyUp, "message_git status"}, {vaxis.KeyUp, "message_git log"}} {
		sendKey(application, step.key)
		state.rows(application)
		if state.messageHistory.Selection != step.want {
			t.Fatalf("selection = %q, want %q", state.messageHistory.Selection, step.want)
		}
	}

	// Left and Backspace edit the composer: "git" becomes "gt".
	sendKey(application, vaxis.KeyLeft)
	sendKey(application, vaxis.KeyBackspace)
	rows = state.rows(application)
	if state.composer != "gt" || state.messageHistory.Query != "gt" {
		t.Fatalf("composer = %q query = %q", state.composer, state.messageHistory.Query)
	}
	assertInlinePickerRows(t, rows, inlineMenuBox(" git log", "▌git status"))

	// Enter puts the chosen prompt into the composer without submitting.
	sendKey(application, vaxis.KeyUp)
	application.Enter()
	state.rows(application)
	if state.composer != "git log" || state.messageHistory.Open {
		t.Fatalf("composer = %q open = %t, want the recalled prompt", state.composer, state.messageHistory.Open)
	}
}

func TestMessageHistoryInlinePickerClickEscapeAndPrePaintKeys(t *testing.T) {
	application, state := openMessageHistory(t, "git status", "deploy staging")
	rows := state.rows(application)
	column, row := findTextCell(t, rows, "deploy staging")
	application.Click(column, row)
	state.rows(application)
	if state.composer != "deploy staging" || state.messageHistory.Open {
		t.Fatalf("clicked composer = %q open = %t", state.composer, state.messageHistory.Open)
	}

	application, state = openMessageHistory(t, "git status", "deploy staging")
	typeText(application, "dep")
	state.rows(application)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	state.rows(application)
	if state.composer != "dep" || state.messageHistory.Open {
		t.Fatalf("escaped composer = %q open = %t", state.composer, state.messageHistory.Open)
	}

	// Text typed while history loads reaches the composer and becomes the
	// query the picker opens with.
	application, state = openMessageHistoryTyping(t, "git", "git status", "deploy staging", "git log")
	if state.composer != "git" || state.messageHistory.Query != "git" {
		t.Fatalf("composer = %q query = %q, want text typed while loading", state.composer, state.messageHistory.Query)
	}
	assertInlinePickerRows(t, state.rows(application), inlineMenuBox(" git log", "▌git status"))
	// Text, a move, and Enter typed in one frame are all applied.
	typeText(application, " ")
	sendKey(application, vaxis.KeyBackspace)
	sendKey(application, vaxis.KeyUp)
	application.Enter()
	state.rows(application)
	if state.composer != "git log" {
		t.Fatalf("pre-paint composer = %q", state.composer)
	}
}

// bashHistoryMenuSession serves durable bash history pages from a stub.
type bashHistoryMenuSession struct {
	fakeSession
	history *stubBashHistorySession
}

func (s *bashHistoryMenuSession) BashHistory(ctx context.Context, before uint64, limit int) (protocol.BashHistoryPage, error) {
	return s.history.BashHistory(ctx, before, limit)
}

// bashHistoryPages splits newest-first commands into durable pages of size.
func bashHistoryPages(size int, commands ...string) []protocol.BashHistoryPage {
	var pages []protocol.BashHistoryPage
	for start := 0; start < len(commands); start += size {
		page := protocol.BashHistoryPage{SessionID: "session_menu"}
		for index := start; index < min(len(commands), start+size); index++ {
			command, exclude := strings.CutPrefix(commands[index], "!")
			sequence := int64(len(commands) - index)
			page.Entries = append(page.Entries, protocol.BashHistoryEntry{
				ID: fmt.Sprintf("bash_%02d", sequence), Sequence: sequence, Command: command,
				Status: "completed", ExcludeFromContext: exclude,
			})
		}
		if start+size < len(commands) {
			page.HasMore = true
			page.NextCursor = strconv.FormatInt(page.Entries[len(page.Entries)-1].Sequence, 10)
		}
		pages = append(pages, page)
	}
	return pages
}

func mountBashHistory(t *testing.T, pages []protocol.BashHistoryPage) (*inlineMenuApp, *inlineMenuState) {
	t.Helper()
	session := &bashHistoryMenuSession{fakeSession: fakeSession{id: "session_menu"}, history: &stubBashHistorySession{pages: pages}}
	return mountInlineMenu(t, func(state *appState) { state.bound = session })
}

// settleBashHistory paints until the durable read has answered.
func settleBashHistory(t *testing.T, application *inlineMenuApp, state *inlineMenuState) []string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for state.bashHistory.Open && state.bashHistory.Loading && time.Now().Before(deadline) {
		state.rows(application)
		time.Sleep(time.Millisecond)
	}
	if state.bashHistory.Loading {
		t.Fatal("bash history did not load")
	}
	return state.rows(application)
}

func TestBashHistoryInlinePickerFiltersByTheCommandAfterTheBang(t *testing.T) {
	application, state := mountBashHistory(t, bashHistoryPages(10, "!git log --oneline", "make test", "git status"))
	// "!git" then Up opens bash history filtered by "git".
	typeText(application, "!git")
	state.rows(application)
	sendKey(application, vaxis.KeyUp)
	rows := settleBashHistory(t, application, state)
	if state.bashHistory.Query != "git" || state.composer != "!git" {
		t.Fatalf("query = %q composer = %q, want git from !git", state.bashHistory.Query, state.composer)
	}
	// Each row is the text Enter inserts, oldest first with the newest match
	// nearest the composer and highlighted; "!!" keeps output out of context.
	assertInlinePickerRows(t, rows, inlineMenuBox(" !git status", "▌!!git log --oneline"))
	assertInlineMenuAnchor(t, rows, 1)

	// Typing edits the composer and filters.
	typeText(application, " s")
	rows = state.rows(application)
	assertInlinePickerRows(t, rows, inlineMenuBox("▌!git status"))
	sendKey(application, vaxis.KeyBackspace)
	sendKey(application, vaxis.KeyBackspace)
	rows = state.rows(application)
	assertInlinePickerRows(t, rows, inlineMenuBox(" !git status", "▌!!git log --oneline"))

	// Up and Down wrap when no older history remains.
	for _, step := range []struct {
		key  rune
		want string
	}{{vaxis.KeyDown, "bash_01"}, {vaxis.KeyUp, "bash_03"}, {vaxis.KeyUp, "bash_01"}} {
		sendKey(application, step.key)
		state.rows(application)
		if state.bashHistory.Selection != step.want {
			t.Fatalf("selection = %q, want %q", state.bashHistory.Selection, step.want)
		}
	}

	// Enter puts the chosen command into the composer as bash input.
	sendKey(application, vaxis.KeyDown)
	application.Enter()
	state.rows(application)
	if state.composer != "!!git log --oneline" || state.bashHistory.Open {
		t.Fatalf("composer = %q open = %t, want the recalled command", state.composer, state.bashHistory.Open)
	}
}

func TestBashHistoryInlinePickerClickEscapeAndLeavingBashMode(t *testing.T) {
	application, state := mountBashHistory(t, bashHistoryPages(10, "make test", "git status"))
	typeText(application, "!")
	state.rows(application)
	sendKey(application, vaxis.KeyUp)
	rows := settleBashHistory(t, application, state)
	column, row := findTextCell(t, rows, "!git status")
	application.Click(column, row)
	state.rows(application)
	if state.composer != "!git status" || state.bashHistory.Open {
		t.Fatalf("clicked composer = %q open = %t", state.composer, state.bashHistory.Open)
	}

	// Escape closes and leaves the composer text.
	sendKey(application, vaxis.KeyUp)
	settleBashHistory(t, application, state)
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	state.rows(application)
	if state.composer != "!git status" || state.bashHistory.Open {
		t.Fatalf("escaped composer = %q open = %t", state.composer, state.bashHistory.Open)
	}

	// Deleting the "!" leaves bash mode and closes the picker.
	application, state = mountBashHistory(t, bashHistoryPages(10, "make test"))
	typeText(application, "!")
	state.rows(application)
	sendKey(application, vaxis.KeyUp)
	settleBashHistory(t, application, state)
	sendKey(application, vaxis.KeyBackspace)
	state.rows(application)
	if state.composer != "" || state.bashHistory.Open {
		t.Fatalf("composer = %q open = %t, want bash history closed", state.composer, state.bashHistory.Open)
	}

	// Keys typed before the picker first paints are applied.
	application, state = mountBashHistory(t, bashHistoryPages(10, "make test", "git status"))
	typeText(application, "!")
	state.rows(application)
	sendKey(application, vaxis.KeyUp)
	typeText(application, "mak")
	rows = settleBashHistory(t, application, state)
	if state.composer != "!mak" || state.bashHistory.Query != "mak" {
		t.Fatalf("composer = %q query = %q", state.composer, state.bashHistory.Query)
	}
	assertInlinePickerRows(t, rows, inlineMenuBox("▌!make test"))
}

func TestBashHistoryInlinePickerLoadsOlderPagesAtTheTop(t *testing.T) {
	commands := make([]string, 13)
	for index := range commands {
		commands[index] = fmt.Sprintf("echo %02d", 13-index)
	}
	application, state := mountBashHistory(t, bashHistoryPages(10, commands...))
	typeText(application, "!")
	state.rows(application)
	sendKey(application, vaxis.KeyUp)
	rows := settleBashHistory(t, application, state)
	// The first durable page holds the newest ten, which fill the list.
	want := []string{" !echo 04", " !echo 05", " !echo 06", " !echo 07", " !echo 08", " !echo 09", " !echo 10", " !echo 11", " !echo 12", "▌!echo 13"}
	if got := inlinePickerListRows(t, rows); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("list = %q, want %q", got, want)
	}
	// Up to the oldest loaded row, then once more loads the next page instead
	// of wrapping; the selection stays and the older rows appear above it.
	for range 9 {
		sendKey(application, vaxis.KeyUp)
	}
	state.rows(application)
	if state.bashHistory.Selection != "bash_04" {
		t.Fatalf("selection = %q, want the oldest loaded entry", state.bashHistory.Selection)
	}
	sendKey(application, vaxis.KeyUp)
	rows = settleBashHistory(t, application, state)
	if state.bashHistory.Selection != "bash_04" || state.bashHistory.PagesLoaded != 1 || len(state.bashHistory.Entries) != 13 {
		t.Fatalf("after paging selection = %q pages = %d entries = %d", state.bashHistory.Selection, state.bashHistory.PagesLoaded, len(state.bashHistory.Entries))
	}
	want = []string{" !echo 01", " !echo 02", " !echo 03", "▌!echo 04", " !echo 05", " !echo 06", " !echo 07", " !echo 08", " !echo 09", " " + glyphEllipsis}
	if got := inlinePickerListRows(t, rows); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("paged list = %q, want %q", got, want)
	}
	// With history exhausted, Up from the oldest wraps to the newest.
	for range 4 {
		sendKey(application, vaxis.KeyUp)
	}
	state.rows(application)
	if state.bashHistory.Selection != "bash_13" {
		t.Fatalf("selection = %q, want the wrap to the newest entry", state.bashHistory.Selection)
	}
}

func TestComposerPickerAnchorFollowsTheWrappedTrigger(t *testing.T) {
	t.Parallel()
	size := ui.Size{Width: 80, Height: 24}
	// 78 columns of text fit one composer line, so "@src" wraps to a second
	// line; the composer then spans rows 20 and 21 above its divider.
	long := strings.Repeat("word ", 15) + "@src"
	many := strings.Repeat("line\n", 12) + "see @"
	for _, test := range []struct {
		name     string
		composer string
		offset   int
		want     ui.Point
	}{
		{name: "start", composer: "", want: ui.Point{X: 1, Y: 21}},
		{name: "after text", composer: "see @", offset: 4, want: ui.Point{X: 5, Y: 21}},
		{name: "wrapped", composer: long, offset: strings.Index(long, "@"), want: ui.Point{X: 1, Y: 21}},
		{name: "first of two lines", composer: long, want: ui.Point{X: 1, Y: 20}},
		// Taller than ten lines, the composer shows its last ten.
		{name: "scrolled", composer: many, offset: strings.Index(many, "@"), want: ui.Point{X: 5, Y: 21}},
		{name: "scrolled out", composer: many, want: ui.Point{X: 1, Y: 12}},
	} {
		if got := composerPickerAnchor(test.composer, test.offset)(size); got != test.want {
			t.Errorf("%s anchor = %+v, want %+v", test.name, got, test.want)
		}
	}
}

func TestFileMentionOnAWrappedLineRestsOnTheLineAbove(t *testing.T) {
	application, state := mountInlineMenu(t, func(state *appState) { state.indexedFiles = fileMentionTestIndex() })
	typeText(application, strings.Repeat("word ", 15)+"@tui")
	rows := state.rows(application)
	assertInlinePickerRows(t, rows, inlineMenuBox("▌internal/tui/app.go", " internal/tui/shell.go"))
	// The composer wraps to rows 20 and 21; the picker covers the first line
	// and rests directly above the "@" on the second.
	assertInlinePickerOrigin(t, rows, 1, inlineMenuHeight-4)
	if got := strings.TrimRight(rows[inlineMenuHeight-3], " "); got != " @tui" {
		t.Fatalf("trigger line = %q, want it visible below the picker", got)
	}
}
