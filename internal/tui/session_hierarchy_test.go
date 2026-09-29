package tui

import (
	"reflect"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

func hierarchyFixture() []sessionExplorerItem {
	return projectSessionExplorerItems([]protocol.SessionInfo{
		{ID: "session_root", Name: "Authentication", UpdatedAt: "2026-06-01T00:00:00Z"},
		{ID: "session_child", Name: "OAuth", ParentSessionID: "session_root", ParentSessionName: "Authentication", UpdatedAt: "2026-06-02T00:00:00Z"},
		{ID: "session_leaf", Name: "Providers", ParentSessionID: "session_child", ParentSessionName: "OAuth", UpdatedAt: "2026-06-05T00:00:00Z"},
		{ID: "session_sibling", Name: "Tokens", ParentSessionID: "session_root", UpdatedAt: "2026-06-03T00:00:00Z"},
		{ID: "session_other", Name: "Release", UpdatedAt: "2026-06-04T00:00:00Z"},
	})
}

func assertVisibleSessions(t *testing.T, c *sessionExplorerController, ids ...string) {
	t.Helper()
	var got []string
	for _, item := range c.Snapshot().Sessions {
		got = append(got, item.ID)
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("visible = %v, want %v", got, ids)
	}
}

func TestSessionHierarchyOrdersFamiliesByActivityAndSiblingsByRecency(t *testing.T) {
	c := &sessionExplorerController{}
	generation := c.Begin("session_leaf")
	c.Resolve(generation, hierarchyFixture(), nil)
	assertVisibleSessions(t, c, "session_root", "session_sibling", "session_child", "session_leaf", "session_other")
	got := c.Snapshot().Sessions
	if got[0].UpdatedAt != "2026-06-01T00:00:00Z" || got[0].ChildCount != 2 || got[3].Depth != 2 || c.Selection != "session_leaf" {
		t.Fatalf("tree projection = %+v; selection = %s", got, c.Selection)
	}
	if got[2].ParentSessionName != "Authentication" {
		t.Fatalf("parent metadata = %+v", got[2])
	}
}

func TestSessionHierarchyNavigationAndRememberedExpansion(t *testing.T) {
	c := &sessionExplorerController{}
	c.Resolve(c.Begin("session_other"), hierarchyFixture(), nil)
	assertVisibleSessions(t, c, "session_root", "session_other")
	c.Select("session_leaf") // Hidden rows cannot steal selection.
	if c.Selection != "session_other" {
		t.Fatalf("hidden selection = %s", c.Selection)
	}
	c.Move(-1)
	if c.Selection != "session_root" {
		t.Fatalf("up = %s", c.Selection)
	}
	c.HandleKey(ui.Key{Keycode: ui.KeyRight})
	assertVisibleSessions(t, c, "session_root", "session_sibling", "session_child", "session_other")
	c.HandleKey(ui.Key{Keycode: ui.KeyRight})
	if c.Selection != "session_sibling" {
		t.Fatalf("enter children = %s", c.Selection)
	}
	c.NavigateTree(false)
	if c.Selection != "session_root" {
		t.Fatalf("parent = %s", c.Selection)
	}
	c.Select("session_child")
	c.NavigateTree(true)
	c.NavigateTree(true)
	if c.Selection != "session_leaf" {
		t.Fatalf("grandchild = %s", c.Selection)
	}
	c.NavigateTree(false)
	c.HandleKey(ui.Key{Keycode: ui.KeyLeft})
	assertVisibleSessions(t, c, "session_root", "session_sibling", "session_child", "session_other")
	c.Close()
	c.Resolve(c.Begin("session_other"), hierarchyFixture(), nil)
	assertVisibleSessions(t, c, "session_root", "session_sibling", "session_child", "session_other")
	c.ToggleExpanded("session_root")
	assertVisibleSessions(t, c, "session_root", "session_other")
	if c.Selection != "session_root" {
		t.Fatalf("collapse selects parent = %s", c.Selection)
	}
	// Attaching a descendant overrides remembered collapse to reveal it.
	c.Close()
	c.Resolve(c.Begin("session_leaf"), hierarchyFixture(), nil)
	assertVisibleSessions(t, c, "session_root", "session_sibling", "session_child", "session_leaf", "session_other")
}

func TestSessionHierarchyParentRemainsActivatableAndBusyTreeIsFrozen(t *testing.T) {
	c := &sessionExplorerController{}
	c.Resolve(c.Begin("session_other"), hierarchyFixture(), nil)
	c.Select("session_root")
	generation, id, ok := c.BeginSwitch()
	if !ok || id != "session_root" {
		t.Fatalf("parent switch = %s %v", id, ok)
	}
	c.NavigateTree(true)
	c.ToggleExpanded("session_root")
	assertVisibleSessions(t, c, "session_root", "session_other")
	if !c.ResolveSwitch(generation, nil) || c.Open {
		t.Fatal("switch did not close")
	}
}

func TestSessionHierarchyRenameAndDeletePreserveReachability(t *testing.T) {
	c := &sessionExplorerController{}
	c.Resolve(c.Begin("session_other"), hierarchyFixture(), nil)
	c.ToggleExpanded("session_root")
	c.Select("session_child")
	c.BeginRename()
	c.SetRenameText("OAuth renamed")
	generation, _, _, ok := c.BeginRenameSave()
	if !ok {
		t.Fatal("rename not started")
	}
	c.ResolveRename(generation, protocol.SessionInfo{ID: "session_child", Name: "OAuth renamed", ParentSessionID: "session_root", ParentSessionName: "Authentication", UpdatedAt: "2026-06-06T00:00:00Z"}, nil)
	assertVisibleSessions(t, c, "session_root", "session_child", "session_sibling", "session_other")
	leaf := c.Sessions[sessionIndex(c.Sessions, "session_leaf")]
	if leaf.ParentSessionName != "OAuth renamed" {
		t.Fatalf("renamed parent = %+v", leaf)
	}
	c.Select("session_root")
	c.BeginDelete()
	generation, _, ok = c.BeginDeleteConfirm()
	if !ok || !c.ResolveDelete(generation, nil) {
		t.Fatal("delete failed")
	}
	assertVisibleSessions(t, c, "session_child", "session_other", "session_sibling")
	if c.Selection != "session_child" {
		t.Fatalf("post-delete selection = %s", c.Selection)
	}
	item := c.Snapshot().Sessions[0]
	if !item.MissingParent || item.Depth != 0 || item.ParentSessionName != "Authentication" {
		t.Fatalf("orphan = %+v", item)
	}
	c.NavigateTree(true)
	c.NavigateTree(true)
	if c.Selection != "session_leaf" {
		t.Fatalf("orphan child = %s", c.Selection)
	}
}

func TestSessionHierarchyRepairsMissingParentsAndCycles(t *testing.T) {
	items := []sessionExplorerItem{
		{ID: "a", ParentSessionID: "b"}, {ID: "b", ParentSessionID: "a"},
		{ID: "self", ParentSessionID: "self"}, {ID: "orphan", ParentSessionID: "missing"},
	}
	c := &sessionExplorerController{}
	c.Resolve(c.Begin("b"), items, nil)
	assertVisibleSessions(t, c, "a", "b", "orphan", "self")
	if c.Sessions[0].TreeParentID != "" || c.Sessions[1].TreeParentID != "a" {
		t.Fatalf("cycle repair = %+v", c.Sessions)
	}
	c.NavigateTree(false)
	if c.Selection != "a" {
		t.Fatalf("cycle navigation = %s", c.Selection)
	}
	c.NavigateTree(false)
	assertVisibleSessions(t, c, "a", "orphan", "self")
}

func TestSessionHierarchyOrphansHaveMetadataWithoutTreeAffordances(t *testing.T) {
	c := &sessionExplorerController{}
	c.Resolve(c.Begin(""), []sessionExplorerItem{{ID: "orphan", ParentSessionID: "missing"}, {ID: "self", ParentSessionID: "self"}}, nil)
	assertVisibleSessions(t, c, "orphan", "self")
	got := c.Snapshot().Sessions
	if got[0].Tree || !got[0].MissingParent || got[1].Tree || !got[1].InvalidParent {
		t.Fatalf("orphan projection = %+v", got)
	}
}
