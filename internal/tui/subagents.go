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
			Name: definition.Name, Description: definition.Description, Status: "inactive",
		}
		if conversation, ok := conversationsByName[definition.Name]; ok {
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
			Status: conversation.State, UpdatedAt: conversation.UpdatedAt,
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

func filteredSubagentRosterItems(items []subagentRosterItem, query string) []subagentRosterItem {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	filtered := make([]subagentRosterItem, 0, len(items))
	for _, item := range items {
		haystack := strings.ToLower(strings.Join([]string{item.Name, item.Description, item.Status}, " "))
		if strings.Contains(haystack, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
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
	return max(0, index*2+headerOffset-2)
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

func (w shellView) subagentsPane(ctx ui.BuildContext, theme ui.Theme) ui.Widget {
	items := filteredSubagentRosterItems(subagentRosterItems(w.Snapshot.SubagentDefinitions, w.Snapshot.SubagentConversations), w.Snapshot.SubagentFilter)
	rowPresentation := resolvePickerRowPresentation(ctx, theme)
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
		rows = append(rows, w.subagentRosterRow(theme, rowPresentation, item, item.Name == selectedName))
	}

	var body ui.Widget
	if len(items) == 0 {
		emptyTitle := "No subagents available"
		emptyHint := "Add .md files to $KIT_HOME/agents/"
		if strings.TrimSpace(w.Snapshot.SubagentFilter) != "" {
			emptyTitle = "No matching subagents"
			emptyHint = "Try another search"
		}
		body = ui.Center(ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
			ui.Text{Value: "k i t", Style: ui.Style{Foreground: theme.Foreground}, MaxLines: 1},
			ui.Text{Value: strings.Repeat(glyphHeavyLine, 11), Style: ui.Style{Foreground: theme.AccentText}, MaxLines: 1},
			ui.Text{Value: emptyTitle, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
			ui.Text{Value: emptyHint, Style: ui.Style{Foreground: theme.DisabledForeground}, MaxLines: 1},
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
		hint += " " + glyphMiddleDot + " ctrl+d dismiss " + glyphMiddleDot + " esc close"
	}
	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	queryCursor := len(w.Snapshot.SubagentFilter)
	query := ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisCenter, Children: []ui.Widget{
		ui.Text{Value: ">", Style: ui.Style{Foreground: theme.Foreground}}, ui.SizedBox{Width: 1},
		textInput(fieldTheme, textInputConfig{
			Value: w.Snapshot.SubagentFilter, Placeholder: "Search subagents…", CursorOffset: &queryCursor,
			OnChanged: w.Callbacks.SubagentFilterChanged, AutoFocus: true,
		}),
	}})
	content := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		query, ui.SizedBox{Height: 1}, ui.Expanded(body),
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
		"Enter": openSubagentIntent{}, "Ctrl+d": dismissSubagentIntent{},
		"Page_Up": scrollActivityIntent{Pages: -1}, "Page_Down": scrollActivityIntent{Pages: 1},
	}, Child: content}}
	if w.Snapshot.ActivityFocus != nil {
		content = mouseActivator{
			OnPrimaryDownCapture: func(ui.EventContext) { w.Snapshot.ActivityFocus.RequestFocus() },
			DefaultMouseShape:    true, Child: content,
		}
		content = ui.FocusScope{AutoFocus: w.Snapshot.SubagentsOpen, Child: content}
	}
	return content
}

func subagentStatusWidget(status string, glyph string, style ui.Style) ui.Widget {
	if status == "running" {
		return spinner{Style: style}
	}
	return ui.Text{Value: glyph, Style: style, MaxLines: 1}
}

func (w shellView) subagentRosterRow(theme ui.Theme, presentation pickerRowPresentation, item subagentRosterItem, selected bool) ui.Widget {
	glyph, _, statusStyle := subagentStatusPresentation(theme, item.Status)
	lastActive := subagentRelativeTime(item.UpdatedAt, time.Now())
	background := theme.Background
	primary := presentation.ItemText
	secondary := theme.MutedForeground
	if selected {
		background = presentation.FocusedBg
		primary = presentation.FocusedText
		secondary = presentation.FocusedText
	}
	statusStyle.Background = background
	status := subagentStatusWidget(item.Status, glyph, statusStyle)
	heading := []ui.Widget{
		status,
		ui.SizedBox{Width: 1},
		ui.Expanded(ui.Text{Value: item.Name, Style: ui.Style{Foreground: primary, Background: background}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
	}
	if lastActive != "" {
		heading = append(heading, ui.Text{Value: lastActive, Style: ui.Style{Foreground: secondary, Background: background}, MaxLines: 1})
	}
	content := ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, Children: heading}},
		ui.Text{Value: item.Description, Style: ui.Style{Foreground: secondary, Background: background}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
	}}
	return ui.Provider[ui.Theme]{Value: presentation.Theme, Child: ui.ListTile{
		Title: content, Selected: selected, MinHeight: 2, Padding: ui.Insets{Right: 1, Left: 1},
		OnPressed: func(ctx ui.EventContext) {
			if w.Callbacks.SelectSubagent != nil {
				w.Callbacks.SelectSubagent(ctx, item.Name)
			}
			if item.HasConversation && w.Callbacks.OpenSubagentConversation != nil {
				w.Callbacks.OpenSubagentConversation(ctx, item.Conversation.ID)
			}
		},
	}}
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

func (w shellView) subagentTranscriptPane(theme ui.Theme, conversationID string, active bool) ui.Widget {
	conversation := protocol.SubagentConversation{ID: conversationID}
	for _, candidate := range w.Snapshot.SubagentConversations {
		if candidate.ID == conversationID {
			conversation = candidate
			break
		}
	}
	transcript, loaded := w.Snapshot.SubagentTranscripts[conversationID]
	loadError := w.Snapshot.SubagentTranscriptErrors[conversationID]
	controller := w.Snapshot.SubagentScrolls[conversationID]
	if controller == nil && active {
		controller = w.Snapshot.ActivityScroll
	}
	focus := w.Snapshot.SubagentFocuses[conversationID]
	if focus == nil && active {
		focus = w.Snapshot.ActivityFocus
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
	hint := "ctrl+d dismiss"
	for _, conversation := range w.Snapshot.SubagentConversations {
		if conversation.ID == conversationID {
			if _, cancellable := selectedSubagentTask(conversation); cancellable {
				hint = "c cancel " + glyphMiddleDot + " ctrl+d dismiss"
			}
			break
		}
	}
	children := []ui.Widget{ui.Expanded(body)}
	if thinking != "" || activity != "" {
		children = append(children, ui.Padding(ui.Symmetric(1, 0), pendingActivityRow(theme, thinking, activity)))
	}
	children = append(children,
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
		ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value: hint, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		})},
		ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}},
	)
	content := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: children})
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
	}
	if focus != nil {
		content = ui.FocusWithOptions(focus, ui.FocusOptions{SkipTraversal: !active}, content)
	}
	content = ui.Actions{Bindings: actions, Child: keyShortcuts{Bindings: ui.ShortcutMap{
		"c": cancelSubagentIntent{}, "Ctrl+d": dismissSubagentIntent{},
	}, Child: content}}
	if focus != nil {
		content = mouseActivator{
			OnPrimaryDownCapture: func(ui.EventContext) { focus.RequestFocus() },
			DefaultMouseShape:    true, Child: content,
		}
		content = ui.FocusScope{AutoFocus: active && w.Snapshot.ActivitySelected, Child: content}
	}
	return content
}
