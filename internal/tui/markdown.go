package tui

import (
	"strconv"
	"strings"
	"unicode"

	kitmarkdown "github.com/akonwi/kit/internal/markdown"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	markdownListIndent         = 2
	maxMarkdownQuoteDepth      = 4
	markdownTableCompactColumn = 12
)

type markdownView struct {
	ID          string
	Source      string
	BaseStyle   ui.Style
	parseSource func(string) kitmarkdown.Document
}

func (w markdownView) WidgetKey() ui.KeyValue {
	return ui.KeyValue("markdown:" + w.ID)
}

func (markdownView) CreateState() ui.State { return &markdownViewState{} }

type markdownViewState struct {
	ui.StateBase
	cache markdownDocumentCache
}

type markdownDocumentCache struct {
	document      kitmarkdown.Document
	parsedSource  string
	pendingSource string
	parsePending  bool
}

func newMarkdownDocumentCache(config markdownView) markdownDocumentCache {
	return markdownDocumentCache{
		document: parseMarkdownDocument(config), parsedSource: config.Source,
	}
}

func (c *markdownDocumentCache) update(source string) {
	c.pendingSource = source
	c.parsePending = source != c.parsedSource
}

func (c *markdownDocumentCache) flush(config markdownView) bool {
	if !c.parsePending {
		return false
	}
	config.Source = c.pendingSource
	c.document = parseMarkdownDocument(config)
	c.parsedSource = c.pendingSource
	c.parsePending = false
	return true
}

func (s *markdownViewState) InitState() {
	s.cache = newMarkdownDocumentCache(s.Widget().(markdownView))
}

func (s *markdownViewState) DidUpdateWidget(ui.Widget) {
	config := s.Widget().(markdownView)
	s.cache.update(config.Source)
	// Parent event reduction already coalesces all deltas queued for this
	// frame. Parse that final source now so layout and follow-output see the
	// exact same document; static messages remain cached.
	s.cache.flush(config)
}

func (s *markdownViewState) Build(ctx ui.BuildContext) ui.Widget {
	config := s.Widget().(markdownView)
	theme := ui.MustDepend[ui.Theme](ctx)
	base := config.BaseStyle
	if base.Foreground == 0 {
		base.Foreground = theme.Foreground
	}
	document := s.cache.document
	children := make([]ui.Widget, 0, len(document.Blocks)*2)
	for index, block := range document.Blocks {
		if index > 0 && markdownBlocksNeedGap(document.Blocks[index-1], block) {
			children = append(children, renderMarkdownGap(theme, base, document.Blocks[index-1], block))
		}
		children = append(children, renderMarkdownBlock(theme, base, block))
	}
	// Keep one stable outer render-object shape as streaming content changes.
	return ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
	}
}

type markdownInlineView struct {
	Source    string
	BaseStyle ui.Style
}

func (w markdownInlineView) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	base := w.BaseStyle
	if base.Foreground == 0 {
		base.Foreground = theme.Foreground
	}
	document := kitmarkdown.Parse(w.Source)
	runs := []kitmarkdown.Run(nil)
	if len(document.Blocks) > 0 {
		block := document.Blocks[len(document.Blocks)-1]
		switch block.Kind {
		case kitmarkdown.BlockCode:
			runs = []kitmarkdown.Run{{Text: block.Code, Code: true}}
		case kitmarkdown.BlockRule:
			runs = []kitmarkdown.Run{{Text: w.Source}}
		case kitmarkdown.BlockTable:
			if len(block.Rows) > 0 {
				for index, cell := range block.Rows[len(block.Rows)-1].Cells {
					if index > 0 {
						runs = append(runs, kitmarkdown.Run{Text: " " + glyphTableSeparator + " "})
					}
					runs = append(runs, cell.Runs...)
				}
			}
		default:
			runs = append(runs, block.Runs...)
			if block.Kind == kitmarkdown.BlockHeading {
				for index := range runs {
					runs[index].Bold = true
				}
			}
		}
	}
	return ui.RichText{
		Spans: markdownTextSpans(theme, base, runs), SoftWrap: true,
		MaxLines: 1, Overflow: ui.TextOverflowEllipsis,
	}
}

func parseMarkdownDocument(config markdownView) kitmarkdown.Document {
	if config.parseSource != nil {
		return config.parseSource(config.Source)
	}
	return kitmarkdown.Parse(config.Source)
}

func markdownBlocksNeedGap(previous, current kitmarkdown.Block) bool {
	shared := false
	loose := false
	for _, left := range previous.Containers {
		if left.Kind != kitmarkdown.ContainerListItem {
			continue
		}
		for _, right := range current.Containers {
			if right.Kind == kitmarkdown.ContainerListItem && left.ListID == right.ListID {
				// Iterating outer-to-inner means the deepest shared list decides
				// spacing for these adjacent blocks.
				shared = true
				loose = left.Loose || right.Loose
			}
		}
	}
	return !shared || loose
}

func renderMarkdownGap(theme ui.Theme, base ui.Style, previous, current kitmarkdown.Block) ui.Widget {
	child := ui.Widget(ui.SizedBox{Height: 1})
	containers := sharedMarkdownContainers(previous.Containers, current.Containers)
	quoteLayers := 0
	for index := len(containers) - 1; index >= 0; index-- {
		container := containers[index]
		switch container.Kind {
		case kitmarkdown.ContainerQuote:
			if quoteLayers < maxMarkdownQuoteDepth {
				child = wrapMarkdownQuote(theme, child)
			}
			quoteLayers++
		case kitmarkdown.ContainerListItem:
			container.Marker = kitmarkdown.MarkerNone
			container.Number = 0
			container.Task = false
			container.Checked = false
			child = markdownListItem(theme, base, container, child)
		}
	}
	return child
}

func sharedMarkdownContainers(left, right []kitmarkdown.Container) []kitmarkdown.Container {
	count := min(len(left), len(right))
	shared := make([]kitmarkdown.Container, 0, count)
	for index := 0; index < count; index++ {
		if !sameMarkdownContainer(left[index], right[index]) {
			break
		}
		shared = append(shared, right[index])
	}
	return shared
}

func sameMarkdownContainer(left, right kitmarkdown.Container) bool {
	if left.Kind != right.Kind {
		return false
	}
	if left.Kind == kitmarkdown.ContainerQuote {
		return left.QuoteID != 0 && left.QuoteID == right.QuoteID
	}
	return left.ListItemID != 0 && left.ListItemID == right.ListItemID
}

func renderMarkdownBlock(
	theme ui.Theme,
	base ui.Style,
	block kitmarkdown.Block,
) ui.Widget {
	var child ui.Widget
	switch block.Kind {
	case kitmarkdown.BlockHeading:
		style := base
		style.Attribute |= ui.AttrBold
		if markdownSemanticColorAllowed(theme, base) && block.HeadingLevel <= 2 {
			style.Foreground = theme.PrimaryText
		}
		child = markdownRichText(theme, style, block.Runs)
	case kitmarkdown.BlockCode:
		child = markdownCodeBlock(theme, base, block.Language, block.Code)
	case kitmarkdown.BlockRule:
		child = ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}}
	case kitmarkdown.BlockTable:
		child = markdownTable(theme, base, block)
	default:
		style := base
		if block.Kind == kitmarkdown.BlockQuote && markdownSemanticColorAllowed(theme, base) {
			style.Foreground = theme.MutedForeground
		}
		child = markdownRichText(theme, style, block.Runs)
	}
	quoteLayers := 0
	for index := len(block.Containers) - 1; index >= 0; index-- {
		container := block.Containers[index]
		switch container.Kind {
		case kitmarkdown.ContainerQuote:
			if quoteLayers < maxMarkdownQuoteDepth {
				child = wrapMarkdownQuote(theme, child)
			}
			quoteLayers++
		case kitmarkdown.ContainerListItem:
			child = markdownListItem(theme, base, container, child)
		}
	}
	return child
}

func markdownListItem(theme ui.Theme, base ui.Style, container kitmarkdown.Container, content ui.Widget) ui.Widget {
	marker := ""
	switch container.Marker {
	case kitmarkdown.MarkerBullet:
		marker = glyphBullet
	case kitmarkdown.MarkerOrdered:
		marker = strconv.Itoa(max(1, container.Number)) + "."
	}
	if container.Task {
		taskMarker := glyphTaskUnchecked
		if container.Checked {
			taskMarker = glyphTaskChecked
		}
		if container.Marker == kitmarkdown.MarkerOrdered {
			marker += " " + taskMarker
		} else {
			marker = taskMarker
		}
	}
	markerWidth := max(1, container.MarkerWidth)
	markerStyle := base
	if markdownSemanticColorAllowed(theme, base) {
		markerStyle.Foreground = theme.MutedForeground
	}
	return ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStart, Children: []ui.Widget{
		ui.SizedBox{Width: markerWidth, Child: ui.Text{Value: marker, Style: markerStyle, Align: ui.TextAlignRight}},
		ui.SizedBox{Width: 1},
		ui.Expanded(ui.ConstrainedBox{Constraints: ui.Constraints{MinHeight: 1}, Child: content}),
	}}
}

func markdownCodeBlock(theme ui.Theme, base ui.Style, _ string, source string) ui.Widget {
	// The semantic block retains its language for future syntax highlighting;
	// this presentation deliberately spends no transcript row displaying it.
	display := expandMarkdownTabs(source)
	base.Background = theme.Surface
	spans := []ui.TextSpan{{Text: display, Style: base}}
	content := ui.ConstrainedBox{
		Constraints: ui.Constraints{MinHeight: 1},
		Child:       ui.RichText{Spans: spans, SoftWrap: true},
	}
	return ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: theme.Surface}},
		ui.Padding(ui.Symmetric(1, 0), content),
	)
}

func markdownTable(theme ui.Theme, base ui.Style, block kitmarkdown.Block) ui.Widget {
	columnCount := 0
	for _, row := range block.Rows {
		columnCount = max(columnCount, len(row.Cells))
	}
	threshold := max(24, columnCount*markdownTableCompactColumn+max(0, columnCount-1)*2)
	return responsiveMarkdownTable{
		Threshold: threshold,
		BuildWide: func() ui.Widget {
			return markdownAlignedTable(theme, base, block, columnCount)
		},
		BuildNarrow: func() ui.Widget {
			return markdownFlatTable(theme, base, block)
		},
	}
}

func markdownAlignedTable(
	theme ui.Theme,
	base ui.Style,
	block kitmarkdown.Block,
	columnCount int,
) ui.Widget {
	columns := make([]ui.TableColumn, columnCount)
	widths := markdownTableContentWidths(block, columnCount)
	for index := range columns {
		if widths[index] <= markdownTableCompactColumn {
			columns[index] = ui.FixedColumn(max(1, widths[index]))
		} else {
			columns[index] = ui.FlexColumn(1)
		}
	}
	rows := make([]ui.TableRow, 0, len(block.Rows)+1)
	for rowIndex, row := range block.Rows {
		cells := make([]ui.Widget, columnCount)
		for index := 0; index < columnCount; index++ {
			style := base
			if row.Header {
				style.Attribute |= ui.AttrBold
			}
			var runs []kitmarkdown.Run
			if index < len(row.Cells) {
				runs = row.Cells[index].Runs
			}
			cells[index] = ui.RichText{
				Spans:    markdownTextSpans(theme, style, runs),
				SoftWrap: true, Align: markdownTableAlignment(block.Alignments, index),
			}
		}
		rows = append(rows, ui.TableRow{Children: cells})
		if row.Header && rowIndex+1 < len(block.Rows) {
			separators := make([]ui.Widget, columnCount)
			for index := range separators {
				separators[index] = ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}}
			}
			rows = append(rows, ui.TableRow{Children: separators})
		}
	}
	return ui.Table{Columns: columns, ColumnGap: 2, Rows: rows}
}

func markdownTableContentWidths(block kitmarkdown.Block, columnCount int) []int {
	widths := make([]int, columnCount)
	for _, row := range block.Rows {
		for index, cell := range row.Cells {
			width := 0
			for _, run := range cell.Runs {
				text := run.Text
				if run.Image {
					text = "image: " + text
				}
				width += markdownCellWidth(text)
				if run.Link != "" && !markdownLinkTextMatchesTarget(text, run.Link) {
					width += markdownCellWidth(run.Link) + 3
				}
			}
			widths[index] = max(widths[index], width)
		}
	}
	return widths
}

func markdownCellWidth(text string) int {
	width := 0
	for _, character := range vaxis.Characters(text) {
		width += character.Width
	}
	return width
}

func markdownFlatTable(theme ui.Theme, base ui.Style, block kitmarkdown.Block) ui.Widget {
	rows := make([]ui.Widget, 0, len(block.Rows)+1)
	for rowIndex, row := range block.Rows {
		style := base
		if row.Header {
			style.Attribute |= ui.AttrBold
		}
		spans := make([]ui.TextSpan, 0, len(row.Cells)*2)
		for cellIndex, cell := range row.Cells {
			if cellIndex > 0 {
				separatorStyle := base
				if markdownSemanticColorAllowed(theme, base) {
					separatorStyle.Foreground = theme.MutedForeground
				}
				spans = append(spans, ui.TextSpan{Text: " " + glyphTableSeparator + " ", Style: separatorStyle})
			}
			spans = append(spans, markdownTextSpans(theme, style, cell.Runs)...)
		}
		rows = append(rows, ui.RichText{Spans: spans, SoftWrap: true})
		if row.Header && rowIndex+1 < len(block.Rows) {
			rows = append(rows, ui.Divider{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}})
		}
	}
	return ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisStretch, Children: rows,
	}
}

func markdownTableAlignment(alignments []kitmarkdown.Alignment, index int) ui.TextAlign {
	if index >= len(alignments) {
		return ui.TextAlignStart
	}
	switch alignments[index] {
	case kitmarkdown.AlignCenter:
		return ui.TextAlignCenter
	case kitmarkdown.AlignRight:
		return ui.TextAlignRight
	default:
		return ui.TextAlignStart
	}
}

func markdownRichText(theme ui.Theme, base ui.Style, runs []kitmarkdown.Run) ui.Widget {
	return ui.RichText{Spans: markdownTextSpans(theme, base, runs), SoftWrap: true}
}

func markdownTextSpans(theme ui.Theme, base ui.Style, runs []kitmarkdown.Run) []ui.TextSpan {
	spans := make([]ui.TextSpan, 0, len(runs)+4)
	for start := 0; start < len(runs); {
		if runs[start].Link == "" {
			spans = append(spans, markdownRunSpan(theme, base, runs[start]))
			start++
			continue
		}
		target := runs[start].Link
		safe := safeExternalHyperlink(target)
		end := start
		visible := strings.Builder{}
		image := false
		for end < len(runs) && runs[end].Link == target {
			run := runs[end]
			if run.Image {
				image = true
				if run.Text == "" {
					run.Text = "image"
				} else {
					run.Text = "image: " + run.Text
				}
			} else if run.Text == "" {
				run.Text = "link"
			}
			visible.WriteString(run.Text)
			span := markdownRunSpan(theme, base, run)
			if safe != "" {
				span.Style.UnderlineStyle = ui.UnderlineSingle
				span.Style.Hyperlink = safe
			}
			spans = append(spans, span)
			end++
		}
		displayTarget := sanitizeMarkdownLinkTarget(target)
		if image || !markdownLinkTextMatchesTarget(visible.String(), target) {
			spans = append(spans, ui.TextSpan{Text: " (", Style: base})
			linkStyle := base
			if safe != "" {
				linkStyle = mergeMarkdownStyle(base, ui.Style{UnderlineStyle: ui.UnderlineSingle})
				if markdownSemanticColorAllowed(theme, base) {
					linkStyle.Foreground = theme.AccentText
				}
			}
			linkSpan := ui.TextSpan{
				Text: displayTarget, Style: linkStyle,
			}
			linkSpan.Style.Hyperlink = safe
			spans = append(spans, linkSpan, ui.TextSpan{Text: ")", Style: base})
		}
		start = end
	}
	return spans
}

func markdownRunSpan(theme ui.Theme, base ui.Style, run kitmarkdown.Run) ui.TextSpan {
	style := base
	if run.Bold {
		style.Attribute |= ui.AttrBold
	}
	if run.Italic {
		style.Attribute |= ui.AttrItalic
	}
	if run.Strikethrough {
		style.Attribute |= ui.AttrStrikethrough
	}
	if run.Code {
		style.Background = theme.Surface
	}
	if safeExternalHyperlink(run.Link) != "" && markdownSemanticColorAllowed(theme, base) {
		style.Foreground = theme.AccentText
	}
	if run.RawHTML && markdownSemanticColorAllowed(theme, base) {
		style.Foreground = theme.MutedForeground
	}
	return ui.TextSpan{Text: run.Text, Style: style}
}

func wrapMarkdownQuote(theme ui.Theme, child ui.Widget) ui.Widget {
	return ui.DecoratedBox(
		ui.Decoration{Border: ui.Border{Style: ui.Style{Foreground: theme.Border, Background: theme.Background}, Left: true}},
		ui.Padding(ui.Insets{Left: 2}, child),
	)
}

func markdownSemanticColorAllowed(theme ui.Theme, base ui.Style) bool {
	return base.Foreground == 0 || base.Foreground == theme.Foreground
}

func mergeMarkdownStyle(base, overlay ui.Style) ui.Style {
	if overlay.Foreground == 0 {
		overlay.Foreground = base.Foreground
	}
	if overlay.Background == 0 {
		overlay.Background = base.Background
	}
	if overlay.UnderlineColor == 0 {
		overlay.UnderlineColor = base.UnderlineColor
	}
	if overlay.UnderlineStyle == ui.UnderlineOff {
		overlay.UnderlineStyle = base.UnderlineStyle
	}
	overlay.Attribute |= base.Attribute
	return overlay
}

func markdownLinkTextMatchesTarget(text, target string) bool {
	if text == target {
		return true
	}
	return strings.HasPrefix(strings.ToLower(target), "mailto:") && text == target[len("mailto:"):]
}

func sanitizeMarkdownLinkTarget(target string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) ||
			unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character) {
			return '�'
		}
		return character
	}, target)
}

func expandMarkdownTabs(source string) string {
	return strings.ReplaceAll(source, "\t", "  ")
}
