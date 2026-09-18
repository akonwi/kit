package tui

import (
	"context"
	"strings"
	"testing"

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
	if got := sessionPickerHeight(sessionExplorerSnapshot{Sessions: make([]sessionExplorerItem, 8)}); got != 14 {
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

func TestStandaloneSessionPickerMouseSelectsAcrossBoundedPositioner(t *testing.T) {
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
	application.Click(20, 4)
	application.Enter()
	if result.selectedSession() != second {
		t.Fatalf("mouse-selected session = %q", result.selectedSession())
	}
}

func TestStandaloneSessionPickerIsCenteredAndWidthBounded(t *testing.T) {
	t.Parallel()
	application := uitest.New(sessionPickerSurfaceHarness{})
	application.Pump(160, sessionPickerMinHeight)
	rows := paintedRows(application, 160, sessionPickerMinHeight)
	left, right := -1, -1
	for column := 0; column < 160; column++ {
		switch application.Cell(column, 0).Grapheme {
		case "┌":
			left = column
		case "┐":
			right = column
		}
	}
	if left != 20 || right-left+1 != 120 {
		t.Fatalf("picker geometry left=%d width=%d\n%s", left, right-left+1, strings.Join(rows, "\n"))
	}
}

func TestStandaloneSessionPickerUsesOpenActionHints(t *testing.T) {
	t.Parallel()
	got := sessionExplorerActionHintText(100, "open")
	want := "↑↓ move · page up/down · enter open · ctrl+r rename · ctrl+d delete · esc close"
	if got != want {
		t.Fatalf("standalone hints = %q, want %q", got, want)
	}
}

type sessionPickerSurfaceHarness struct{}

func (sessionPickerSurfaceHarness) Build(ctx ui.BuildContext) ui.Widget {
	snapshot := sessionExplorerSnapshot{
		Open: true, Sessions: []sessionExplorerItem{{ID: "session_0123456789abcdef", Name: "Session"}},
		Selection: "session_0123456789abcdef", Layout: &pickerDialogLayoutState{},
	}
	surface := sessionExplorerSurface{Snapshot: snapshot, Action: "open"}
	return boundedHorizontalCenter{
		Percent: 85, MinWidth: 44, MaxWidth: 120,
		Child: ui.SizedBox{Height: sessionPickerMinHeight, Child: surface.content(ctx)},
	}
}
