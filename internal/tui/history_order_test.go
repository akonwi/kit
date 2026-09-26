package tui

import (
	"fmt"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestMessageHistoryPickerChronologicalOrderAndNavigation(t *testing.T) {
	h := &messageHistoryController{}
	if !h.OpenFor([]messageHistoryEntry{
		{ID: "new", Text: "newest prompt"},
		{ID: "middle", Text: "middle prompt"},
		{ID: "old", Text: "oldest prompt"},
	}) {
		t.Fatal("history did not open")
	}
	assertMessageHistoryRows(t, h, []string{"oldest prompt", "middle prompt", "newest prompt"}, "newest prompt")
	h.Move(1) // Down at the newest boundary does not wrap.
	assertMessageHistoryRows(t, h, []string{"oldest prompt", "middle prompt", "newest prompt"}, "newest prompt")
	h.Move(-1)
	assertMessageHistoryRows(t, h, []string{"oldest prompt", "middle prompt", "newest prompt"}, "middle prompt")
	h.Move(-1)
	h.Move(-1) // Up at the oldest boundary does not wrap.
	assertMessageHistoryRows(t, h, []string{"oldest prompt", "middle prompt", "newest prompt"}, "oldest prompt")
	h.Move(1)
	assertMessageHistoryRows(t, h, []string{"oldest prompt", "middle prompt", "newest prompt"}, "middle prompt")
}

func TestMessageHistoryPickerFilteredChronologicalWindow(t *testing.T) {
	entries := make([]messageHistoryEntry, 0, 14)
	for i := 14; i >= 1; i-- {
		entries = append(entries, messageHistoryEntry{ID: fmt.Sprint(i), Text: fmt.Sprintf("prompt %02d", i)})
	}
	h := &messageHistoryController{}
	h.OpenFor(entries)
	assertMessageHistoryRows(t, h, []string{"prompt 05", "prompt 06", "prompt 07", "prompt 08", "prompt 09", "prompt 10", "prompt 11", "prompt 12", "prompt 13", "prompt 14"}, "prompt 14")
	for range 10 {
		h.Move(-1)
	}
	assertMessageHistoryRows(t, h, []string{"prompt 01", "prompt 02", "prompt 03", "prompt 04", "prompt 05", "prompt 06", "prompt 07", "prompt 08", "prompt 09", "prompt 10"}, "prompt 04")

	// Fuzzy scoring favors an exact match, but the visible order stays chronological.
	h = &messageHistoryController{}
	h.OpenFor([]messageHistoryEntry{{ID: "new", Text: "git status"}, {ID: "old", Text: "git"}})
	h.SetQuery("git")
	assertMessageHistoryRows(t, h, []string{"git", "git status"}, "git status")
	h.SetQuery("status")
	assertMessageHistoryRows(t, h, []string{"git status"}, "git status")
	h.SetQuery("unmatched")
	if h.Selection != "" || len(h.filtered()) != 0 {
		t.Fatalf("unmatched message query selected %q from %+v", h.Selection, h.filtered())
	}
	h.SetQuery("git")
	assertMessageHistoryRows(t, h, []string{"git", "git status"}, "git status")
}

func TestBashHistoryPickerChronologicalOrderAndPaging(t *testing.T) {
	h := &bashHistoryController{}
	h.OpenFor([]bashHistoryEntry{
		{ID: "new", Command: "echo newest"},
		{ID: "middle", Command: "echo middle", ExcludeFromContext: true},
		{ID: "old", Command: "echo oldest"},
	}, "!")
	h.Loading = false
	h.HasMore = true
	loads := 0
	h.OnExhausted = func() { loads++ }
	assertBashHistoryRows(t, h, []string{"echo oldest", "echo middle", "echo newest"}, "echo newest")
	h.Move(1)
	assertBashHistoryRows(t, h, []string{"echo oldest", "echo middle", "echo newest"}, "echo newest")
	h.Move(-1)
	assertBashHistoryRows(t, h, []string{"echo oldest", "echo middle", "echo newest"}, "echo middle")
	h.Move(-1)
	h.Move(-1) // Up from the oldest loaded row requests more, without wrapping.
	if loads != 1 {
		t.Fatalf("older-page requests = %d, want 1", loads)
	}
	assertBashHistoryRows(t, h, []string{"echo oldest", "echo middle", "echo newest"}, "echo oldest")
	h.MergeOlder([]bashHistoryEntry{{ID: "older", Command: "echo even older"}}, 0, false)
	assertBashHistoryRows(t, h, []string{"echo even older", "echo oldest", "echo middle", "echo newest"}, "echo oldest")
	h.Move(-1)
	assertBashHistoryRows(t, h, []string{"echo even older", "echo oldest", "echo middle", "echo newest"}, "echo even older")
	h.Move(-1)
	if loads != 1 {
		t.Fatalf("older-page requests after exhaustion = %d, want 1", loads)
	}
	h.Move(1)
	assertBashHistoryRows(t, h, []string{"echo even older", "echo oldest", "echo middle", "echo newest"}, "echo oldest")
}

func TestBashHistoryPickerFilteredOlderPage(t *testing.T) {
	h := &bashHistoryController{}
	h.OpenFor([]bashHistoryEntry{{ID: "new", Command: "git status"}, {ID: "old", Command: "git log"}}, "!")
	h.SetQuery("git")
	h.Loading = false
	h.HasMore = true
	loads := 0
	h.OnExhausted = func() { loads++ }
	h.Move(-1)
	h.Move(-1)
	if loads != 1 || h.Selection != "old" {
		t.Fatalf("filtered older-page request = %d, selected = %q", loads, h.Selection)
	}
	h.MergeOlder([]bashHistoryEntry{{ID: "older", Command: "git diff"}, {ID: "unmatched", Command: "echo hi"}}, 0, false)
	h.Move(-1)
	assertBashHistoryRows(t, h, []string{"git diff", "git log", "git status"}, "git diff")
}

func TestBashHistoryPickerLoadsOlderWhenNoLoadedRowMatches(t *testing.T) {
	h := &bashHistoryController{}
	h.OpenFor([]bashHistoryEntry{{ID: "new", Command: "echo hello"}}, "!")
	h.SetQuery("git")
	h.Loading = false
	h.HasMore = true
	loads := 0
	h.OnExhausted = func() { loads++ }
	h.Move(-1)
	if loads != 1 {
		t.Fatalf("older-page requests for unmatched query = %d, want 1", loads)
	}
	h.MergeOlder([]bashHistoryEntry{{ID: "old", Command: "git status"}}, 0, false)
	// The new match becomes selected, even though the previous page had none.
	assertBashHistoryRows(t, h, []string{"git status"}, "git status")
}

func TestBashHistoryPickerFilteredChronologicalWindow(t *testing.T) {
	entries := make([]bashHistoryEntry, 0, 14)
	for i := 14; i >= 1; i-- {
		entries = append(entries, bashHistoryEntry{ID: fmt.Sprint(i), Command: fmt.Sprintf("echo %02d", i)})
	}
	h := &bashHistoryController{}
	h.OpenFor(entries, "!")
	assertBashHistoryRows(t, h, []string{"echo 05", "echo 06", "echo 07", "echo 08", "echo 09", "echo 10", "echo 11", "echo 12", "echo 13", "echo 14"}, "echo 14")
	for range 10 {
		h.Move(-1)
	}
	assertBashHistoryRows(t, h, []string{"echo 01", "echo 02", "echo 03", "echo 04", "echo 05", "echo 06", "echo 07", "echo 08", "echo 09", "echo 10"}, "echo 04")

	h = &bashHistoryController{}
	h.OpenFor([]bashHistoryEntry{{ID: "new", Command: "git status"}, {ID: "old", Command: "git"}}, "!")
	h.SetQuery("git")
	assertBashHistoryRows(t, h, []string{"git", "git status"}, "git status")
	h.SetQuery("status")
	assertBashHistoryRows(t, h, []string{"git status"}, "git status")
	h.SetQuery("unmatched")
	if h.Selection != "" || len(h.filtered()) != 0 {
		t.Fatalf("unmatched bash query selected %q from %+v", h.Selection, h.filtered())
	}
	h.SetQuery("git")
	assertBashHistoryRows(t, h, []string{"git", "git status"}, "git status")
}

func assertMessageHistoryRows(t *testing.T, h *messageHistoryController, want []string, selected string) {
	t.Helper()
	const width, height = 80, 24
	theme := ui.DefaultTheme()
	app := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: messageHistorySurface{Controller: h, PrimaryPercent: 100}})
	app.Pump(width, height)
	assertHistoryPaintedRows(t, app, width, height, want, selected, theme.Selection)
}

func assertBashHistoryRows(t *testing.T, h *bashHistoryController, want []string, selected string) {
	t.Helper()
	const width, height = 80, 24
	theme := ui.DefaultTheme()
	app := uitest.New(ui.Provider[ui.Theme]{Value: theme, Child: bashHistorySurface{Controller: h, Composer: "!", PrimaryPercent: 100}})
	app.Pump(width, height)
	assertHistoryPaintedRows(t, app, width, height, want, selected, theme.Selection)
}

func assertHistoryPaintedRows(t *testing.T, app *uitest.App, width, height int, want []string, selected string, selectedBackground ui.Color) {
	t.Helper()
	rows := paintedRows(app, width, height)
	previous := -1
	for _, label := range want {
		row := findPaintedRow(rows, label)
		if row <= previous {
			t.Fatalf("history row %q at %d follows row %d; want %v in order:\n%s", label, row, previous, want, strings.Join(rows, "\n"))
		}
		previous = row
		column := strings.Index(rows[row], label)
		if column < 0 {
			t.Fatalf("missing history label %q in row %d: %q", label, row, rows[row])
		}
		background := app.Cell(column, row).Style.Background
		if label == selected && background != selectedBackground {
			t.Fatalf("selected row %q background = %v, want %v", label, background, selectedBackground)
		}
	}
}
