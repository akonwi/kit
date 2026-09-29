package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

// filteredSessions fuzzy-matches only explicit session names, never IDs or
// paths. Matches are flat results, even under collapsed ancestors, and keep
// their parent as a lineage note.
func (c *sessionExplorerController) filteredSessions(query string) []sessionExplorerItem {
	named := make([]sessionExplorerItem, 0, len(c.Sessions))
	for _, item := range c.Sessions {
		if strings.TrimSpace(item.Name) != "" {
			named = append(named, item)
		}
	}
	matches := ui.DefaultFuzzySelectFilter(query, named, func(item sessionExplorerItem) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: item.Name}
	})
	for index, match := range matches {
		match = flatSessionExplorerItem(match)
		if match.ParentSessionName == "" {
			if parent := sessionIndex(c.Sessions, match.ParentSessionID); parent >= 0 {
				match.ParentSessionName = c.Sessions[parent].Name
			}
		}
		matches[index] = match
	}
	return matches
}

// flatSessionExplorerItem is a session shown outside the tree.
func flatSessionExplorerItem(item sessionExplorerItem) sessionExplorerItem {
	item.Tree, item.Expanded = false, false
	item.Depth, item.ChildCount = 0, 0
	item.MissingParent = item.MissingParent || item.ParentSessionID != ""
	return item
}

// SetQuery filters the sessions. A non-blank query highlights the first match;
// clearing it restores the previous selection or its nearest visible ancestor.
func (c *sessionExplorerController) SetQuery(query string) {
	if !c.Open || c.Switching || c.RenameOpen || c.DeleteOpen || c.Query == query {
		return
	}
	c.Query = query
	if strings.TrimSpace(query) != "" {
		c.Selection = firstEnabledPickerKey(c.pickerItems(query))
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
