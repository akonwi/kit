package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

func (w shellView) activityListItem(theme ui.Theme, item activityListItem, states map[transcriptToolStateKey]transcriptMessage) ui.Widget {
	switch item.Kind {
	case activityListSpacer:
		return keyedActivityItem{ID: item.ID, Child: ui.SizedBox{Height: 1}}
	case activityListThinking:
		return keyedActivityItem{ID: item.ID, Child: ui.Flex{
			Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
			Children: []ui.Widget{
				ui.Text{Value: "Thinking", Style: ui.Style{Foreground: theme.MutedForeground, Attribute: ui.AttrBold}},
				markdownView{ID: item.ID, Source: item.Section.Thinking, BaseStyle: ui.Style{Foreground: theme.MutedForeground}},
			},
		}}
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
		subagentName := subagentToolAgentName(item.Call)
		if subagentToolConversationID(item.Call, state, exists, w.Snapshot.SubagentConversations) == "" {
			subagentName = ""
		}
		return activityToolRowWidget{
			Key: item.Key, Call: item.Call, State: state, Exists: exists, SourceAborted: item.Section.Aborted,
			Expanded: w.Snapshot.ActivityExpanded[item.Key], Selected: w.Snapshot.ActivityCursor == item.Key,
			OuterScroll:       w.Snapshot.ActivityScroll,
			SubagentAgentName: subagentName,
			OnToggle:          w.Callbacks.ToggleActivityTool, OnSelect: w.Callbacks.SelectActivityTool,
			OnOpenSubagent: w.Callbacks.OpenSubagentFromTool,
		}
	}
}

type keyedActivityItem struct {
	ID    string
	Child ui.Widget
}

func (w keyedActivityItem) WidgetKey() ui.KeyValue { return ui.KeyValue(w.ID) }

func (w keyedActivityItem) Build(ui.BuildContext) ui.Widget { return w.Child }

type subagentToolLink struct {
	Name      string
	Style     ui.Style
	OnPressed ui.VoidCallback
}

func (subagentToolLink) CreateState() ui.State { return &subagentToolLinkState{} }

type subagentToolLinkState struct {
	ui.StateBase
	hovered bool
}

func (s *subagentToolLinkState) Build(ctx ui.BuildContext) ui.Widget {
	link := s.Widget().(subagentToolLink)
	theme := ui.MustDepend[ui.Theme](ctx)
	style := link.Style
	if s.hovered {
		style.Foreground = theme.Foreground
	}
	return mouseActivator{
		OnPressed: link.OnPressed,
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
		Child: ui.Text{Value: link.Name, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
	}
}

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
	SubagentAgentName   string
	OnToggle            func(ui.EventContext, activityToolKey)
	OnSelect            func(ui.EventContext, activityToolKey)
	OnOpenSubagent      func(ui.EventContext, string)
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
	name := ui.Widget(ui.Text{Value: toolDisplayName(row.Call), Style: style, MaxLines: 1})
	if agentName := row.SubagentAgentName; agentName != "" && row.OnOpenSubagent != nil {
		name = subagentToolLink{
			Name: agentName, Style: style,
			OnPressed: func(ctx ui.EventContext) { row.OnOpenSubagent(ctx, agentName) },
		}
	}
	header := ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: background}}, ui.Flex{
		Axis: ui.Horizontal, Children: []ui.Widget{
			icon, ui.SizedBox{Width: 1},
			ui.Text{Value: disclosure, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1},
			ui.SizedBox{Width: 1},
			name,
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
