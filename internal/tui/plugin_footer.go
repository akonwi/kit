package tui

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis/ui"
)

type pluginFooterView struct {
	Footer   *protocol.PluginFooter
	Location string
	// LocationLinkText is the PR label within Location, not the cwd/branch.
	LocationLinkText string
	LocationURL      string
	OpenURL          ui.TextChangedCallback
}

func (pluginFooterView) CreateState() ui.State { return &pluginFooterViewState{} }

type pluginFooterViewState struct {
	ui.StateBase
	width int
}

func (s *pluginFooterViewState) Build(ctx ui.BuildContext) ui.Widget {
	w := s.Widget().(pluginFooterView)
	theme := ui.MustDepend[ui.Theme](ctx)
	semantic, ok := ui.Depend[SemanticTheme](ctx)
	if !ok {
		semantic = semanticFallback(theme)
	}
	spans := footerSpans(w.Footer, w.Location, w.LocationLinkText, w.LocationURL, w.OpenURL, s.width, theme, semantic)
	return widthProbe{WidthChanged: func(width int) {
		if width != s.width {
			s.width = width
			s.MarkNeedsBuild()
		}
	}, Child: ui.RichText{Spans: spans, MaxLines: 1, Overflow: ui.TextOverflowEllipsis, Align: ui.TextAlignRight}}
}

func footerStyle(source protocol.PluginFooterStyle, base ui.Theme, semantic SemanticTheme) ui.Style {
	style := ui.Style{Foreground: base.MutedForeground}
	if source.FG != "" {
		style.Foreground = semantic.Token(source.FG)
	}
	if source.BG != "" {
		style.Background = semantic.Token(source.BG)
	}
	if source.Bold {
		style.Attribute |= ui.AttrBold
	}
	if source.Dim {
		style.Attribute |= ui.AttrDim
	}
	if source.Italic {
		style.Attribute |= ui.AttrItalic
	}
	if source.Strikethrough {
		style.Attribute |= ui.AttrStrikethrough
	}
	if source.Underline {
		style.UnderlineStyle = ui.UnderlineSingle
	}
	return style
}

// locationSpans links only the complete PR label at the end of the Git suffix.
// Truncation that clips the label leaves plain text rather than a misleading link.
func locationSpans(location, linkText, locationURL string, openURL ui.TextChangedCallback, muted ui.Style) []ui.TextSpan {
	if locationURL == "" || linkText == "" || !strings.HasSuffix(location, linkText+")") {
		return []ui.TextSpan{{Text: location, Style: muted}}
	}
	start := len(location) - len(linkText) - 1
	link := ui.TextSpan{Text: linkText, Style: muted}
	link.Style.Hyperlink = locationURL
	link.Style.HyperlinkParams = "id=kit-footer-pull-request"
	if openURL != nil {
		link.OnPressed = func(ctx ui.EventContext) { openURL(ctx, locationURL) }
	}
	return []ui.TextSpan{{Text: location[:start], Style: muted}, link, {Text: ")", Style: muted}}
}

func footerSpans(footer *protocol.PluginFooter, location, linkText, locationURL string, openURL ui.TextChangedCallback, width int, base ui.Theme, semantic SemanticTheme) []ui.TextSpan {
	muted := ui.Style{Foreground: base.MutedForeground}
	if footer == nil || len(footer.Items) == 0 {
		if footer != nil && footer.LocationHidden {
			return nil
		}
		return locationSpans(location, linkText, locationURL, openURL, muted)
	}
	var spans []ui.TextSpan
	used := 0
	if !footer.LocationHidden && location != "" {
		// Reserve room for contribution overflow rather than letting a long cwd
		// consume the entire configurable region.
		overflowWidth := uucode.StringWidth(fmt.Sprintf("… %d more", len(footer.Items)))
		budget := max(0, min(width/2, width-overflowWidth-3))
		location = truncateStartCells(location, budget)
		if location != "" {
			spans = append(spans, locationSpans(location, linkText, locationURL, openURL, muted)...)
			used = uucode.StringWidth(location)
		}
	}
	for index, item := range footer.Items {
		text := ""
		for _, segment := range item.Content {
			text += segment.Text
		}
		gap := 0
		if used > 0 {
			gap = 3
		}
		reserve := 0
		if index+1 < len(footer.Items) {
			reserve = 3 + uucode.StringWidth(fmt.Sprintf("… %d more", len(footer.Items)-index-1))
		}
		if used+gap+uucode.StringWidth(text)+reserve > width {
			if used > 0 {
				spans = append(spans, ui.TextSpan{Text: " · ", Style: muted})
			}
			spans = append(spans, ui.TextSpan{Text: fmt.Sprintf("… %d more", len(footer.Items)-index), Style: muted})
			break
		}
		if used > 0 {
			spans = append(spans, ui.TextSpan{Text: " · ", Style: muted})
		}
		for _, segment := range item.Content {
			spans = append(spans, ui.TextSpan{Text: segment.Text, Style: footerStyle(segment.Style, base, semantic)})
		}
		used += gap + uucode.StringWidth(text)
	}
	return spans
}
