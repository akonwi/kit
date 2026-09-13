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
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
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

func subagentConversationIDForAgent(conversations []protocol.SubagentConversation, agentName string) string {
	for _, conversation := range conversations {
		if conversation.AgentName == agentName {
			return conversation.ID
		}
	}
	return ""
}

func subagentConversationLabel(conversations []protocol.SubagentConversation, conversationID string) string {
	for _, conversation := range conversations {
		if conversation.ID == conversationID {
			return conversation.AgentName
		}
	}
	return "Subagent"
}

func subagentTranscriptHasFinalResponse(transcript protocol.SubagentTranscript) bool {
	latestUser := -1
	for index := len(transcript.Messages) - 1; index >= 0; index-- {
		if transcript.Messages[index].Role == "user" {
			latestUser = index
			break
		}
	}
	for index := len(transcript.Messages) - 1; index > latestUser; index-- {
		message := transcript.Messages[index]
		if message.Role == "assistant" {
			return strings.TrimSpace(assistantProse(message)) != ""
		}
	}
	return false
}

func subagentLiveTranscript(messages []protocol.TranscriptMessage, events []protocol.SubagentLiveEvent) []transcriptMessage {
	durableMessages := make(map[string]bool, len(messages))
	durableToolResults := make(map[transcriptToolStateKey]bool)
	for _, message := range messages {
		durableMessages[message.ID] = true
		if message.Role == "tool" && message.ToolCallID != "" {
			durableToolResults[transcriptToolStateKey{TurnID: message.TurnID, ToolCallID: message.ToolCallID}] = true
		}
	}
	live := make([]transcriptMessage, 0)
	assistants := make(map[string]int)
	tools := make(map[transcriptToolStateKey]int)
	ensureAssistant := func(event protocol.SubagentLiveEvent) *transcriptMessage {
		if event.MessageID == "" || durableMessages[event.MessageID] {
			return nil
		}
		if index, exists := assistants[event.MessageID]; exists {
			return &live[index]
		}
		assistants[event.MessageID] = len(live)
		live = append(live, transcriptMessage{ID: event.MessageID, TurnID: event.TurnID, Role: "assistant", Pending: true})
		return &live[len(live)-1]
	}
	ensureTool := func(event protocol.SubagentLiveEvent) *transcriptMessage {
		key := transcriptToolStateKey{TurnID: event.TurnID, ToolCallID: event.ToolCallID}
		if event.ToolCallID == "" || durableToolResults[key] {
			return nil
		}
		if index, exists := tools[key]; exists {
			return &live[index]
		}
		tools[key] = len(live)
		live = append(live, transcriptMessage{
			ID: "live-tool:" + event.ToolCallID, TurnID: event.TurnID, Role: "tool",
			ToolCallID: event.ToolCallID, ToolName: event.ToolName, ToolStatus: "Running…", Pending: true,
		})
		return &live[len(live)-1]
	}
	for _, event := range events {
		switch event.Kind {
		case "message.text.delta":
			if assistant := ensureAssistant(event); assistant != nil {
				assistant.Text += event.Delta
			}
		case "message.thinking.delta":
			if assistant := ensureAssistant(event); assistant != nil {
				assistant.Thinking += event.Delta
			}
		case "message.completed":
			if assistant := ensureAssistant(event); assistant != nil {
				assistant.Pending = false
			}
		case "tool.planned":
			if assistant := ensureAssistant(event); assistant != nil && event.ToolCallID != "" {
				found := false
				for _, call := range assistant.ToolCalls {
					found = found || call.ID == event.ToolCallID
				}
				if !found {
					assistant.ToolCalls = append(assistant.ToolCalls, transcriptToolCall{ID: event.ToolCallID, Name: event.ToolName})
				}
			}
		case "tool.started":
			if tool := ensureTool(event); tool != nil {
				tool.ToolName = event.ToolName
				tool.ToolStatus = "Running…"
				tool.Pending = true
			}
		case "tool.updated":
			if tool := ensureTool(event); tool != nil {
				tool.ToolName = event.ToolName
				tool.Text += event.Text
				tool.ToolStatus = "Running…"
				tool.Pending = true
			}
		case "tool.completed":
			if tool := ensureTool(event); tool != nil {
				tool.ToolName = event.ToolName
				tool.Text = event.Text
				tool.ToolContent = []protocol.TranscriptContent{{Kind: protocol.TranscriptContentText, Text: event.Text}}
				tool.IsError = event.IsError
				tool.Pending = false
				if event.IsError {
					tool.ToolStatus = "Failed"
				} else {
					tool.ToolStatus = "Completed"
				}
			}
		}
	}
	return live
}

func subagentPaneMessages(conversation protocol.SubagentConversation, transcript protocol.SubagentTranscript, live protocol.SubagentLiveEventPage) []transcriptMessage {
	messages := projectTranscript(transcript.Messages)
	if conversation.State == "running" {
		messages = append(messages, subagentLiveTranscript(transcript.Messages, live.Events)...)
	}
	if conversation.State != "running" && strings.TrimSpace(conversation.LastResultSummary) != "" && !subagentTranscriptHasFinalResponse(transcript) {
		messages = append(messages, transcriptMessage{
			ID: "subagent-final:" + conversation.ID, Role: "assistant", Text: strings.TrimSpace(conversation.LastResultSummary),
		})
	}
	return messages
}

func subagentPendingStatus(messages []transcriptMessage) (thinking, activity string) {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if !message.Pending {
			continue
		}
		if message.Role == "assistant" && strings.TrimSpace(message.Thinking) != "" {
			return message.Thinking, ""
		}
		return "", "Working…"
	}
	return "", ""
}

func (w shellView) subagentTranscriptPane(theme ui.Theme, conversationID string) ui.Widget {
	label := subagentConversationLabel(w.Snapshot.SubagentConversations, conversationID)
	conversation := protocol.SubagentConversation{ID: conversationID}
	for _, candidate := range w.Snapshot.SubagentConversations {
		if candidate.ID == conversationID {
			conversation = candidate
			break
		}
	}
	state := conversation.State
	transcript, loaded := w.Snapshot.SubagentTranscripts[conversationID]
	loadError := w.Snapshot.SubagentTranscriptErrors[conversationID]
	controller := w.Snapshot.SubagentScroll
	if controller == nil {
		controller = w.Snapshot.ActivityScroll
	}
	messages := subagentPaneMessages(conversation, transcript, w.Snapshot.SubagentLive[conversationID])
	presentation := presentTranscript(messages)
	var body ui.Widget
	if len(presentation.Items) > 0 {
		transcriptView := w
		transcriptView.Snapshot.Scroll = controller
		transcriptView.Callbacks.OpenActivity = func(ctx ui.EventContext, sourceID string) {
			if w.Callbacks.OpenSubagentActivity != nil {
				w.Callbacks.OpenSubagentActivity(ctx, conversationID, sourceID)
			}
		}
		var leading ui.Widget
		if loaded && loadError != "" {
			leading = ui.Padding(ui.Insets{Top: 1, Left: 1, Right: 1}, ui.Text{
				Value: "Transcript refresh failed: " + loadError,
				Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true,
			})
		}
		body = transcriptView.transcriptList(theme, presentation, true, "subagent:"+conversationID, controller, nil, true, leading)
	} else {
		var state ui.Widget
		if !loaded && loadError != "" {
			state = ui.Center(ui.Flex{
				Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter,
				Children: []ui.Widget{
					ui.Text{Value: "Could not load transcript", Style: ui.Style{Foreground: theme.DangerText, Attribute: ui.AttrBold}},
					ui.Text{Value: loadError, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true},
				},
			})
		} else if !loaded {
			state = ui.Center(spinnerWithLabel("Loading transcript…", ui.Style{Foreground: theme.MutedForeground}))
		} else {
			state = ui.Center(ui.Text{Value: "No transcript yet", Style: ui.Style{Foreground: theme.MutedForeground}})
		}
		body = ui.Scrollbar{Child: ui.CustomScrollView{
			Controller: controller, FollowOutput: true,
			Slivers: []ui.Widget{ui.SliverToBox{Child: ui.Padding(ui.All(1), state)}},
		}}
	}
	thinking, activity := subagentPendingStatus(messages)
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
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
		ui.Expanded(body),
		ui.Padding(ui.Symmetric(1, 0), pendingActivityRow(theme, thinking, activity)),
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
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
			if w.Callbacks.ScrollSubagentTranscript != nil {
				w.Callbacks.ScrollSubagentTranscript(ctx, conversationID, intent.(scrollActivityIntent).Pages)
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
