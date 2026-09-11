package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type subagentRosterItem struct {
	Name            string
	Description     string
	Model           string
	Source          protocol.SubagentSource
	Status          string
	UpdatedAt       string
	Conversation    protocol.SubagentConversation
	HasConversation bool
}

func subagentRosterItems(definitions []protocol.SubagentDefinition, conversations []protocol.SubagentConversation) []subagentRosterItem {
	conversationsByName := make(map[string]protocol.SubagentConversation, len(conversations))
	for _, conversation := range conversations {
		conversationsByName[conversation.AgentName] = conversation
	}
	items := make([]subagentRosterItem, 0, len(definitions)+len(conversations))
	definitionNames := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		definitionNames[definition.Name] = struct{}{}
		item := subagentRosterItem{
			Name: definition.Name, Description: definition.Description, Model: definition.Model,
			Source: definition.Source, Status: "inactive",
		}
		if conversation, ok := conversationsByName[definition.Name]; ok {
			item.Model = conversation.Model
			item.Status = conversation.State
			item.UpdatedAt = conversation.UpdatedAt
			item.Conversation = conversation
			item.HasConversation = true
		}
		items = append(items, item)
	}
	for _, conversation := range conversations {
		if _, ok := definitionNames[conversation.AgentName]; ok {
			continue
		}
		items = append(items, subagentRosterItem{
			Name: conversation.AgentName, Description: "Previously active subagent conversation",
			Model: conversation.Model, Status: conversation.State, UpdatedAt: conversation.UpdatedAt,
			Conversation: conversation, HasConversation: true,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := subagentStatusRank(items[i].Status), subagentStatusRank(items[j].Status)
		if left != right {
			return left < right
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

func selectedSubagentRosterItem(items []subagentRosterItem, selectedName string) (subagentRosterItem, bool) {
	if len(items) == 0 {
		return subagentRosterItem{}, false
	}
	for _, item := range items {
		if item.Name == selectedName {
			return item, true
		}
	}
	return items[0], true
}

func subagentRosterSelectionIndex(items []subagentRosterItem, selectedName string) int {
	for index, item := range items {
		if item.Name == selectedName {
			return index
		}
	}
	return -1
}

func subagentRosterOffset(items []subagentRosterItem, index int) int {
	headerOffset := 0
	for itemIndex, item := range items {
		if item.Status == "inactive" {
			if index >= itemIndex {
				headerOffset = 1
			}
			break
		}
	}
	return max(0, index*3+headerOffset-3)
}

func selectedSubagentTask(conversation protocol.SubagentConversation) (protocol.SubagentTask, bool) {
	for _, task := range conversation.Tasks {
		if task.ID == conversation.ActiveTaskID {
			return task, true
		}
	}
	for _, task := range conversation.Tasks {
		if task.State == "queued" || task.State == "running" {
			return task, true
		}
	}
	return protocol.SubagentTask{}, false
}

func subagentStatusRank(status string) int {
	switch status {
	case "running":
		return 0
	case "failed":
		return 1
	case "aborted":
		return 2
	case "interrupted":
		return 3
	case "idle":
		return 4
	default:
		return 5
	}
}

func subagentStatusPresentation(theme ui.Theme, status string) (string, string, ui.Style) {
	switch status {
	case "running":
		return glyphCircleFilled, "running", ui.Style{Foreground: theme.AccentText}
	case "failed":
		return glyphCross, "failed", ui.Style{Foreground: theme.DangerText}
	case "aborted":
		return glyphCircleSlash, "aborted", ui.Style{Foreground: theme.WarningText}
	case "interrupted":
		return glyphCircleSlash, "interrupted", ui.Style{Foreground: theme.WarningText}
	case "idle":
		return glyphCircleEmpty, "completed", ui.Style{Foreground: theme.MutedForeground}
	default:
		return glyphCircleEmpty, "available", ui.Style{Foreground: theme.MutedForeground}
	}
}

func subagentSourceLabel(source protocol.SubagentSource, hasConversation bool) string {
	switch source.Kind {
	case "plugin":
		if source.PluginID != "" {
			return "plugin:" + source.PluginID
		}
		return "plugin"
	case "user", "project":
		return source.Kind
	default:
		if hasConversation {
			return "active"
		}
		return ""
	}
}

func subagentRelativeTime(value string, now time.Time) string {
	updated, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	elapsed := now.Sub(updated)
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(elapsed/(24*time.Hour)))
	}
}

func (w shellView) subagentsPane(theme ui.Theme) ui.Widget {
	items := subagentRosterItems(w.Snapshot.SubagentDefinitions, w.Snapshot.SubagentConversations)
	selectedName := w.Snapshot.SubagentSelection
	if selectedName == "" && len(items) > 0 {
		selectedName = items[0].Name
	}
	rows := make([]ui.Widget, 0, len(items)+2)
	if len(w.Snapshot.SubagentDiagnostics) > 0 {
		rows = append(rows, ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value: fmt.Sprintf("%s %d definition warnings", glyphTriangleRight, len(w.Snapshot.SubagentDiagnostics)),
			Style: ui.Style{Foreground: theme.WarningText}, MaxLines: 1,
		}))
	}
	availableStarted := false
	for _, item := range items {
		if item.Status == "inactive" && !availableStarted {
			rows = append(rows, ui.Padding(ui.Symmetric(1, 0), ui.Text{Value: "Available", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1}))
			availableStarted = true
		}
		rows = append(rows, w.subagentRosterRow(theme, item, item.Name == selectedName))
	}

	var body ui.Widget
	if len(items) == 0 {
		body = ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Text{Value: "k i t", Style: ui.Style{Foreground: theme.Foreground}, MaxLines: 1},
			ui.Text{Value: strings.Repeat(glyphHeavyLine, 11), Style: ui.Style{Foreground: theme.AccentText}, MaxLines: 1},
			ui.Text{Value: "No subagents available", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
			ui.Text{Value: "Add .md files to $KIT_HOME/agents/", Style: ui.Style{Foreground: theme.DisabledForeground}, MaxLines: 1},
		}})
	} else {
		body = ui.Scrollbar{Child: ui.ScrollView{
			Controller: w.Snapshot.ActivityScroll,
			Child:      ui.Padding(ui.Symmetric(0, 1), ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}),
		}}
	}
	hint := "↑↓ select " + glyphMiddleDot + " esc close"
	if selected, ok := selectedSubagentRosterItem(items, selectedName); ok && selected.HasConversation {
		hint = "↑↓ select " + glyphMiddleDot + " enter open"
		if _, cancellable := selectedSubagentTask(selected.Conversation); cancellable {
			hint += " " + glyphMiddleDot + " c cancel"
		}
		hint += " " + glyphMiddleDot + " ctrl+d dismiss " + glyphMiddleDot + " esc close"
	}
	content := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.Expanded(body),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value: hint,
			Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		})},
	}})
	actions := map[ui.IntentType]ui.ActionFunc{
		moveSubagentIntent{}.IntentType(): func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if w.Callbacks.MoveSubagentSelection != nil {
				w.Callbacks.MoveSubagentSelection(ctx, intent.(moveSubagentIntent).Delta)
			}
			return ui.EventHandled
		},
		openSubagentIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if selected, ok := selectedSubagentRosterItem(items, selectedName); ok && selected.HasConversation && w.Callbacks.OpenSubagentConversation != nil {
				w.Callbacks.OpenSubagentConversation(ctx, selected.Conversation.ID)
			}
			return ui.EventHandled
		},
		cancelSubagentIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if selected, ok := selectedSubagentRosterItem(items, selectedName); ok && selected.HasConversation && w.Callbacks.CancelSubagentTask != nil {
				if task, cancellable := selectedSubagentTask(selected.Conversation); cancellable {
					w.Callbacks.CancelSubagentTask(ctx, task.ID, task.CancellationGeneration)
				}
			}
			return ui.EventHandled
		},
		dismissSubagentIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if selected, ok := selectedSubagentRosterItem(items, selectedName); ok && selected.HasConversation && w.Callbacks.DismissSubagent != nil {
				w.Callbacks.DismissSubagent(ctx, selected.Conversation.ID, selected.Conversation.Generation)
			}
			return ui.EventHandled
		},
		scrollActivityIntent{}.IntentType(): func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if w.Callbacks.ScrollActivity != nil {
				w.Callbacks.ScrollActivity(ctx, intent.(scrollActivityIntent).Pages)
			}
			return ui.EventHandled
		},
		ui.DismissIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.CloseActivity != nil {
				w.Callbacks.CloseActivity(ctx)
			}
			return ui.EventHandled
		},
	}
	if w.Snapshot.ActivityFocus != nil {
		content = ui.Focus(w.Snapshot.ActivityFocus, content)
	}
	content = ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{
		"Up": moveSubagentIntent{Delta: -1}, "Down": moveSubagentIntent{Delta: 1},
		"Enter": openSubagentIntent{}, "c": cancelSubagentIntent{}, "Ctrl+d": dismissSubagentIntent{},
		"Page_Up": scrollActivityIntent{Pages: -1}, "Page_Down": scrollActivityIntent{Pages: 1},
	}, Child: content}}
	if w.Snapshot.ActivityFocus != nil {
		content = mouseActivator{
			OnPrimaryDownCapture: func(ui.EventContext) { w.Snapshot.ActivityFocus.RequestFocus() },
			DefaultMouseShape:    true, Child: content,
		}
		content = ui.FocusScope{AutoFocus: w.Snapshot.ActivitySelected, Child: content}
	}
	return content
}

func (w shellView) subagentRosterRow(theme ui.Theme, item subagentRosterItem, selected bool) ui.Widget {
	glyph, status, statusStyle := subagentStatusPresentation(theme, item.Status)
	metadata := item.Model
	if source := subagentSourceLabel(item.Source, item.HasConversation); source != "" {
		if metadata != "" {
			metadata += " " + glyphMiddleDot + " "
		}
		metadata += source
	}
	if relative := subagentRelativeTime(item.UpdatedAt, time.Now()); relative != "" {
		if metadata != "" {
			metadata += " " + glyphMiddleDot + " "
		}
		metadata += relative
	}
	statusText := status
	if !item.HasConversation {
		statusText = ""
	} else {
		statusText += " " + glyphChevronRight
	}
	background := theme.Background
	if selected {
		background = theme.SurfacePressed
	}
	heading := []ui.Widget{
		ui.Expanded(ui.Text{Value: glyph + " " + item.Name, Style: statusStyle, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
	}
	if statusText != "" {
		heading = append(heading, ui.Text{Value: statusText, Style: statusStyle, MaxLines: 1})
	}
	content := ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: background}}, ui.Padding(ui.Symmetric(1, 0), ui.Flex{
		Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, Children: heading}},
			ui.Text{Value: item.Description, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
			ui.Text{Value: metadata, Style: ui.Style{Foreground: theme.DisabledForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		},
	}))
	return mouseActivator{
		OnPressed: func(ctx ui.EventContext) {
			if w.Callbacks.SelectSubagent != nil {
				w.Callbacks.SelectSubagent(ctx, item.Name)
			}
			if item.HasConversation && w.Callbacks.OpenSubagentConversation != nil {
				w.Callbacks.OpenSubagentConversation(ctx, item.Conversation.ID)
			}
		},
		Child: content,
	}
}

func subagentConversationLabel(conversations []protocol.SubagentConversation, conversationID string) string {
	for _, conversation := range conversations {
		if conversation.ID == conversationID {
			return conversation.AgentName
		}
	}
	return "Subagent"
}

func (w shellView) subagentTranscriptPane(theme ui.Theme, conversationID string) ui.Widget {
	label := subagentConversationLabel(w.Snapshot.SubagentConversations, conversationID)
	state := ""
	for _, conversation := range w.Snapshot.SubagentConversations {
		if conversation.ID == conversationID {
			state = conversation.State
			break
		}
	}
	transcript, loaded := w.Snapshot.SubagentTranscripts[conversationID]
	var rows []ui.Widget
	if !loaded {
		rows = []ui.Widget{ui.Center(spinnerWithLabel("Loading transcript…", ui.Style{Foreground: theme.MutedForeground}))}
	} else if len(transcript.Messages) == 0 {
		rows = []ui.Widget{ui.Center(ui.Text{Value: "No transcript yet", Style: ui.Style{Foreground: theme.MutedForeground}})}
	} else {
		for index, message := range transcript.Messages {
			if index > 0 {
				rows = append(rows, ui.SizedBox{Height: 1})
			}
			switch message.Role {
			case "user":
				rows = append(rows, transcriptUserEntry(theme, message))
			case "assistant":
				rows = append(rows, transcriptAssistantEntry(theme, message))
			case "tool":
				rows = append(rows, ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
					ui.Text{Value: glyphCheck + " " + message.ToolName, Style: ui.Style{Foreground: theme.AccentText}, MaxLines: 1},
					ui.SizedBox{Width: 1},
					ui.Expanded(ui.Text{Value: message.TextContent(), Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
				}})
			case "context":
				rows = append(rows, ui.Text{Value: message.TextContent(), Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true})
			}
		}
	}
	if live := w.Snapshot.SubagentLive[conversationID]; len(live.Events) > 0 && state == "running" {
		rows = append(rows, ui.SizedBox{Height: 1}, ui.Text{Value: "Live activity", Style: ui.Style{Foreground: theme.MutedForeground, Attribute: ui.AttrBold}})
		rows = append(rows, subagentLiveRows(theme, live.Events)...)
	}
	controller := w.Snapshot.SubagentScroll
	if controller == nil {
		controller = w.Snapshot.ActivityScroll
	}
	body := ui.Scrollbar{Child: ui.ScrollView{
		Controller: controller,
		Child:      ui.Padding(ui.All(1), ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}),
	}}
	hint := "page up/down scroll " + glyphMiddleDot + " ctrl+d dismiss " + glyphMiddleDot + " esc back"
	for _, conversation := range w.Snapshot.SubagentConversations {
		if conversation.ID == conversationID {
			if _, cancellable := selectedSubagentTask(conversation); cancellable {
				hint = "page up/down scroll " + glyphMiddleDot + " c cancel " + glyphMiddleDot + " ctrl+d dismiss " + glyphMiddleDot + " esc back"
			}
			break
		}
	}
	content := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Text{Value: label, Style: ui.Style{Foreground: theme.AccentText}, MaxLines: 1},
			ui.Text{Value: " " + glyphMiddleDot + " " + state, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
			ui.Expanded(ui.SizedBox{}),
			workspaceWideOnly{LayoutState: w.Snapshot.WorkspaceLayout, Child: mouseActivator{
				OnPressed: func(ctx ui.EventContext) {
					if w.Callbacks.CloseSubagentConversation != nil {
						w.Callbacks.CloseSubagentConversation(ctx, conversationID)
					}
				},
				Child: ui.Text{Value: glyphTimes, Style: ui.Style{Foreground: theme.MutedForeground}},
			}},
		}})},
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.Expanded(body),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value: hint, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		})},
	}})
	actions := map[ui.IntentType]ui.ActionFunc{
		cancelSubagentIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			for _, conversation := range w.Snapshot.SubagentConversations {
				if conversation.ID == conversationID && w.Callbacks.CancelSubagentTask != nil {
					if task, cancellable := selectedSubagentTask(conversation); cancellable {
						w.Callbacks.CancelSubagentTask(ctx, task.ID, task.CancellationGeneration)
					}
					break
				}
			}
			return ui.EventHandled
		},
		dismissSubagentIntent{}.IntentType(): func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			for _, conversation := range w.Snapshot.SubagentConversations {
				if conversation.ID == conversationID && w.Callbacks.DismissSubagent != nil {
					w.Callbacks.DismissSubagent(ctx, conversation.ID, conversation.Generation)
					break
				}
			}
			return ui.EventHandled
		},
		scrollActivityIntent{}.IntentType(): func(ctx ui.EventContext, intent ui.Intent) ui.EventResult {
			if w.Callbacks.ScrollActivity != nil {
				w.Callbacks.ScrollActivity(ctx, intent.(scrollActivityIntent).Pages)
			}
			return ui.EventHandled
		},
		ui.DismissIntentType: func(ctx ui.EventContext, _ ui.Intent) ui.EventResult {
			if w.Callbacks.ShowSubagentRoster != nil {
				w.Callbacks.ShowSubagentRoster(ctx)
			}
			return ui.EventHandled
		},
	}
	if w.Snapshot.ActivityFocus != nil {
		content = ui.Focus(w.Snapshot.ActivityFocus, content)
	}
	content = ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{
		"c": cancelSubagentIntent{}, "Ctrl+d": dismissSubagentIntent{},
		"Page_Up": scrollActivityIntent{Pages: -1}, "Page_Down": scrollActivityIntent{Pages: 1},
	}, Child: content}}
	if w.Snapshot.ActivityFocus != nil {
		content = mouseActivator{
			OnPrimaryDownCapture: func(ui.EventContext) { w.Snapshot.ActivityFocus.RequestFocus() },
			DefaultMouseShape:    true, Child: content,
		}
		content = ui.FocusScope{AutoFocus: w.Snapshot.ActivitySelected, Child: content}
	}
	return content
}

func subagentLiveRows(theme ui.Theme, events []protocol.SubagentLiveEvent) []ui.Widget {
	var thinking, text strings.Builder
	toolOrder := make([]string, 0)
	tools := make(map[string]protocol.SubagentLiveEvent)
	for _, event := range events {
		switch event.Kind {
		case "message.thinking.delta":
			thinking.WriteString(event.Delta)
		case "message.text.delta":
			text.WriteString(event.Delta)
		case "tool.started", "tool.updated", "tool.completed":
			if _, exists := tools[event.ToolCallID]; !exists {
				toolOrder = append(toolOrder, event.ToolCallID)
			}
			tools[event.ToolCallID] = event
		}
	}
	rows := make([]ui.Widget, 0, len(toolOrder)+2)
	if value := strings.TrimSpace(thinking.String()); value != "" {
		rows = append(rows, ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			spinner{Style: ui.Style{Foreground: theme.MutedForeground}}, ui.SizedBox{Width: 1},
			ui.Expanded(ui.Text{Value: value, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
		}})
	}
	if value := strings.TrimSpace(text.String()); value != "" {
		rows = append(rows, ui.Text{Value: value, Style: ui.Style{Foreground: theme.Foreground}, SoftWrap: true})
	}
	for _, id := range toolOrder {
		event := tools[id]
		icon := ui.Widget(spinner{Style: ui.Style{Foreground: theme.MutedForeground}})
		if event.Kind == "tool.completed" {
			if event.IsError {
				icon = ui.Text{Value: glyphCross, Style: ui.Style{Foreground: theme.DangerText}}
			} else {
				icon = ui.Text{Value: glyphCheck, Style: ui.Style{Foreground: theme.AccentText}}
			}
		}
		rows = append(rows, ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			icon, ui.SizedBox{Width: 1}, ui.Text{Value: event.ToolName, Style: ui.Style{Foreground: theme.AccentText}, MaxLines: 1},
			ui.SizedBox{Width: 1}, ui.Expanded(ui.Text{Value: event.Text, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
		}})
	}
	return rows
}
