package tui

import (
	"fmt"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

const annotationPickerWindow = 10

type annotationPickerSnapshot struct {
	Open        bool
	Selection   int
	Annotations []protocol.AnnotationSummary
}

type annotationPickerController struct {
	Open      bool
	Selection int
}

func (c *annotationPickerController) Begin() { c.Open = true; c.Selection = 0 }
func (c *annotationPickerController) Close() { c.Open = false; c.Selection = 0 }
func (c *annotationPickerController) Move(delta, count int) {
	if !c.Open || count == 0 {
		return
	}
	c.Selection = max(0, min(count-1, c.Selection+delta))
}

type annotationPickerCallbacks struct {
	Activate func(ui.EventContext, protocol.AnnotationSummary)
	Remove   func(ui.EventContext, uint64)
}

type annotationPickerSurface struct {
	Snapshot  annotationPickerSnapshot
	Callbacks annotationPickerCallbacks
}

func (w annotationPickerSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	start := max(0, w.Snapshot.Selection-annotationPickerWindow/2)
	end := min(len(w.Snapshot.Annotations), start+annotationPickerWindow)
	start = max(0, end-annotationPickerWindow)
	rows := make([]ui.Widget, 0, end-start)
	for index := start; index < end; index++ {
		annotation := w.Snapshot.Annotations[index]
		anchor := annotation.Anchor.WorkspaceFile
		label := "Annotation"
		if anchor != nil {
			label = fmt.Sprintf("%s  L%d–%d", anchor.Path, anchor.StartLine, anchor.EndLine)
		}
		style := ui.Style{Foreground: theme.Foreground}
		if index == w.Snapshot.Selection {
			style.Background = theme.SurfaceHovered
		}
		if annotation.Stale {
			style.Foreground = theme.WarningText
		}
		remove := ui.Widget(ui.Text{Value: glyphTimes, Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1})
		if w.Callbacks.Remove != nil {
			remove = mouseActivator{Child: remove, OnPressed: func(event ui.EventContext) { w.Callbacks.Remove(event, annotation.ID) }}
		}
		labelWidget := ui.Widget(ui.Text{Value: label, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1})
		if w.Callbacks.Activate != nil {
			labelWidget = mouseActivator{Child: labelWidget, OnPressed: func(event ui.EventContext) { w.Callbacks.Activate(event, annotation) }}
		}
		rows = append(rows, ui.SizedBox{Height: 1, Child: ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
			ui.Expanded(labelWidget), remove,
		}}})
	}
	bodyChildren := append([]ui.Widget(nil), rows...)
	if w.Snapshot.Selection < len(w.Snapshot.Annotations) {
		selected := w.Snapshot.Annotations[w.Snapshot.Selection]
		if selected.Stale {
			bodyChildren = append(bodyChildren,
				ui.SizedBox{Height: 1},
				ui.Text{Value: "Frozen evidence", Style: ui.Style{Foreground: theme.WarningText}, MaxLines: 1},
				ui.Text{Value: selected.Preview, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true, MaxLines: 3},
				ui.Text{Value: selected.BodyPreview, Style: ui.Style{Foreground: theme.Foreground}, SoftWrap: true, MaxLines: 3},
			)
		}
	}
	body := ui.Widget(ui.Flex{Axis: ui.Vertical, CrossAxisAlignment: ui.CrossAxisStretch, MainAxisSize: ui.MainAxisSizeMin, Children: bodyChildren})
	if len(rows) == 0 {
		body = ui.Text{Value: "No live annotations", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1}
	}
	footer := ui.Text{Value: "↑↓ select · enter reveal · r re-anchor · delete remove · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, MaxLines: 1, Overflow: ui.TextOverflowEllipsis}
	return dialogSurface(theme, "Annotations", fmt.Sprintf("%d live", len(w.Snapshot.Annotations)), body, footer, true)
}
