package tui

import "go.rockorager.dev/vaxis/ui"

type workspacePickerController struct {
	Query     string
	Selection string
}

func (c *workspacePickerController) HandleKey(key ui.Key, catalog []workspacePickerItem, current workspacePaneIdentity) pickerKeyResult {
	model := pickerKeyModel{Query: c.Query, Selection: c.Selection}
	result := model.HandleKey(key, func(query string) []pickerItem {
		return workspacePickerRows(filterWorkspacePickerItems(query, catalog), current)
	})
	c.Query, c.Selection = model.Query, model.Selection
	return result
}

func (c *workspacePickerController) SetQuery(query string, catalog []workspacePickerItem, current workspacePaneIdentity) {
	model := pickerKeyModel{Query: c.Query, Selection: c.Selection}
	model.SetQuery(query, workspacePickerRows(filterWorkspacePickerItems(query, catalog), current))
	c.Query, c.Selection = model.Query, model.Selection
}

type workspacePickerItem struct {
	Identity   workspacePaneIdentity
	Descriptor workspacePaneDescriptor
	Label      string
	Metadata   string
	Available  bool
	Closable   bool
}

func workspacePickerCatalog(snapshot shellSnapshot) []workspacePickerItem {
	workspace := snapshot.Workspace
	items := []workspacePickerItem{{
		Identity: workspaceAgentIdentity, Label: "Agent", Metadata: "conversation", Available: true,
	}}
	labels := workspacePaneLabels(snapshot, workspace.Panes)
	for paneIndex, descriptor := range workspace.Panes {
		definition, ok := workspacePaneDefinitions[descriptor.Kind]
		if !ok {
			continue
		}
		identity, err := definition.Identity(descriptor)
		if err != nil {
			continue
		}
		available := definition.Available(snapshot, descriptor)
		metadata := string(descriptor.Kind)
		if descriptor.Kind == workspacePaneSubagentConversation {
			metadata = "subagent"
			for _, conversation := range snapshot.SubagentConversations {
				if conversation.ID == descriptor.ResourceID {
					metadata = conversation.State
					break
				}
			}
		}
		items = append(items, workspacePickerItem{
			Identity: identity, Descriptor: descriptor, Label: labels[paneIndex], Metadata: metadata,
			Available: available, Closable: definition.Closable,
		})
	}
	return items
}

func filterWorkspacePickerItems(query string, catalog []workspacePickerItem) []workspacePickerItem {
	return ui.DefaultFuzzySelectFilter(query, catalog, func(item workspacePickerItem) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: item.Label, Description: item.Metadata + " " + item.Descriptor.ResourceID}
	})
}

func workspacePickerRows(items []workspacePickerItem, current workspacePaneIdentity) []pickerItem {
	rows := make([]pickerItem, 0, len(items))
	for _, item := range items {
		row := pickerItem{Key: string(item.Identity), Label: item.Label, Meta: item.Metadata, Current: item.Identity == current}
		if !item.Available {
			row.DisabledReason = "unavailable"
		}
		rows = append(rows, row)
	}
	return rows
}

func workspacePickerItemByKey(items []workspacePickerItem, key string) (workspacePickerItem, bool) {
	for _, item := range items {
		if string(item.Identity) == key {
			return item, true
		}
	}
	return workspacePickerItem{}, false
}

func (w shellView) workspacePickerItems() []workspacePickerItem {
	return filterWorkspacePickerItems(w.Snapshot.WorkspacePickerQuery, workspacePickerCatalog(w.Snapshot))
}

func (w shellView) workspacePickerDialog(ui.BuildContext, ui.Theme) ui.Widget {
	catalog := workspacePickerCatalog(w.Snapshot)
	items := filterWorkspacePickerItems(w.Snapshot.WorkspacePickerQuery, catalog)
	activate := func(ctx ui.EventContext, key string) {
		item, ok := workspacePickerItemByKey(items, key)
		if !ok || !item.Available {
			return
		}
		if item.Identity == workspaceAgentIdentity {
			if w.Callbacks.ShowTranscript != nil {
				w.Callbacks.ShowTranscript(ctx)
			}
		} else if w.Callbacks.SelectWorkspacePane != nil {
			w.Callbacks.SelectWorkspacePane(ctx, item.Descriptor)
		}
		if w.Callbacks.CloseWorkspacePicker != nil {
			w.Callbacks.CloseWorkspacePicker(ctx)
		}
	}
	cursor := len(w.Snapshot.WorkspacePickerQuery)
	return palettePicker{
		Title: "Open workspace tab",
		Search: &textInputConfig{
			Value: w.Snapshot.WorkspacePickerQuery, Placeholder: "Search workspace tabs…", CursorOffset: &cursor,
			OnChanged: w.Callbacks.WorkspacePickerQuery, AutoFocus: true,
		},
		Items:     workspacePickerRows(items, w.Snapshot.Workspace.Selected),
		Catalog:   workspacePickerRows(catalog, w.Snapshot.Workspace.Selected),
		Selection: w.Snapshot.WorkspacePickerSelection,
		Message: func() string {
			if len(items) == 0 {
				return "No matching tabs"
			}
			return ""
		}(),
		Footer:     "↑↓ move · enter open · ctrl+d close tab · esc close",
		OnActivate: activate,
	}
}
