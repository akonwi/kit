package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const fileMentionMaxVisible = 10

type inlineMentionController struct {
	Open      bool
	Query     string
	Selection string
	Anchor    int
	QueryEnd  int
}

func (f *inlineMentionController) Close() {
	f.Open = false
	f.Query = ""
	f.Selection = ""
	f.Anchor = 0
	f.QueryEnd = 0
}

// Observe tracks an inline trigger/query after a contiguous composer edit. Mentions
// only start at the beginning of the draft or after whitespace.
func (f *inlineMentionController) Observe(previous, next string, pasted bool, trigger byte) (opened bool) {
	start, oldEnd, newEnd := changedRange(previous, next)
	if f.Open {
		if pasted || start < f.Anchor || oldEnd != f.QueryEnd {
			f.Close()
			return false
		}
		f.QueryEnd += newEnd - oldEnd
		if f.QueryEnd <= f.Anchor || f.QueryEnd > len(next) || next[f.Anchor] != trigger {
			f.Close()
			return false
		}
		query := next[f.Anchor+1 : f.QueryEnd]
		if strings.IndexFunc(query, func(r rune) bool { return r == rune(trigger) || unicode.IsSpace(r) }) >= 0 {
			f.Close()
			return false
		}
		f.Query = query
		return false
	}
	if pasted || newEnd-start != 1 || start >= len(next) || next[start] != trigger {
		return false
	}
	if start > 0 {
		before := next[:start]
		r, _ := utf8LastRune(before)
		if !isMentionBoundary(r) {
			return false
		}
	}
	f.Open = true
	f.Query = ""
	f.Anchor = start
	f.QueryEnd = newEnd
	f.Selection = ""
	return true
}

type fileMentionController inlineMentionController

func (f *fileMentionController) Close() { (*inlineMentionController)(f).Close() }
func (f *fileMentionController) Observe(previous, next string, pasted bool) bool {
	return (*inlineMentionController)(f).Observe(previous, next, pasted, '@')
}

func utf8LastRune(value string) (rune, int) { return utf8.DecodeLastRuneInString(value) }

func isMentionBoundary(r rune) bool { return unicode.IsSpace(r) }

func changedRange(previous, next string) (start, previousEnd, nextEnd int) {
	for start < len(previous) && start < len(next) && previous[start] == next[start] {
		start++
	}
	previousEnd, nextEnd = len(previous), len(next)
	for previousEnd > start && nextEnd > start && previous[previousEnd-1] == next[nextEnd-1] {
		previousEnd--
		nextEnd--
	}
	return start, previousEnd, nextEnd
}

func (f *fileMentionController) filtered(entries []protocol.FileIndexEntry) []protocol.FileIndexEntry {
	return ui.DefaultFuzzySelectFilter(f.Query, entries, func(entry protocol.FileIndexEntry) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: entry.Path}
	})
}

func firstFileMentionPath(entries []protocol.FileIndexEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[0].Path
}

func (f *fileMentionController) ensureSelection(entries []protocol.FileIndexEntry) {
	filtered := f.filtered(entries)
	for _, entry := range filtered {
		if entry.Path == f.Selection {
			return
		}
	}
	f.Selection = firstFileMentionPath(filtered)
}

func (f *fileMentionController) Move(entries []protocol.FileIndexEntry, delta int) {
	entries = f.filtered(entries)
	if len(entries) == 0 {
		return
	}
	index := 0
	for candidate, entry := range entries {
		if entry.Path == f.Selection {
			index = candidate
			break
		}
	}
	index = (index + delta) % len(entries)
	if index < 0 {
		index += len(entries)
	}
	f.Selection = entries[index].Path
}

func (f *fileMentionController) Selected(entries []protocol.FileIndexEntry) (protocol.FileIndexEntry, bool) {
	for _, entry := range f.filtered(entries) {
		if entry.Path == f.Selection {
			return entry, true
		}
	}
	return protocol.FileIndexEntry{}, false
}

func (f *fileMentionController) Insert(text string, entry protocol.FileIndexEntry) (string, int, bool) {
	if !f.Open || f.Anchor < 0 || f.QueryEnd > len(text) {
		return text, 0, false
	}
	token := "@" + entry.Path + " "
	next := text[:f.Anchor] + token + text[f.QueryEnd:]
	cursor := f.Anchor + len(token)
	f.Close()
	return next, cursor, true
}

func (f *fileMentionController) HandleKey(entries []protocol.FileIndexEntry, key ui.Key) (protocol.FileIndexEntry, bool, bool) {
	if !f.Open || key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
		return protocol.FileIndexEntry{}, false, false
	}
	switch {
	case key.MatchString("Up"):
		f.Move(entries, -1)
		return protocol.FileIndexEntry{}, false, true
	case key.MatchString("Down"):
		f.Move(entries, 1)
		return protocol.FileIndexEntry{}, false, true
	case key.MatchString("Enter"):
		entry, ok := f.Selected(entries)
		return entry, ok, true
	default:
		return protocol.FileIndexEntry{}, false, false
	}
}

type fileMentionSurface struct {
	Controller     *fileMentionController
	Source         indexedFileSource
	Composer       string
	BottomInset    int
	PrimaryPercent int
	OnSelect       func(ui.EventContext, string)
}

func (w fileMentionSurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	rowPresentation := resolvePickerRowPresentation(ctx, theme)
	entries := w.Controller.filtered(w.Source.Entries)
	selection := 0
	for index, entry := range entries {
		if entry.Path == w.Controller.Selection {
			selection = index
			break
		}
	}
	if len(entries) > fileMentionMaxVisible {
		offset := max(0, min(selection-fileMentionMaxVisible/2, len(entries)-fileMentionMaxVisible))
		entries = entries[offset : offset+fileMentionMaxVisible]
	}
	rows := make([]ui.Widget, 0, max(1, len(entries)))
	if len(entries) == 0 {
		label := "No results"
		if w.Source.Loading {
			label = "Loading…"
		} else if w.Source.Error != "" {
			label = "Could not load files: " + w.Source.Error
		}
		rows = append(rows, ui.Text{Value: label, Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	for _, entry := range entries {
		entry := entry
		selected := entry.Path == w.Controller.Selection
		style := ui.Style{Foreground: rowPresentation.ItemText}
		secondary := ui.Style{Foreground: theme.MutedForeground}
		if selected {
			style = ui.Style{Foreground: rowPresentation.FocusedText, Background: rowPresentation.FocusedBg}
			secondary = style
		}
		description := ""
		if entry.IsDir {
			description = "directory"
		}
		row := ui.DecoratedBox(ui.Decoration{Style: style}, ui.Flex{Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
			ui.Expanded(ui.Text{Value: entry.Path, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
			ui.SizedBox{Width: 1},
			ui.SizedBox{Width: 9, Child: ui.Text{Value: description, Style: secondary, MaxLines: 1}},
		}})
		rows = append(rows, mouseActivator{Child: ui.SizedBox{Height: 1, Child: row}, OnPressed: func(event ui.EventContext) {
			if w.OnSelect != nil {
				w.OnSelect(event, entry.Path)
			}
		}})
	}
	if w.Source.Error != "" && len(entries) > 0 {
		rows = append(rows, ui.Text{Value: "Refresh failed: " + w.Source.Error, Style: ui.Style{Foreground: theme.Warning}, MaxLines: 1})
	}
	if w.Source.Truncated {
		rows = append(rows, ui.Text{Value: "Showing first 4,000 indexed paths", Style: ui.Style{Foreground: theme.Warning}, MaxLines: 1})
	}
	content := ui.Padding(ui.All(1), ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: append(rows,
		ui.SizedBox{Height: 1},
		ui.Text{Value: "↑↓ move · enter insert · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
	)})
	anchor := w.Controller.Anchor
	return composerOverlayPositioner{BottomInset: w.BottomInset, PrimaryPercent: w.PrimaryPercent, Composer: w.Composer, Anchor: &anchor, Child: proportionalWidth{Percent: 80, Min: 48, Max: composerOverlayMaxWidth, Child: ui.DecoratedBox(
		ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}, Border: ui.BorderAll(ui.Style{Foreground: theme.Border, Background: theme.Background})}, content,
	)}}
}
