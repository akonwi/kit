package tui

import "time"

// Session explorer rows are filtered by filterPickerTree, the picker filter
// hook for hierarchies: matches rank like every picker's items, by label and
// working directory, but each is shown under its ancestors and families move
// together. The explorer needs this because a child session is meaningless
// without the parent it was forked from.

// pickerCatalog returns every session in tree order as picker items, with
// the remembered expansion as each parent's disclosure.
func (c *sessionExplorerController) pickerCatalog(now time.Time) []pickerItem {
	return sessionExplorerPickerItems(c.treeSessions(), c.CurrentSessionID, now)
}

// treeSessions returns the catalog with the remembered expansion applied.
func (c *sessionExplorerController) treeSessions() []sessionExplorerItem {
	sessions := make([]sessionExplorerItem, len(c.Sessions))
	for index, item := range c.Sessions {
		item.Expanded = c.expanded[item.ID]
		sessions[index] = item
	}
	return sessions
}

// keyModel returns the explorer's canonical picker key model.
func (c *sessionExplorerController) keyModel() pickerKeyModel {
	return pickerKeyModel{Query: c.Query, Selection: c.Selection, Filter: filterPickerTree}
}

// bestSessionMatch returns the best-ranked session for a non-blank query. It
// is highlighted even when its ancestors are listed above it.
func (c *sessionExplorerController) bestSessionMatch(query string) string {
	if matches := filterPickerItems(query, c.pickerCatalog(time.Time{})); len(matches) > 0 {
		return matches[0].Key
	}
	return ""
}

// SetQuery filters the sessions. A non-blank query highlights the first match;
// clearing it restores the previous selection or its nearest visible ancestor.
func (c *sessionExplorerController) SetQuery(query string) {
	if !c.Open || c.Switching || c.RenameOpen || c.DeleteOpen || c.Query == query {
		return
	}
	c.Query = query
	if !pickerQueryBlank(query) {
		c.Selection = c.bestSessionMatch(query)
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
