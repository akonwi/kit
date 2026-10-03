package tui

import (
	"strings"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis/ui/uitest"
)

func TestSessionFilterKeepsMatchesWithTheirAncestors(t *testing.T) {
	c := &sessionExplorerController{}
	c.Resolve(c.Begin("session_other"), append(explorerFixture(), sessionExplorerItem{ID: "session_abc12345", UpdatedAt: "2026-05-01T00:00:00Z"}), nil)

	// A collapsed descendant is shown under its ancestors and highlighted.
	c.SetQuery("  PROVID  ")
	assertVisibleSessions(t, c, "session_root", "session_child", "session_leaf")
	if c.Selection != "session_leaf" {
		t.Fatalf("filtered selection = %s", c.Selection)
	}
	got := c.Snapshot().Sessions
	if got[0].Depth != 0 || got[1].Depth != 1 || got[2].Depth != 2 || got[0].ChildCount != 0 || got[1].ChildCount != 0 {
		t.Fatalf("filtered rows = %+v", got)
	}
	if _, id, ok := c.BeginSwitch(); !ok || id != "session_leaf" {
		t.Fatalf("switch filtered descendant = %s %v", id, ok)
	}
	c.CancelSwitch()

	// Families move as one group, placed by their best-ranked match.
	ranked := &sessionExplorerController{}
	ranked.Resolve(ranked.Begin("session_other"), hierarchyFixture(), nil)
	ranked.SetQuery("re")
	assertVisibleSessions(t, ranked, "session_other", "session_root", "session_child", "session_leaf")
	if ranked.Selection != "session_other" {
		t.Fatalf("best match selection = %s", ranked.Selection)
	}
	// Like other pickers, the description and a fallback label match too.
	c.SetQuery("repo/tok")
	assertVisibleSessions(t, c, "session_root", "session_sibling")
	c.SetQuery("abc1")
	assertVisibleSessions(t, c, "session_abc12345")

	c.SetQuery("zzz")
	assertVisibleSessions(t, c)
	if _, ok := c.ActivatableSelection(); ok {
		t.Fatal("empty result is activatable")
	}
	// Clearing the query restores the tree and the nearest visible ancestor.
	c.SetQuery("provid")
	c.SetQuery("")
	assertVisibleSessions(t, c, "session_root", "session_other", "session_abc12345")
	if c.Selection != "session_root" {
		t.Fatalf("restored tree selection = %s", c.Selection)
	}
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
	// The match is shown under its parent.
	rows := paintedRows(app, 100, 20)
	_, searchRow := assertPickerSearchField(t, rows, "Child")
	assertDialogRow(t, rows, "│ Parent", "│ Parent                                                               unknown │")
	assertDialogRow(t, rows, "│▌  Child", "│▌  Child                                                              unknown │")
	if findPaintedRow(rows, "│ Parent") != searchRow+2 {
		t.Fatalf("parent is not the first result:\n%s", strings.Join(rows, "\n"))
	}
	app.Enter()
	if got := result.selectedSession(); got != "session_child" {
		t.Fatalf("selected result = %s", got)
	}
}
