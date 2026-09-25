package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func readyFilePickerController() workspaceFilePickerController {
	return workspaceFilePickerController{
		Open: true, Generation: 1,
		Workspace: protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: "workspace_1", CWD: "/repo", State: protocol.WorkspaceReady},
	}
}

func indexedPickerSource(entries ...protocol.FileIndexEntry) indexedFileSource {
	return indexedFileSource{SessionID: "session_1", CWD: "/repo", Entries: entries}
}

func TestWorkspaceFilePickerUsesFlatFuzzyIndexedPaths(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	controller.Query = "tui/app"
	source := indexedPickerSource(
		protocol.FileIndexEntry{Path: "docs/", IsDir: true},
		protocol.FileIndexEntry{Path: "internal/tui/app.go"},
		protocol.FileIndexEntry{Path: "internal/server/app.go"},
	)
	rows := controller.rows(source)
	if len(rows) != 1 || rows[0].Entry.Path != "internal/tui/app.go" {
		t.Fatalf("flat fuzzy rows = %+v", rows)
	}
	if rows[0].Key.WorkspaceID != "workspace_1" {
		t.Fatalf("row workspace = %+v", rows[0].Key)
	}
}

func TestWorkspaceFilePickerDirectoriesMatchWithoutDrillDown(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	controller.Query = "docs"
	rows := controller.rows(indexedPickerSource(
		protocol.FileIndexEntry{Path: "docs/", IsDir: true},
		protocol.FileIndexEntry{Path: "docs/guide.md"},
	))
	if len(rows) != 2 || !rows[0].Entry.IsDir || rows[1].Entry.IsDir {
		t.Fatalf("directory-aided flat rows = %+v", rows)
	}
}

func TestWorkspaceFilePickerMovesAcrossIndexedRows(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	source := indexedPickerSource(protocol.FileIndexEntry{Path: "a.go"}, protocol.FileIndexEntry{Path: "b.go"})
	controller.ensureSelection(source)
	if controller.Selection.Path != "a.go" {
		t.Fatalf("initial selection = %+v", controller.Selection)
	}
	controller.move(source, 1)
	if controller.Selection.Path != "b.go" {
		t.Fatalf("moved selection = %+v", controller.Selection)
	}
	controller.move(source, 1)
	if controller.Selection.Path != "a.go" {
		t.Fatalf("wrapped selection = %+v", controller.Selection)
	}
}

func TestWorkspaceFilePickerExactSourceStates(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	cases := []struct {
		name   string
		source indexedFileSource
		kind   workspaceFilePickerRowKind
		text   string
	}{
		{name: "loading", source: indexedFileSource{Loading: true}, kind: workspaceFilePickerLoadingRow, text: "Loading indexed files…"},
		{name: "empty", source: indexedPickerSource(), kind: workspaceFilePickerEmptyRow, text: "No indexed files"},
		{name: "error", source: indexedFileSource{Error: "index failed"}, kind: workspaceFilePickerErrorRow, text: "index failed"},
	}
	for _, test := range cases {
		rows := controller.rows(test.source)
		if len(rows) != 1 || rows[0].Kind != test.kind || rows[0].Text != test.text {
			t.Errorf("%s rows = %+v", test.name, rows)
		}
	}
	rows := controller.rows(indexedFileSource{Entries: []protocol.FileIndexEntry{{Path: "main.go"}}, Truncated: true})
	if len(rows) != 2 || rows[1].Kind != workspaceFilePickerTruncatedRow || rows[1].Text != "Showing first 4,000 indexed paths" {
		t.Fatalf("truncated rows = %+v", rows)
	}
}
