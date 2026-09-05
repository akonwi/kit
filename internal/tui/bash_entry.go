package tui

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

const bashTranscriptVisibleLines = 20

func transcriptBashEntry(theme ui.Theme, execution protocol.BashExecution, collapsed bool, onToggle ui.VoidCallback) ui.Widget {
	pending := execution.Status == protocol.BashExecutionRunning
	output := bashTranscriptOutput(execution)
	lines := []string(nil)
	if output != "" {
		lines = strings.Split(output, "\n")
	}
	expanded := !collapsed
	prefix := ui.Widget(spinner{Style: ui.Style{Foreground: theme.SuccessText}})
	if !pending {
		glyph, style := bashStatusPresentation(theme, execution)
		prefix = ui.Text{Value: glyph, Style: style}
	}
	headerChildren := []ui.Widget{
		prefix,
		ui.SizedBox{Width: 1},
		ui.Expanded(codePresentation{Lines: strings.Split(execution.Command, "\n")}),
	}
	if !pending && len(lines) > 0 {
		glyph := glyphTriangleDown
		if collapsed {
			glyph = glyphTriangleRight
		}
		headerChildren = append(headerChildren, ui.SizedBox{Width: 1}, ui.Text{
			Value: glyph, Style: ui.Style{Foreground: theme.MutedForeground},
		})
	}
	header := ui.Widget(ui.Flex{
		Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStart, Children: headerChildren,
	})
	if !pending && len(lines) > 0 && onToggle != nil {
		header = mouseActivator{Child: header, OnPressed: onToggle}
	}
	children := []ui.Widget{header}
	if !pending && expanded && len(lines) > 0 {
		display := lines
		if len(lines) > bashTranscriptVisibleLines {
			display = append(append([]string(nil), lines[:bashTranscriptVisibleLines-2]...), fmt.Sprintf("… (%d more lines)", len(lines)-(bashTranscriptVisibleLines-2)))
		}
		children = append(children, ui.Padding(ui.Insets{Left: 2}, ui.Text{
			Value: strings.Join(display, "\n"), Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true,
		}))
	}
	return ui.DecoratedBox(
		ui.Decoration{Border: ui.Border{Style: ui.Style{Foreground: theme.SuccessText}, Left: true}},
		ui.Padding(ui.Insets{Left: 2}, ui.Flex{
			Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
			CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
		}),
	)
}

func bashStatusPresentation(theme ui.Theme, execution protocol.BashExecution) (string, ui.Style) {
	switch execution.Status {
	case protocol.BashExecutionAborted, protocol.BashExecutionInterrupted:
		return glyphCircleSlash, ui.Style{Foreground: theme.MutedForeground}
	case protocol.BashExecutionFailed:
		return glyphCross, ui.Style{Foreground: theme.DangerText}
	case protocol.BashExecutionCompleted:
		if execution.TimedOut || execution.ExitCode == nil || *execution.ExitCode != 0 {
			return glyphCross, ui.Style{Foreground: theme.DangerText}
		}
		return glyphCheck, ui.Style{Foreground: theme.SuccessText}
	default:
		return glyphCross, ui.Style{Foreground: theme.DangerText}
	}
}

func bashTranscriptOutput(execution protocol.BashExecution) string {
	output := strings.TrimRight(execution.Output, "\r\n")
	appendNotice := func(notice string) {
		if output != "" {
			output += "\n"
		}
		output += notice
	}
	if execution.TimedOut {
		appendNotice("[timed out]")
	}
	if execution.Truncated {
		appendNotice("[output truncated]")
	}
	if execution.ErrorMessage != "" && output == "" {
		output = execution.ErrorMessage
	}
	return output
}
