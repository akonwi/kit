package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestSessionExplorerControllerLoadsAllSessionsAndKeepsIdentitySelection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 5, 12, 0, 0, 0, time.UTC)
	controller := sessionExplorerController{}
	generation := controller.Begin("session_current")
	if !controller.Open || !controller.Loading || controller.Selection != "session_current" {
		t.Fatalf("begin = %+v", controller)
	}
	if !controller.Resolve(generation, []sessionExplorerItem{
		{ID: "session_current", UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano)},
		{ID: "session_newest", UpdatedAt: now.Format(time.RFC3339Nano)},
	}, nil) {
		t.Fatal("current load was ignored")
	}
	if controller.Loading || controller.Selection != "session_current" {
		t.Fatalf("resolved controller = %+v", controller)
	}
	if got := []string{controller.Sessions[0].ID, controller.Sessions[1].ID}; fmt.Sprint(got) != "[session_newest session_current]" {
		t.Fatalf("ordered sessions = %v", got)
	}

	controller.Move(-1)
	if controller.Selection != "session_newest" {
		t.Fatalf("selection after Up = %q", controller.Selection)
	}
	controller.Move(-1)
	if controller.Selection != "session_current" {
		t.Fatalf("selection did not wrap from the first row = %q", controller.Selection)
	}
	controller.Move(1)
	if controller.Selection != "session_newest" {
		t.Fatalf("selection did not wrap from the last row = %q", controller.Selection)
	}
}

func TestSessionExplorerControllerIgnoresClosedAndStaleLoads(t *testing.T) {
	t.Parallel()

	controller := sessionExplorerController{}
	stale := controller.Begin("session_current")
	controller.Close()
	if controller.Resolve(stale, []sessionExplorerItem{{ID: "session_stale"}}, nil) {
		t.Fatal("closed explorer accepted a stale load")
	}
	current := controller.Begin("session_current")
	if controller.Resolve(stale, nil, errors.New("stale")) {
		t.Fatal("new explorer generation accepted an old load")
	}
	if !controller.Resolve(current, nil, errors.New("offline")) || controller.Error != "offline" || controller.Loading {
		t.Fatalf("current error load = %+v", controller)
	}
}

func TestSessionExplorerControllerGuardsSwitchLifecycleByGeneration(t *testing.T) {
	t.Parallel()

	controller := sessionExplorerController{}
	loadGeneration := controller.Begin("session_current")
	if _, activatable := controller.ActivatableSelection(); activatable {
		t.Fatal("loading explorer exposed an activatable seeded selection")
	}
	controller.Resolve(loadGeneration, []sessionExplorerItem{
		{ID: "session_target", UpdatedAt: "2026-06-05T12:00:00Z"},
		{ID: "session_current", UpdatedAt: "2026-06-05T11:00:00Z"},
	}, nil)
	controller.Select("session_target")
	switchGeneration, target, started := controller.BeginSwitch()
	if !started || target != "session_target" || !controller.Switching || controller.SwitchError != "" {
		t.Fatalf("begin switch generation=%d target=%q controller=%+v", switchGeneration, target, controller)
	}
	controller.Move(1)
	controller.Select("session_current")
	if controller.Selection != "session_target" {
		t.Fatalf("selection changed while switching: %q", controller.Selection)
	}
	if controller.ResolveSwitch(switchGeneration-1, errors.New("stale")) {
		t.Fatal("stale switch result was accepted")
	}
	if !controller.ResolveSwitch(switchGeneration, errors.New("offline")) || controller.Switching || controller.SwitchError != "offline" {
		t.Fatalf("failed switch result = %+v", controller)
	}
	retryGeneration, retryTarget, started := controller.BeginSwitch()
	if !started || retryGeneration == switchGeneration || retryTarget != target {
		t.Fatalf("retry generation=%d target=%q started=%t", retryGeneration, retryTarget, started)
	}
	controller.CancelSwitch()
	if !controller.Open || controller.Switching || controller.SwitchError != "" {
		t.Fatalf("cancelled switch did not return to list: %+v", controller)
	}
	finalGeneration, _, started := controller.BeginSwitch()
	if !started || !controller.ResolveSwitch(finalGeneration, nil) || controller.Open {
		t.Fatalf("successful switch did not close explorer: %+v", controller)
	}
}

func TestSessionExplorerControllerRenamesSelectedSessionAndCancelsEmptyInput(t *testing.T) {
	t.Parallel()

	controller := sessionExplorerController{}
	loadGeneration := controller.Begin("session_current")
	controller.Resolve(loadGeneration, []sessionExplorerItem{{
		ID: "session_current", Name: "Before", CWD: "/repo", UpdatedAt: "2026-06-05T12:00:00Z",
	}}, nil)
	if !controller.BeginRename() || !controller.RenameOpen || controller.RenameText != "Before" {
		t.Fatalf("begin rename = %+v", controller)
	}
	controller.SetRenameText("  After  ")
	generation, sessionID, name, started := controller.BeginRenameSave()
	if !started || sessionID != "session_current" || name != "After" || !controller.RenamePending {
		t.Fatalf("begin save generation=%d id=%q name=%q controller=%+v", generation, sessionID, name, controller)
	}
	if controller.ResolveRename(generation-1, protocol.SessionInfo{}, errors.New("stale")) {
		t.Fatal("stale rename result was accepted")
	}
	if !controller.ResolveRename(generation, protocol.SessionInfo{}, errors.New("offline")) || controller.RenameError != "offline" {
		t.Fatalf("rename error = %+v", controller)
	}
	controller.SetRenameText("After")
	generation, _, _, started = controller.BeginRenameSave()
	renamed := protocol.SessionInfo{
		ID: "session_current", Name: "After", CWD: "/repo", UpdatedAt: "2026-06-05T13:00:00Z",
	}
	if !started || !controller.ResolveRename(generation, renamed, nil) || controller.RenameOpen || controller.Sessions[0].Name != "After" {
		t.Fatalf("rename success = %+v", controller)
	}
	controller.BeginRename()
	controller.SetRenameText("   ")
	if _, _, _, started := controller.BeginRenameSave(); started || controller.RenameOpen {
		t.Fatalf("empty rename was not cancelled: %+v", controller)
	}
}

func TestSessionExplorerControllerConfirmsAndRemovesDeletedSession(t *testing.T) {
	t.Parallel()

	controller := sessionExplorerController{}
	generation := controller.Begin("session_current")
	controller.Resolve(generation, []sessionExplorerItem{
		{ID: "session_current", Name: "Current"},
		{ID: "session_target", Name: "Target"},
		{ID: "session_after", Name: "After"},
	}, nil)
	if controller.BeginDelete() || controller.DeleteOpen || controller.DeleteError == "" {
		t.Fatalf("attached delete guard = %+v", controller)
	}
	controller.Select("session_target")
	if controller.DeleteError != "" || !controller.BeginDelete() || !controller.DeleteOpen {
		t.Fatalf("begin delete = %+v", controller)
	}
	generation, sessionID, started := controller.BeginDeleteConfirm()
	if !started || sessionID != "session_target" || !controller.DeletePending {
		t.Fatalf("begin delete confirm generation=%d id=%q controller=%+v", generation, sessionID, controller)
	}
	if controller.ResolveDelete(generation-1, errors.New("stale")) {
		t.Fatal("stale delete result was accepted")
	}
	if !controller.ResolveDelete(generation, errors.New("offline")) || controller.DeleteError != "offline" {
		t.Fatalf("delete error = %+v", controller)
	}
	generation, _, started = controller.BeginDeleteConfirm()
	if !started || !controller.ResolveDelete(generation, nil) || controller.DeleteOpen {
		t.Fatalf("delete success = %+v", controller)
	}
	if len(controller.Sessions) != 2 || controller.Selection != "session_current" || sessionIndex(controller.Sessions, "session_target") >= 0 {
		t.Fatalf("sessions after delete = %+v selection=%q", controller.Sessions, controller.Selection)
	}
}

func TestSessionRenameFieldPlacesInitialCursorAtEndWithoutPinningIt(t *testing.T) {
	t.Parallel()

	state := &sessionRenameFieldHarnessState{value: "Before", generation: 1}
	application := uitest.New(sessionRenameFieldHarness{State: state})
	application.Pump(80, 12)
	application.Pump(80, 12)
	application.Send(ui.Key{Keycode: vaxis.KeyLeft})
	application.Key("?")
	application.Pump(80, 12)
	if state.value != "Befor?e" {
		t.Fatalf("first cursor movement edit = %q, want one-shot initial end placement", state.value)
	}
}

type sessionRenameFieldHarness struct {
	State *sessionRenameFieldHarnessState
}

func (w sessionRenameFieldHarness) CreateState() ui.State { return w.State }

type sessionRenameFieldHarnessState struct {
	ui.StateBase
	value      string
	generation uint64
}

func (s *sessionRenameFieldHarnessState) Build(ui.BuildContext) ui.Widget {
	return sessionRenameSurface{
		Snapshot: sessionRenameSnapshot{
			Open: true, Target: "Before", Text: s.value,
			CursorOffset: len((ui.LayoutContext{}).Characters(s.value)), CursorEnd: s.generation,
		},
		Callbacks: sessionRenameCallbacks{Changed: func(_ ui.EventContext, value string) {
			s.SetState(func() { s.value = value })
		}},
	}
}

// explorerFixture is hierarchyFixture with working directories.
func explorerFixture() []sessionExplorerItem {
	items := hierarchyFixture()
	for index := range items {
		items[index].CWD = "/repo/" + strings.ToLower(items[index].Name)
	}
	return items
}

func openExplorer(currentSessionID string, items []sessionExplorerItem) sessionExplorerController {
	controller := sessionExplorerController{}
	controller.Resolve(controller.Begin(currentSessionID), items, nil)
	return controller
}

type sessionExplorerHarness struct{ State *sessionExplorerHarnessState }

func (w sessionExplorerHarness) CreateState() ui.State { return w.State }

type sessionExplorerHarnessState struct {
	ui.StateBase
	controller sessionExplorerController
	activated  []string
}

func (s *sessionExplorerHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Session: protocol.SessionInfo{ID: s.controller.CurrentSessionID},
			Scroll: &ui.ScrollController{}, SessionExplorer: s.controller.Snapshot(),
		},
		Callbacks: shellCallbacks{
			ActivateSession: func(_ ui.EventContext, id string) { s.activated = append(s.activated, id) },
			ToggleSessionTree: func(_ ui.EventContext, id string) {
				s.SetState(func() { s.controller.ToggleExpanded(id) })
			},
		},
	}
}

func renderExplorer(t *testing.T, controller sessionExplorerController, width, height int) (*uitest.App, *sessionExplorerHarnessState, []string) {
	t.Helper()
	state := &sessionExplorerHarnessState{controller: controller}
	application := uitest.New(sessionExplorerHarness{State: state})
	application.Pump(width, height)
	application.Pump(width, height)
	return application, state, paintedRows(application, width, height)
}

func TestSessionExplorerRendersTheCanonicalPicker(t *testing.T) {
	t.Parallel()
	theme := ui.DefaultThemeSet().Dark
	controller := openExplorer("session_child", explorerFixture())
	application, _, rows := renderExplorer(t, controller, 100, 24)

	_, searchRow := assertPickerSearchField(t, rows, "Search sessions…")
	assertPickerTitleSpacing(t, rows, "Sessions", searchRow)
	assertDialogRow(t, rows, "Sessions", "│ Sessions                                                          5 sessions │")
	// Children are indented under their parent with the disclosure in the
	// hint column; the working directory and updated time share columns.
	assertDialogRow(t, rows, "Authentication", "│ Authentication  ▾ 2                  /repo/authentication         2026-06-01 │")
	assertDialogRow(t, rows, "Tokens", "│   Tokens                             /repo/tokens                 2026-06-03 │")
	assertDialogRow(t, rows, "OAuth", "│▌  OAuth         ▸ 1                  /repo/oauth                  2026-06-02 │")
	assertDialogRow(t, rows, "Release", "│ Release                              /repo/release                2026-06-04 │")
	assertPickerFooter(t, rows, "←→ expand · enter switch · ctrl+r rename · ctrl+d delete · esc close")

	column, row := findTextCell(t, rows, "OAuth")
	if cell := application.Cell(column, row); cell.Style.Foreground != theme.PrimaryText {
		t.Fatalf("current session label style = %+v, want accent %v", cell.Style, theme.PrimaryText)
	}
}

func TestSessionExplorerShowsLineageNotesInTheHintColumn(t *testing.T) {
	t.Parallel()
	items := append(explorerFixture(),
		sessionExplorerItem{ID: "session_orphan", Name: "Orphan", CWD: "/repo/orphan", ParentSessionID: "session_gone", ParentSessionName: "Gone", UpdatedAt: "2026-06-04T00:00:00Z"},
		sessionExplorerItem{ID: "session_unnamed_parent", Name: "Adopted", CWD: "/repo/adopted", ParentSessionID: "session_0123456789", UpdatedAt: "2026-06-04T00:00:00Z"},
		sessionExplorerItem{ID: "session_self", Name: "Loop", CWD: "/repo/loop", ParentSessionID: "session_self", UpdatedAt: "2026-06-04T00:00:00Z"},
	)
	controller := openExplorer("session_other", items)
	_, _, rows := renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Orphan", "│ Orphan          from Gone            /repo/orphan                 2026-06-04 │")
	assertDialogRow(t, rows, "Adopted", "│ Adopted         from 01234567        /repo/adopted                2026-06-04 │")
	assertDialogRow(t, rows, "Loop", "│ Loop            invalid parent       /repo/loop                   2026-06-04 │")

	// Filtered results are flat; each match keeps its parent as a note.
	controller.SetQuery("o")
	_, _, rows = renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "│ o ", "│ o                                                                            │")
	assertDialogRow(t, rows, "OAuth", "│▌OAuth           from Authentication  /repo/oauth                  2026-06-02 │")
	assertDialogRow(t, rows, "Tokens", "│ Tokens          from Authentication  /repo/tokens                 2026-06-03 │")
	assertDialogRow(t, rows, "Providers", "│ Providers       from OAuth           /repo/providers              2026-06-05 │")
	assertDialogRow(t, rows, "Orphan", "│ Orphan          from Gone            /repo/orphan                 2026-06-04 │")
}

func TestSessionExplorerTitleShowsSwitchingSpinnerAndFooterStatus(t *testing.T) {
	t.Parallel()
	controller := openExplorer("session_other", explorerFixture())
	controller.Select("session_root")
	if _, _, ok := controller.BeginSwitch(); !ok {
		t.Fatal("switch did not start")
	}
	_, _, rows := renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Sessions", "│ Sessions                                                        ⠋ switching… │")
	assertDialogRow(t, rows, "esc cancel", "│ esc cancel                                                                   │")

	controller.CancelSwitch()
	controller.SwitchError = "offline"
	_, _, rows = renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Sessions", "│ Sessions                                                          5 sessions │")
	assertDialogRow(t, rows, "Switch failed", "│ Switch failed: offline · enter retry                               esc close │")
}

func TestSessionExplorerMessagesReplaceTheList(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		snapshot sessionExplorerSnapshot
		row      string
		footer   string
	}{
		{name: "loading", snapshot: sessionExplorerSnapshot{Open: true, Loading: true}, row: "⠋ Loading sessions…", footer: "esc close"},
		{name: "error", snapshot: sessionExplorerSnapshot{Open: true, Error: "offline"}, row: "Could not load sessions: offline", footer: "esc close"},
		{name: "empty", snapshot: sessionExplorerSnapshot{Open: true}, row: "No sessions", footer: "esc close"},
		{name: "no matches", snapshot: sessionExplorerSnapshot{Open: true, Query: "zzz", All: explorerFixture()}, row: "No matching sessions", footer: "←→ expand · enter switch · ctrl+r rename · ctrl+d delete · esc close"},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(shellView{Snapshot: shellSnapshot{
				Phase: phaseReady, Scroll: &ui.ScrollController{}, SessionExplorer: test.snapshot,
			}})
			application.Pump(100, 24)
			application.Pump(100, 24)
			rows := paintedRows(application, 100, 24)
			search := "Search sessions…"
			if test.snapshot.Query != "" {
				search = test.snapshot.Query
			}
			_, searchRow := assertPickerSearchField(t, rows, search)
			if got := dialogRowText(rows, searchRow+2); got != test.row {
				t.Fatalf("first list row = %q, want %q:\n%s", got, test.row, strings.Join(rows, "\n"))
			}
			assertPickerFooter(t, rows, test.footer)
		})
	}
}

func TestSessionExplorerDeleteConfirmationStaysInTheFrame(t *testing.T) {
	t.Parallel()
	controller := openExplorer("session_other", explorerFixture())
	controller.Select("session_root")
	if !controller.BeginDelete() {
		t.Fatal("delete did not open")
	}
	application, _, rows := renderExplorer(t, controller, 100, 24)
	// The target stays highlighted in the list while the footer asks.
	assertDialogRow(t, rows, "Authentication", "│▌Authentication  ▸ 2                  /repo/authentication         2026-06-01 │")
	assertDialogRow(t, rows, "Delete", "│ Delete \"Authentication\"? enter confirm                            esc cancel │")
	column, row := findTextCell(t, rows, "Delete \"")
	if cell := application.Cell(column, row); cell.Style.Foreground != ui.DefaultThemeSet().Dark.DangerText {
		t.Fatalf("delete prompt style = %+v, want danger text", cell.Style)
	}

	generation, _, ok := controller.BeginDeleteConfirm()
	if !ok {
		t.Fatal("delete confirm did not start")
	}
	_, _, rows = renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Sessions", "│ Sessions                                                         ⠋ deleting… │")
	assertDialogRow(t, rows, "Deleting", "│ Deleting \"Authentication\"…                                                   │")

	controller.ResolveDelete(generation, errors.New("session is busy"))
	_, _, rows = renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Delete failed", "│ Delete failed: session is busy · enter retry                      esc cancel │")

	controller.CancelDelete()
	controller.Select("session_other")
	controller.BeginDelete()
	_, _, rows = renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Cannot delete", "│ Cannot delete the attached session                                 esc close │")
}

func TestSessionExplorerRenameUsesThePromptInTheSameFrame(t *testing.T) {
	t.Parallel()
	controller := openExplorer("session_other", explorerFixture())
	controller.Select("session_root")
	if !controller.BeginRename() || controller.RenameText != "Authentication" {
		t.Fatalf("rename did not open with the current name: %+v", controller)
	}
	controller.SetRenameText("Auth v2")
	_, _, rows := renderExplorer(t, controller, 100, 24)
	_, inputRow := assertPickerSearchField(t, rows, "Auth v2")
	assertPickerTitleSpacing(t, rows, "Rename session", inputRow)
	assertDialogRow(t, rows, "Rename session", "│ Rename session                                                Authentication │")
	assertPickerFooter(t, rows, "enter save · esc cancel")

	controller.SetRenameText("Auth")
	generation, _, _, ok := controller.BeginRenameSave()
	if !ok {
		t.Fatal("rename save did not start")
	}
	_, _, rows = renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Rename session", "│ Rename session                                                     ⠋ saving… │")

	controller.ResolveRename(generation, protocol.SessionInfo{}, errors.New("offline"))
	_, _, rows = renderExplorer(t, controller, 100, 24)
	assertDialogRow(t, rows, "Rename failed", "│ Rename failed: offline                                                       │")
}

func TestSessionExplorerDisclosureClickTogglesAndRowClickActivates(t *testing.T) {
	t.Parallel()
	controller := openExplorer("session_other", explorerFixture())
	application, state, rows := renderExplorer(t, controller, 100, 24)
	column, row := findTextCell(t, rows, glyphTriangleRight+" 2")
	application.Click(column, row)
	application.Pump(100, 24)
	rows = paintedRows(application, 100, 24)
	assertDialogRow(t, rows, "Authentication", "│▌Authentication  ▾ 2                  /repo/authentication         2026-06-01 │")
	assertVisibleSessions(t, &state.controller, "session_root", "session_sibling", "session_child", "session_other")

	column, row = findTextCell(t, rows, "Release")
	application.Click(column, row)
	if got := strings.Join(state.activated, ","); got != "session_other" {
		t.Fatalf("activated = %q, want the clicked session", got)
	}
}

func TestSessionExplorerKeysRouteThroughThePickerModel(t *testing.T) {
	t.Parallel()
	explorer := openExplorer("session_other", explorerFixture())
	c := &explorer
	press := func(key ui.Key) pickerKeyResult {
		t.Helper()
		return c.HandleKey(key)
	}
	// Up and Down wrap; page keys no longer move.
	press(ui.Key{Keycode: vaxis.KeyDown})
	if c.Selection != "session_root" {
		t.Fatalf("down from the last row = %s, want wrap to the first", c.Selection)
	}
	if result := press(ui.Key{Keycode: vaxis.KeyPgDown}); !result.Handled || c.Selection != "session_root" {
		t.Fatalf("page down = %+v selection %s, want swallowed", result, c.Selection)
	}
	// Left and Right fold the tree while the query is empty.
	press(ui.Key{Keycode: vaxis.KeyRight})
	assertVisibleSessions(t, c, "session_root", "session_sibling", "session_child", "session_other")
	press(ui.Key{Keycode: vaxis.KeyLeft})
	assertVisibleSessions(t, c, "session_root", "session_other")

	// Typing filters and highlights the first match.
	for _, character := range "tok" {
		press(ui.Key{Keycode: character, Text: string(character)})
	}
	if c.Query != "tok" || c.Selection != "session_sibling" {
		t.Fatalf("typed query = %q selection = %s", c.Query, c.Selection)
	}
	assertVisibleSessions(t, c, "session_sibling")
	// With a query, Left and Right are swallowed and leave the tree alone.
	if result := press(ui.Key{Keycode: vaxis.KeyRight}); !result.Handled || c.Query != "tok" || c.expanded["session_sibling"] || c.Selection != "session_sibling" {
		t.Fatalf("right with a query = %+v controller %+v", result, c)
	}
	// Clearing the query restores the nearest visible ancestor.
	for range 3 {
		press(ui.Key{Keycode: vaxis.KeyBackspace})
	}
	if c.Query != "" || c.Selection != "session_root" {
		t.Fatalf("cleared query = %q selection = %s", c.Query, c.Selection)
	}
	assertVisibleSessions(t, c, "session_root", "session_other")

	if result := press(ui.Key{Keycode: vaxis.KeyEnter}); !result.Activate || c.Selection != "session_root" {
		t.Fatalf("enter = %+v", result)
	}
	if result := press(ui.Key{Keycode: vaxis.KeyEsc}); !result.Dismiss {
		t.Fatalf("escape = %+v", result)
	}
	// Explorer shortcuts run after the model leaves modified keys unhandled.
	if result := press(ui.Key{Keycode: 'r', Modifiers: vaxis.ModCtrl}); !result.Handled || !c.RenameOpen || c.RenameSessionID != "session_root" {
		t.Fatalf("ctrl+r = %+v controller %+v", result, c)
	}
	if result := press(ui.Key{Keycode: 'x', Text: "x"}); !result.Handled || c.Query != "" {
		t.Fatalf("typing during rename = %+v query %q", result, c.Query)
	}
	c.CancelRename()
	if result := press(ui.Key{Keycode: 'd', Modifiers: vaxis.ModCtrl}); !result.Handled || !c.DeleteOpen || c.DeleteSessionID != "session_root" {
		t.Fatalf("ctrl+d = %+v controller %+v", result, c)
	}
	c.CancelDelete()
	if result := press(ui.Key{Keycode: 'k', Modifiers: vaxis.ModCtrl}); result.Handled {
		t.Fatalf("unrelated shortcut = %+v, want it left for the app", result)
	}
}

type sessionExplorerAppHarness struct{ state *sessionExplorerAppState }

func (w sessionExplorerAppHarness) CreateState() ui.State { return w.state }

type sessionExplorerAppState struct {
	appState
	scroll ui.ScrollController
}

func (*sessionExplorerAppState) InitState() {}
func (*sessionExplorerAppState) Dispose()   {}
func (s *sessionExplorerAppState) HandleEvent(ctx ui.EventContext, event ui.Event) ui.EventResult {
	return s.appState.HandleEvent(ctx, event)
}
func (s *sessionExplorerAppState) Build(ui.BuildContext) ui.Widget {
	s.reconcileInputOwner()
	s.renderedInput = s.inputToken()
	return shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Session: s.session, Scroll: &s.scroll, SessionExplorer: s.sessionExplorer.Snapshot(),
		},
		Callbacks: shellCallbacks{
			InputOwner:          s.inputOwner,
			SessionQueryChanged: func(_ ui.EventContext, value string) { s.SetState(func() { s.sessionExplorer.SetQuery(value) }) },
			Dismiss:             s.dismiss,
		},
	}
}

func TestSessionExplorerAppAppliesKeysTypedBeforePaintAndCatalog(t *testing.T) {
	t.Parallel()
	state := &sessionExplorerAppState{appState: appState{phase: phaseReady, session: protocol.SessionInfo{ID: "session_other", Name: "Release"}}}
	backend := &themePickerBackend{events: make(chan ui.Event), dispatches: make(chan func(), 8)}
	runner := ui.NewRunner(ui.NewApp(sessionExplorerAppHarness{state: state}), backend, ui.NewFrameScheduler(time.Second/60))
	application := &themePickerTestApp{runner: runner, backend: backend, now: time.Now()}
	runner.Start(application.now)
	application.pump(t)

	// Open the explorer and type before it paints or its catalog arrives.
	var generation uint64
	state.SetState(func() { generation = state.sessionExplorer.Begin("session_other") })
	application.key("rel")
	application.send(vaxis.Key{Keycode: vaxis.KeyDown, EventType: vaxis.EventPress})
	if state.sessionExplorer.Query != "rel" {
		t.Fatalf("pre-paint query = %q", state.sessionExplorer.Query)
	}
	application.pump(t)
	rows := application.rows()
	assertDialogRow(t, rows, "│ rel", "│ rel                                                          │")
	assertDialogRow(t, rows, "Loading", "│ ⠋ Loading sessions…                                          │")

	state.SetState(func() { state.sessionExplorer.Resolve(generation, explorerFixture(), nil) })
	application.pump(t)
	rows = application.rows()
	if state.sessionExplorer.Selection != "session_other" {
		t.Fatalf("selection after catalog = %s, want the first match", state.sessionExplorer.Selection)
	}
	assertDialogRow(t, rows, "/repo/release", "│▌Release                          /repo/release    2026-06-04 │")

	// Ctrl+r opens the rename prompt in the same frame; Esc returns.
	application.send(vaxis.Key{Keycode: 'r', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPress})
	application.pump(t)
	assertDialogRow(t, application.rows(), "Rename session", "│ Rename session                                       Release │")
	application.send(vaxis.Key{Keycode: vaxis.KeyEsc, EventType: vaxis.EventPress})
	application.pump(t)
	if state.sessionExplorer.RenameOpen || !state.sessionExplorer.Open {
		t.Fatalf("escape from rename = %+v", state.sessionExplorer.Snapshot())
	}

	// Enter on the attached session closes the explorer; nothing to switch.
	application.send(vaxis.Key{Keycode: vaxis.KeyEnter, EventType: vaxis.EventPress})
	application.pump(t)
	if state.sessionExplorer.Open {
		t.Fatalf("enter on the attached session left the explorer open: %+v", state.sessionExplorer.Snapshot())
	}
}
