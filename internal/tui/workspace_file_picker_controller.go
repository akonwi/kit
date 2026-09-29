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

const workspaceFilePickerEntryRow workspaceFilePickerRowKind = 0

type workspaceFilePickerRow struct {
	Kind  workspaceFilePickerRowKind
	Key   workspaceFileKey
	Entry protocol.FileIndexEntry
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

func workspaceFilePickerKeyString(key workspaceFileKey) string {
	if key.WorkspaceID == "" || key.Path == "" {
		return ""
	}
	return key.WorkspaceID + "\x00" + key.Path
}

func (c workspaceFilePickerController) pickerItems(source indexedFileSource, query string) []pickerItem {
	if c.Workspace.WorkspaceID == "" {
		return nil
	}
	entries := ui.DefaultFuzzySelectFilter(query, source.Entries, func(entry protocol.FileIndexEntry) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: strings.TrimSuffix(entry.Path, "/")}
	})
	items := make([]pickerItem, 0, len(entries))
	for _, entry := range entries {
		item := pickerItem{
			Key:   workspaceFilePickerKeyString(workspaceFileKey{WorkspaceID: c.Workspace.WorkspaceID, Path: entry.Path}),
			Label: entry.Path,
		}
		if entry.IsDir {
			item.Meta = "directory"
		}
		items = append(items, item)
	}
	return items
}

func (c workspaceFilePickerController) rowByPickerKey(source indexedFileSource, key string) (workspaceFilePickerRow, bool) {
	for _, entry := range source.Entries {
		rowKey := workspaceFileKey{WorkspaceID: c.Workspace.WorkspaceID, Path: entry.Path}
		if workspaceFilePickerKeyString(rowKey) == key {
			return workspaceFilePickerRow{Kind: workspaceFilePickerEntryRow, Key: rowKey, Entry: entry}, true
		}
	}
	return workspaceFilePickerRow{}, false
}

func (c *workspaceFilePickerController) HandleKey(source indexedFileSource, key ui.Key) pickerKeyResult {
	if !c.Open {
		return pickerKeyResult{}
	}
	model := pickerKeyModel{Query: c.Query, Selection: workspaceFilePickerKeyString(c.Selection)}
	result := model.HandleKey(key, func(query string) []pickerItem { return c.pickerItems(source, query) })
	c.Query = model.Query
	if row, ok := c.rowByPickerKey(source, model.Selection); ok {
		c.Selection = row.Key
	} else {
		c.Selection = workspaceFileKey{}
	}
	return result
}

func (c *workspaceFilePickerController) ensureSelection(source indexedFileSource) {
	items := c.pickerItems(source, c.Query)
	current := workspaceFilePickerKeyString(c.Selection)
	if pickerItemIndex(items, current) >= 0 {
		return
	}
	if row, ok := c.rowByPickerKey(source, firstEnabledPickerKey(items)); ok {
		c.Selection = row.Key
	} else {
		c.Selection = workspaceFileKey{}
	}
}

func workspaceFilePickerRowSelectable(row workspaceFilePickerRow) bool {
	return row.Kind == workspaceFilePickerEntryRow
}
