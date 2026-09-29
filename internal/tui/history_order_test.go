package tui

import (
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

var (
	historyUp   = ui.Key{Keycode: vaxis.KeyUp}
	historyDown = ui.Key{Keycode: vaxis.KeyDown}
)

// messageHistoryPickerLabels returns the rows the message history picker
// shows, top to bottom.
func messageHistoryPickerLabels(h messageHistoryController) []string {
	items := h.keys().Items(h.catalog())
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func assertHistoryPicker(t *testing.T, rows []string, selection string, wantRows []string, wantSelection string) {
	t.Helper()
	if strings.Join(rows, "|") != strings.Join(wantRows, "|") || selection != wantSelection {
		t.Fatalf("rows = %q selection = %q, want %q selecting %q", rows, selection, wantRows, wantSelection)
	}
}

func TestMessageHistoryPickerChronologicalOrderAndWrappingNavigation(t *testing.T) {
	h := &messageHistoryController{}
	if !h.OpenFor([]messageHistoryEntry{
		{ID: "new", Text: "newest prompt"},
		{ID: "middle", Text: "middle\n  prompt"},
		{ID: "old", Text: "oldest prompt"},
	}, "") {
		t.Fatal("history did not open")
	}
	// Multi-line prompts show on one line.
	want := []string{"oldest prompt", "middle prompt", "newest prompt"}
	// Oldest first: the newest prompt sits nearest the composer, highlighted.
	assertHistoryPicker(t, messageHistoryPickerLabels(*h), h.Selection, want, "new")
	for _, step := range []struct {
		key  ui.Key
		want string
	}{{historyDown, "old"}, {historyUp, "new"}, {historyUp, "middle"}, {historyUp, "old"}, {historyUp, "new"}} {
		if result := h.HandleKey(step.key); !result.Handled {
			t.Fatalf("key %+v unhandled", step.key)
		}
		if h.Selection != step.want {
			t.Fatalf("selection = %q, want %q", h.Selection, step.want)
		}
	}
	if result := h.HandleKey(ui.Key{Text: "x", Keycode: 'x'}); result.Handled {
		t.Fatalf("text result = %+v, want it left to the composer", result)
	}
}

func TestMessageHistoryPickerFiltersChronologically(t *testing.T) {
	// Fuzzy scoring favors an exact match, but the visible order stays
	// chronological and the newest match is highlighted.
	h := &messageHistoryController{}
	h.OpenFor([]messageHistoryEntry{{ID: "new", Text: "git status"}, {ID: "old", Text: "git"}}, "git")
	assertHistoryPicker(t, messageHistoryPickerLabels(*h), h.Selection, []string{"git", "git status"}, "new")
	h.SetQuery("status")
	assertHistoryPicker(t, messageHistoryPickerLabels(*h), h.Selection, []string{"git status"}, "new")
	h.SetQuery("unmatched")
	assertHistoryPicker(t, messageHistoryPickerLabels(*h), h.Selection, []string{}, "")
	if _, ok := h.Selected(); ok {
		t.Fatal("unmatched query selected an entry")
	}
	h.SetQuery("git")
	assertHistoryPicker(t, messageHistoryPickerLabels(*h), h.Selection, []string{"git", "git status"}, "new")
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
	rows := []string{"!echo oldest", "!!echo middle", "!echo newest"}
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, rows, "new")
	h.HandleKey(historyDown) // Down wraps from the newest to the oldest.
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, rows, "old")
	h.HandleKey(historyDown)
	h.HandleKey(historyDown)
	h.HandleKey(historyUp)
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, rows, "middle")
	h.HandleKey(historyUp)
	h.HandleKey(historyUp) // Up from the oldest loaded row requests more, without wrapping.
	if loads != 1 {
		t.Fatalf("older-page requests = %d, want 1", loads)
	}
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, rows, "old")
	// While the page loads, Up at the oldest row neither wraps nor requests.
	h.Loading = true
	h.HandleKey(historyUp)
	if loads != 1 || h.Selection != "old" {
		t.Fatalf("loading Up requests = %d selection = %q", loads, h.Selection)
	}
	h.MergeOlder([]bashHistoryEntry{{ID: "older", Command: "echo even older"}}, 0, false)
	rows = append([]string{"!echo even older"}, rows...)
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, rows, "old")
	h.HandleKey(historyUp)
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, rows, "older")
	h.HandleKey(historyUp) // History is exhausted, so Up wraps to the newest.
	if loads != 1 {
		t.Fatalf("older-page requests after exhaustion = %d, want 1", loads)
	}
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, rows, "new")
}

func TestBashHistoryPickerFilteredOlderPage(t *testing.T) {
	h := &bashHistoryController{}
	h.OpenFor([]bashHistoryEntry{{ID: "new", Command: "git status"}, {ID: "old", Command: "git log"}}, "!git")
	h.Loading = false
	h.HasMore = true
	loads := 0
	h.OnExhausted = func() { loads++ }
	h.HandleKey(historyUp)
	h.HandleKey(historyUp)
	if loads != 1 || h.Selection != "old" {
		t.Fatalf("filtered older-page request = %d, selected = %q", loads, h.Selection)
	}
	h.MergeOlder([]bashHistoryEntry{{ID: "older", Command: "git diff"}, {ID: "unmatched", Command: "echo hi"}}, 0, false)
	h.HandleKey(historyUp)
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, []string{"!git diff", "!git log", "!git status"}, "older")
}

func TestBashHistoryPickerLoadsOlderWhenNoLoadedRowMatches(t *testing.T) {
	h := &bashHistoryController{}
	h.OpenFor([]bashHistoryEntry{{ID: "new", Command: "echo hello"}}, "!git")
	h.Loading = false
	h.HasMore = true
	loads := 0
	h.OnExhausted = func() { loads++ }
	h.HandleKey(historyUp)
	if loads != 1 {
		t.Fatalf("older-page requests for unmatched query = %d, want 1", loads)
	}
	h.MergeOlder([]bashHistoryEntry{{ID: "old", Command: "git status"}}, 0, false)
	// The new match becomes selected, even though the previous page had none.
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, []string{"!git status"}, "old")
}

func TestBashHistoryPickerFollowsTheComposer(t *testing.T) {
	h := &bashHistoryController{}
	h.OpenFor([]bashHistoryEntry{{ID: "new", Command: "git status"}, {ID: "old", Command: "go test"}}, "!! g")
	if h.Query != "g" {
		t.Fatalf("query = %q, want the command after the bang", h.Query)
	}
	h.ObserveComposer("!go")
	assertHistoryPicker(t, bashHistoryPickerLabels(*h), h.Selection, []string{"!go test"}, "old")
	h.ObserveComposer("go")
	if h.Open {
		t.Fatal("picker stayed open after the composer left bash mode")
	}
}
