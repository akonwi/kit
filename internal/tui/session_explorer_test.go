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
	if controller.Selection != "session_newest" {
		t.Fatalf("selection moved above first row = %q", controller.Selection)
	}
	controller.Move(1)
	if controller.Selection != "session_current" {
		t.Fatalf("selection after Down = %q", controller.Selection)
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

func TestSessionExplorerControllerHandlesRapidNavigationAndConsumesModalInput(t *testing.T) {
	t.Parallel()

	controller := sessionExplorerController{}
	generation := controller.Begin("session_00")
	sessions := make([]sessionExplorerItem, 20)
	for index := range sessions {
		sessions[index] = sessionExplorerItem{
			ID:        "session_" + fmt.Sprintf("%02d", index),
			UpdatedAt: time.Date(2026, time.June, 5, 12, 0, index, 0, time.UTC).Format(time.RFC3339Nano),
		}
	}
	controller.Resolve(generation, sessions, nil)
	controller.Selection = controller.Sessions[0].ID
	if !controller.HandleKey(ui.Key{Keycode: vaxis.KeyPgDown}) || controller.Selection != controller.Sessions[sessionExplorerMaxVisible].ID {
		t.Fatalf("PageDown selection = %q", controller.Selection)
	}
	if !controller.HandleKey(ui.Key{Text: "j", Keycode: 'j'}) || controller.Selection != controller.Sessions[sessionExplorerMaxVisible+1].ID {
		t.Fatalf("j selection = %q", controller.Selection)
	}
	if !controller.HandleKey(ui.Key{Text: "x", Keycode: 'x'}) {
		t.Fatal("modal text input was not consumed")
	}
	if controller.HandleKey(ui.Key{Keycode: vaxis.KeyEsc}) {
		t.Fatal("Escape should remain available to the root dismiss intent")
	}
}

func TestSessionExplorerRowsRevealMetadataAtResponsiveWidths(t *testing.T) {
	t.Parallel()

	updated := time.Now().Add(-2*time.Hour - 5*time.Minute).Format(time.RFC3339Nano)
	row := sessionExplorerRow{Session: sessionExplorerItem{
		ID: "session_0123456789abcdef", Name: "Named session", CWD: "/tmp/project", UpdatedAt: updated,
	}}
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 39, want: fmt.Sprintf("%-39s", "  Named session")},
		{width: 40, want: fmt.Sprintf("%-29s %10s", "  Named session", "2h ago")},
		{width: 68, want: fmt.Sprintf("%-24s %10s %32s", "  Named session", "2h ago", "/tmp/project")},
		{width: 76, want: fmt.Sprintf("%-24s %10s %40s", "  Named session", "2h ago", "/tmp/project")},
		{width: 104, want: fmt.Sprintf("%-43s %10s %40s %8s", "  Named session", "2h ago", "/tmp/project", "01234567")},
	} {
		t.Run(fmt.Sprintf("width_%d", test.width), func(t *testing.T) {
			application := uitest.New(row)
			application.Pump(test.width, 1)
			got := paintedRows(application, test.width, 1)[0]
			if got != test.want {
				t.Fatalf("row = %q\nwant  %q", got, test.want)
			}
		})
	}
}

func TestSessionExplorerRowsKeepMixedLabelsAlignedAndReserveScrollbarInsideHighlight(t *testing.T) {
	t.Parallel()

	updated := time.Now().Add(-2*time.Hour - 5*time.Minute).Format(time.RFC3339Nano)
	layout := &pickerDialogLayoutState{AvailableRows: 1}
	rowsWidget := ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		sessionExplorerRow{
			Session:  sessionExplorerItem{ID: "session_named0000", Name: "Named", CWD: "/tmp/project", UpdatedAt: updated},
			Selected: true, SessionCount: 2, Layout: layout,
		},
		sessionExplorerRow{
			Session:      sessionExplorerItem{ID: "session_unnamed00", CWD: "/tmp/project", UpdatedAt: updated},
			SessionCount: 2, Layout: layout,
		},
	}}
	application := uitest.New(rowsWidget)
	application.Pump(104, 2)
	rows := paintedRows(application, 104, 2)
	if namedTime, unnamedTime := strings.Index(rows[0], "2h ago"), strings.Index(rows[1], "2h ago"); namedTime != unnamedTime {
		t.Fatalf("mixed timestamp columns = %d and %d:\n%s", namedTime, unnamedTime, strings.Join(rows, "\n"))
	}
	if namedCWD, unnamedCWD := strings.Index(rows[0], "/tmp/project"), strings.Index(rows[1], "/tmp/project"); namedCWD != unnamedCWD {
		t.Fatalf("mixed cwd columns = %d and %d:\n%s", namedCWD, unnamedCWD, strings.Join(rows, "\n"))
	}
	if application.Cell(0, 0).Style.Background != application.Cell(103, 0).Style.Background {
		t.Fatal("scrollbar reserve escaped the selected-row highlight")
	}
}

func TestSessionExplorerPresentationShowsCurrentSessionAndStableDialog(t *testing.T) {
	t.Parallel()

	now := time.Now()
	sessions := []protocol.SessionInfo{
		{ID: "session_0123456789abcdef", Name: "Current session", CWD: "/workspace/Developer/agent/kit-v2", UpdatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339Nano)},
		{ID: "session_fedcba9876543210", Name: "Other workspace", CWD: "/tmp/other", UpdatedAt: now.Add(-24 * time.Hour).Format(time.RFC3339Nano)},
	}
	selected := ""
	application := uitest.New(shellView{
		Snapshot: shellSnapshot{
			Phase: phaseReady, Session: sessions[0], Scroll: &ui.ScrollController{},
			SessionExplorer: sessionExplorerSnapshot{
				Open: true, Sessions: projectSessionExplorerItems(sessions), Selection: sessions[0].ID, CurrentSessionID: sessions[0].ID,
				Scroll: &ui.ScrollController{},
			},
		},
		Callbacks: shellCallbacks{SelectSession: func(_ ui.EventContext, sessionID string) { selected = sessionID }},
	})
	application.Pump(140, 24)
	rows := paintedRows(application, 140, 24)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		"Session Explorer", "2 sessions", "✓ Current session", "Other workspace",
		"/workspace/Developer/agent/kit-v2", "01234567", "fedcba98",
		"↑↓ move · page up/down · enter switch · esc close",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("session explorer missing %q:\n%s", expected, text)
		}
	}
	left, right, top := sessionDialogBorder(rows)
	bottom := dialogBottom(rows)
	if right-left+1 != 119 || top != 4 || bottom-top+1 != pickerModalMinHeight {
		t.Fatalf("dialog geometry left=%d right=%d top=%d bottom=%d", left, right, top, bottom)
	}
	assertPickerFooter(t, rows, "↑↓ move · page up/down · enter switch · esc close")
	currentColumn, currentRow := findTextCell(t, rows, "✓ Current session")
	currentColumn += len([]rune("✓ "))
	otherColumn, otherRow := findTextCell(t, rows, "Other workspace")
	selectedBackground := application.Cell(currentColumn, currentRow).Style.Background
	if selectedBackground == application.Cell(otherColumn, otherRow).Style.Background {
		t.Fatalf("selected session row is not visually distinct: current=%+v other=%+v", application.Cell(currentColumn, currentRow).Style, application.Cell(otherColumn, otherRow).Style)
	}
	selectedLeft, selectedRight := currentColumn, currentColumn
	for selectedLeft > 0 && application.Cell(selectedLeft-1, currentRow).Style.Background == selectedBackground {
		selectedLeft--
	}
	for selectedRight+1 < 140 && application.Cell(selectedRight+1, currentRow).Style.Background == selectedBackground {
		selectedRight++
	}
	if leftGap, rightGap := selectedLeft-left, right-selectedRight; leftGap != 2 || rightGap != leftGap {
		t.Fatalf("selected row horizontal gaps = %d left and %d right", leftGap, rightGap)
	}
	application.Click(otherColumn, otherRow)
	if selected != sessions[1].ID {
		t.Fatalf("dialog row click selected %q", selected)
	}
}

func TestSessionExplorerSharesCommandPaletteTopPlacement(t *testing.T) {
	t.Parallel()

	const width, height = 100, 24
	palette := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, PaletteOpen: true, Scroll: &ui.ScrollController{},
	}})
	palette.Pump(width, height)
	_, _, paletteTop := paletteBorder(paintedRows(palette, width, height))

	explorer := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Scroll: &ui.ScrollController{},
		SessionExplorer: sessionExplorerSnapshot{Open: true, Loading: true},
	}})
	explorer.Pump(width, height)
	_, _, explorerTop := sessionDialogBorder(paintedRows(explorer, width, height))

	if paletteTop != 4 || explorerTop != paletteTop {
		t.Fatalf("picker modal tops: palette=%d explorer=%d, want shared row 4", paletteTop, explorerTop)
	}
}

func TestSessionExplorerPresentationHasExplicitLoadingErrorAndEmptyStates(t *testing.T) {
	t.Parallel()

	states := []struct {
		name     string
		snapshot sessionExplorerSnapshot
		want     string
	}{
		{name: "loading", snapshot: sessionExplorerSnapshot{Open: true, Loading: true}, want: "Loading sessions…"},
		{name: "error", snapshot: sessionExplorerSnapshot{Open: true, Error: "offline"}, want: "Could not load sessions."},
		{name: "empty", snapshot: sessionExplorerSnapshot{Open: true}, want: "No sessions"},
	}
	for _, test := range states {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(shellView{Snapshot: shellSnapshot{
				Phase: phaseReady, Scroll: &ui.ScrollController{}, SessionExplorer: test.snapshot,
			}})
			application.Pump(80, 16)
			text := strings.Join(paintedRows(application, 80, 16), "\n")
			if !strings.Contains(text, test.want) {
				t.Fatalf("%s state =\n%s", test.name, text)
			}
			assertPickerFooter(t, paintedRows(application, 80, 16), "esc close")
		})
	}
}

func TestSessionExplorerPresentationCommunicatesSwitchProgressAndRetry(t *testing.T) {
	t.Parallel()

	base := sessionExplorerSnapshot{
		Open: true, Sessions: []sessionExplorerItem{{ID: "session_target", Name: "Target"}}, Selection: "session_target",
	}
	for _, test := range []struct {
		name     string
		snapshot sessionExplorerSnapshot
		want     []string
	}{
		{name: "switching", snapshot: func() sessionExplorerSnapshot {
			snapshot := base
			snapshot.Switching = true
			return snapshot
		}(), want: []string{"⠋ Switching…", "esc cancel"}},
		{name: "retry", snapshot: func() sessionExplorerSnapshot {
			snapshot := base
			snapshot.SwitchError = "offline"
			return snapshot
		}(), want: []string{"Switch failed", "Switch failed: offline", "enter retry · esc close"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(shellView{Snapshot: shellSnapshot{
				Phase: phaseReady, Scroll: &ui.ScrollController{}, SessionExplorer: test.snapshot,
			}})
			application.Pump(100, 24)
			text := strings.Join(paintedRows(application, 100, 24), "\n")
			for _, want := range test.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s state missing %q:\n%s", test.name, want, text)
				}
			}
		})
	}

	narrow := base
	narrow.SwitchError = strings.Repeat("connection unavailable ", 8)
	application := uitest.New(shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Scroll: &ui.ScrollController{}, SessionExplorer: narrow,
	}})
	application.Pump(44, 24)
	assertPickerFooter(t, paintedRows(application, 44, 24), "enter retry · esc close")
}

func TestSessionExplorerKeepsSelectionAndChromeVisibleInShortViewport(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	controller := sessionExplorerController{}
	generation := controller.Begin("session_11")
	items := make([]sessionExplorerItem, 12)
	for index := range items {
		items[index] = sessionExplorerItem{
			ID: "session_" + fmt.Sprintf("%02d", index), Name: fmt.Sprintf("Session %02d", index),
			CWD: "/tmp", UpdatedAt: now.Add(-time.Duration(index) * time.Minute).Format(time.RFC3339Nano),
		}
	}
	controller.Resolve(generation, items, nil)
	state := &sessionExplorerHarnessState{controller: controller}
	application := uitest.New(sessionExplorerHarness{State: state})
	for range 6 {
		application.Pump(80, 7)
		state.TickFrame(time.Now())
	}
	application.Pump(80, 7)
	rows := paintedRows(application, 80, 7)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{"Session Explorer", "✓ Session 11", "↑↓ move · page up/down · enter switch · esc close"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("short explorer missing %q (reveal=%t layout=%t attached=%t metrics=%+v):\n%s", expected, state.controller.needsReveal, state.controller.revealPendingLayout, state.controller.scroll.Attached(), state.controller.scroll.Metrics(), text)
		}
	}
	if !strings.Contains(rows[0], "┌") || !strings.Contains(rows[len(rows)-1], "└") {
		t.Fatalf("short explorer chrome =\n%s", text)
	}
}

func TestSessionExplorerRevealsSelectionAfterViewportShrinks(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	controller := sessionExplorerController{}
	generation := controller.Begin("session_11")
	items := make([]sessionExplorerItem, 12)
	for index := range items {
		items[index] = sessionExplorerItem{
			ID: "session_" + fmt.Sprintf("%02d", index), Name: fmt.Sprintf("Session %02d", index),
			UpdatedAt: now.Add(-time.Duration(index) * time.Minute).Format(time.RFC3339Nano),
		}
	}
	controller.Resolve(generation, items, nil)
	state := &sessionExplorerHarnessState{controller: controller}
	application := uitest.New(sessionExplorerHarness{State: state})
	pumpSessionExplorerFrames(application, state, 80, 19, 5)
	pumpSessionExplorerFrames(application, state, 80, 8, 6)
	text := strings.Join(paintedRows(application, 80, 8), "\n")
	if !strings.Contains(text, "✓ Session 11") || !strings.Contains(text, "esc close") {
		t.Fatalf("resized explorer lost selection or chrome:\n%s", text)
	}
}

func pumpSessionExplorerFrames(application *uitest.App, state *sessionExplorerHarnessState, width, height, frames int) {
	for range frames {
		application.Pump(width, height)
		state.TickFrame(time.Now())
	}
	application.Pump(width, height)
}

func TestSessionExplorerRestoresSelectionAfterZeroBodyResize(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	controller := sessionExplorerController{}
	generation := controller.Begin("session_25")
	items := make([]sessionExplorerItem, 30)
	for index := range items {
		items[index] = sessionExplorerItem{
			ID: "session_" + fmt.Sprintf("%02d", index), Name: fmt.Sprintf("Session %02d", index),
			UpdatedAt: now.Add(-time.Duration(index) * time.Minute).Format(time.RFC3339Nano),
		}
	}
	controller.Resolve(generation, items, nil)
	state := &sessionExplorerHarnessState{controller: controller}
	application := uitest.New(sessionExplorerHarness{State: state})
	pumpSessionExplorerFrames(application, state, 80, 19, 6)
	pumpSessionExplorerFrames(application, state, 80, 6, 1)
	pumpSessionExplorerFrames(application, state, 80, 19, 6)
	text := strings.Join(paintedRows(application, 80, 19), "\n")
	if !strings.Contains(text, "✓ Session 25") || !strings.Contains(text, "esc close") {
		t.Fatalf("zero-body resize lost selection or chrome:\n%s", text)
	}
}

func TestSessionExplorerSuspendsRevealWhenViewportHasNoAvailableRows(t *testing.T) {
	t.Parallel()

	controller := sessionExplorerController{}
	generation := controller.Begin("session_current")
	controller.Resolve(generation, []sessionExplorerItem{{
		ID: "session_current", Name: "Current session", UpdatedAt: time.Now().Format(time.RFC3339Nano),
	}}, nil)
	state := &sessionExplorerHarnessState{controller: controller}
	application := uitest.New(sessionExplorerHarness{State: state})
	application.Pump(80, 6)
	if state.TickFrame(time.Now()) || state.controller.layout.AvailableRows != 0 {
		t.Fatalf("zero-body reveal kept ticking: body=%d reveal=%t", state.controller.layout.AvailableRows, state.controller.needsReveal)
	}
	rows := paintedRows(application, 80, 6)
	if !strings.Contains(rows[0], "┌") || !strings.Contains(rows[1], "Session Explorer") ||
		!strings.Contains(rows[len(rows)-2], "esc close") || !strings.Contains(rows[len(rows)-1], "└") {
		t.Fatalf("six-row explorer boundary =\n%s", strings.Join(rows, "\n"))
	}
}

func TestSessionExplorerUsesShortIDsForUnnamedSessions(t *testing.T) {
	t.Parallel()

	first := sessionExplorerItemLabel(sessionExplorerItem{ID: "session_0123456789abcdef"})
	second := sessionExplorerItemLabel(sessionExplorerItem{ID: "session_fedcba9876543210"})
	if first != "01234567" || second != "fedcba98" {
		t.Fatalf("unnamed labels = %q and %q", first, second)
	}
	row := sessionExplorerRow{Session: sessionExplorerItem{
		ID: "session_0123456789abcdef", CWD: "/tmp/project",
		UpdatedAt: time.Now().Add(-2*time.Hour - 5*time.Minute).Format(time.RFC3339Nano),
	}}
	application := uitest.New(row)
	application.Pump(104, 1)
	want := fmt.Sprintf("%-43s %10s %40s %8s", "  01234567", "2h ago", "/tmp/project", "")
	if got := paintedRows(application, 104, 1)[0]; got != want {
		t.Fatalf("unnamed row = %q\nwant          %q", got, want)
	}
}

func TestSessionExplorerCWDTruncationPreservesUnicodePathTailByCellWidth(t *testing.T) {
	t.Parallel()

	display := truncateStartCells("/tmp/資料/📁/a-significantly-longer-workspace-filename.go", sessionCWDExpandedWidth)
	width := (ui.LayoutContext{}).MeasureText(display, ui.Style{}).Width
	if width > sessionCWDExpandedWidth || !strings.HasPrefix(display, glyphEllipsis) || !strings.HasSuffix(display, "filename.go") {
		t.Fatalf("truncated cwd = %q (%d cells)", display, width)
	}
}

func TestSessionExplorerRowsSupportFullRowMouseSelection(t *testing.T) {
	t.Parallel()

	selected := ""
	row := sessionExplorerRow{
		Session:   sessionExplorerItem{ID: "session_target", Name: "Target session"},
		OnPressed: func(ui.EventContext) { selected = "session_target" },
	}
	application := uitest.New(row)
	application.Pump(60, 1)
	application.Click(50, 0)
	if selected != "session_target" {
		t.Fatalf("mouse selection = %q", selected)
	}
}

type sessionExplorerHarness struct{ State *sessionExplorerHarnessState }

func (w sessionExplorerHarness) CreateState() ui.State { return w.State }

type sessionExplorerHarnessState struct {
	ui.StateBase
	controller sessionExplorerController
}

func (s *sessionExplorerHarnessState) TickFrame(time.Time) bool {
	return s.controller.TickFrame()
}

func (s *sessionExplorerHarnessState) Build(ui.BuildContext) ui.Widget {
	return shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{ID: s.controller.CurrentSessionID},
		Scroll: &ui.ScrollController{}, SessionExplorer: s.controller.Snapshot(),
	}}
}

func sessionDialogBorder(rows []string) (int, int, int) {
	for rowIndex, row := range rows {
		left := strings.Index(row, "┌")
		right := strings.LastIndex(row, "┐")
		if left >= 0 && right >= left {
			return len([]rune(row[:left])), len([]rune(row[:right])), rowIndex
		}
	}
	return -1, -1, -1
}
