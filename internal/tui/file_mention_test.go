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
	if got := controller.filtered(entries); len(got) != 2 {
		t.Fatalf("filtered entries = %+v, want two tui files", got)
	}
	first, ok := controller.Selected(entries)
	if !ok || first.Path != "internal/tui/app.go" {
		t.Fatalf("selected = %+v, %v", first, ok)
	}
	controller.Move(entries, 1)
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
	_, _, handled := controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyDown})
	if !handled || controller.Selection != "b.go" {
		t.Fatalf("down handled = %v, selection = %q", handled, controller.Selection)
	}
	entry, selected, handled := controller.HandleKey(entries, ui.Key{Keycode: vaxis.KeyEnter})
	if !handled || !selected || entry.Path != "b.go" {
		t.Fatalf("enter = %+v, selected %v, handled %v", entry, selected, handled)
	}
}

func TestFileMentionSurfaceShowsPathsAndDirectoryDescription(t *testing.T) {
	t.Parallel()
	view := shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{Name: "Mention files"}, Composer: "see @src",
		FileMention: fileMentionController{Open: true, Anchor: len("see "), Query: "src", Selection: "src/"},
		IndexedFiles: indexedFileSource{Entries: []protocol.FileIndexEntry{
			{Path: "src/", IsDir: true}, {Path: "src/main.go"},
		}},
	}}
	application := uitest.New(view)
	application.Pump(100, 24)
	rows := paintedRows(application, 100, 24)
	painted := strings.Join(rows, "\n")
	for _, want := range []string{"src/", "directory", "src/main.go", "↑↓ move · enter insert · esc close"} {
		if !strings.Contains(painted, want) {
			t.Fatalf("file mention picker missing %q:\n%s", want, painted)
		}
	}
	column, row := findTextCell(t, rows, "src/")
	if wantColumn := view.Snapshot.FileMention.Anchor + 2; column != wantColumn {
		t.Fatalf("file mention path column = %d, want %d aligned near trigger", column, wantColumn)
	}
	_, triggerRow := findTextCell(t, rows, "@src")
	if triggerRow-row != 5 {
		t.Fatalf("picker selected row = %d and trigger row = %d; picker is not immediately above trigger", row, triggerRow)
	}
	if application.Cell(column, row).Style.Background == application.Cell(column, row+1).Style.Background {
		t.Fatal("selected file mention row does not have a distinct background")
	}
}

func TestFileMentionSurfaceShowsSharedIndexErrorAndTruncation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source indexedFileSource
		want   string
	}{
		{name: "error", source: indexedFileSource{Error: "index unavailable"}, want: "Could not load files: index unavailable"},
		{name: "stale error", source: indexedFileSource{Entries: []protocol.FileIndexEntry{{Path: "main.go"}}, Error: "refresh failed"}, want: "Refresh failed: refresh failed"},
		{name: "truncated", source: indexedFileSource{Entries: []protocol.FileIndexEntry{{Path: "main.go"}}, Truncated: true}, want: "Showing first 4,000 indexed paths"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			view := shellView{Snapshot: shellSnapshot{
				Phase: phaseReady, Session: protocol.SessionInfo{Name: "Mention files"}, Composer: "@",
				FileMention: fileMentionController{Open: true}, IndexedFiles: test.source,
			}}
			application := uitest.New(view)
			application.Pump(80, 24)
			if text := strings.Join(paintedRows(application, 80, 24), "\n"); !strings.Contains(text, test.want) {
				t.Fatalf("mention state missing %q:\n%s", test.want, text)
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
