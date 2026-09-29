package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
	"go.rockorager.dev/vaxis/widgets/term"
)

func TestSessionPickerHeightIsBoundedByContent(t *testing.T) {
	t.Parallel()
	if got := sessionPickerHeight(sessionExplorerSnapshot{Loading: true}); got != sessionPickerMinHeight {
		t.Fatalf("loading height = %d", got)
	}
	if got := sessionPickerHeight(sessionExplorerSnapshot{Sessions: make([]sessionExplorerItem, 8)}); got != 8+sessionPickerChromeRows {
		t.Fatalf("content height = %d", got)
	}
	if got := sessionPickerHeight(sessionExplorerSnapshot{Sessions: make([]sessionExplorerItem, 40)}); got != sessionPickerMaxHeight {
		t.Fatalf("bounded height = %d", got)
	}
}

func TestSessionPickerCleanupRemovesLiveRegionAndPreservesPriorOutput(t *testing.T) {
	t.Parallel()
	terminal := term.New()
	terminal.Resize(20, 8)
	terminal.WriteString("before\r\npicker 1\r\npicker 2\r\npicker 3\r\n\r")
	if err := writeSessionPickerCleanup(terminal, 3); err != nil {
		t.Fatalf("writeSessionPickerCleanup() error = %v", err)
	}
	terminal.WriteString("after")

	want := []string{"before", "after", "", "", "", "", "", ""}
	for row, expected := range want {
		if got := strings.TrimRight(terminal.RowString(row), " "); got != expected {
			t.Fatalf("row %d = %q, want %q", row, got, expected)
		}
	}
}

func TestStandaloneSessionPickerCanDeleteAnyListedSession(t *testing.T) {
	t.Parallel()
	controller := sessionExplorerController{}
	generation := controller.Begin("")
	controller.Resolve(generation, []sessionExplorerItem{{ID: "session_target", Name: "Target"}}, nil)
	if !controller.BeginDelete() || !controller.DeleteOpen {
		t.Fatalf("standalone delete = %+v", controller)
	}
}

func TestStandaloneSessionPickerSelectsAndCancelsFromFocusedRoot(t *testing.T) {
	t.Parallel()
	const sessionID = "session_0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name      string
		key       ui.Key
		selection string
	}{
		{name: "select", key: ui.Key{Keycode: vaxis.KeyEnter}, selection: sessionID},
		{name: "cancel", key: ui.Key{Keycode: vaxis.KeyEsc}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := &sessionPickerResult{}
			application := uitest.New(sessionPicker{
				Options:         SessionPickerOptions{Context: context.Background(), Server: &fakeServer{}},
				Result:          result,
				initialSet:      true,
				initialSessions: []protocol.SessionInfo{{ID: sessionID, Name: "Session"}},
			})
			application.Pump(80, sessionPickerMinHeight)
			application.Send(test.key)
			if !application.ShouldQuit() || result.selectedSession() != test.selection {
				t.Fatalf("quit = %t selection = %q", application.ShouldQuit(), result.selectedSession())
			}
		})
	}
}

func TestStandaloneSessionPickerClickOpensSession(t *testing.T) {
	t.Parallel()
	const second = "session_22222222222222222222222222222222"
	result := &sessionPickerResult{}
	application := uitest.New(sessionPicker{
		Options:    SessionPickerOptions{Context: context.Background(), Server: &fakeServer{}},
		Result:     result,
		initialSet: true,
		initialSessions: []protocol.SessionInfo{
			{ID: "session_11111111111111111111111111111111", Name: "First"},
			{ID: second, Name: "Second"},
		},
	})
	application.Pump(80, sessionPickerMinHeight)
	application.Pump(80, sessionPickerMinHeight)
	column, row := findTextCell(t, paintedRows(application, 80, sessionPickerMinHeight), "Second")
	application.Click(column, row)
	if !application.ShouldQuit() || result.selectedSession() != second {
		t.Fatalf("quit = %t selection = %q", application.ShouldQuit(), result.selectedSession())
	}
}

func TestStandaloneSessionPickerRendersTheCanonicalPicker(t *testing.T) {
	t.Parallel()
	now := time.Now()
	result := &sessionPickerResult{}
	application := uitest.New(sessionPicker{
		Options:    SessionPickerOptions{Context: context.Background(), Server: &fakeServer{}},
		Result:     result,
		initialSet: true,
		initialSessions: []protocol.SessionInfo{
			{ID: "session_parent", Name: "Parent", CWD: "/repo", UpdatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339Nano)},
			{ID: "session_child", Name: "Child", CWD: "/repo/sub", ParentSessionID: "session_parent", UpdatedAt: now.Add(-3 * time.Hour).Format(time.RFC3339Nano)},
		},
	})
	const width = 120
	height := sessionPickerMinHeight + 1
	application.Pump(width, height)
	application.Pump(width, height)
	rows := paintedRows(application, width, height)
	if top := findPaintedRow(rows, "┌"); top != 0 {
		t.Fatalf("picker top row = %d, want the region's first row:\n%s", top, strings.Join(rows, "\n"))
	}
	_, searchRow := assertPickerSearchField(t, rows, "Search sessions…")
	assertPickerTitleSpacing(t, rows, "Sessions", searchRow)
	assertDialogRow(t, rows, "Sessions", "│ Sessions                                                                          2 sessions │")
	assertDialogRow(t, rows, "Parent", "│▌Parent   ▸ 1  /repo                                                                   2h ago │")
	assertPickerFooter(t, rows, "←→ expand · enter open · ctrl+r rename · ctrl+d delete")

	application.Send(ui.Key{Keycode: vaxis.KeyRight})
	application.Pump(width, height)
	rows = paintedRows(application, width, height)
	assertDialogRow(t, rows, "Parent", "│▌Parent   ▾ 1  /repo                                                                   2h ago │")
	assertDialogRow(t, rows, "Child", "│   Child       /repo/sub                                                               3h ago │")
}
