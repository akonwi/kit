package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

type moveWorkspacePickerIntent struct{ Delta int }

func (moveWorkspacePickerIntent) IntentType() ui.IntentType { return "kit.workspace-picker.move" }

type closeWorkspacePickerPaneIntent struct{}

func (closeWorkspacePickerPaneIntent) IntentType() ui.IntentType {
	return "kit.workspace-picker.close-pane"
}

type workspacePickerItem struct {
	Identity   workspacePaneIdentity
	Descriptor workspacePaneDescriptor
	Label      string
	Metadata   string
	Available  bool
	Closable   bool
}

func (w shellView) workspacePickerItems() []workspacePickerItem {
	workspace := w.workspaceSnapshot()
	items := []workspacePickerItem{{
		Identity: workspaceAgentIdentity, Label: "Agent", Metadata: "conversation", Available: true,
	}}
	labels := workspacePaneLabels(w.Snapshot, workspace.Panes)
	for paneIndex, descriptor := range workspace.Panes {
		definition, ok := workspacePaneDefinitions[descriptor.Kind]
		if !ok {
			continue
		}
		identity, err := definition.Identity(descriptor)
		if err != nil {
			continue
		}
		available := definition.Available(w.Snapshot, descriptor)
		metadata := string(descriptor.Kind)
		if descriptor.Kind == workspacePaneSubagentConversation {
			metadata = "unavailable"
			for _, conversation := range w.Snapshot.SubagentConversations {
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
	query := strings.ToLower(strings.TrimSpace(w.Snapshot.WorkspacePickerQuery))
	if query == "" {
		return items
	}
	filtered := make([]workspacePickerItem, 0, len(items))
	for _, item := range items {
		haystack := strings.ToLower(item.Label + " " + item.Metadata + " " + item.Descriptor.ResourceID)
		if strings.Contains(haystack, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (w shellView) workspacePickerDialog(ctx ui.BuildContext, theme ui.Theme) ui.Widget {
	items := w.workspacePickerItems()
	rowPresentation := resolvePickerRowPresentation(ctx, theme)
	selection := 0
	if len(items) > 0 {
		selection = min(max(0, w.Snapshot.WorkspacePickerSelection), len(items)-1)
	}
	activate := func(ctx ui.EventContext, item workspacePickerItem) {
		if !item.Available {
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
	closePane := func(ctx ui.EventContext, item workspacePickerItem) {
		if item.Closable && w.Callbacks.CloseWorkspacePane != nil {
			w.Callbacks.CloseWorkspacePane(ctx, item.Descriptor)
		}
	}
	rows := make([]ui.Widget, 0, len(items))
	for index, item := range items {
		item := item
		marker := "  "
		if item.Identity == w.workspaceSnapshot().Selected {
			marker = glyphCheck + " "
		}
		selected := index == selection
		background := theme.Background
		primary := rowPresentation.ItemText
		secondary := theme.MutedForeground
		rowTheme := rowPresentation.Theme
		if selected {
			background = rowPresentation.FocusedBg
			primary = rowPresentation.FocusedText
			secondary = rowPresentation.FocusedText
		}
		if !item.Available {
			primary = theme.DisabledForeground
			secondary = theme.DisabledForeground
			rowTheme.Primary = theme.SurfaceHovered
			rowTheme.PrimaryHovered = theme.SurfaceHovered
			if selected {
				background = theme.SurfaceHovered
			}
		}
		main := ui.Provider[ui.Theme]{Value: rowTheme, Child: ui.ListTile{
			Selected: selected, Disabled: !item.Available, MinHeight: 1, Padding: ui.Insets{Left: 1},
			OnPressed: func(ctx ui.EventContext) { activate(ctx, item) },
			Title: ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
				ui.Expanded(ui.Text{Value: marker + item.Label, Style: ui.Style{Foreground: primary, Background: background}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
				ui.Text{Value: item.Metadata, Style: ui.Style{Foreground: secondary, Background: background}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
			}},
		}}
		children := []ui.Widget{ui.Expanded(main)}
		if item.Closable {
			children = append(children, mouseActivator{
				OnPressed: func(ctx ui.EventContext) { closePane(ctx, item) },
				Child: ui.SizedBox{Width: 3, Height: 1, Child: ui.Text{
					Value: " " + glyphTimes + " ", Style: ui.Style{Foreground: secondary, Background: background}, MaxLines: 1, Overflow: ui.TextOverflowClip,
				}},
			})
		}
		rows = append(rows, ui.Flex{Axis: ui.Horizontal, Children: children})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text{Value: "No matching tabs", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
	}

	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	queryCursor := len(w.Snapshot.WorkspacePickerQuery)
	query := ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
		ui.Text{Value: ">", Style: ui.Style{Foreground: theme.Foreground}},
		ui.SizedBox{Width: 1},
		textInput(fieldTheme, textInputConfig{
			Value: w.Snapshot.WorkspacePickerQuery, Placeholder: "Search workspace tabs…", CursorOffset: &queryCursor,
			OnChanged: w.Callbacks.WorkspacePickerQuery, AutoFocus: true,
			OnSubmitted: func(ctx ui.EventContext, _ string) {
				if len(items) > 0 {
					activate(ctx, items[selection])
				}
			},
		}),
	}}
	body := ui.Padding(ui.Insets{Top: 1, Right: 2, Left: 2}, ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Text{Value: "Open workspace tab", Style: ui.Style{Foreground: theme.Foreground}, MaxLines: 1},
			ui.SizedBox{Height: 1}, query, ui.SizedBox{Height: 1},
			ui.Expanded(ui.ScrollView{Controller: w.Snapshot.WorkspacePickerScroll, Child: ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}}),
		},
	})
	footer := ui.Text{Value: "↑↓ move · enter open · ctrl+d close tab · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}
	content := pickerDialogContent(theme, body, footer)
	actions := map[ui.IntentType]ui.ActionFunc{
		moveWorkspacePickerIntent{}.IntentType(): func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if len(items) > 0 && w.Callbacks.WorkspacePickerSelection != nil {
				next := min(max(0, selection+intent.(moveWorkspacePickerIntent).Delta), len(items)-1)
				w.Callbacks.WorkspacePickerSelection(ctx, next)
			}
			return ui.EventHandled
		},
		closeWorkspacePickerPaneIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if len(items) > 0 {
				closePane(ctx, items[selection])
			}
			return ui.EventHandled
		},
		ui.DismissIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.CloseWorkspacePicker != nil {
				w.Callbacks.CloseWorkspacePicker(ctx)
			}
			return ui.EventHandled
		},
	}
	content = ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{
		"Up": moveWorkspacePickerIntent{Delta: -1}, "Down": moveWorkspacePickerIntent{Delta: 1},
		"Ctrl+d": closeWorkspacePickerPaneIntent{},
	}, Child: content}}
	return pickerDialogPositioner{
		Percent: 70, MinWidth: 44, MaxWidth: 96, Height: pickerModalMinHeight,
		Child: ui.FocusScope{Trap: true, AutoFocus: true, Child: content},
	}
}
