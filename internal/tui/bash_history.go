package tui

import (
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const bashHistoryMaxVisible = 10

const (
	// bashHistoryInitialLimit bounds the first durable history read so opening
	// the picker costs one bounded request.
	bashHistoryInitialLimit = bashHistoryMaxVisible
	// bashHistoryPageLimit bounds each subsequent older-entry read.
	bashHistoryPageLimit = 100
	// bashHistoryMaxPages bounds total older-entry loads while the picker is
	// open so navigation cannot page indefinitely.
	bashHistoryMaxPages = 5
)

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
	// HasMore reports that durable history holds older entries beyond Entries.
	HasMore bool
	// OlderBefore is the exclusive cursor for the next older read, or zero
	// when no further read is available.
	OlderBefore uint64
	// PagesLoaded counts older-entry reads performed while open.
	PagesLoaded int
	// Loading reports that an older-entry read is in flight.
	Loading bool
	// OnExhausted requests the next older page when navigation reaches the
	// oldest loaded entry.
	OnExhausted func()
}

// OpenFor admits the picker on the composer alone. Durable history is the
// source of truth for entries, so an empty loaded projection must not prevent
// the picker from opening: after a reload or restart, remembered executions
// exist only in session history, never in the loaded transcript.
func (h *bashHistoryController) OpenFor(entries []bashHistoryEntry, composer string) bool {
	if h.Open || !strings.HasPrefix(composer, "!") {
		return false
	}
	h.Open = true
	h.Query = strings.TrimLeft(strings.TrimLeft(composer, "!"), " \t")
	h.Entries = append([]bashHistoryEntry(nil), entries...)
	h.Selection = firstBashHistoryID(h.filtered())
	// The first durable page is in flight, so an empty list is not yet an
	// answer and must not be presented as one.
	h.Loading = true
	h.OnExhausted = nil
	return true
}

func (h *bashHistoryController) Close() { *h = bashHistoryController{} }

// Exhausted reports whether navigation reached the oldest matching loaded
// entry (or found no matches) while older durable history remains.
func (h *bashHistoryController) Exhausted() bool {
	if !h.Open || h.Loading || !h.HasMore || h.OnExhausted == nil {
		return false
	}
	if h.PagesLoaded >= bashHistoryMaxPages {
		return false
	}
	entries := h.filtered()
	return len(entries) == 0 || entries[len(entries)-1].ID == h.Selection
}

// MergeOlder appends an older page while preserving the current query and
// selection. The older page is older, so the selection does not move.
func (h *bashHistoryController) MergeOlder(entries []bashHistoryEntry, before uint64, hasMore bool) {
	if !h.Open {
		return
	}
	known := make(map[string]struct{}, len(h.Entries))
	for _, entry := range h.Entries {
		known[entry.ID] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := known[entry.ID]; ok {
			continue
		}
		h.Entries = append(h.Entries, entry)
		known[entry.ID] = struct{}{}
	}
	h.HasMore, h.OlderBefore, h.PagesLoaded, h.Loading = hasMore, before, h.PagesLoaded+1, false
	if h.Selection == "" {
		h.Selection = firstBashHistoryID(h.filtered())
	}
}

func (h *bashHistoryController) SetQuery(query string) {
	h.Query = query
	h.Selection = firstBashHistoryID(h.filtered())
}

func (h *bashHistoryController) Move(delta int) {
	if !h.Open {
		return
	}
	// Up moves toward older entries; request the next page at the oldest row
	// or when no loaded entry matches the current query.
	if h.Exhausted() && delta < 0 {
		if request := h.OnExhausted; request != nil {
			request()
		}
		return
	}
	entries := h.filtered()
	if len(entries) == 0 {
		return
	}
	index := 0
	for candidate, entry := range entries {
		if entry.ID == h.Selection {
			index = candidate
			break
		}
	}
	index = max(0, min(index-delta, len(entries)-1))
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
	matches := ui.DefaultFuzzySelectFilter(h.Query, h.Entries, func(entry bashHistoryEntry) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: entry.Command}
	})
	// Keep fuzzy matching, but present matches chronologically rather than by
	// score so the newest match remains nearest the composer.
	if h.Query == "" {
		return matches
	}
	matched := make(map[string]bool, len(matches))
	for _, entry := range matches {
		matched[entry.ID] = true
	}
	ordered := make([]bashHistoryEntry, 0, len(matches))
	for _, entry := range h.Entries {
		if matched[entry.ID] {
			ordered = append(ordered, entry)
		}
	}
	return ordered
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
	Composer       string
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
		label := "No results"
		if w.Controller.Loading {
			label = "Loading history…"
		}
		rows = append(rows, ui.Text{Value: label, Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
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
	anchor := strings.Index(w.Composer, "!")
	if anchor < 0 {
		anchor = 0
	}
	return composerOverlayPositioner{
		BottomInset: w.BottomInset, PrimaryPercent: w.PrimaryPercent, Composer: w.Composer, Anchor: &anchor,
		Child: proportionalWidth{Percent: 80, Min: 48, Max: composerOverlayMaxWidth, Child: ui.FocusScope{
			Trap: true, AutoFocus: true, Child: ui.DecoratedBox(
				ui.Decoration{Style: ui.Style{Foreground: theme.Foreground, Background: theme.Background}, Border: ui.BorderAll(ui.Style{Foreground: theme.Border, Background: theme.Background})},
				content,
			),
		}},
	}
}
