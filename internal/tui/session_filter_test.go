package tui

import (
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestSessionFilterMatchesOnlyNamesAndFindsCollapsedDescendants(t *testing.T) {
	c := &sessionExplorerController{}
	items := append(hierarchyFixture(), sessionExplorerItem{ID: "session_providers", CWD: "/Providers", ParentSessionName: "Providers"})
	c.Resolve(c.Begin("session_other"), items, nil)
	c.SetQuery("  PROVID  ")
	assertVisibleSessions(t, c, "session_leaf")
	if c.Selection != "session_leaf" {
		t.Fatalf("filtered selection = %s", c.Selection)
	}
	row := c.Snapshot().Sessions[0]
	if row.Tree || row.Depth != 0 || row.ChildCount != 0 || !row.MissingParent || row.ParentSessionName != "OAuth" {
		t.Fatalf("filtered row = %+v", row)
	}
	_, id, ok := c.BeginSwitch()
	if !ok || id != "session_leaf" {
		t.Fatalf("switch filtered descendant = %s %v", id, ok)
	}
	c.CancelSwitch()
	c.SetQuery("")
	assertVisibleSessions(t, c, "session_root", "session_other", "session_providers")
	if c.Selection != "session_root" {
		t.Fatalf("restored tree selection = %s", c.Selection)
	}
	c.SetQuery("/Providers")
	assertVisibleSessions(t, c)
	if _, ok := c.ActivatableSelection(); ok {
		t.Fatal("empty result is activatable")
	}
	c.Move(1)
	c.NavigateTree(true)
	if c.Selection != "" {
		t.Fatalf("empty selection = %s", c.Selection)
	}
	c.SetQuery("OAuth")
	assertVisibleSessions(t, c, "session_child") // Parent names must not match the leaf.
	c.SetQuery("session_")
	assertVisibleSessions(t, c)
	c.SetQuery("   ")
	assertVisibleSessions(t, c, "session_root", "session_other", "session_providers")
}

func TestSessionFilterMutationsKeepSelectionVisible(t *testing.T) {
	c := &sessionExplorerController{}
	c.Resolve(c.Begin("session_other"), hierarchyFixture(), nil)
	c.SetQuery("OAuth")
	c.BeginRename()
	c.SetRenameText("New name")
	generation, _, _, ok := c.BeginRenameSave()
	if !ok {
		t.Fatal("rename did not start")
	}
	c.ResolveRename(generation, protocol.SessionInfo{ID: "session_child", Name: "New name", ParentSessionID: "session_root"}, nil)
	assertVisibleSessions(t, c)
	if c.Selection != "" {
		t.Fatalf("rename selection = %s", c.Selection)
	}
	c.SetQuery("Tokens")
	c.BeginDelete()
	generation, _, ok = c.BeginDeleteConfirm()
	if !ok || !c.ResolveDelete(generation, nil) {
		t.Fatal("delete failed")
	}
	assertVisibleSessions(t, c)
	if c.Selection != "" {
		t.Fatalf("delete selection = %s", c.Selection)
	}
	c.SetQuery("Providers")
	c.ApplyExternalRename("session_leaf", "Renamed externally")
	assertVisibleSessions(t, c)
	if c.Selection != "" {
		t.Fatalf("external rename selection = %s", c.Selection)
	}
}

func TestStandaloneSessionPickerFiltersTypedNames(t *testing.T) {
	sessions := []protocol.SessionInfo{{ID: "session_parent", Name: "Parent"}, {ID: "session_child", Name: "Child", ParentSessionID: "session_parent", ParentSessionName: "Parent"}}
	result := &sessionPickerResult{}
	app := uitest.New(sessionPicker{Options: SessionPickerOptions{Context: t.Context()}, Result: result, initialSet: true, initialSessions: sessions})
	app.Pump(100, 20)
	for _, r := range "Child" {
		app.Key(string(r))
	}
	app.Pump(100, 20)
	app.Pump(100, 20)
	// Filtered results are flat and keep their parent as a lineage note.
	rows := paintedRows(app, 100, 20)
	assertPickerSearchField(t, rows, "Child")
	assertDialogRow(t, rows, "from Parent", "│▌Child    from Parent                                                 unknown │")
	app.Enter()
	if got := result.selectedSession(); got != "session_child" {
		t.Fatalf("selected result = %s", got)
	}
}
