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
	root, serial := controller.beginLoad("", "")
	controller.applyPage(root, serial, "", directoryPage("", "dir_root", "opaque",
		protocol.WorkspaceDirectoryEntry{Path: "docs", Name: "docs", Kind: protocol.WorkspaceEntryDirectory},
		protocol.WorkspaceDirectoryEntry{Path: "main.go", Name: "main.go", Kind: protocol.WorkspaceEntryFile},
		protocol.WorkspaceDirectoryEntry{Path: "socket", Name: "socket", Kind: protocol.WorkspaceEntryOther},
	))
	controller.Selection = workspaceFileKey{WorkspaceID: "workspace_1", Path: "main.go"}
	controller.SelectionKind = workspaceFilePickerEntryRow
	return controller
}

func filePickerShell(controller workspaceFilePickerController, callbacks shellCallbacks) shellView {
	return shellView{Snapshot: shellSnapshot{
		Phase: phaseReady, Session: protocol.SessionInfo{ID: "session_1", Name: "Picker"},
		WorkspaceFilePicker: controller, WorkspaceFilePickerScroll: &ui.ScrollController{},
	}, Callbacks: callbacks}
}

func TestWorkspaceFilePickerPresentationUsesStandardDialogStates(t *testing.T) {
	t.Parallel()
	controller := presentedFilePickerController()
	application := uitest.New(filePickerShell(controller, shellCallbacks{}))
	application.Pump(80, 24)
	rows := paintedRows(application, 80, 24)
	text := strings.Join(rows, "\n")
	for _, expected := range []string{
		"Open file  /repo", "Filter workspace paths…", glyphTriangleRight + " docs/", "main.go", "socket  unsupported", "Load more…",
		"↑↓ move · enter open/expand · ctrl+r refresh · esc close",
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

func TestWorkspaceFilePickerKeyboardMouseRefreshAndDismiss(t *testing.T) {
	t.Parallel()
	controller := presentedFilePickerController()
	moved, refreshed, closed := 0, 0, 0
	query := ""
	var activated, selected workspaceFilePickerRow
	view := filePickerShell(controller, shellCallbacks{
		WorkspaceFilePickerQuery:    func(_ ui.EventContext, value string) { query = value },
		MoveWorkspaceFilePicker:     func(_ ui.EventContext, delta int) { moved += delta },
		ActivateWorkspaceFilePicker: func(_ ui.EventContext, row workspaceFilePickerRow) { activated = row },
		SelectWorkspaceFilePicker:   func(_ ui.EventContext, row workspaceFilePickerRow) { selected = row },
		RefreshWorkspaceFilePicker:  func(ui.EventContext) { refreshed++ }, CloseWorkspaceFilePicker: func(ui.EventContext) { closed++ },
	})
	application := uitest.New(view)
	application.Pump(80, 24)
	application.Send(vaxis.Key{Keycode: vaxis.KeyDown})
	application.Send(vaxis.Key{Keycode: 'r', Text: "r"})
	if query != "r" || refreshed != 0 {
		t.Fatalf("typing r = query:%q refresh:%d", query, refreshed)
	}
	application.Send(vaxis.Key{Keycode: 'r', Modifiers: vaxis.ModCtrl})
	if moved != 1 || refreshed != 1 {
		t.Fatalf("keyboard actions = move:%d refresh:%d", moved, refreshed)
	}
	rows := paintedRows(application, 80, 24)
	column, row := findTextCell(t, rows, "docs/")
	application.Click(column+2, row)
	if selected.Entry.Path != "docs" || activated.Entry.Path != "docs" {
		t.Fatalf("primary click = selected:%+v activated:%+v", selected, activated)
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if closed != 1 {
		t.Fatalf("escape close count = %d", closed)
	}
}

func TestWorkspaceFilePickerShortcutOpensOnlyWithoutActiveModal(t *testing.T) {
	t.Parallel()
	opened := 0
	view := filePickerShell(workspaceFilePickerController{}, shellCallbacks{OpenWorkspaceFilePicker: func(ui.EventContext) { opened++ }})
	application := uitest.New(view)
	application.Pump(80, 24)
	application.Send(vaxis.Key{Keycode: 'o', Modifiers: vaxis.ModCtrl})
	if opened != 1 {
		t.Fatalf("ctrl+o open count = %d", opened)
	}

	modalController := presentedFilePickerController()
	modalView := filePickerShell(modalController, shellCallbacks{OpenWorkspaceFilePicker: func(ui.EventContext) { opened++ }})
	modalApplication := uitest.New(modalView)
	modalApplication.Pump(80, 24)
	modalApplication.Send(vaxis.Key{Keycode: 'o', Modifiers: vaxis.ModCtrl})
	if opened != 1 {
		t.Fatalf("active modal leaked ctrl+o; open count = %d", opened)
	}
}

func TestWorkspaceFilePickerModalTrapsShellFocusAndWorkspaceNavigation(t *testing.T) {
	t.Parallel()
	controller := presentedFilePickerController()
	focusMoves, tabMoves, closed := 0, 0, 0
	view := shellView{
		Snapshot: shellSnapshot{Phase: phaseReady, Session: protocol.SessionInfo{ID: "session_1", Name: "Picker"}, WorkspaceFilePicker: controller, WorkspaceFilePickerScroll: &ui.ScrollController{}},
		Callbacks: shellCallbacks{
			MoveWorkspaceFocus:       func(ui.EventContext) { focusMoves++ },
			MoveWorkspaceSelection:   func(ui.EventContext, int) { tabMoves++ },
			CloseWorkspaceFilePicker: func(ui.EventContext) { closed++ },
		},
	}
	application := uitest.New(view)
	application.Pump(80, 24)
	application.Tab()
	application.Send(vaxis.Key{Keycode: ']', Modifiers: vaxis.ModCtrl})
	if focusMoves != 0 || tabMoves != 0 {
		t.Fatalf("modal leaked shell input: focus=%d tabs=%d", focusMoves, tabMoves)
	}
	application.Send(vaxis.Key{Keycode: vaxis.KeyEsc})
	if closed != 1 {
		t.Fatalf("modal escape close count = %d", closed)
	}
}

func TestWorkspaceFilePickerExactLoadingEmptyTruncatedAndErrorRows(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		set  func(*workspaceFilePickerController)
		want string
	}{
		{name: "loading", set: func(c *workspaceFilePickerController) { c.beginLoad("", "") }, want: "Loading…"},
		{name: "empty", set: func(c *workspaceFilePickerController) {
			key, serial := c.beginLoad("", "")
			c.applyPage(key, serial, "", directoryPage("", "dir_empty", ""))
		}, want: "Empty directory"},
		{name: "truncated", set: func(c *workspaceFilePickerController) {
			key, serial := c.beginLoad("", "")
			page := directoryPage("", "dir_cut", "")
			page.Truncated, page.TruncationReason = true, "observation_limit"
			c.applyPage(key, serial, "", page)
		}, want: "Directory truncated (observation limit)"},
		{name: "error", set: func(c *workspaceFilePickerController) {
			key, serial := c.beginLoad("", "")
			c.applyError(key, serial, &protocol.WorkspaceError{Code: protocol.WorkspaceErrorNotFound, Message: "Directory disappeared"})
		}, want: "Directory disappeared · enter retry"},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controller := readyFilePickerController()
			test.set(&controller)
			application := uitest.New(filePickerShell(controller, shellCallbacks{}))
			application.Pump(80, 24)
			if text := strings.Join(paintedRows(application, 80, 24), "\n"); !strings.Contains(text, test.want) {
				t.Fatalf("state missing %q:\n%s", test.want, text)
			}
		})
	}
}
