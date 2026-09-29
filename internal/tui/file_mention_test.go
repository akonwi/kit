package tui

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestFileMentionObserve(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		previous     string
		next         string
		pasted       bool
		wantOpen     bool
		wantOpened   bool
		wantQuery    string
		continueWith string
	}{
		{name: "opens at start", previous: "", next: "@", wantOpen: true, wantOpened: true},
		{name: "opens after space", previous: "see ", next: "see @", wantOpen: true, wantOpened: true},
		{name: "opens after newline", previous: "see\n", next: "see\n@", wantOpen: true, wantOpened: true},
		{name: "does not open inside word", previous: "mail", next: "mail@"},
		{name: "does not open from paste", previous: "", next: "@", pasted: true},
		{name: "tracks query", previous: "", next: "@", wantOpen: true, wantOpened: true, continueWith: "@src", wantQuery: "src"},
		{name: "closes on whitespace", previous: "", next: "@", wantOpened: true, continueWith: "@src ", wantOpen: false},
		{name: "closes when prefix is removed", previous: "", next: "@", wantOpened: true, continueWith: "", wantOpen: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var controller fileMentionController
			opened := controller.Observe(tc.previous, tc.next, tc.pasted)
			if tc.continueWith != "" || tc.name == "closes when prefix is removed" {
				controller.Observe(tc.next, tc.continueWith, false)
			}
			if opened != tc.wantOpened {
				t.Fatalf("opened = %v, want %v", opened, tc.wantOpened)
			}
			if controller.Open != tc.wantOpen {
				t.Fatalf("open = %v, want %v", controller.Open, tc.wantOpen)
			}
			if controller.Query != tc.wantQuery {
				t.Fatalf("query = %q, want %q", controller.Query, tc.wantQuery)
			}
		})
	}
}

func TestFileMentionSelectionAndInsertion(t *testing.T) {
	t.Parallel()
	controller := fileMentionController{}
	controller.Observe("say ", "say @", false)
	entries := []protocol.FileIndexEntry{
		{Path: "docs/", IsDir: true},
		{Path: "internal/tui/app.go"},
		{Path: "internal/tui/shell.go"},
	}
	controller.ensureSelection(entries)
	controller.Observe("say @", "say @tui", false)
	controller.ensureSelection(entries)
	if got := controller.keys().Items(fileMentionCatalog(entries)); len(got) != 2 {
		t.Fatalf("filtered entries = %+v, want two tui files", got)
	}
	first, ok := controller.Selected(entries)
	if !ok || first.Path != "internal/tui/app.go" {
		t.Fatalf("selected = %+v, %v", first, ok)
	}
	controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyDown})
	selected, ok := controller.Selected(entries)
	if !ok || selected.Path != "internal/tui/shell.go" {
		t.Fatalf("selected after move = %+v, %v", selected, ok)
	}
	next, cursor, inserted := controller.Insert("say @tui please", selected)
	if !inserted {
		t.Fatal("inserted = false")
	}
	if next != "say @internal/tui/shell.go  please" {
		t.Fatalf("text = %q", next)
	}
	if cursor != len("say @internal/tui/shell.go ") {
		t.Fatalf("cursor = %d", cursor)
	}
	if controller.Open {
		t.Fatal("controller remains open after insertion")
	}
}

func TestFileMentionKeyNavigation(t *testing.T) {
	t.Parallel()
	entries := []protocol.FileIndexEntry{{Path: "a.go"}, {Path: "b.go"}}
	controller := fileMentionController{Open: true, Selection: "a.go"}
	result := controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyDown})
	if !result.Handled || controller.Selection != "b.go" {
		t.Fatalf("down = %+v, selection = %q", result, controller.Selection)
	}
	result = controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyDown})
	if !result.Handled || controller.Selection != "a.go" {
		t.Fatalf("wrapping down = %+v, selection = %q", result, controller.Selection)
	}
	result = controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyEnter})
	if !result.Handled || !result.Activate {
		t.Fatalf("enter = %+v, want activation", result)
	}
	if result := controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyLeft}); result.Handled {
		t.Fatalf("left = %+v, want it left to the composer", result)
	}
}

func TestFileMentionCatalogLabelsRowsWithFullPaths(t *testing.T) {
	t.Parallel()
	catalog := fileMentionCatalog([]protocol.FileIndexEntry{
		{Path: "src/", IsDir: true}, {Path: "src/main.go"}, {Path: "go.mod"},
	})
	want := []pickerItem{
		{Key: "src/", Label: "src/", LabelTruncation: pickerLabelTruncationStart},
		{Key: "src/main.go", Label: "src/main.go", LabelTruncation: pickerLabelTruncationStart},
		{Key: "go.mod", Label: "go.mod", LabelTruncation: pickerLabelTruncationStart},
	}
	if len(catalog) != len(want) {
		t.Fatalf("catalog = %+v, want %+v", catalog, want)
	}
	for index := range want {
		if got := catalog[index]; got.Key != want[index].Key || got.Label != want[index].Label || got.LabelTruncation != want[index].LabelTruncation || got.Description != "" {
			t.Fatalf("item %d = %+v, want %+v", index, got, want[index])
		}
	}
}

func TestFileMentionPickerKeepsLongPathFileNamesVisible(t *testing.T) {
	t.Parallel()
	_, rows := renderInlinePicker(inlinePicker{
		Catalog: fileMentionCatalog([]protocol.FileIndexEntry{
			{Path: "internal/very/long/directory/structure/with/many/levels/tui/inline_picker.go"},
			{Path: "go.mod"},
		}),
		Selection: "internal/very/long/directory/structure/with/many/levels/tui/inline_picker.go",
		Anchor:    anchorAt(0, 21),
	}, 80, 24)
	assertInlinePickerRows(t, rows, []string{
		"┌──────────────────────────────────────────────────────────────┐",
		"│▌…g/directory/structure/with/many/levels/tui/inline_picker.go │",
		"│ go.mod                                                       │",
		"└──────────────────────────────────────────────────────────────┘",
	})
}

func TestFileMentionSurfaceShowsTheSharedIndexStates(t *testing.T) {
	t.Parallel()
	entries := []protocol.FileIndexEntry{{Path: "main.go"}}
	cases := []struct {
		name   string
		source indexedFileSource
		want   []string
	}{
		{name: "loading", source: indexedFileSource{Loading: true}, want: []string{"│ " + spinnerFrames[0] + " Loading files…"}},
		{name: "error", source: indexedFileSource{Error: "index unavailable"}, want: []string{"│ Could not load files: index unavailable"}},
		{name: "stale error", source: indexedFileSource{Entries: entries, Error: "refresh failed"}, want: []string{"│▌main.go", "├", "│ Refresh failed: refresh failed"}},
		{name: "truncated", source: indexedFileSource{Entries: entries, Truncated: true}, want: []string{"│▌main.go", "├", "│ Showing first 4,000 indexed paths"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			view := shellView{Snapshot: shellSnapshot{
				Phase: phaseReady, Session: protocol.SessionInfo{Name: "Mention files"}, Composer: "@",
				FileMention: fileMentionController{Open: true}, IndexedFiles: test.source,
			}}
			application := uitest.New(view)
			application.Pump(80, 24)
			_, box := inlinePickerBox(t, paintedRows(application, 80, 24))
			inner := box[1 : len(box)-1]
			matches := len(inner) == len(test.want)
			for index := 0; matches && index < len(inner); index++ {
				matches = strings.HasPrefix(inner[index], test.want[index])
			}
			if !matches {
				t.Fatalf("picker =\n%s\nwant rows starting %q", strings.Join(box, "\n"), test.want)
			}
		})
	}
}

func TestChangedRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		previous, next string
		start, oldEnd  int
		newEnd         int
	}{
		{previous: "abc", next: "abXc", start: 2, oldEnd: 2, newEnd: 3},
		{previous: "abc", next: "ac", start: 1, oldEnd: 2, newEnd: 1},
		{previous: "abc", next: "aXc", start: 1, oldEnd: 2, newEnd: 2},
		{previous: "same", next: "same", start: 4, oldEnd: 4, newEnd: 4},
	}
	for _, tc := range cases {
		start, oldEnd, newEnd := changedRange(tc.previous, tc.next)
		if start != tc.start || oldEnd != tc.oldEnd || newEnd != tc.newEnd {
			t.Errorf("changedRange(%q, %q) = (%d,%d,%d), want (%d,%d,%d)", tc.previous, tc.next, start, oldEnd, newEnd, tc.start, tc.oldEnd, tc.newEnd)
		}
	}
}
