package tui

import (
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	pickerColumnGap     = 2
	pickerMaxLabelWidth = 32
	pickerMaxHintWidth  = 24
	pickerMaxMetaWidth  = 20
	// pickerMaxIndentDepth bounds hierarchy indentation so deep items keep
	// room for their labels.
	pickerMaxIndentDepth = 6
	// pickerHighlightPercent and pickerHoverPercent blend the picker focus
	// color into the background, like diff line highlights, so row text keeps
	// its own colors while highlighted or hovered.
	pickerHighlightPercent = 20
	pickerHoverPercent     = 8
	pickerDisabledPercent  = 10
)

// pickerColumns are the uniform column widths shared by every row of a picker.
// Widths come from the whole catalog so filtering never shifts columns.
type pickerColumns struct {
	Label, Hint, Meta int
	Description       bool
}

func measurePickerColumns(items []pickerItem) pickerColumns {
	var columns pickerColumns
	for _, item := range items {
		columns.Label = max(columns.Label, pickerTextWidth(pickerItemLabel(item)))
		columns.Hint = max(columns.Hint, pickerTextWidth(pickerItemHint(item)))
		columns.Meta = max(columns.Meta, pickerTextWidth(item.Meta))
		columns.Description = columns.Description || item.Description != "" || item.DisabledReason != ""
	}
	columns.Label = max(1, min(columns.Label, pickerMaxLabelWidth))
	columns.Hint = min(columns.Hint, pickerMaxHintWidth)
	columns.Meta = min(columns.Meta, pickerMaxMetaWidth)
	return columns
}

// pickerItemLabel is the label indented for the item's depth.
func pickerItemLabel(item pickerItem) string {
	return strings.Repeat("  ", max(0, min(item.Depth, pickerMaxIndentDepth))) + item.Label
}

// pickerDisclosureText is "▸ N" or "▾ N" for items with a disclosure.
func pickerDisclosureText(item pickerItem) string {
	switch item.Disclosure {
	case pickerDisclosureCollapsed:
		return glyphTriangleRight + " " + strconv.Itoa(item.ChildCount)
	case pickerDisclosureExpanded:
		return glyphTriangleDown + " " + strconv.Itoa(item.ChildCount)
	default:
		return ""
	}
}

// pickerItemHint is the full hint column text: disclosure, then Hint.
func pickerItemHint(item pickerItem) string {
	disclosure := pickerDisclosureText(item)
	switch {
	case disclosure == "":
		return item.Hint
	case item.Hint == "":
		return disclosure
	default:
		return disclosure + " " + glyphMiddleDot + " " + item.Hint
	}
}

func pickerTextWidth(value string) int {
	width := 0
	for _, character := range vaxis.Characters(value) {
		width += character.Width
	}
	return width
}

// truncateStartCells shortens value to maximum terminal cells, preserving its
// suffix and prefixing it with an ellipsis.
func truncateStartCells(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	characters := (ui.LayoutContext{}).Characters(value)
	if pickerTextWidth(value) <= maximum {
		return value
	}
	remaining := maximum - 1
	start := len(characters)
	for start > 0 && characters[start-1].Width <= remaining {
		start--
		remaining -= characters[start].Width
	}
	var suffix strings.Builder
	for _, character := range characters[start:] {
		suffix.WriteString(character.Grapheme)
	}
	return glyphTruncation + suffix.String()
}

// pickerStartTruncatedText paints a single line whose suffix remains visible
// when its constrained width requires shortening.
type pickerStartTruncatedText struct {
	Value string
	Style ui.Style
}

func (w pickerStartTruncatedText) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderPickerStartTruncatedText{Value: w.Value, Style: w.Style}
}

func (w pickerStartTruncatedText) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderPickerStartTruncatedText)
	if render.Value != w.Value || render.Style != w.Style {
		render.Value, render.Style = w.Value, w.Style
		render.MarkNeedsLayout()
	}
}

type renderPickerStartTruncatedText struct {
	ui.LeafRenderObject
	Value string
	Style ui.Style
	text  string
}

func (r *renderPickerStartTruncatedText) Layout(_ ui.LayoutContext, constraints ui.Constraints) {
	size := constraints.Constrain(ui.Size{Width: pickerTextWidth(r.Value), Height: 1})
	r.text = truncateStartCells(r.Value, size.Width)
	r.SetSize(size)
}

func (r *renderPickerStartTruncatedText) Paint(painter *ui.Painter, offset ui.Offset) {
	painter.DrawText(offset, r.text, r.Style)
}

func (*renderPickerStartTruncatedText) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

// pickerRowColors resolves one row's colors. Selection and hover change only
// the fill and gutter bar; text keeps its role color so the current item stays
// distinct while highlighted.
type pickerRowColors struct {
	Fill, Bar, Label, Muted ui.Color
}

func resolvePickerRowColors(ctx ui.BuildContext, theme ui.Theme, item pickerItem, selected, hovered bool) pickerRowColors {
	presentation := resolvePickerRowPresentation(ctx, theme)
	focus := presentation.FocusedBg
	colors := pickerRowColors{Fill: theme.Background, Label: presentation.ItemText, Muted: theme.MutedForeground}
	disabled := item.DisabledReason != ""
	switch {
	case disabled:
		colors.Label, colors.Muted = theme.DisabledForeground, theme.DisabledForeground
	case item.Current:
		colors.Label = theme.PrimaryText
	}
	switch {
	case selected && disabled:
		colors.Fill = blendPickerColor(focus, theme.Background, pickerDisabledPercent, theme.SurfaceHovered)
		colors.Bar = theme.DisabledForeground
	case selected:
		colors.Fill = blendPickerColor(focus, theme.Background, pickerHighlightPercent, theme.SurfaceHovered)
		colors.Bar = focus
	case hovered && !disabled:
		colors.Fill = blendPickerColor(focus, theme.Background, pickerHoverPercent, theme.SurfaceHovered)
	}
	return colors
}

// blendPickerColor mixes percent of color into background. Terminals with
// indexed colors cannot be blended, so those use fallback.
func blendPickerColor(color, background ui.Color, percent int, fallback ui.Color) ui.Color {
	top, bottom := color.Params(), background.Params()
	if len(top) != 3 || len(bottom) != 3 {
		return fallback
	}
	mix := func(a, b uint8) uint8 {
		return uint8((int(a)*percent + int(b)*(100-percent) + 50) / 100)
	}
	return ui.RGB(mix(top[0], bottom[0]), mix(top[1], bottom[1]), mix(top[2], bottom[2]))
}

// pickerRow is one clickable single-line row: gutter bar, uniform columns, and
// a trailing padding cell that mirrors the gutter.
type pickerRow struct {
	Item     pickerItem
	Columns  pickerColumns
	Selected bool
	OnActive ui.VoidCallback
	// OnToggle makes the disclosure clickable.
	OnToggle ui.VoidCallback
}

func (pickerRow) CreateState() ui.State { return &pickerRowState{} }

type pickerRowState struct {
	ui.StateBase
	hovered bool
}

func (s *pickerRowState) Build(ctx ui.BuildContext) ui.Widget {
	row := s.Widget().(pickerRow)
	theme := ui.MustDepend[ui.Theme](ctx)
	colors := resolvePickerRowColors(ctx, theme, row.Item, row.Selected, s.hovered)
	gutter := ui.Widget(ui.SizedBox{Width: 1, Height: 1})
	if row.Selected {
		gutter = ui.SizedBox{Width: 1, Height: 1, Child: ui.Text{Value: glyphLeftBar, Style: ui.Style{Foreground: colors.Bar, Background: theme.Background}, MaxLines: 1}}
	}
	content := ui.SizedBox{Height: 1, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Background: colors.Fill}},
		pickerRowColumns(row.Item, row.Columns, colors, row.OnToggle),
	)}
	widget := ui.Widget(ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
		gutter, ui.Expanded(content), ui.SizedBox{Width: 1, Height: 1},
	}})
	if row.Item.DisabledReason != "" || row.OnActive == nil {
		return widget
	}
	return mouseActivator{
		OnPressed: row.OnActive,
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
		Child: widget,
	}
}

func pickerRowColumns(item pickerItem, columns pickerColumns, colors pickerRowColors, onToggle ui.VoidCallback) ui.Widget {
	text := func(value string, color ui.Color, align ui.TextAlign) ui.Widget {
		return ui.Text{Value: value, Style: ui.Style{Foreground: color}, Align: align, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}
	}
	description := item.Description
	if item.DisabledReason != "" {
		description = glyphCircleSlash + " " + item.DisabledReason
		if item.Description != "" {
			description += " " + glyphMiddleDot + " " + item.Description
		}
	}
	hint := text(item.Hint, colors.Muted, ui.TextAlignLeft)
	if disclosure := pickerDisclosureText(item); disclosure != "" {
		// Only the disclosure toggles; the rest of the hint belongs to the row.
		toggle := text(disclosure, colors.Muted, ui.TextAlignLeft)
		if onToggle != nil {
			toggle = mouseActivator{OnPressed: onToggle, Child: toggle}
		}
		hint = toggle
		if item.Hint != "" {
			hint = ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
				ui.SizedBox{Width: pickerTextWidth(disclosure), Height: 1, Child: toggle},
				ui.Expanded(text(" "+glyphMiddleDot+" "+item.Hint, colors.Muted, ui.TextAlignLeft)),
			}}
		}
	}
	label := ui.Widget(text(pickerItemLabel(item), colors.Label, ui.TextAlignLeft))
	if item.LabelTruncation == pickerLabelTruncationStart {
		label = pickerStartTruncatedText{Value: pickerItemLabel(item), Style: ui.Style{Foreground: colors.Label}}
	}
	return pickerRowLayout{Columns: columns, Children: []ui.Widget{
		label,
		hint,
		text(description, colors.Muted, ui.TextAlignLeft),
		text(item.Meta, colors.Muted, ui.TextAlignRight),
	}}
}

// pickerRowLayout places a row's label, hint, description, and metadata in
// the picker's uniform columns. Widths depend only on the shared columns and
// the row width, so every row of a picker resolves the same column edges.
type pickerRowLayout struct {
	Columns  pickerColumns
	Children []ui.Widget
}

func (w pickerRowLayout) WidgetChildren() []ui.Widget { return w.Children }

func (w pickerRowLayout) CreateRenderObject(ui.BuildContext) ui.RenderObject {
	return &renderPickerRowLayout{Columns: w.Columns}
}

func (w pickerRowLayout) UpdateRenderObject(_ ui.BuildContext, object ui.RenderObject) {
	render := object.(*renderPickerRowLayout)
	if render.Columns != w.Columns {
		render.Columns = w.Columns
		render.MarkNeedsLayout()
	}
}

type renderPickerRowLayout struct {
	ui.MultiChildRenderObject
	Columns pickerColumns
}

// pickerColumnEdges resolves column widths for a row width. Hint and metadata
// keep their widths up to a quarter of the row each. Without a hint or
// description, the label fills the space before trailing metadata; otherwise
// it keeps its measured width unless that would leave the description less
// than a third of the remainder.
type pickerColumnEdges struct {
	LabelX, LabelW, HintX, HintW, DescriptionX, DescriptionW, MetaX, MetaW int
}

func resolvePickerColumnEdges(columns pickerColumns, width int) pickerColumnEdges {
	edges := pickerColumnEdges{HintW: min(columns.Hint, width/4), MetaW: min(columns.Meta, width/4)}
	available := width
	if edges.HintW > 0 {
		available -= edges.HintW + pickerColumnGap
	}
	if edges.MetaW > 0 {
		available -= edges.MetaW + pickerColumnGap
	}
	available = max(0, available)
	if columns.Description {
		edges.LabelW = min(columns.Label, max(1, available-pickerColumnGap-available/3))
		edges.DescriptionW = max(0, available-edges.LabelW-pickerColumnGap)
	} else {
		edges.LabelW = min(columns.Label, available)
		if edges.HintW == 0 {
			edges.LabelW = available
		}
	}
	x := edges.LabelW + pickerColumnGap
	if edges.HintW > 0 {
		edges.HintX = x
		x += edges.HintW + pickerColumnGap
	}
	edges.DescriptionX = x
	edges.MetaX = width - edges.MetaW
	return edges
}

func (r *renderPickerRowLayout) Layout(ctx ui.LayoutContext, constraints ui.Constraints) {
	size := pickerDialogViewportSize(constraints)
	edges := resolvePickerColumnEdges(r.Columns, size.Width)
	slots := [][2]int{{edges.LabelX, edges.LabelW}, {edges.HintX, edges.HintW}, {edges.DescriptionX, edges.DescriptionW}, {edges.MetaX, edges.MetaW}}
	for index, child := range r.Children() {
		slot := [2]int{}
		if index < len(slots) {
			slot = slots[index]
		}
		child.Layout(ctx, ui.Tight(ui.Size{Width: slot[1], Height: min(1, size.Height)}))
		child.Base().SetParentData(pickerViewportParentData{Offset: ui.Offset{X: slot[0]}, Visible: slot[1] > 0})
	}
	r.SetSize(size)
}

func (r *renderPickerRowLayout) DryLayout(_ ui.LayoutContext, constraints ui.Constraints) ui.Size {
	return pickerDialogViewportSize(constraints)
}

func (r *renderPickerRowLayout) Paint(painter *ui.Painter, offset ui.Offset) {
	for _, child := range r.Children() {
		if data, _ := child.Base().ParentData().(pickerViewportParentData); data.Visible {
			child.Paint(painter, offset.Add(data.Offset))
		}
	}
}

func (r *renderPickerRowLayout) ChildOffset(child ui.RenderObject) ui.Offset {
	data, _ := child.Base().ParentData().(pickerViewportParentData)
	return data.Offset
}

func (*renderPickerRowLayout) HitTest(*ui.HitTestResult, ui.Point) bool { return false }

// pickerOverflowRow marks items hidden above or below the list. Clicking it
// scrolls a page toward them.
type pickerOverflowRow struct {
	OnPressed ui.VoidCallback
}

func (w pickerOverflowRow) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	return mouseActivator{OnPressed: w.OnPressed, Child: ui.Padding(ui.Insets{Left: 1, Right: 1}, ui.Text{
		Value: glyphEllipsis, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1,
	})}
}
