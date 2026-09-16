package tui

import (
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type workspaceFileKey struct {
	WorkspaceID string
	Path        string
}

type workspaceFilePickerRowKind uint8

const (
	workspaceFilePickerEntryRow workspaceFilePickerRowKind = iota
	workspaceFilePickerLoadingRow
	workspaceFilePickerEmptyRow
	workspaceFilePickerTruncatedRow
	workspaceFilePickerErrorRow
)

type workspaceFilePickerRow struct {
	Kind  workspaceFilePickerRowKind
	Key   workspaceFileKey
	Entry protocol.FileIndexEntry
	Text  string
}

type workspaceFilePickerController struct {
	Open           bool
	Query          string
	Workspace      protocol.WorkspaceRef
	Selection      workspaceFileKey
	Generation     uint64
	WorkspaceError string
}

func (c *workspaceFilePickerController) reset() {
	generation := c.Generation + 1
	*c = workspaceFilePickerController{Open: true, Generation: generation}
}

func (c *workspaceFilePickerController) close() {
	generation := c.Generation + 1
	*c = workspaceFilePickerController{Generation: generation}
}

func (c workspaceFilePickerController) rows(source indexedFileSource) []workspaceFilePickerRow {
	workspaceID := c.Workspace.WorkspaceID
	if c.WorkspaceError != "" {
		return []workspaceFilePickerRow{{Kind: workspaceFilePickerErrorRow, Text: c.WorkspaceError}}
	}
	if workspaceID == "" {
		return []workspaceFilePickerRow{{Kind: workspaceFilePickerLoadingRow, Text: "Loading workspace…"}}
	}
	if source.Error != "" && len(source.Entries) == 0 {
		return []workspaceFilePickerRow{{Kind: workspaceFilePickerErrorRow, Text: source.Error}}
	}
	entries := ui.DefaultFuzzySelectFilter(c.Query, source.Entries, func(entry protocol.FileIndexEntry) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: strings.TrimSuffix(entry.Path, "/")}
	})
	rows := make([]workspaceFilePickerRow, 0, len(entries)+1)
	for _, entry := range entries {
		rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerEntryRow, Key: workspaceFileKey{WorkspaceID: workspaceID, Path: entry.Path}, Entry: entry})
	}
	if len(rows) == 0 {
		switch {
		case source.Loading:
			return []workspaceFilePickerRow{{Kind: workspaceFilePickerLoadingRow, Text: "Loading indexed files…"}}
		case strings.TrimSpace(c.Query) != "":
			return []workspaceFilePickerRow{{Kind: workspaceFilePickerEmptyRow, Text: "No matching files"}}
		default:
			return []workspaceFilePickerRow{{Kind: workspaceFilePickerEmptyRow, Text: "No indexed files"}}
		}
	}
	if source.Loading {
		rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerLoadingRow, Text: "Refreshing indexed files…"})
	}
	if source.Error != "" {
		rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerErrorRow, Text: source.Error})
	}
	if source.Truncated {
		rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerTruncatedRow, Text: "Showing first 4,000 indexed paths"})
	}
	return rows
}

func (c workspaceFilePickerController) selectedIndex(rows []workspaceFilePickerRow) int {
	for index, row := range rows {
		if row.Kind == workspaceFilePickerEntryRow && row.Key == c.Selection {
			return index
		}
	}
	for index, row := range rows {
		if workspaceFilePickerRowSelectable(row) {
			return index
		}
	}
	return 0
}

func (c *workspaceFilePickerController) ensureSelection(source indexedFileSource) {
	rows := c.rows(source)
	for _, row := range rows {
		if row.Kind == workspaceFilePickerEntryRow && row.Key == c.Selection {
			return
		}
	}
	c.Selection = workspaceFileKey{}
	for _, row := range rows {
		if workspaceFilePickerRowSelectable(row) {
			c.Selection = row.Key
			return
		}
	}
}

func (c *workspaceFilePickerController) move(source indexedFileSource, delta int) {
	rows := c.rows(source)
	selectable := make([]workspaceFilePickerRow, 0, len(rows))
	for _, row := range rows {
		if workspaceFilePickerRowSelectable(row) {
			selectable = append(selectable, row)
		}
	}
	if len(selectable) == 0 || delta == 0 {
		return
	}
	index := 0
	for candidate, row := range selectable {
		if row.Key == c.Selection {
			index = candidate
			break
		}
	}
	index = (index + delta) % len(selectable)
	if index < 0 {
		index += len(selectable)
	}
	c.Selection = selectable[index].Key
}

func workspaceFilePickerRowSelectable(row workspaceFilePickerRow) bool {
	return row.Kind == workspaceFilePickerEntryRow
}
