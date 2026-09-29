package tui

import "go.rockorager.dev/vaxis/ui"

type workspacePickerController struct {
	Query     string
	Selection string
}

func (c *workspacePickerController) HandleKey(key ui.Key, catalog []workspacePickerItem, current workspacePaneIdentity) pickerKeyResult {
	model := pickerKeyModel{Query: c.Query, Selection: c.Selection}
	result := model.HandleKey(key, workspacePickerRows(catalog, current))
	c.Query, c.Selection = model.Query, model.Selection
	return result
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

func workspacePickerRows(items []workspacePickerItem, current workspacePaneIdentity) []pickerItem {
	rows := make([]pickerItem, 0, len(items))
	for _, item := range items {
		row := pickerItem{
			Key: string(item.Identity), Label: item.Label, Meta: item.Metadata, Current: item.Identity == current,
			SearchText: item.Metadata + " " + item.Descriptor.ResourceID,
		}
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

func (w shellView) workspacePickerDialog(ui.BuildContext, ui.Theme) ui.Widget {
	catalog := workspacePickerCatalog(w.Snapshot)
	rows := workspacePickerRows(catalog, w.Snapshot.Workspace.Selected)
	activate := func(ctx ui.EventContext, key string) {
		item, ok := workspacePickerItemByKey(catalog, key)
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
	result := palettePicker{
		Title: "Open workspace tab",
		Query: w.Snapshot.WorkspacePickerQuery, Search: &pickerSearch{Placeholder: "Search workspace tabs…"},
		Catalog: rows, Selection: w.Snapshot.WorkspacePickerSelection,
		Footer:     "↑↓ move · enter open · ctrl+d close tab · esc close",
		OnActivate: activate,
	}
	if len(result.items()) == 0 {
		result.Message = "No matching tabs"
	}
	return result
}
