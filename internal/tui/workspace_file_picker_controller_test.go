package tui

import (
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
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
	items := controller.keyModel().Items(controller.pickerCatalog(source))
	if len(items) != 1 || items[0].Label != "internal/tui/app.go" {
		t.Fatalf("flat fuzzy items = %+v", items)
	}
	if row, ok := controller.rowByPickerKey(source, items[0].Key); !ok || row.Key.WorkspaceID != "workspace_1" {
		t.Fatalf("item workspace = %+v, %t", row.Key, ok)
	}
}

func TestWorkspaceFilePickerDirectoriesMatchWithoutDrillDown(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	controller.Query = "docs"
	items := controller.keyModel().Items(controller.pickerCatalog(indexedPickerSource(
		protocol.FileIndexEntry{Path: "docs/", IsDir: true},
		protocol.FileIndexEntry{Path: "docs/guide.md"},
	)))
	if len(items) != 2 || items[0].Meta != "directory" || items[1].Meta != "" {
		t.Fatalf("directory-aided flat items = %+v", items)
	}
}

func TestWorkspaceFilePickerHandleKeyMovesWrapsAndFiltersBeforePaint(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	source := indexedPickerSource(
		protocol.FileIndexEntry{Path: "alpha.go"},
		protocol.FileIndexEntry{Path: "beta.go"},
	)
	controller.ensureSelection(source)
	controller.HandleKey(source, ui.Key{Keycode: vaxis.KeyDown})
	if controller.Selection.Path != "beta.go" {
		t.Fatalf("moved selection = %+v", controller.Selection)
	}
	controller.HandleKey(source, ui.Key{Keycode: vaxis.KeyDown})
	if controller.Selection.Path != "alpha.go" {
		t.Fatalf("wrapped selection = %+v", controller.Selection)
	}
	controller.HandleKey(source, ui.Key{Text: "b", Keycode: 'b'})
	if controller.Query != "b" || controller.Selection.Path != "beta.go" {
		t.Fatalf("pre-paint query state = query:%q selection:%+v", controller.Query, controller.Selection)
	}
}
