package tui

import (
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis/ui"
)

// transcriptPluginMessageEntry presents a plugin-submitted message as one quiet
// row naming the plugin and the message's first line. Expanding it shows the
// full message beneath a left rule. The message is context the plugin gave the
// agent, so it stays subordinate to user and assistant messages.
func transcriptPluginMessageEntry(theme ui.Theme, message protocol.TranscriptMessage, expanded bool, toggle ui.VoidCallback) ui.Widget {
	text := strings.TrimSpace(message.TextContent())
	indicator := glyphTriangleRight
	if expanded {
		indicator = glyphTriangleDown
	}
	muted := ui.Style{Foreground: theme.MutedForeground}
	identity := ui.Style{Foreground: theme.AccentText}
	header := ui.Widget(ui.Flex{Axis: ui.Horizontal, MainAxisSize: ui.MainAxisSizeMin, Children: []ui.Widget{
		ui.Text{Value: glyphDiamond + " ", Style: identity, MaxLines: 1},
		ui.Text{Value: message.BoundarySource, Style: identity, MaxLines: 1},
		ui.Text{Value: " " + glyphMiddleDot + " ", Style: muted, MaxLines: 1},
		ui.Flexible(ui.Text{Value: pluginMessageSummary(text), Style: muted, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
		ui.SizedBox{Width: 1},
		ui.Text{Value: indicator, Style: muted, MaxLines: 1},
	}})
	if toggle != nil {
		header = mouseActivator{Child: header, OnPressed: toggle}
	}
	if !expanded {
		return header
	}
	// The left rule occupies the first padded column.
	body := ui.DecoratedBox(
		ui.Decoration{Border: ui.Border{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}, Left: true}},
		ui.Padding(ui.Insets{Left: 2}, markdownView{ID: "transcript-plugin:" + message.TurnID, Source: text, BaseStyle: muted}),
	)
	return ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, MainAxisSize: ui.MainAxisSizeMin, Children: []ui.Widget{
		header,
		ui.Padding(ui.Insets{Left: 2}, body),
	}}
}

// pluginMessageSummary is the first non-blank line of a plugin message.
func pluginMessageSummary(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
