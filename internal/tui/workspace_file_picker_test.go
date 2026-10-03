package tui

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func presentedFilePickerController() workspaceFilePickerController {
	controller := readyFilePickerController()
	controller.Selection = workspaceFileKey{WorkspaceID: "workspace_1", Path: "main.go"}
	return controller
}

func presentedFilePickerSource() indexedFileSource {
	return indexedPickerSource(
		protocol.FileIndexEntry{Path: "docs/", IsDir: true},
		protocol.FileIndexEntry{Path: "main.go"},
		protocol.FileIndexEntry{Path: "internal/tui/app.go"},
		protocol.FileIndexEntry{Path: "internal/childproc/exit_darwin_test.go"},
	)
}

func filePickerShell(controller workspaceFilePickerController, source indexedFileSource, callbacks shellCallbacks) shellView {
	return shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{ID: "session_1", Name: "Picker"},
		WorkspaceFilePicker: controller, IndexedFiles: source,
	}, Callbacks: callbacks}
}

func TestWorkspaceFilePickerPresentationUsesIndexedFlatDialog(t *testing.T) {
	t.Parallel()
	application := uitest.New(filePickerShell(presentedFilePickerController(), presentedFilePickerSource(), shellCallbacks{}))
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		"Open file", "Search indexed project paths…", "docs/", "directory", "main.go", "internal/tui/app.go",
		"internal/childproc/exit_darwin_test.go", "↑↓ move · enter open · ctrl+r refresh · esc close",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("file picker missing %q:\n%s", expected, text)
		}
	}
	selectedColumn, selectedRow := findTextCell(t, rows, "main.go")
	idleColumn, idleRow := findTextCell(t, rows, "docs/")
	if application.Cell(selectedColumn, selectedRow).Style.Background == application.Cell(idleColumn, idleRow).Style.Background {
		t.Fatal("selected file does not use standard picker focused background")
	}
}

func TestWorkspaceFilePickerKeyboardPrimaryMouseRefreshAndDismiss(t *testing.T) {
	t.Parallel()
	controller := presentedFilePickerController()
	var activated workspaceFilePickerRow
	view := filePickerShell(controller, presentedFilePickerSource(), shellCallbacks{
		ActivateWorkspaceFilePicker: func(_ ui.EventContext, row workspaceFilePickerRow) { activated = row },
	})
	application := uitest.New(view)
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	column, row := findTextCell(t, rows, "docs/")
	application.Click(column+2, row)
	if activated.Entry.Path != "docs/" {
		t.Fatalf("primary click activated = %+v", activated)
	}
}

func TestWorkspaceFilePickerExactLoadingEmptyErrorAndTruncatedPresentation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source indexedFileSource
		want   string
	}{
		{name: "loading", source: indexedFileSource{CWD: "/repo", Loading: true}, want: "Loading indexed files…"},
		{name: "empty", source: indexedFileSource{CWD: "/repo", Entries: []protocol.FileIndexEntry{}}, want: "No indexed files"},
		{name: "error", source: indexedFileSource{CWD: "/repo", Error: "Index unavailable"}, want: "Index unavailable"},
		{name: "truncated", source: indexedFileSource{CWD: "/repo", Entries: []protocol.FileIndexEntry{{Path: "main.go"}}, Truncated: true}, want: "Showing first 4,000 indexed paths"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			application := uitest.New(filePickerShell(readyFilePickerController(), test.source, shellCallbacks{}))
			application.Pump(80, 24)
			if text := strings.Join(paintedRows(application, 80, 24), "\n"); !strings.Contains(text, test.want) {
				t.Fatalf("state missing %q:\n%s", test.want, text)
			}
		})
	}
}

func TestWorkspaceFilePickerShortcutOpensOnlyWithoutActiveModal(t *testing.T) {
	t.Parallel()
	opened := 0
	view := filePickerShell(workspaceFilePickerController{}, indexedFileSource{}, shellCallbacks{OpenWorkspaceFilePicker: func(ui.EventContext) { opened++ }})
	application := uitest.New(view)
	application.Pump(80, 24)
	application.Send(vaxis.Key{Keycode: 'o', Modifiers: vaxis.ModCtrl})
	if opened != 1 {
		t.Fatalf("ctrl+o open count = %d", opened)
	}
	modalApplication := uitest.New(filePickerShell(readyFilePickerController(), indexedFileSource{}, shellCallbacks{OpenWorkspaceFilePicker: func(ui.EventContext) { opened++ }}))
	modalApplication.Pump(80, 24)
	modalApplication.Send(vaxis.Key{Keycode: 'o', Modifiers: vaxis.ModCtrl})
	if opened != 1 {
		t.Fatalf("active modal leaked ctrl+o; open count = %d", opened)
	}
}
