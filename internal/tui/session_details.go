package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type sessionDetailsSurface struct {
	Session       protocol.SessionInfo
	ContextTokens int
	ContextWindow int
	Usage         protocol.SessionUsage
}

func (surface sessionDetailsSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	thinking := surface.Session.ThinkingLevel
	if thinking == "" {
		thinking = "default"
	}
	contextValue := "Unavailable"
	if surface.ContextWindow > 0 {
		percentage, _ := contextPercentage(surface.ContextTokens, surface.ContextWindow)
		contextValue = fmt.Sprintf(
			"%s / %s tokens (%d%%)",
			formatUsageInteger(surface.ContextTokens), formatUsageInteger(surface.ContextWindow), percentage,
		)
	}
	body := ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			sessionDetailsHeading(theme, "Configuration"),
			sessionDetailsRow(theme, "Model", surface.Session.Model),
			sessionDetailsRow(theme, "Thinking", thinking),
			sessionDetailsRow(theme, "Context", contextValue),
			ui.SizedBox{Height: 1},
			sessionDetailsHeading(theme, "Cumulative usage"),
			sessionDetailsRow(theme, "Input", formatUsageInteger(surface.Usage.Input)),
			sessionDetailsRow(theme, "Output", formatUsageInteger(surface.Usage.Output)),
			sessionDetailsRow(theme, "Cache read", formatUsageInteger(surface.Usage.CacheRead)),
			sessionDetailsRow(theme, "Cache write", formatUsageInteger(surface.Usage.CacheWrite)),
			sessionDetailsRow(theme, "Reasoning", formatUsageInteger(surface.Usage.Reasoning)),
			sessionDetailsRow(theme, "Total", formatUsageInteger(surface.Usage.TotalTokens)),
			sessionDetailsRow(theme, "Cost", formatUsageCost(surface.Usage.Cost.Total)),
		},
	}
	return dialogSurface(
		theme, "Session details", sessionDisplayName(surface.Session), body,
		ui.Text{Value: "esc close", Style: ui.Style{Foreground: theme.MutedForeground}}, true,
	)
}

func sessionDetailsHeading(theme ui.Theme, value string) ui.Widget {
	return ui.Text{Value: value, Style: ui.Style{Foreground: theme.Foreground, Attribute: ui.AttrBold}}
}

func sessionDetailsRow(theme ui.Theme, label, value string) ui.Widget {
	return ui.Flex{
		Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			ui.SizedBox{Width: 14, Child: ui.Text{Value: label, Style: ui.Style{Foreground: theme.MutedForeground}}},
			ui.Expanded(ui.Text{Value: value, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
		},
	}
}

func formatUsageInteger(value int) string {
	digits := strconv.Itoa(value)
	if len(digits) <= 3 {
		return digits
	}
	var builder strings.Builder
	first := len(digits) % 3
	if first == 0 {
		first = 3
	}
	builder.WriteString(digits[:first])
	for index := first; index < len(digits); index += 3 {
		builder.WriteByte(',')
		builder.WriteString(digits[index : index+3])
	}
	return builder.String()
}

func formatUsageCost(value float64) string {
	if value == 0 {
		return "$0.00"
	}
	if value < 0.01 {
		return fmt.Sprintf("$%.6f", value)
	}
	return fmt.Sprintf("$%.2f", value)
}
