package tui

import (
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/rockorager/go-uucode"
	"go.rockorager.dev/vaxis/ui"
)

type pluginFooterView struct {
	Footer       *protocol.PluginFooter
	Location     string
	LocationBase string // cwd before the appended VCS suffix
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
	spans := footerSpans(w.Footer, w.Location, w.LocationBase, w.LocationLinkText, w.LocationURL, w.OpenURL, s.width, theme, semantic)
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

// footerLocation keeps the VCS suffix intact when it fits, shortening only
// the cwd first. At very narrow widths a complete PR label outranks branch
// and cwd; an incomplete label is never rendered as a clickable fragment.
func footerLocation(location, cwd, linkText string, width int) string {
	if width <= 0 {
		return ""
	}
	if cwd == "" || !strings.HasPrefix(location, cwd+" (") || !strings.HasSuffix(location, ")") {
		return truncateStartCells(location, width)
	}
	suffix := location[len(cwd):]
	suffixWidth := uucode.StringWidth(suffix)
	if suffixWidth < width {
		return truncateStartCells(cwd, width-suffixWidth) + suffix
	}
	// The separating space belongs to the cwd; omit it when no cwd fits.
	if bare := strings.TrimPrefix(suffix, " "); uucode.StringWidth(bare) <= width {
		return bare
	}
	if linkText != "" && strings.HasSuffix(suffix, " "+glyphMiddleDot+" "+linkText+")") {
		pr := "(" + linkText + ")"
		if uucode.StringWidth(pr) <= width {
			return pr
		}
		return glyphEllipsis
	}
	return truncateStartCells(cwd, width)
}

func footerSpans(footer *protocol.PluginFooter, location, cwd, linkText, locationURL string, openURL ui.TextChangedCallback, width int, base ui.Theme, semantic SemanticTheme) []ui.TextSpan {
	muted := ui.Style{Foreground: base.MutedForeground}
	// The width probe reports after the first layout. Render unbounded content
	// until then so the first frame still contains location and plugin items.
	if width <= 0 {
		width = 1 << 20
	}
	if footer == nil || len(footer.Items) == 0 {
		if footer != nil && footer.LocationHidden {
			return nil
		}
		return locationSpans(footerLocation(location, cwd, linkText, width), linkText, locationURL, openURL, muted)
	}
	var spans []ui.TextSpan
	used := 0
	if !footer.LocationHidden && location != "" {
		// Keep the customary half-width cap for cwd-only locations. A VCS
		// suffix can borrow space from contributions, but preserve their
		// labeled overflow whenever the viewport can fit both.
		overflowWidth := uucode.StringWidth(fmt.Sprintf("… %d more", len(footer.Items)))
		withOverflow := max(0, width-overflowWidth-3)
		budget := min(width/2, withOverflow)
		if cwd != "" && strings.HasPrefix(location, cwd+" (") {
			suffix := location[len(cwd):]
			minimum := uucode.StringWidth(strings.TrimPrefix(suffix, " "))
			if linkText != "" && strings.HasSuffix(suffix, " "+glyphMiddleDot+" "+linkText+")") && minimum > width {
				minimum = uucode.StringWidth("(" + linkText + ")")
			}
			firstItemWidth := 0
			for _, segment := range footer.Items[0].Content {
				firstItemWidth += uucode.StringWidth(segment.Text)
			}
			budget = max(budget, min(withOverflow, max(minimum, width-firstItemWidth-3)))
			if len(footer.Items) == 1 && minimum+firstItemWidth+3 <= width {
				budget = max(budget, width-firstItemWidth-3)
			}
			if minimum > withOverflow {
				budget = min(minimum, width)
			}
		}
		location = footerLocation(location, cwd, linkText, budget)
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
			overflow := fmt.Sprintf("… %d more", len(footer.Items)-index)
			if used+gap+uucode.StringWidth(overflow) <= width {
				if used > 0 {
					spans = append(spans, ui.TextSpan{Text: " · ", Style: muted})
				}
				spans = append(spans, ui.TextSpan{Text: overflow, Style: muted})
			}
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
