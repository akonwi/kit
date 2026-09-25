package tui

import (
	"fmt"
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

const maxEnrichedPresentationLines = 2000

type codePresentation struct {
	Path   string
	Lines  []string
	Notice string
}

func (w codePresentation) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	children := []ui.Widget{ui.Text{
		Value: strings.ReplaceAll(strings.Join(w.Lines, "\n"), "\t", "  "),
		Style: ui.Style{Foreground: theme.Foreground}, SoftWrap: true,
	}}
	if w.Notice != "" {
		children = append(children, ui.Text{
			Value: w.Notice, Style: ui.Style{Foreground: theme.MutedForeground}, SoftWrap: true,
		})
	}
	return ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
	}
}

type unifiedDiffPresentation struct {
	Lines []activityDiffLine
}

func (w unifiedDiffPresentation) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	children := make([]ui.Widget, 0)
	for start := 0; start < len(w.Lines); {
		end := start + 1
		for end < len(w.Lines) && w.Lines[end].Kind == w.Lines[start].Kind {
			end++
		}
		kind := w.Lines[start].Kind
		style := ui.Style{Foreground: theme.MutedForeground}
		if kind == activityDiffDelete {
			style.Foreground = theme.DangerText
		} else if kind == activityDiffAdd {
			style.Foreground = theme.SuccessText
		}
		rows := make([]string, end-start)
		for index, line := range w.Lines[start:end] {
			prefix := "  "
			if kind == activityDiffDelete {
				prefix = "- "
			} else if kind == activityDiffAdd {
				prefix = "+ "
			}
			rows[index] = prefix + strings.ReplaceAll(line.Text, "\t", "  ")
		}
		children = append(children, ui.Text{
			Value: strings.Join(rows, "\n"), Style: style, SoftWrap: true,
		})
		start = end
	}
	return ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin,
		CrossAxisAlignment: ui.CrossAxisStretch, Children: children,
	}
}

func activityEnrichedPresentation(detail activityEnrichment) (ui.Widget, string, string, bool) {
	switch detail.Kind {
	case activityEnrichmentFile:
		lines := splitActivityLines(detail.Content)
		display, omitted := boundedPresentationLines(lines)
		if omitted > 0 {
			display = append(display, fmt.Sprintf("… %d more lines", omitted))
		}
		content := strings.Join(display, "\n")
		if detail.Notice != "" {
			content += "\n" + detail.Notice
		}
		lineCount := detail.LineCount
		if lineCount <= 0 {
			lineCount = len(lines)
		}
		return codePresentation{Path: detail.Path, Lines: display, Notice: detail.Notice}, content, activityLineMetadata(lineCount), true
	case activityEnrichmentEdits:
		lines := make([]activityDiffLine, 0)
		remainingCells := maxActivityDiffCells
		remainingEdits := 0
		for index, edit := range detail.Edits {
			cost, ok := activityDiffCellCost(edit)
			if !ok || cost > remainingCells {
				return nil, "", "", false
			}
			remainingCells -= cost
			diff, ok := buildActivityDiff(edit.OldText, edit.NewText)
			if !ok {
				return nil, "", "", false
			}
			if index > 0 {
				lines = append(lines, activityDiffLine{Kind: activityDiffContext, Text: glyphEllipsis})
			}
			lines = append(lines, diff...)
			if len(lines) > maxEnrichedPresentationLines {
				remainingEdits = len(detail.Edits) - index - 1
				break
			}
		}
		display, omitted := boundedPresentationLines(lines)
		if omitted > 0 || remainingEdits > 0 {
			note := fmt.Sprintf("… %d more lines", omitted)
			if remainingEdits > 0 {
				note += fmt.Sprintf(" across %d more edits", remainingEdits)
			}
			display = append(display, activityDiffLine{Kind: activityDiffContext, Text: note})
		}
		measurement := activityDiffText(display)
		return unifiedDiffPresentation{Lines: display}, measurement, activityEditMetadata(len(detail.Edits)), true
	default:
		return nil, "", "", false
	}
}

func activityDiffText(lines []activityDiffLine) string {
	rows := make([]string, len(lines))
	for index, line := range lines {
		prefix := "  "
		if line.Kind == activityDiffDelete {
			prefix = "- "
		} else if line.Kind == activityDiffAdd {
			prefix = "+ "
		}
		rows[index] = prefix + strings.ReplaceAll(line.Text, "\t", "  ")
	}
	return strings.Join(rows, "\n")
}

func activityLineMetadata(lines int) string {
	if lines == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", lines)
}

func activityEditMetadata(edits int) string {
	if edits == 1 {
		return "1 edit"
	}
	return fmt.Sprintf("%d edits", edits)
}

func activityEnrichmentUnavailable(detail activityEnrichment) string {
	if detail.Kind == activityEnrichmentEdits {
		return "diff too large to display " + glyphMiddleDot + " " + activityEditMetadata(len(detail.Edits))
	}
	return "enriched output unavailable"
}

func boundedPresentationLines[T any](lines []T) ([]T, int) {
	if len(lines) <= maxEnrichedPresentationLines {
		return lines, 0
	}
	return lines[:maxEnrichedPresentationLines], len(lines) - maxEnrichedPresentationLines
}
