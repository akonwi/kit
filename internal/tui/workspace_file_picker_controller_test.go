package tui

import (
	"errors"
	"reflect"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func readyFilePickerController() workspaceFilePickerController {
	var controller workspaceFilePickerController
	controller.reset()
	controller.setWorkspace(protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: "workspace_1", CWD: "/repo", State: protocol.WorkspaceReady})
	return controller
}

func directoryPage(path, revision, cursor string, entries ...protocol.WorkspaceDirectoryEntry) protocol.DirectoryPage {
	return protocol.DirectoryPage{
		SessionID: "session_1", Workspace: protocol.WorkspaceRef{SessionID: "session_1", WorkspaceID: "workspace_1", CWD: "/repo", State: protocol.WorkspaceReady},
		Path: path, Revision: revision, Entries: entries, NextCursor: cursor,
	}
}

func TestWorkspaceFilePickerLazilyExpandsFiltersAndPreservesCanonicalSelection(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	root, serial := controller.beginLoad("", "")
	if !controller.applyPage(root, serial, "", directoryPage("", "dir_root", "",
		protocol.WorkspaceDirectoryEntry{Path: "docs", Name: "docs", Kind: protocol.WorkspaceEntryDirectory},
		protocol.WorkspaceDirectoryEntry{Path: "main.go", Name: "main.go", Kind: protocol.WorkspaceEntryFile},
	)) {
		t.Fatal("root page was rejected")
	}
	rows := controller.rows()
	if len(rows) != 2 || rows[0].Entry.Path != "docs" || rows[1].Entry.Path != "main.go" {
		t.Fatalf("root rows = %#v", rows)
	}
	if !controller.toggleDirectory(rows[0].Entry) {
		t.Fatal("first expansion did not request a lazy directory load")
	}
	controller.beginLoad("docs", "")
	rows = controller.rows()
	if len(rows) != 3 || rows[1].Kind != workspaceFilePickerLoadingRow || rows[1].Depth != 1 {
		t.Fatalf("expanded loading rows = %#v", rows)
	}
	child, childSerial := controller.beginLoad("docs", "")
	controller.applyPage(child, childSerial, "", directoryPage("docs", "dir_docs", "",
		protocol.WorkspaceDirectoryEntry{Path: "docs/design.md", Name: "design.md", Kind: protocol.WorkspaceEntryFile},
	))
	controller.Selection = workspaceFileKey{WorkspaceID: "workspace_1", Path: "docs/design.md"}
	controller.SelectionKind = workspaceFilePickerEntryRow
	controller.Query = "design"
	rows = controller.rows()
	if len(rows) != 1 || rows[0].Entry.Path != "docs/design.md" || controller.selectedIndex(rows) != 0 {
		t.Fatalf("filtered rows = %#v, selection=%d", rows, controller.selectedIndex(rows))
	}
	controller.Query = ""
	if controller.toggleDirectory(protocol.WorkspaceDirectoryEntry{Path: "docs", Kind: protocol.WorkspaceEntryDirectory}) {
		t.Fatal("collapsing a loaded directory requested I/O")
	}
	if controller.Selection.Path != "docs" {
		t.Fatalf("collapsed selection = %+v, want nearest visible canonical path", controller.Selection)
	}
}

func TestWorkspaceFilePickerPaginatesOneRevisionAndShowsObservationLimits(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, first := controller.beginLoad("", "")
	page := directoryPage("", "dir_root", "opaque-next", protocol.WorkspaceDirectoryEntry{Path: "a", Name: "a", Kind: protocol.WorkspaceEntryFile})
	if !controller.applyPage(key, first, "", page) {
		t.Fatal("first page was rejected")
	}
	rows := controller.rows()
	if len(rows) != 2 || rows[1].Kind != workspaceFilePickerMoreRow {
		t.Fatalf("first-page rows = %#v", rows)
	}
	_, second := controller.beginLoad("", "opaque-next")
	if got := controller.rows(); got[len(got)-1].Kind != workspaceFilePickerLoadingRow {
		t.Fatalf("pagination loading row = %#v", got)
	}
	page = directoryPage("", "dir_root", "", protocol.WorkspaceDirectoryEntry{Path: "b", Name: "b", Kind: protocol.WorkspaceEntryFile})
	page.Truncated, page.TruncationReason = true, "entry_limit"
	if !controller.applyPage(key, second, "opaque-next", page) {
		t.Fatal("second page was rejected")
	}
	observation, ok := controller.observation(key)
	if !ok || len(observation.Entries) != 2 || observation.Entries[1].Path != "b" {
		t.Fatalf("combined observation = %+v, found=%v", observation, ok)
	}
	rows = controller.rows()
	if rows[len(rows)-1].Kind != workspaceFilePickerTruncatedRow || rows[len(rows)-1].Text != "Directory truncated (entry limit)" {
		t.Fatalf("truncation row = %#v", rows[len(rows)-1])
	}
}

func TestWorkspaceFilePickerFreshPageRemovesSupersededDirectoryRevision(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, first := controller.beginLoad("", "")
	controller.applyPage(key, first, "", directoryPage("", "dir_old", ""))
	_, refreshed := controller.beginLoad("", "")
	controller.applyPage(key, refreshed, "", directoryPage("", "dir_new", ""))
	if len(controller.observations) != 1 {
		t.Fatalf("fresh page retained superseded observations: %#v", controller.observations)
	}
	if _, ok := controller.observations[directoryObservationKey{workspaceFileKey: key, Revision: "dir_new"}]; !ok {
		t.Fatalf("fresh revision missing: %#v", controller.observations)
	}
}

func TestWorkspaceFilePickerRejectsChangedRevisionContinuation(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, first := controller.beginLoad("", "")
	controller.applyPage(key, first, "", directoryPage("", "dir_first", "cursor", protocol.WorkspaceDirectoryEntry{Path: "a", Name: "a", Kind: protocol.WorkspaceEntryFile}))
	_, continuation := controller.beginLoad("", "cursor")
	if controller.applyPage(key, continuation, "cursor", directoryPage("", "dir_changed", "", protocol.WorkspaceDirectoryEntry{Path: "b", Name: "b", Kind: protocol.WorkspaceEntryFile})) {
		t.Fatal("changed-revision continuation was accepted as a complete observation")
	}
	observation, _ := controller.observation(key)
	if len(observation.Entries) != 1 || observation.Entries[0].Path != "a" {
		t.Fatalf("changed continuation corrupted observation: %+v", observation)
	}
}

func TestWorkspaceFilePickerRejectsSupersededCompletionsAndKeepsRevisionPagesSeparate(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, stale := controller.beginLoad("", "")
	_, current := controller.beginLoad("", "")
	if controller.applyPage(key, stale, "", directoryPage("", "dir_old", "", protocol.WorkspaceDirectoryEntry{Path: "old", Name: "old", Kind: protocol.WorkspaceEntryFile})) {
		t.Fatal("superseded completion mutated picker state")
	}
	if !controller.applyPage(key, current, "", directoryPage("", "dir_new", "", protocol.WorkspaceDirectoryEntry{Path: "new", Name: "new", Kind: protocol.WorkspaceEntryFile})) {
		t.Fatal("current completion was rejected")
	}
	if _, found := controller.observations[directoryObservationKey{workspaceFileKey: key, Revision: "dir_old"}]; found {
		t.Fatal("stale revision was retained")
	}
	if got := controller.rows()[0].Entry.Path; got != "new" {
		t.Fatalf("visible revision entry = %q", got)
	}
	generation := controller.Generation
	controller.close()
	if controller.Open || controller.Generation == generation || controller.applyPage(key, current, "", directoryPage("", "dir_new", "")) {
		t.Fatalf("closed generation accepted completion: %+v", controller)
	}
}

func TestWorkspaceFilePickerErrorAndEmptyTransitions(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, serial := controller.beginLoad("", "")
	workspaceErr := &protocol.WorkspaceError{Code: protocol.WorkspaceErrorPermissionDenied, Message: "Directory is not readable"}
	if !controller.applyError(key, serial, workspaceErr) {
		t.Fatal("current error was rejected")
	}
	rows := controller.rows()
	if len(rows) != 1 || rows[0].Kind != workspaceFilePickerErrorRow || rows[0].Text != "Directory is not readable" {
		t.Fatalf("error rows = %#v", rows)
	}
	key, serial = controller.beginLoad("", "")
	if !controller.applyPage(key, serial, "", directoryPage("", "dir_empty", "")) {
		t.Fatal("empty page was rejected")
	}
	rows = controller.rows()
	if len(rows) != 1 || rows[0].Kind != workspaceFilePickerEmptyRow || rows[0].Text != "Empty directory" {
		t.Fatalf("empty rows = %#v", rows)
	}
	key, serial = controller.beginLoad("", "")
	if controller.applyError(key, serial, errors.New("request canceled")) != true {
		t.Fatal("ordinary errors should remain visible")
	}
}

func TestWorkspaceFilePickerReportsOmittedEntriesWithoutCallingDirectoryEmpty(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	key, serial := controller.beginLoad("", "")
	page := directoryPage("", "dir_omitted", "")
	page.Omissions = []protocol.WorkspaceOmission{{Reason: "unsupported_name", Count: 2}}
	controller.applyPage(key, serial, "", page)
	rows := controller.rows()
	if len(rows) != 1 || rows[0].Kind != workspaceFilePickerNoticeRow || rows[0].Text != "2 entries omitted" {
		t.Fatalf("omission rows = %#v", rows)
	}
}

func TestWorkspaceFilePickerExpansionKeysIncludeWorkspaceIncarnation(t *testing.T) {
	t.Parallel()
	controller := readyFilePickerController()
	oldKey := workspaceFileKey{WorkspaceID: "workspace_1", Path: "docs"}
	controller.expanded[oldKey] = true
	controller.Selection = workspaceFileKey{WorkspaceID: "workspace_1", Path: "docs/a.md"}
	controller.setWorkspace(protocol.WorkspaceRef{WorkspaceID: "workspace_2", CWD: "/other", State: protocol.WorkspaceReady})
	if controller.Selection != (workspaceFileKey{}) {
		t.Fatalf("selection crossed workspace incarnation: %+v", controller.Selection)
	}
	if got := controller.expandedPaths(); !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("new-workspace expanded paths = %#v, want root only", got)
	}
}
