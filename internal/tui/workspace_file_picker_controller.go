package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
)

type workspaceFileKey struct {
	WorkspaceID string
	Path        string
}

type directoryObservationKey struct {
	workspaceFileKey
	Revision string
}

type workspaceFilePickerRowKind uint8

const (
	workspaceFilePickerEntryRow workspaceFilePickerRowKind = iota
	workspaceFilePickerLoadingRow
	workspaceFilePickerEmptyRow
	workspaceFilePickerMoreRow
	workspaceFilePickerTruncatedRow
	workspaceFilePickerNoticeRow
	workspaceFilePickerErrorRow
)

type workspaceFilePickerRow struct {
	Kind  workspaceFilePickerRowKind
	Key   workspaceFileKey
	Entry protocol.WorkspaceDirectoryEntry
	Depth int
	Text  string
}

type workspaceFilePickerObservation struct {
	Entries          []protocol.WorkspaceDirectoryEntry
	NextCursor       string
	Truncated        bool
	TruncationReason string
	Omissions        []protocol.WorkspaceOmission
}

type workspaceFilePickerController struct {
	Open          bool
	Query         string
	Workspace     protocol.WorkspaceRef
	Selection     workspaceFileKey
	SelectionKind workspaceFilePickerRowKind
	Generation    uint64

	expanded       map[workspaceFileKey]bool
	activeRevision map[workspaceFileKey]string
	observations   map[directoryObservationKey]workspaceFilePickerObservation
	loading        map[workspaceFileKey]uint64
	errors         map[workspaceFileKey]string
	requestSerial  uint64
	workspaceError bool
}

func (c *workspaceFilePickerController) reset() {
	c.Generation++
	*c = workspaceFilePickerController{
		Open: true, Generation: c.Generation,
		expanded: make(map[workspaceFileKey]bool), activeRevision: make(map[workspaceFileKey]string),
		observations: make(map[directoryObservationKey]workspaceFilePickerObservation),
		loading:      make(map[workspaceFileKey]uint64), errors: make(map[workspaceFileKey]string),
	}
}

func (c *workspaceFilePickerController) close() {
	c.Generation++
	*c = workspaceFilePickerController{Generation: c.Generation}
}

func (c *workspaceFilePickerController) setWorkspace(workspace protocol.WorkspaceRef) {
	c.Workspace = workspace
	c.workspaceError = workspace.State != protocol.WorkspaceReady
	root := workspaceFileKey{WorkspaceID: workspace.WorkspaceID}
	c.expanded[root] = true
	if c.Selection.WorkspaceID != workspace.WorkspaceID {
		c.Selection = workspaceFileKey{}
	}
}

func (c *workspaceFilePickerController) beginLoad(path, cursor string) (workspaceFileKey, uint64) {
	key := workspaceFileKey{WorkspaceID: c.Workspace.WorkspaceID, Path: path}
	c.requestSerial++
	c.loading[key] = c.requestSerial
	delete(c.errors, key)
	if cursor == "" {
		delete(c.activeRevision, key)
	}
	return key, c.requestSerial
}

func (c *workspaceFilePickerController) applyPage(key workspaceFileKey, serial uint64, cursor string, page protocol.DirectoryPage) bool {
	if !c.Open || key.WorkspaceID != c.Workspace.WorkspaceID || c.loading[key] != serial || page.Workspace.WorkspaceID != key.WorkspaceID || page.Path != key.Path {
		return false
	}
	delete(c.loading, key)
	delete(c.errors, key)
	if cursor != "" && c.activeRevision[key] != page.Revision {
		return false
	}
	observationKey := directoryObservationKey{workspaceFileKey: key, Revision: page.Revision}
	observation := c.observations[observationKey]
	if cursor == "" {
		for candidate := range c.observations {
			if candidate.workspaceFileKey == key && candidate.Revision != page.Revision {
				delete(c.observations, candidate)
			}
		}
		observation = workspaceFilePickerObservation{}
	}
	observation.Entries = append(observation.Entries, page.Entries...)
	observation.NextCursor = page.NextCursor
	observation.Truncated = page.Truncated
	observation.TruncationReason = page.TruncationReason
	observation.Omissions = append([]protocol.WorkspaceOmission(nil), page.Omissions...)
	c.observations[observationKey] = observation
	c.activeRevision[key] = page.Revision
	c.ensureSelection()
	return true
}

func (c *workspaceFilePickerController) applyError(key workspaceFileKey, serial uint64, err error) bool {
	if !c.Open || c.loading[key] != serial {
		return false
	}
	delete(c.loading, key)
	if errors.Is(err, context.Canceled) {
		return false
	}
	c.errors[key] = workspaceFilePickerErrorText(err)
	c.ensureSelection()
	return true
}

func workspaceFilePickerErrorText(err error) string {
	if err == nil {
		return "Could not load directory"
	}
	var workspaceErr *protocol.WorkspaceError
	if errors.As(err, &workspaceErr) && strings.TrimSpace(workspaceErr.Message) != "" {
		return workspaceErr.Message
	}
	return err.Error()
}

func (c *workspaceFilePickerController) observation(key workspaceFileKey) (workspaceFilePickerObservation, bool) {
	revision := c.activeRevision[key]
	if revision == "" {
		return workspaceFilePickerObservation{}, false
	}
	observation, ok := c.observations[directoryObservationKey{workspaceFileKey: key, Revision: revision}]
	return observation, ok
}

func (c *workspaceFilePickerController) toggleDirectory(entry protocol.WorkspaceDirectoryEntry) (load bool) {
	key := workspaceFileKey{WorkspaceID: c.Workspace.WorkspaceID, Path: entry.Path}
	if c.expanded[key] {
		delete(c.expanded, key)
		c.ensureSelection()
		return false
	}
	c.expanded[key] = true
	_, loaded := c.observation(key)
	return !loaded && c.loading[key] == 0
}

func (c *workspaceFilePickerController) rows() []workspaceFilePickerRow {
	if c.Workspace.WorkspaceID == "" {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(c.Query))
	var rows []workspaceFilePickerRow
	var appendDirectory func(string, int)
	appendDirectory = func(path string, depth int) {
		key := workspaceFileKey{WorkspaceID: c.Workspace.WorkspaceID, Path: path}
		observation, loaded := c.observation(key)
		if !loaded {
			if c.loading[key] != 0 {
				rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerLoadingRow, Key: key, Depth: depth, Text: "Loading…"})
			} else if message := c.errors[key]; message != "" {
				rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerErrorRow, Key: key, Depth: depth, Text: message})
			}
			return
		}
		if len(observation.Entries) == 0 && len(observation.Omissions) == 0 {
			rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerEmptyRow, Key: key, Depth: depth, Text: "Empty directory"})
		}
		for _, entry := range observation.Entries {
			entryKey := workspaceFileKey{WorkspaceID: key.WorkspaceID, Path: entry.Path}
			if query == "" || strings.Contains(strings.ToLower(entry.Path), query) {
				rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerEntryRow, Key: entryKey, Entry: entry, Depth: depth})
			}
			if entry.Kind == protocol.WorkspaceEntryDirectory && c.expanded[entryKey] {
				appendDirectory(entry.Path, depth+1)
			}
		}
		if c.loading[key] != 0 {
			rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerLoadingRow, Key: key, Depth: depth, Text: "Loading…"})
		} else if observation.NextCursor != "" {
			rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerMoreRow, Key: key, Depth: depth, Text: "Load more…"})
		}
		omitted := 0
		for _, omission := range observation.Omissions {
			omitted += omission.Count
		}
		if omitted > 0 {
			rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerNoticeRow, Key: key, Depth: depth, Text: fmt.Sprintf("%d entries omitted", omitted)})
		}
		if observation.Truncated {
			text := "Directory truncated"
			if observation.TruncationReason != "" {
				text += " (" + strings.ReplaceAll(observation.TruncationReason, "_", " ") + ")"
			}
			rows = append(rows, workspaceFilePickerRow{Kind: workspaceFilePickerTruncatedRow, Key: key, Depth: depth, Text: text})
		}
	}
	appendDirectory("", 0)
	return rows
}

func (c *workspaceFilePickerController) selectedIndex(rows []workspaceFilePickerRow) int {
	for index, row := range rows {
		if row.Kind == c.SelectionKind && row.Key == c.Selection && workspaceFilePickerRowSelectable(row) {
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

func (c *workspaceFilePickerController) move(delta int) {
	rows := c.rows()
	if len(rows) == 0 || delta == 0 {
		return
	}
	index := c.selectedIndex(rows)
	for next := index + delta; next >= 0 && next < len(rows); next += delta {
		if workspaceFilePickerRowSelectable(rows[next]) {
			c.Selection = rows[next].Key
			c.SelectionKind = rows[next].Kind
			return
		}
	}
}

func (c *workspaceFilePickerController) ensureSelection() {
	rows := c.rows()
	for _, row := range rows {
		if row.Kind == c.SelectionKind && row.Key == c.Selection && workspaceFilePickerRowSelectable(row) {
			return
		}
	}
	c.Selection = workspaceFileKey{}
	c.SelectionKind = workspaceFilePickerEntryRow
	for _, row := range rows {
		if workspaceFilePickerRowSelectable(row) {
			c.Selection = row.Key
			c.SelectionKind = row.Kind
			return
		}
	}
}

func (c *workspaceFilePickerController) expandedPaths() []string {
	paths := make([]string, 0, len(c.expanded))
	for key, expanded := range c.expanded {
		if expanded && key.WorkspaceID == c.Workspace.WorkspaceID {
			paths = append(paths, key.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

func workspaceFilePickerRowSelectable(row workspaceFilePickerRow) bool {
	return row.Kind == workspaceFilePickerEntryRow || row.Kind == workspaceFilePickerMoreRow || row.Kind == workspaceFilePickerErrorRow
}
