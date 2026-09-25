package tui

import (
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const bashHistoryMaxVisible = 10

type bashHistoryEntry struct {
	ID                 string
	Command            string
	ExcludeFromContext bool
}

type bashHistoryController struct {
	Open      bool
	Query     string
	Selection string
	Entries   []bashHistoryEntry
}

func (h *bashHistoryController) OpenFor(entries []bashHistoryEntry, composer string) bool {
	if h.Open || len(entries) == 0 || !strings.HasPrefix(composer, "!") {
		return false
	}
	h.Open = true
	h.Query = strings.TrimLeft(strings.TrimLeft(composer, "!"), " \t")
	h.Entries = append([]bashHistoryEntry(nil), entries...)
	h.Selection = firstBashHistoryID(h.filtered())
	return true
}

func (h *bashHistoryController) Close() { *h = bashHistoryController{} }

func (h *bashHistoryController) SetQuery(query string) {
	h.Query = query
	h.Selection = firstBashHistoryID(h.filtered())
}

func (h *bashHistoryController) Move(delta int) {
	entries := h.filtered()
	if !h.Open || len(entries) == 0 {
		return
	}
	index := 0
	for candidate, entry := range entries {
		if entry.ID == h.Selection {
			index = candidate
			break
		}
	}
	index = (index + delta) % len(entries)
	if index < 0 {
		index += len(entries)
	}
	h.Selection = entries[index].ID
}

func (h *bashHistoryController) Selected() (bashHistoryEntry, bool) {
	for _, entry := range h.filtered() {
		if entry.ID == h.Selection {
			return entry, true
		}
	}
	return bashHistoryEntry{}, false
}

func (h *bashHistoryController) filtered() []bashHistoryEntry {
	return ui.DefaultFuzzySelectFilter(h.Query, h.Entries, func(entry bashHistoryEntry) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: entry.Command}
	})
}

func (h *bashHistoryController) HandleKey(key ui.Key) (bashHistoryEntry, bool, bool) {
	if !h.Open || key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
		return bashHistoryEntry{}, false, false
	}
	switch {
	case key.MatchString("Up"):
		h.Move(-1)
		return bashHistoryEntry{}, false, true
	case key.MatchString("Down"):
		h.Move(1)
		return bashHistoryEntry{}, false, true
	case key.MatchString("Enter"):
		entry, ok := h.Selected()
		return entry, ok, true
	default:
		return bashHistoryEntry{}, false, false
	}
}

func (h *bashHistoryController) HandleEditorKey(key ui.Key) bool {
	if !h.Open || key.EventType == ui.EventRelease {
		return false
	}
	query := h.Query
	if key.EventType == vaxis.EventPaste {
		query += palettePasteText(key)
		h.SetQuery(query)
		return true
	}
	modifiers := key.Modifiers &^ (vaxis.ModShift | vaxis.ModCapsLock | vaxis.ModNumLock)
	if modifiers != 0 {
		return false
	}
	switch {
	case key.MatchString("Backspace"):
		runes := []rune(query)
		if len(runes) > 0 {
			query = string(runes[:len(runes)-1])
		}
	case key.Text != "":
		query += key.Text
	default:
		return true
	}
	h.SetQuery(query)
	return true
}

func firstBashHistoryID(entries []bashHistoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[0].ID
}

type bashHistorySurface struct {
	Controller     *bashHistoryController
	BottomInset    int
	PrimaryPercent int
	OnQuery        ui.TextChangedCallback
	OnSelect       func(ui.EventContext, string)
}

func (w bashHistorySurface) Build(ctx ui.BuildContext) ui.Widget {
	theme := ui.MustDepend[ui.Theme](ctx)
	rowPresentation := resolvePickerRowPresentation(ctx, theme)
	entries := w.Controller.filtered()
	selection := 0
	for index, entry := range entries {
		if entry.ID == w.Controller.Selection {
			selection = index
			break
		}
	}
	if len(entries) > bashHistoryMaxVisible {
		offset := max(0, min(selection-bashHistoryMaxVisible/2, len(entries)-bashHistoryMaxVisible))
		entries = entries[offset : offset+bashHistoryMaxVisible]
	}
	rows := make([]ui.Widget, 0, max(1, len(entries)))
	if len(entries) == 0 {
		rows = append(rows, ui.Text{Value: "No results", Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	for _, entry := range entries {
		entry := entry
		prefix := "!"
		description := "included in context"
		if entry.ExcludeFromContext {
			prefix = "!!"
			description = "excluded from context"
		}
		selected := entry.ID == w.Controller.Selection
		style := ui.Style{Foreground: rowPresentation.ItemText}
		secondary := ui.Style{Foreground: theme.MutedForeground}
		if selected {
			style = ui.Style{Foreground: rowPresentation.FocusedText, Background: rowPresentation.FocusedBg}
			secondary = style
		}
		row := ui.DecoratedBox(ui.Decoration{Style: style}, ui.Flex{
			Axis: ui.Horizontal, CrossAxisAlignment: ui.CrossAxisStretch, Children: []ui.Widget{
				ui.SizedBox{Width: 2, Child: ui.Text{Value: prefix, Style: style, MaxLines: 1}},
				ui.SizedBox{Width: 1},
				ui.Expanded(ui.Text{Value: entry.Command, Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}),
				ui.SizedBox{Width: 1},
				ui.SizedBox{Width: 21, Child: ui.Text{Value: description, Style: secondary, Overflow: ui.TextOverflowEllipsis, MaxLines: 1}},
			},
		})
		rows = append(rows, mouseActivator{Child: ui.SizedBox{Height: 1, Child: row}, OnPressed: func(event ui.EventContext) {
			if w.OnSelect != nil {
				w.OnSelect(event, entry.ID)
			}
		}})
	}
	fieldTheme := theme
	fieldTheme.Surface = theme.Background
	fieldTheme.SurfaceHovered = theme.Background
	cursor := len(w.Controller.Query)
	content := ui.Padding(ui.All(1), ui.Flex{
		Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch,
		Children: []ui.Widget{
			ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
				ui.Text{Value: ">", Style: ui.Style{Foreground: theme.SuccessText}},
				ui.SizedBox{Width: 1},
				textInput(fieldTheme, textInputConfig{
					Value: w.Controller.Query, Placeholder: "Search bash history…", CursorOffset: &cursor,
					OnChanged: w.OnQuery, AutoFocus: true,
				}),
			}},
			ui.SizedBox{Height: 1},
			ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows},
			ui.SizedBox{Height: 1},
			ui.Text{Value: "↑↓ move · enter insert · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		},
	})
	return composerOverlayPositioner{
		BottomInset: w.BottomInset, PrimaryPercent: w.PrimaryPercent,
		Child: proportionalWidth{Percent: 80, Min: 48, Max: composerOverlayMaxWidth, Child: ui.FocusScope{
			Trap: true, AutoFocus: true, Child: ui.DecoratedBox(
				ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}, Border: ui.BorderAll(ui.Style{Foreground: theme.Border, Background: theme.Background})},
				content,
			),
		}},
	}
}
