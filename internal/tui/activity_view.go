package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

// activityTextIndent aligns interleaved thinking and prose with tool titles,
// past the shared state-icon column.
const activityTextIndent = 2

func (w shellView) activityListItem(theme ui.Theme, item activityListItem, states map[transcriptToolStateKey]transcriptMessage) ui.Widget {
	switch item.Kind {
	case activityListThinking:
		return keyedActivityItem{ID: item.ID, Child: ui.Padding(ui.Insets{Left: activityTextIndent}, markdownView{
			ID: item.ID, Source: item.Section.Thinking,
			BaseStyle: ui.Style{Foreground: theme.MutedForeground, Attribute: ui.AttrItalic},
		})}
	case activityListProse:
		style := ui.Style{Foreground: theme.Foreground}
		if item.Section.Aborted {
			style.Foreground = theme.MutedForeground
		}
		return keyedActivityItem{ID: item.ID, Child: ui.Padding(ui.Insets{Left: activityTextIndent}, markdownView{
			ID: item.ID, Source: item.Section.Prose, BaseStyle: style,
		})}
	default:
		state, exists := states[transcriptToolStateKey{TurnID: item.Key.TurnID, ToolCallID: item.Key.ToolCallID}]
		subagentName := subagentToolAgentName(item.Call)
		if subagentToolConversationID(item.Call, state, exists, w.Snapshot.SubagentConversations) == "" {
			subagentName = ""
		}
		return activityToolRowWidget{
			Key: item.Key, Call: item.Call, State: state, Exists: exists, SourceAborted: item.Section.Aborted,
			Selected:          w.Snapshot.ActivityCursor == item.Key,
			SubagentAgentName: subagentName,
			OnSelect:          w.Callbacks.SelectActivityTool,
			OnOpenSubagent:    w.Callbacks.OpenSubagentFromTool,
			OnOpenFile:        w.Callbacks.OpenActivityFile,
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
	Selected            bool
	SubagentAgentName   string
	OnSelect            func(ui.EventContext, activityToolKey)
	OnOpenSubagent      func(ui.EventContext, string)
	OnOpenFile          func(ui.EventContext, toolFileTarget)
}

func (w activityToolRowWidget) WidgetKey() ui.KeyValue {
	return ui.KeyValue("activity-tool:" + w.Key.TurnID + ":" + w.Key.ToolCallID)
}

func (activityToolRowWidget) CreateState() ui.State { return &activityToolRowWidgetState{} }

type activityToolRowWidgetState struct {
	ui.StateBase
	width int
}

func (s *activityToolRowWidgetState) Build(ctx ui.BuildContext) ui.Widget {
	row := s.Widget().(activityToolRowWidget)
	theme := ui.MustDepend[ui.Theme](ctx)
	state := resolveActivityToolState(row.State, row.Exists, row.SourceAborted)
	presentation := presentToolCall(row.Call, row.State, row.Exists)
	narrow := s.width > 0 && s.width < 60
	summary := presentation.Summary
	if narrow && toolSummaryPrefersTail(row.Call) {
		summary = truncateToolPathSummary(row.Call, summary, max(2, s.width-6))
	}

	background := theme.Background
	if row.Selected {
		background = theme.SurfacePressed
	}
	name := ui.Widget(ui.Text{Value: presentation.Title, Style: activityToolHeaderStyle(theme, state), MaxLines: 1})
	chipStyle := ui.Style{Foreground: theme.Foreground, Background: theme.Surface}
	if state == activityToolAborted {
		chipStyle.Foreground = theme.MutedForeground
	}
	chip := ui.Widget(ui.Text{
		Value: " " + summary + " ", Style: chipStyle,
		Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
	})
	if target, ok := toolRowFileTarget(row.Call, row.State, row.Exists); ok && row.OnOpenFile != nil {
		chip = toolFileChip{Text: " " + summary + " ", Style: chipStyle, OnPressed: func(ctx ui.EventContext) { row.OnOpenFile(ctx, target) }}
	}
	if agentName := row.SubagentAgentName; agentName != "" && row.OnOpenSubagent != nil {
		chip = subagentToolLink{
			Name: " " + agentName + " ", Style: chipStyle,
			OnPressed: func(ctx ui.EventContext) { row.OnOpenSubagent(ctx, agentName) },
		}
	}

	headerHeight := 1
	var headerContent ui.Widget
	if narrow {
		headerHeight = 2
		headerContent = ui.Flex{
			Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
			CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
				ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
					activityToolStateIcon(theme, state), ui.SizedBox{Width: 1}, ui.Expanded(name),
				}},
				ui.Padding(ui.Insets{Left: 2}, ui.Flexible(chip)),
			},
		}
	} else {
		headerContent = ui.Flex{
			Axis: ui.Horizontal, Children: []ui.Widget{
				activityToolStateIcon(theme, state), ui.SizedBox{Width: 1},
				ui.SizedBox{Width: 18, Child: name}, ui.SizedBox{Width: 1}, ui.Flexible(chip),
			},
		}
	}
	header := ui.DecoratedBox(ui.Decoration{Style: ui.Style{Background: background}}, headerContent)
	return ui.SizedBox{Height: headerHeight, Child: mouseActivator{
		OnPressed: func(eventContext ui.EventContext) {
			if row.OnSelect != nil {
				row.OnSelect(eventContext, row.Key)
			}
		},
		Child: widthProbe{
			WidthChanged: func(width int) {
				if width != s.width {
					s.width = width
					s.MarkNeedsBuild()
				}
			},
			Child: header,
		},
	}}
}

func activityToolStateIcon(theme ui.Theme, state activityToolState) ui.Widget {
	switch state {
	case activityToolPending, activityToolRunning:
		return spinner{Style: ui.Style{Foreground: theme.MutedForeground}}
	case activityToolSucceeded:
		return ui.Text{Value: " "}
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
