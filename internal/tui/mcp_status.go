package tui

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type mcpStatusSurface struct {
	Servers  []protocol.MCPServerStatus
	Warnings []string
}

func (mcpStatusSurface) CreateState() ui.State { return &mcpStatusSurfaceState{} }

type mcpStatusSurfaceState struct {
	ui.StateBase
	scroll ui.ScrollController
}

func (s *mcpStatusSurfaceState) Build(ctx ui.BuildContext) ui.Widget {
	surface := s.Widget().(mcpStatusSurface)
	theme := ui.MustDepend[ui.Theme](ctx)
	rows := make([]ui.Widget, 0, len(surface.Servers)*5+len(surface.Warnings)+3)
	if len(surface.Servers) == 0 {
		rows = append(rows, ui.Center(ui.Text{Value: "No MCP servers are configured.", Style: ui.Style{Foreground: theme.MutedForeground}}))
	} else {
		for index, server := range surface.Servers {
			if index > 0 {
				rows = append(rows, ui.SizedBox{Height: 1})
			}
			rows = append(rows, mcpServerHeading(theme, server))
			if server.Description != "" {
				rows = append(rows, ui.Padding(ui.Insets{Left: 2}, ui.Text{Value: server.Description, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true, MaxLines: 2, Overflow: ui.TextOverflowEllipsis}))
			}
			metadata := fmt.Sprintf("%s %s %s %s %d tool%s", strings.ToUpper(server.Transport), glyphMiddleDot, server.State, glyphMiddleDot, server.ToolCount, pluralSuffix(server.ToolCount))
			if server.OAuthSaved {
				metadata += " " + glyphMiddleDot + " OAuth saved"
			}
			rows = append(rows, ui.Padding(ui.Insets{Left: 2}, ui.Text{Value: metadata, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}))
			if server.ConfigPath != "" {
				rows = append(rows, ui.Padding(ui.Insets{Left: 2}, ui.Text{Value: mcpSourceLabel(server.Source) + " " + glyphMiddleDot + " " + server.ConfigPath, Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}))
			}
			if server.LastError != "" {
				rows = append(rows, ui.Padding(ui.Insets{Left: 2}, ui.Text{Value: server.LastError, Style: ui.Style{Foreground: theme.DangerText}, SoftWrap: true, MaxLines: 2}))
			}
		}
	}
	if len(surface.Warnings) > 0 {
		rows = append(rows, ui.SizedBox{Height: 1}, sessionDetailsHeading(theme, "Warnings"))
		for _, warning := range surface.Warnings {
			rows = append(rows, ui.Text{Value: glyphTriangleUp + " " + warning, Style: ui.Style{Foreground: theme.WarningText}, SoftWrap: true, MaxLines: 2, Overflow: ui.TextOverflowEllipsis})
		}
	}
	header := ui.Text{Value: "MCP servers", Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	body := ui.Scrollbar{Child: ui.ScrollView{
		Controller: &s.scroll,
		Child:      ui.Padding(ui.Insets{Right: 1}, ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows}),
	}}
	border := ui.Style{Foreground: theme.Border, Background: theme.Background}
	content := ui.DecoratedBox(ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}, Border: ui.BorderAll(border)},
		ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Padding(ui.Insets{Top: 1, Right: 2, Bottom: 1, Left: 2}, header),
			ui.Expanded(ui.Padding(ui.Insets{Right: 2, Bottom: 1, Left: 2}, body)),
			dialogDivider{Style: border},
			ui.Padding(ui.Insets{Right: 2, Bottom: 1, Left: 2}, ui.Text{Value: "↑↓ scroll · esc close", Style: ui.Style{Foreground: theme.MutedForeground}}),
		}},
	)
	return centeredDialogPositioner{Percent: 70, MinWidth: 48, MaxWidth: 96, Height: 22, Child: content}
}

func mcpServerHeading(theme ui.Theme, server protocol.MCPServerStatus) ui.Widget {
	glyph, style := glyphCircleEmpty, ui.Style{Foreground: theme.MutedForeground}
	switch server.State {
	case "connected":
		glyph, style = glyphCheck, ui.Style{Foreground: theme.SuccessText}
	case "connecting", "authorizing":
		return ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			spinner{Style: ui.Style{Foreground: theme.WarningText}}, ui.SizedBox{Width: 1},
			ui.Expanded(ui.Text{Value: server.Name, Style: ui.Style{Attribute: ui.AttrBold}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
			ui.Text{Value: server.State, Style: ui.Style{Foreground: theme.WarningText}},
		}}
	case "disabled":
		glyph = glyphCircleSlash
	case "error":
		glyph, style = glyphCross, ui.Style{Foreground: theme.DangerText}
	}
	return ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
		ui.Text{Value: glyph, Style: style}, ui.SizedBox{Width: 1},
		ui.Expanded(ui.Text{Value: server.Name, Style: ui.Style{Attribute: ui.AttrBold}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
		ui.Text{Value: server.State, Style: style},
	}}
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func mcpSourceLabel(source string) string {
	switch source {
	case "kit-user":
		return "User config"
	case "shared-project":
		return "Project"
	case "kit-project":
		return "Kit project"
	default:
		return source
	}
}
