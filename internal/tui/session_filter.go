package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

// filteredSessions returns the rows matching a query. Sessions are matched
// like every picker's items, by label and description, and ranked by the
// shared fuzzy filter. A match is shown under its ancestors, and each family
// moves as one group placed by its best-ranked match. Filtered rows have no
// disclosure because every path to a match is open.
func (c *sessionExplorerController) filteredSessions(query string) []sessionExplorerItem {
	visible, _ := c.filterSessions(query)
	return visible
}

// filterSessions returns the filtered rows and the best-ranked match.
func (c *sessionExplorerController) filterSessions(query string) ([]sessionExplorerItem, string) {
	matches := ui.DefaultFuzzySelectFilter(query, c.Sessions, func(item sessionExplorerItem) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: sessionExplorerItemLabel(item), Description: sessionDisplayCWD(item.CWD, sessionCWDMaxWidth)}
	})
	if len(matches) == 0 {
		return []sessionExplorerItem{}, ""
	}
	// Collect each match's ancestors and order families by their best match.
	shown := make(map[string]bool, len(matches))
	families := make([]string, 0, len(matches))
	grouped := make(map[string]bool, len(matches))
	for _, match := range matches {
		root := match.ID
		for id := match.ID; id != ""; {
			shown[id] = true
			root = id
			index := sessionIndex(c.Sessions, id)
			if index < 0 {
				break
			}
			id = c.Sessions[index].TreeParentID
		}
		if !grouped[root] {
			grouped[root] = true
			families = append(families, root)
		}
	}
	// Rows within a family keep tree order; c.Sessions lists each root
	// followed by its descendants.
	members := make(map[string][]sessionExplorerItem, len(families))
	root := ""
	for _, item := range c.Sessions {
		if item.TreeParentID == "" {
			root = item.ID
		}
		if shown[item.ID] {
			item.Expanded, item.ChildCount = false, 0
			members[root] = append(members[root], item)
		}
	}
	visible := make([]sessionExplorerItem, 0, len(shown))
	for _, family := range families {
		visible = append(visible, members[family]...)
	}
	return visible, matches[0].ID
}

// SetQuery filters the sessions. A non-blank query highlights the first match;
// clearing it restores the previous selection or its nearest visible ancestor.
func (c *sessionExplorerController) SetQuery(query string) {
	if !c.Open || c.Switching || c.RenameOpen || c.DeleteOpen || c.Query == query {
		return
	}
	c.Query = query
	if strings.TrimSpace(query) != "" {
		_, c.Selection = c.filterSessions(query)
	} else {
		c.reconcileVisibleSelection()
	}
	c.SwitchError, c.DeleteError = "", ""
}

func (c *sessionExplorerController) reconcileVisibleSelection() {
	visible := c.visibleSessions()
	if sessionIndex(visible, c.Selection) >= 0 {
		return
	}
	// Clearing a filter restores the matching session's nearest visible ancestor
	// without changing the user's saved expansion state.
	id := c.Selection
	for index := sessionIndex(c.Sessions, id); index >= 0; index = sessionIndex(c.Sessions, id) {
		id = c.Sessions[index].TreeParentID
		if id == "" {
			break
		}
		if sessionIndex(visible, id) >= 0 {
			c.Selection = id
			return
		}
	}
	c.Selection = ""
	if len(visible) > 0 {
		c.Selection = visible[0].ID
	}
}
