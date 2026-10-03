package tui

import (
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
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

// pickerCatalog maps every indexed path onto a picker item.
func (c workspaceFilePickerController) pickerCatalog(source indexedFileSource) []pickerItem {
	if c.Workspace.WorkspaceID == "" {
		return nil
	}
	items := make([]pickerItem, 0, len(source.Entries))
	for _, entry := range source.Entries {
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

// keyModel returns the file picker's canonical key model.
func (c workspaceFilePickerController) keyModel() pickerKeyModel {
	return pickerKeyModel{Query: c.Query, Selection: workspaceFilePickerKeyString(c.Selection), Filter: filterWorkspaceFileItems}
}

// filterWorkspaceFileItems is the file picker's filter hook. Indexed
// directories are listed with a trailing slash, which is not part of the name
// a user types: matching the bare path lets "internal" rank the internal/
// directory as an exact match rather than a prefix of it. Scoring is the shared
// filter's.
func filterWorkspaceFileItems(query string, catalog []pickerItem) []pickerItem {
	return filterPickerItemsBy(query, catalog, func(item pickerItem) ui.FuzzySelectItem {
		match := pickerFuzzyItem(item)
		match.Title = strings.TrimSuffix(match.Title, "/")
		return match
	})
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
	model := c.keyModel()
	result := model.HandleKey(key, c.pickerCatalog(source))
	c.Query = model.Query
	if row, ok := c.rowByPickerKey(source, model.Selection); ok {
		c.Selection = row.Key
	} else {
		c.Selection = workspaceFileKey{}
	}
	return result
}

func (c *workspaceFilePickerController) ensureSelection(source indexedFileSource) {
	items := c.keyModel().Items(c.pickerCatalog(source))
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
