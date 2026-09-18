package tui

import (
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

// filteredSessions searches only explicit session names, never IDs or paths.
// Matching children are independent results, even under collapsed ancestors.
func (c *sessionExplorerController) filteredSessions() []sessionExplorerItem {
	query := strings.ToLower(strings.TrimSpace(c.Query))
	matches := make([]sessionExplorerItem, 0)
	for _, item := range c.Sessions {
		if strings.TrimSpace(item.Name) == "" || !strings.Contains(strings.ToLower(item.Name), query) {
			continue
		}
		item.Tree, item.Expanded = false, false
		item.Depth, item.ChildCount = 0, 0
		item.MissingParent = item.ParentSessionID != ""
		matches = append(matches, item)
	}
	return matches
}

func (c *sessionExplorerController) SetQuery(query string) {
	if !c.Open || c.Switching || c.RenameOpen || c.DeleteOpen || c.Query == query {
		return
	}
	c.Query = query
	c.reconcileVisibleSelection()
	c.SwitchError, c.DeleteError = "", ""
	c.requestReveal()
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

// HandleEditorKey follows the command palette's controller-owned query input,
// including input delivered before the newly opened field has painted.
func (c *sessionExplorerController) HandleEditorKey(key ui.Key) bool {
	if !c.Open || key.EventType == ui.EventRelease || key.MatchString("Escape") || key.MatchString("Ctrl+c") {
		return false
	}
	if c.Switching || c.RenameOpen || c.DeleteOpen {
		return true
	}
	query := c.Query
	if key.EventType == vaxis.EventPaste {
		c.SetQuery(query + palettePasteText(key))
		return true
	}
	if key.MatchString("Ctrl+u") {
		c.SetQuery("")
		return true
	}
	modifiers := key.Modifiers &^ (vaxis.ModShift | vaxis.ModCapsLock | vaxis.ModNumLock)
	if modifiers != 0 {
		return false
	}
	switch {
	case key.MatchString("Backspace"):
		runes := []rune(query)
		if len(runes) > 0 {
			query = string(runes[:len(runes)-1])
		}
	case key.Text != "":
		query += key.Text
	default:
		return true
	}
	c.SetQuery(query)
	return true
}

func (w sessionExplorerSurface) queryField(theme ui.Theme) ui.Widget {
	theme.Surface, theme.SurfaceHovered = theme.Background, theme.Background
	cursor := len(w.Snapshot.Query)
	return ui.Padding(ui.Insets{Left: 2, Right: 2}, ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
		ui.Text{Value: "> ", Style: ui.Style{Foreground: theme.MutedForeground}},
		textInput(theme, textInputConfig{
			Value: w.Snapshot.Query, Placeholder: "Filter session names…", CursorOffset: &cursor,
			AutoFocus: true, OnChanged: w.Callbacks.QueryChanged,
		}),
	}})
}

func sessionFilterHintText(width int, action string) string {
	if action == "" {
		action = "switch"
	}
	for _, candidate := range []string{
		"↑↓ move · enter " + action + " · ctrl+d delete · ctrl+u clear · esc close",
		"enter " + action + " · ctrl+d delete · ctrl+u clear · esc close",
	} {
		if len([]rune(candidate)) <= width {
			return candidate
		}
	}
	return "enter " + action + " · ctrl+u clear · esc close"
}
