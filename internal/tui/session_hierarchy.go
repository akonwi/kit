package tui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis/ui"
)

const sessionTreeMaxIndentDepth = 6

// orderedSessions projects a forest without discarding sessions whose parents
// are unavailable. Broken cycles are rooted deterministically by session ID.
func orderedSessions(sessions []sessionExplorerItem) []sessionExplorerItem {
	items := make(map[string]sessionExplorerItem, len(sessions))
	ids := make([]string, 0, len(sessions))
	for _, item := range sessions {
		if _, exists := items[item.ID]; exists {
			continue
		}
		item.Depth, item.ChildCount = 0, 0
		item.Tree, item.Expanded, item.MissingParent, item.InvalidParent = false, false, false, false
		items[item.ID] = item
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	parents := make(map[string]string, len(items))
	for _, id := range ids {
		item := items[id]
		if item.ParentSessionID != "" {
			if _, exists := items[item.ParentSessionID]; exists {
				parents[id] = item.ParentSessionID
			} else {
				item.MissingParent = true
				items[id] = item
			}
		}
	}
	done := make(map[string]bool, len(items))
	for _, id := range ids {
		path := make(map[string]bool)
		for current := id; current != "" && !done[current]; current = parents[current] {
			if path[current] {
				delete(parents, current)
				item := items[current]
				item.InvalidParent = true
				items[current] = item
				break
			}
			path[current] = true
		}
		for visited := range path {
			done[visited] = true
		}
	}
	tree := len(parents) > 0
	children := make(map[string][]string)
	for _, id := range ids {
		children[parents[id]] = append(children[parents[id]], id)
	}
	activity := make(map[string]time.Time, len(items))
	var latest func(string) time.Time
	latest = func(id string) time.Time {
		value := sessionTimestamp(items[id].UpdatedAt)
		for _, child := range children[id] {
			if childTime := latest(child); childTime.After(value) {
				value = childTime
			}
		}
		activity[id] = value
		return value
	}
	for _, id := range children[""] {
		latest(id)
	}
	for parent, siblings := range children {
		sort.SliceStable(siblings, func(i, j int) bool {
			left, right := sessionTimestamp(items[siblings[i]].UpdatedAt), sessionTimestamp(items[siblings[j]].UpdatedAt)
			if parent == "" {
				left, right = activity[siblings[i]], activity[siblings[j]]
			}
			if left.Equal(right) {
				return siblings[i] < siblings[j]
			}
			return left.After(right)
		})
	}
	ordered := make([]sessionExplorerItem, 0, len(items))
	var appendBranch func(string, int)
	appendBranch = func(id string, depth int) {
		item := items[id]
		item.Depth, item.ChildCount, item.Tree = depth, len(children[id]), tree
		// Navigation uses the effective parent, including repaired cycles.
		item.TreeParentID = parents[id]
		ordered = append(ordered, item)
		for _, child := range children[id] {
			appendBranch(child, depth+1)
		}
	}
	for _, id := range children[""] {
		appendBranch(id, 0)
	}
	return ordered
}

func (c *sessionExplorerController) visibleSessions() []sessionExplorerItem {
	if strings.TrimSpace(c.Query) != "" {
		return c.filteredSessions()
	}
	visible := make([]sessionExplorerItem, 0, len(c.Sessions))
	hiddenBelow := -1
	for _, item := range c.Sessions {
		if hiddenBelow >= 0 && item.Depth > hiddenBelow {
			continue
		}
		hiddenBelow = -1
		item.Expanded = c.expanded[item.ID]
		visible = append(visible, item)
		if item.ChildCount > 0 && !item.Expanded {
			hiddenBelow = item.Depth
		}
	}
	return visible
}

func (c *sessionExplorerController) expandAncestors(id string) {
	for index := sessionIndex(c.Sessions, id); index >= 0; {
		parent := c.Sessions[index].TreeParentID
		if parent == "" {
			return
		}
		c.expanded[parent] = true
		index = sessionIndex(c.Sessions, parent)
	}
}

func (c *sessionExplorerController) NavigateTree(right bool) {
	if strings.TrimSpace(c.Query) != "" {
		return
	}
	if _, ok := c.ActivatableSelection(); !ok {
		return
	}
	item := c.Sessions[sessionIndex(c.Sessions, c.Selection)]
	if right {
		if item.ChildCount == 0 {
			return
		}
		if !c.expanded[item.ID] {
			c.ToggleExpanded(item.ID)
		} else {
			c.Move(1)
		}
	} else if item.ChildCount > 0 && c.expanded[item.ID] {
		c.ToggleExpanded(item.ID)
	} else if item.TreeParentID != "" {
		c.Select(item.TreeParentID)
	}
}

func (c *sessionExplorerController) ToggleExpanded(id string) {
	if _, ok := c.ActivatableSelection(); !ok {
		return
	}
	visible := c.visibleSessions()
	index := sessionIndex(visible, id)
	if index < 0 {
		return
	}
	item := visible[index]
	if item.ChildCount == 0 {
		return
	}
	c.Select(id)
	if c.expanded == nil {
		c.expanded = make(map[string]bool)
	}
	c.expanded[id] = !c.expanded[id]
	c.requestReveal()
}

func sessionTreePrefix(item sessionExplorerItem) string {
	if !item.Tree {
		return ""
	}
	// Preserve label space even for unusually deep lineages on narrow terminals.
	prefix := strings.Repeat("  ", min(item.Depth, sessionTreeMaxIndentDepth))
	if item.Depth > sessionTreeMaxIndentDepth {
		prefix += glyphEllipsis + " "
	}
	if item.ChildCount == 0 {
		return prefix + "  "
	}
	if item.Expanded {
		return prefix + glyphTriangleDown + " "
	}
	return prefix + glyphTriangleRight + " "
}

func sessionExplorerLabelSpans(item sessionExplorerItem, primary, secondary ui.Style) []ui.TextSpan {
	spans := []ui.TextSpan{{Text: sessionExplorerItemLabel(item), Style: primary}}
	if item.ChildCount > 0 && !item.Expanded {
		label := " children"
		if item.ChildCount == 1 {
			label = " child"
		}
		spans = append(spans, ui.TextSpan{Text: " · " + strconv.Itoa(item.ChildCount) + label, Style: secondary})
	}
	if item.InvalidParent {
		spans = append(spans, ui.TextSpan{Text: " · invalid parent", Style: secondary})
	} else if item.MissingParent {
		parent := strings.TrimSpace(item.ParentSessionName)
		if parent == "" {
			parent = shortSessionID(item.ParentSessionID)
		}
		spans = append(spans, ui.TextSpan{Text: " · from " + parent, Style: secondary})
	}
	return spans
}

func sessionTreeDisclosure(row sessionExplorerRow, style ui.Style) ui.Widget {
	text := ui.Text{Value: sessionTreePrefix(row.Session), Style: style, MaxLines: 1}
	if !row.Interactive || row.Session.ChildCount == 0 {
		return text
	}
	return mouseActivator{OnPressed: row.OnToggle, Child: text}
}

func sessionExplorerTreeHintText(width int, action string) string {
	if action == "" {
		action = "switch"
	}
	for _, candidate := range []string{
		"↑↓ move · ←→ fold · enter " + action + " · ctrl+r rename · ctrl+d delete · esc close",
		"←→ fold · enter " + action + " · ctrl+r rename · ctrl+d delete · esc close",
		"←→ fold · enter " + action + " · ctrl+r rename · esc close",
	} {
		if uucode.StringWidth(candidate) <= width {
			return candidate
		}
	}
	return "←→ fold · enter " + action + " · esc close"
}
