package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

// pickerLabelTruncation controls how a label is shortened when it exceeds its
// column width.
type pickerLabelTruncation uint8

const (
	// pickerLabelTruncationEnd keeps the beginning of a label, which is the
	// standard picker behavior.
	pickerLabelTruncationEnd pickerLabelTruncation = iota
	// pickerLabelTruncationStart keeps the end of a label, useful when its
	// identifying part is a suffix such as a file name.
	pickerLabelTruncationStart
)

// pickerItem is one picker row. Every row is a single line in uniform columns:
// label, optional hint, optional description, and optional trailing metadata.
// Current marks the active value; DisabledReason makes the row unavailable.
//
// Hierarchical pickers set Depth to indent the label two cells per level, and
// Disclosure with ChildCount to show "▸ N" or "▾ N" at the start of the hint
// column, followed by Hint when both are present. ParentKey names the item's
// parent row so a filter can keep a match under its ancestors.
//
// SearchText and SearchAliases are what the shared filter matches besides
// Label; displayed columns other than the label are not matched.
type pickerItem struct {
	Key             string
	Label           string
	LabelTruncation pickerLabelTruncation
	Hint            string
	Description     string
	Meta            string
	Current         bool
	DisabledReason  string
	Depth           int
	Disclosure      pickerDisclosure
	ChildCount      int
	ParentKey       string
	SearchText      string
	SearchAliases   []string
}

// pickerFilter returns the catalog items matching query in display order.
// Every picker filters with filterPickerItems unless it documents why it
// needs one of the exceptions that build on it.
type pickerFilter func(query string, catalog []pickerItem) []pickerItem

// filterPickerItems is the shared picker filter. It fuzzy-matches the label,
// then SearchText, then SearchAliases, ranks by score, and keeps catalog order
// for ties. A blank query returns the catalog unchanged.
func filterPickerItems(query string, catalog []pickerItem) []pickerItem {
	return filterPickerItemsBy(query, catalog, pickerFuzzyItem)
}

// filterPickerItemsBy ranks catalog with the shared fuzzy matcher using match
// to describe each item; custom filters use it to adjust what one item
// offers to the matcher without changing how matches are scored.
func filterPickerItemsBy(query string, catalog []pickerItem, match func(pickerItem) ui.FuzzySelectItem) []pickerItem {
	return ui.DefaultFuzzySelectFilter(query, catalog, match)
}

func pickerFuzzyItem(item pickerItem) ui.FuzzySelectItem {
	return ui.FuzzySelectItem{Title: item.Label, Description: item.SearchText, Aliases: item.SearchAliases}
}

// filterPickerTree filters a hierarchical catalog listed in tree order, where
// ParentKey links each child to its parent and Disclosure records whether a
// parent is expanded.
//
// A blank query shows the tree, hiding the descendants of collapsed rows. A
// non-blank query ranks matches with filterPickerItems and shows each match
// under its ancestors so it keeps its context. Each family (the rows sharing
// a root) moves as one group placed by its best-ranked match, and rows within
// a family keep tree order. Filtered rows have no disclosure because every
// path to a match is open.
func filterPickerTree(query string, catalog []pickerItem) []pickerItem {
	if pickerQueryBlank(query) {
		visible := make([]pickerItem, 0, len(catalog))
		hiddenBelow := -1
		for _, item := range catalog {
			if hiddenBelow >= 0 && item.Depth > hiddenBelow {
				continue
			}
			hiddenBelow = -1
			visible = append(visible, item)
			if item.Disclosure == pickerDisclosureCollapsed {
				hiddenBelow = item.Depth
			}
		}
		return visible
	}
	matches := filterPickerItems(query, catalog)
	if len(matches) == 0 {
		return []pickerItem{}
	}
	parents := make(map[string]string, len(catalog))
	for _, item := range catalog {
		parents[item.Key] = item.ParentKey
	}
	shown := make(map[string]bool, len(matches))
	families := make([]string, 0, len(matches))
	grouped := make(map[string]bool, len(matches))
	for _, match := range matches {
		for key := match.Key; key != "" && !shown[key]; key = parents[key] {
			shown[key] = true
		}
		if root := pickerTreeRoot(parents, match.Key); !grouped[root] {
			grouped[root] = true
			families = append(families, root)
		}
	}
	members := make(map[string][]pickerItem, len(families))
	for _, item := range catalog {
		if shown[item.Key] {
			root := pickerTreeRoot(parents, item.Key)
			item.Disclosure, item.ChildCount = pickerDisclosureNone, 0
			members[root] = append(members[root], item)
		}
	}
	visible := make([]pickerItem, 0, len(shown))
	for _, family := range families {
		visible = append(visible, members[family]...)
	}
	return visible
}

// filterPickerItemsInOrder is the history pickers' filter hook. It matches
// with filterPickerItems but keeps catalog order instead of ranking by score,
// so chronological history stays chronological and the newest match stays
// nearest the composer.
func filterPickerItemsInOrder(query string, catalog []pickerItem) []pickerItem {
	matches := filterPickerItems(query, catalog)
	if pickerQueryBlank(query) {
		return matches
	}
	matched := make(map[string]bool, len(matches))
	for _, item := range matches {
		matched[item.Key] = true
	}
	ordered := make([]pickerItem, 0, len(matches))
	for _, item := range catalog {
		if matched[item.Key] {
			ordered = append(ordered, item)
		}
	}
	return ordered
}

// lastPickerKey is the key of the last visible item, the newest entry of a
// history picker listed oldest first.
func lastPickerKey(items []pickerItem) string {
	if len(items) == 0 {
		return ""
	}
	return items[len(items)-1].Key
}

func pickerTreeRoot(parents map[string]string, key string) string {
	for seen := 0; seen <= len(parents); seen++ {
		parent := parents[key]
		if parent == "" {
			return key
		}
		key = parent
	}
	return key
}

// pickerQueryBlank reports whether query matches everything.
func pickerQueryBlank(query string) bool { return strings.TrimSpace(query) == "" }

// pickerDisclosure is the expand state of an item that has children.
type pickerDisclosure uint8

const (
	pickerDisclosureNone pickerDisclosure = iota
	pickerDisclosureCollapsed
	pickerDisclosureExpanded
)

// pickerTone colors picker messages and footer status.
type pickerTone uint8

const (
	pickerToneMuted pickerTone = iota
	pickerToneDanger
	pickerToneLoading
)
