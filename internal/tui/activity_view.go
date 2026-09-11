package tui

import (
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

func (w shellView) activityPane(theme ui.Theme) ui.Widget {
	presentation := w.presentation
	source, ok := transcriptActivitySource(presentation.Items, w.Snapshot.ActivitySourceID)
	if !ok {
		return ui.Center(ui.Text{Value: "No activity to display", Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	sections := buildActivitySections(source)
	calls := displayItemToolCalls(source)
	metadata := activityMetadata(len(calls), len(sections))
	items := buildActivityListItems(source)
	if len(items) == 0 {
		items = []activityListItem{{ID: "activity-empty", Kind: activityListProse, Section: activitySection{Prose: "Nothing to show here yet"}}}
	}
	itemWidgets := make([]ui.Widget, len(items))
	toolKeys := make([]activityToolKey, len(items))
	for index, item := range items {
		itemWidgets[index] = w.activityListItem(theme, item, presentation.ToolStates)
		if item.Kind == activityListTool {
			toolKeys[index] = item.Key
		}
	}
	body := ui.Scrollbar{Child: ui.ScrollView{
		Controller: w.Snapshot.ActivityScroll,
		Child: ui.Padding(ui.All(1), activityList{
			Controller: w.Snapshot.ActivityList, OuterScroll: w.Snapshot.ActivityScroll,
			ToolKeys: toolKeys, Children: itemWidgets,
		}),
	}}
	hint := "click details " + glyphMiddleDot + " page up/down scroll " + glyphMiddleDot + " esc close"
	if w.Snapshot.ActivitySelected && w.Snapshot.WorkspaceLayout != nil && !w.Snapshot.WorkspaceLayout.Wide {
		hint = "↑↓ rows " + glyphMiddleDot + " enter details " + glyphMiddleDot + " page up/down scroll " + glyphMiddleDot + " esc close"
	}
	if w.Snapshot.Running {
		hint = strings.TrimSuffix(hint, "esc close") + "esc abort"
	}
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
		ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Expanded(ui.Text{Value: metadata, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1}),
			workspaceWideOnly{LayoutState: w.Snapshot.WorkspaceLayout, Child: mouseActivator{
				OnPressed: w.Callbacks.CloseActivity, Child: ui.Text{
					Value: glyphTimes, Style: ui.Style{Foreground: theme.MutedForeground},
				},
			}},
		}})},
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.Expanded(body),
		ui.Divider{Style: ui.Style{Foreground: theme.Border}},
		ui.SizedBox{Height: 1, Child: ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value: hint, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1,
		})},
	}}
}

func activityMetadata(toolCalls, steps int) string {
	toolLabel := "tool calls"
	if toolCalls == 1 {
		toolLabel = "tool call"
	}
	stepLabel := "steps"
	if steps == 1 {
		stepLabel = "step"
	}
	return strconv.Itoa(toolCalls) + " " + toolLabel + " " + glyphMiddleDot + " " + strconv.Itoa(steps) + " " + stepLabel
}

func (w shellView) activityListItem(theme ui.Theme, item activityListItem, states map[transcriptToolStateKey]transcriptMessage) ui.Widget {
	switch item.Kind {
	case activityListSpacer:
		return keyedActivityItem{ID: item.ID, Child: ui.SizedBox{Height: 1}}
	case activityListThinking:
		return keyedActivityItem{ID: item.ID, Child: ui.SizedBox{}}
	case activityListProse:
		style := ui.Style{Foreground: theme.Foreground}
		if item.Section.Aborted {
			style.Foreground = theme.MutedForeground
		}
		return keyedActivityItem{ID: item.ID, Child: markdownView{
			ID: item.ID, Source: item.Section.Prose, BaseStyle: style,
		}}
	default:
		state, exists := states[transcriptToolStateKey{TurnID: item.Key.TurnID, ToolCallID: item.Key.ToolCallID}]
		return activityToolRowWidget{
			Key: item.Key, Call: item.Call, State: state, Exists: exists, SourceAborted: item.Section.Aborted,
			Expanded: w.Snapshot.ActivityExpanded[item.Key], Selected: w.Snapshot.ActivityCursor == item.Key,
			OuterScroll: w.Snapshot.ActivityScroll,
			OnToggle:    w.Callbacks.ToggleActivityTool, OnSelect: w.Callbacks.SelectActivityTool,
		}
	}
}

type keyedActivityItem struct {
	ID    string
	Child ui.Widget
}

func (w keyedActivityItem) WidgetKey() ui.KeyValue { return ui.KeyValue(w.ID) }

func (w keyedActivityItem) Build(ui.BuildContext) ui.Widget { return w.Child }

type activityToolRowWidget struct {
	Key                 activityToolKey
	Call                transcriptToolCall
	ContainerBackground bool
	State               transcriptMessage
	Exists              bool
	SourceAborted       bool
	Expanded            bool
	Selected            bool
	OuterScroll         *ui.ScrollController
	OnToggle            func(ui.EventContext, activityToolKey)
	OnSelect            func(ui.EventContext, activityToolKey)
}

func (w activityToolRowWidget) WidgetKey() ui.KeyValue {
	return ui.KeyValue("activity-tool:" + w.Key.TurnID + ":" + w.Key.ToolCallID)
}

func (activityToolRowWidget) CreateState() ui.State { return &activityToolRowWidgetState{} }

type activityToolRowWidgetState struct {
	ui.StateBase
	hovered           bool
	followFinalOutput bool
}

func (s *activityToolRowWidgetState) DidUpdateWidget(old ui.Widget) {
	previous := old.(activityToolRowWidget)
	current := s.Widget().(activityToolRowWidget)
	if previous.Key != current.Key || !current.Expanded {
		s.followFinalOutput = false
		return
	}
	previousState := resolveActivityToolState(previous.State, previous.Exists, previous.SourceAborted)
	currentState := resolveActivityToolState(current.State, current.Exists, current.SourceAborted)
	if previous.Expanded && (previousState == activityToolPending || previousState == activityToolRunning) &&
		currentState == activityToolSucceeded && activityToolOutput(current.State, current.Exists) != "" {
		s.followFinalOutput = true
	}
}

func (s *activityToolRowWidgetState) Build(ctx ui.BuildContext) ui.Widget {
	row := s.Widget().(activityToolRowWidget)
	theme := ui.MustDepend[ui.Theme](ctx)
	state := resolveActivityToolState(row.State, row.Exists, row.SourceAborted)
	output := activityToolOutput(row.State, row.Exists)
	command, commandSummarized := activityBashCommand(row.Call)
	enrichment, enriched := detectActivityEnrichment(row.Call, row.State, row.Exists)
	hasDetails := state != activityToolAborted && (enriched || output != "" || commandSummarized)
	background := theme.Background
	if row.ContainerBackground {
		background = theme.Surface
	}
	if row.ContainerBackground {
		if s.hovered {
			background = theme.SurfaceHovered
		}
	} else if row.Selected {
		background = theme.SurfacePressed
	} else if s.hovered {
		background = theme.SurfaceHovered
	}
	style := activityToolHeaderStyle(theme, state)
	icon := activityToolStateIcon(theme, state)
	disclosure := " "
	if hasDetails {
		disclosure = glyphTriangleRight
		if row.Expanded {
			disclosure = glyphTriangleDown
		}
	}
	argument := activityToolArgument(row.Call)
	header := ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: background}}, ui.Flex{
		Axis: ui.Horizontal, Children: []ui.Widget{
			icon, ui.SizedBox{Width: 1},
			ui.Text{Value: disclosure, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
			ui.SizedBox{Width: 1},
			ui.Text{Value: toolDisplayName(row.Call), Style: style, MaxLines: 1},
			ui.SizedBox{Width: 1},
			ui.Expanded(ui.Text{Value: argument, Style: ui.Style{Foreground: theme.Foreground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
		},
	})
	activate := func(eventContext ui.EventContext) {
		if row.OnSelect != nil {
			row.OnSelect(eventContext, row.Key)
		}
		if hasDetails && row.OnToggle != nil {
			row.OnToggle(eventContext, row.Key)
		}
	}
	header = ui.DecoratedBox(ui.Decoration{}, mouseActivator{
		OnPressed: activate,
		OnHover: func(ui.EventContext) {
			if !s.hovered {
				s.SetState(func() { s.hovered = true })
			}
		},
		OnHoverExit: func(ui.EventContext) {
			if s.hovered {
				s.SetState(func() { s.hovered = false })
			}
		},
		Child: header,
	})
	children := []ui.Widget{ui.SizedBox{Height: 1, Child: header}}
	if row.Expanded && hasDetails {
		details := make([]ui.Widget, 0, 2)
		usedEnrichment := false
		if enriched {
			content, measurement, metadata, ok := activityEnrichedPresentation(enrichment)
			if ok {
				details = append(details, toolOutputWell{
					Key: row.Key, Output: measurement, Content: content, Rich: true,
					Metadata: metadata, OuterScroll: row.OuterScroll,
				})
			} else {
				details = append(details, toolOutputWell{
					Key: row.Key, Output: activityEnrichmentUnavailable(enrichment), OuterScroll: row.OuterScroll,
				})
			}
			usedEnrichment = true
		}
		if !usedEnrichment {
			if commandSummarized {
				details = append(details, ui.DecoratedBox(
					ui.Decoration{Style: ui.Style{Background: theme.Surface}},
					ui.Padding(ui.Symmetric(1, 0), ui.Text{Value: command, Style: ui.Style{Foreground: theme.Foreground}, SoftWrap: true}),
				))
			}
			if output != "" {
				details = append(details, toolOutputWell{
					Key: row.Key, Output: output, OuterScroll: row.OuterScroll,
					StickyBottom: state == activityToolPending || state == activityToolRunning || state == activityToolFailed || s.followFinalOutput,
				})
			}
		}
		children = append(children, ui.Padding(ui.Insets{Left: 2}, ui.Flex{
			Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: details,
		}))
	}
	return ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: children}
}

func activityToolStateIcon(theme ui.Theme, state activityToolState) ui.Widget {
	switch state {
	case activityToolPending, activityToolRunning:
		return spinner{Style: ui.Style{Foreground: theme.MutedForeground}}
	case activityToolSucceeded:
		return ui.Text{Value: glyphCheck, Style: ui.Style{Foreground: theme.AccentText}}
	case activityToolFailed:
		return ui.Text{Value: glyphCross, Style: ui.Style{Foreground: theme.DangerText}}
	default:
		return ui.Text{Value: glyphCircleSlash, Style: ui.Style{Foreground: theme.MutedForeground}}
	}
}

func activityToolHeaderStyle(theme ui.Theme, state activityToolState) ui.Style {
	switch state {
	case activityToolFailed:
		return ui.Style{Foreground: theme.DangerText}
	case activityToolAborted:
		return ui.Style{Foreground: theme.MutedForeground}
	default:
		return ui.Style{Foreground: theme.AccentText}
	}
}

func activityToolArgument(call transcriptToolCall) string {
	if call.ArgumentsTruncated {
		return "arguments truncated"
	}
	if call.Name == "bash" {
		command, _ := activityBashCommand(call)
		return presentBashCommand(command).Text
	}
	return formatToolArguments(call, true)
}

func activityBashCommand(call transcriptToolCall) (string, bool) {
	if call.Name != "bash" || call.ArgumentsTruncated {
		return "", false
	}
	command, _ := toolArguments(call)["command"].(string)
	presentation := presentBashCommand(command)
	return command, presentation.Summarized
}

func activityToolOutput(state transcriptMessage, exists bool) string {
	if !exists {
		return ""
	}
	output := state.Text
	if state.ToolDetailsOmitted && !strings.Contains(output, "tool details omitted") {
		output = appendToolNotice(output, "… tool details omitted")
	}
	return output
}
