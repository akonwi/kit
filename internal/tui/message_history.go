package tui

import (
	"context"
	"strconv"
	"strings"

	protocol "github.com/akonwi/kit/api/contract"
	"go.rockorager.dev/vaxis/ui"
)

const (
	messageHistoryPageLimit = 100
	messageHistoryMaxPages  = 5
)

type messageHistoryEntry struct {
	ID   string
	Text string
}

// messageHistoryController is the message history inline picker. Entries are
// newest first; the picker lists them oldest first so the newest sits nearest
// the composer. The composer text is the query.
type messageHistoryController struct {
	Open      bool
	Query     string
	Selection string
	Entries   []messageHistoryEntry
}

// OpenFor opens the picker on the loaded entries, filtered by the composer
// text, with the newest match highlighted.
func (h *messageHistoryController) OpenFor(entries []messageHistoryEntry, composer string) bool {
	if h.Open || len(entries) == 0 {
		return false
	}
	h.Open = true
	h.Entries = append([]messageHistoryEntry(nil), entries...)
	h.SetQuery(composer)
	return true
}

func (h *messageHistoryController) Close() { *h = messageHistoryController{} }

// SetQuery follows the composer text and highlights the newest match.
func (h *messageHistoryController) SetQuery(query string) {
	h.Query = query
	h.Selection = lastPickerKey(h.keys().Items(h.catalog()))
}

// catalog lists entries oldest first, as the picker shows them.
func (h *messageHistoryController) catalog() []pickerItem {
	items := make([]pickerItem, 0, len(h.Entries))
	for index := len(h.Entries) - 1; index >= 0; index-- {
		entry := h.Entries[index]
		items = append(items, pickerItem{Key: entry.ID, Label: oneLine(entry.Text)})
	}
	return items
}

// keys returns the picker's navigation key model.
func (h *messageHistoryController) keys() pickerKeyModel {
	return pickerKeyModel{Query: h.Query, Selection: h.Selection, Filter: filterPickerItemsInOrder}
}

// Selected returns the highlighted entry when it matches the query.
func (h *messageHistoryController) Selected() (messageHistoryEntry, bool) {
	if pickerItemIndex(h.keys().Items(h.catalog()), h.Selection) < 0 {
		return messageHistoryEntry{}, false
	}
	for _, entry := range h.Entries {
		if entry.ID == h.Selection {
			return entry, true
		}
	}
	return messageHistoryEntry{}, false
}

// HandleKey applies one key through the navigation-only picker key model.
// Unhandled keys belong to the composer, which owns the query.
func (h *messageHistoryController) HandleKey(key ui.Key) pickerKeyResult {
	if !h.Open {
		return pickerKeyResult{}
	}
	model := h.keys()
	result := model.HandleNavigationKey(key, h.catalog())
	h.Selection = model.Selection
	return result
}

func messageHistoryEntries(messages []protocol.TranscriptMessage) []messageHistoryEntry {
	entries := make([]messageHistoryEntry, 0, len(messages))
	seen := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(userMessageText(message))
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

func loadMessageHistory(ctx context.Context, pager MessagePager) ([]messageHistoryEntry, error) {
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

// messageHistorySurface maps message history onto the inline picker.
type messageHistorySurface struct {
	Controller messageHistoryController
	Anchor     func(ui.Size) ui.Point
	OnSelect   func(ui.EventContext, string)
}

func (w messageHistorySurface) Build(ui.BuildContext) ui.Widget {
	model := w.Controller.keys()
	return inlinePicker{
		Query: model.Query, Catalog: w.Controller.catalog(), Filter: model.Filter, Selection: model.Selection,
		OnActivate: w.OnSelect,
		Anchor:     w.Anchor,
	}
}

// userMessageText is the visible and recalled text of a user message: its text
// blocks and a prompt command's invocation, in content order. Submitting a
// recalled invocation runs the command again with its current template.
func userMessageText(message protocol.TranscriptMessage) string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		switch value := block.Payload.(type) {
		case protocol.TextContent:
			if value.Text != "" {
				parts = append(parts, value.Text)
			}
		case protocol.PromptCommandContent:
			parts = append(parts, value.InvocationText())
		}
	}
	return strings.Join(parts, "\n")
}
