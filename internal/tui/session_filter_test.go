package tui

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
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

func TestSessionFilterInputBeforePaintAndReservedActions(t *testing.T) {
	c := &sessionExplorerController{}
	generation := c.Begin("session_other")
	for _, value := range []string{"j", "k", "r"} {
		key := ui.Key{Text: value, Keycode: []rune(value)[0]}
		if c.HandleKey(key) || !c.HandleEditorKey(key) {
			t.Fatalf("text key %q was not routed to filter", value)
		}
	}
	if c.Query != "jkr" || c.RenameOpen {
		t.Fatalf("typed query = %+v", c)
	}
	c.Resolve(generation, []sessionExplorerItem{{ID: "session_match", Name: "jkr project"}}, nil)
	assertVisibleSessions(t, c, "session_match")
	if !c.HandleKey(ui.Key{Keycode: 'r', Modifiers: vaxis.ModCtrl}) || !c.RenameOpen {
		t.Fatal("ctrl+r did not rename")
	}
	c.CancelRename()
	c.HandleEditorKey(ui.Key{Keycode: 'u', Modifiers: vaxis.ModCtrl})
	paste := ui.Key{Text: "OAuth\nwork", EventType: vaxis.EventPaste}
	if c.HandleKey(paste) || !c.HandleEditorKey(paste) || c.Query != "OAuth work" {
		t.Fatalf("pasted query = %q", c.Query)
	}
	c.SetQuery("Résumé")
	c.HandleEditorKey(ui.Key{Keycode: vaxis.KeyBackspace})
	if c.Query != "Résum" {
		t.Fatalf("unicode backspace = %q", c.Query)
	}
	for _, key := range []ui.Key{{Keycode: vaxis.KeyEsc}, {Keycode: 'c', Modifiers: vaxis.ModCtrl}} {
		if c.HandleKey(key) || c.HandleEditorKey(key) {
			t.Fatal("dismiss key was consumed by filter")
		}
	}
	c.Close()
	c.Begin("")
	if c.Query != "" {
		t.Fatalf("reopened query = %q", c.Query)
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

func TestSessionFilterVisibleInputAndEmptyResults(t *testing.T) {
	c := sessionExplorerController{}
	c.Resolve(c.Begin("session_other"), hierarchyFixture(), nil)
	query := "A long session name to filter for"
	c.SetQuery(query)
	state := &sessionExplorerHarnessState{controller: c}
	app := uitest.New(sessionExplorerHarness{State: state})
	pumpSessionExplorerFrames(app, state, 100, 20, 4)
	rows := paintedRows(app, 100, 20)
	for _, text := range []string{query, "No matching sessions", "0 of 5 sessions", "ctrl+u clear"} {
		findTextCell(t, rows, text)
	}
	assertPickerFooter(t, rows, "ctrl+u clear · esc close")
	x, y := findTextCell(t, rows, query)
	for offset, r := range []rune(query) {
		if got := app.Cell(x+offset, y).Grapheme; got != string(r) {
			t.Fatalf("query cell %d = %q, want %q", offset, got, string(r))
		}
	}
	state.SetState(func() { state.controller.SetQuery("Providers") })
	pumpSessionExplorerFrames(app, state, 100, 20, 4)
	rows = paintedRows(app, 100, 20)
	findTextCell(t, rows, "Providers · from OAuth")
	findTextCell(t, rows, "1 of 5 sessions")
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
	if !strings.Contains(app.Text(), "Child · from Parent") {
		t.Fatalf("filtered picker:\n%s", app.Text())
	}
	app.Enter()
	if got := result.selectedSession(); got != "session_child" {
		t.Fatalf("selected result = %s", got)
	}
}

func TestSessionFilterFooterShowsDeleteWhenWidthPermits(t *testing.T) {
	for _, test := range []struct {
		width        int
		action, want string
	}{
		{100, "switch", "↑↓ move · enter switch · ctrl+d delete · ctrl+u clear · esc close"},
		{60, "switch", "enter switch · ctrl+d delete · ctrl+u clear · esc close"},
		{39, "switch", "enter switch · ctrl+u clear · esc close"},
		{39, "open", "enter open · ctrl+u clear · esc close"},
	} {
		app := uitest.New(sessionExplorerHints{Filtering: true, Action: test.action})
		app.Pump(test.width, 1)
		app.Pump(test.width, 1)
		if got := strings.TrimSpace(paintedRows(app, test.width, 1)[0]); got != test.want {
			t.Fatalf("footer width %d action %s = %q, want %q", test.width, test.action, got, test.want)
		}
	}
}
