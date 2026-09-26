package tui

import (
	"context"
	"strconv"
	"strings"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/ui"
)

const (
	messageHistoryMaxVisible = 10
	messageHistoryPageLimit  = 100
	messageHistoryMaxPages   = 5
)

type messageHistoryEntry struct {
	ID   string
	Text string
}

type messageHistoryController struct {
	Open      bool
	Query     string
	Selection string
	Entries   []messageHistoryEntry
}

func (h *messageHistoryController) OpenFor(entries []messageHistoryEntry) bool {
	if h.Open || len(entries) == 0 {
		return false
	}
	h.Open = true
	h.Query = ""
	h.Entries = append([]messageHistoryEntry(nil), entries...)
	h.Selection = firstMessageHistoryID(h.filtered())
	return true
}

func (h *messageHistoryController) Close() { *h = messageHistoryController{} }

func (h *messageHistoryController) SetQuery(query string) {
	h.Query = query
	h.Selection = firstMessageHistoryID(h.filtered())
}

func (h *messageHistoryController) Move(delta int) {
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
	index = max(0, min(index-delta, len(entries)-1))
	h.Selection = entries[index].ID
}

func (h *messageHistoryController) Selected() (messageHistoryEntry, bool) {
	for _, entry := range h.filtered() {
		if entry.ID == h.Selection {
			return entry, true
		}
	}
	return messageHistoryEntry{}, false
}

func (h *messageHistoryController) filtered() []messageHistoryEntry {
	matches := ui.DefaultFuzzySelectFilter(h.Query, h.Entries, func(entry messageHistoryEntry) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: oneLine(entry.Text)}
	})
	// Preserve fuzzy matching without letting relevance scores change the
	// chronological order of matching history rows.
	if h.Query == "" {
		return matches
	}
	matched := make(map[string]bool, len(matches))
	for _, entry := range matches {
		matched[entry.ID] = true
	}
	ordered := make([]messageHistoryEntry, 0, len(matches))
	for _, entry := range h.Entries {
		if matched[entry.ID] {
			ordered = append(ordered, entry)
		}
	}
	return ordered
}

func (h *messageHistoryController) HandleKey(key ui.Key) (messageHistoryEntry, bool, bool) {
	if !h.Open || key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
		return messageHistoryEntry{}, false, false
	}
	switch {
	case key.MatchString("Up"):
		h.Move(-1)
		return messageHistoryEntry{}, false, true
	case key.MatchString("Down"):
		h.Move(1)
		return messageHistoryEntry{}, false, true
	case key.MatchString("Enter"):
		entry, ok := h.Selected()
		return entry, ok, true
	default:
		return messageHistoryEntry{}, false, false
	}
}

func (h *messageHistoryController) HandleEditorKey(key ui.Key) bool {
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

func firstMessageHistoryID(entries []messageHistoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[0].ID
}

func messageHistoryEntries(messages []protocol.TranscriptMessage) []messageHistoryEntry {
	entries := make([]messageHistoryEntry, 0, len(messages))
	seen := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(message.TextContent())
		if text == "" {
			continue
		}
		if _, duplicate := seen[text]; duplicate {
			continue
		}
		seen[text] = struct{}{}
		entries = append(entries, messageHistoryEntry{ID: message.ID, Text: text})
	}
	return entries
}

func loadMessageHistory(ctx context.Context, pager sessionclient.MessagePager) ([]messageHistoryEntry, error) {
	var messages []protocol.TranscriptMessage
	var before uint64
	for pageIndex := 0; pageIndex < messageHistoryMaxPages; pageIndex++ {
		query := protocol.MessagePageQuery{Limit: messageHistoryPageLimit, Roles: []string{"user"}}
		if before != 0 {
			query.Before = before
		}
		page, err := pager.MessagePage(ctx, query)
		if err != nil {
			return nil, err
		}
		messages = append(messages, page.Messages...)
		if !page.HasMore || page.NextCursor == "" {
			break
		}
		cursor, err := strconv.ParseUint(page.NextCursor, 10, 64)
		if err != nil || cursor == 0 || cursor == before {
			break
		}
		before = cursor
	}
	return messageHistoryEntries(messages), nil
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

type messageHistorySurface struct {
	Controller     *messageHistoryController
	Composer       string
	BottomInset    int
	PrimaryPercent int
	OnQuery        ui.TextChangedCallback
	OnSelect       func(ui.EventContext, string)
}

func (w messageHistorySurface) Build(ctx ui.BuildContext) ui.Widget {
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
	if len(entries) > messageHistoryMaxVisible {
		offset := max(0, min(selection-messageHistoryMaxVisible/2, len(entries)-messageHistoryMaxVisible))
		entries = entries[offset : offset+messageHistoryMaxVisible]
	}
	rows := make([]ui.Widget, 0, max(1, len(entries)))
	if len(entries) == 0 {
		rows = append(rows, ui.Text{Value: "No results", Style: ui.Style{Foreground: theme.MutedForeground}})
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		selected := entry.ID == w.Controller.Selection
		style := ui.Style{Foreground: rowPresentation.ItemText}
		if selected {
			style = ui.Style{Foreground: rowPresentation.FocusedText, Background: rowPresentation.FocusedBg}
		}
		row := ui.DecoratedBox(ui.Decoration{Style: style}, ui.Padding(ui.Symmetric(1, 0), ui.Text{
			Value: oneLine(entry.Text), Style: style, Overflow: ui.TextOverflowEllipsis, MaxLines: 1,
		}))
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
			ui.Text{Value: "Message history", Style: ui.Style{Foreground: theme.Foreground}, MaxLines: 1},
			ui.SizedBox{Height: 1},
			ui.Flex{Axis: ui.Horizontal, Children: []ui.Widget{
				ui.Text{Value: ">", Style: ui.Style{Foreground: theme.SuccessText}},
				ui.SizedBox{Width: 1},
				textInput(fieldTheme, textInputConfig{
					Value: w.Controller.Query, Placeholder: "Search message history…", CursorOffset: &cursor,
					OnChanged: w.OnQuery, AutoFocus: true,
				}),
			}},
			ui.SizedBox{Height: 1},
			ui.Flex{Axis: ui.Vertical, MainAxisSize: ui.MainAxisSizeMin, CrossAxisAlignment: ui.CrossAxisStretch, Children: rows},
			ui.SizedBox{Height: 1},
			ui.Text{Value: "↑↓ move · enter insert · esc close", Style: ui.Style{Foreground: theme.MutedForeground}, Overflow: ui.TextOverflowEllipsis, MaxLines: 1},
		},
	})
	anchor := 0
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
