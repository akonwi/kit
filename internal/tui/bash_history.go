package tui

import (
	"strings"

	"go.rockorager.dev/vaxis/ui"
)

const (
	// bashHistoryInitialLimit bounds the first durable history read so opening
	// the picker costs one bounded request.
	bashHistoryInitialLimit = inlinePickerMaxRows
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

// bashHistoryController is the bash history inline picker. Entries are newest
// first; the picker lists them oldest first so the newest sits nearest the
// composer. The composer text after its leading "!" is the query.
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
	h.Entries = append([]bashHistoryEntry(nil), entries...)
	h.SetQuery(bashHistoryQuery(composer))
	// The first durable page is in flight, so an empty list is not yet an
	// answer and must not be presented as one.
	h.Loading = true
	h.OnExhausted = nil
	return true
}

func (h *bashHistoryController) Close() { *h = bashHistoryController{} }

// bashHistoryQuery is the composer text after its "!" or "!!" prefix, so
// "!git" filters by "git".
func bashHistoryQuery(composer string) string {
	return strings.TrimLeft(strings.TrimLeft(composer, "!"), " \t")
}

// ObserveComposer follows a composer edit: the picker closes when the text
// leaves bash mode and otherwise filters by the command typed after "!".
func (h *bashHistoryController) ObserveComposer(composer string) {
	if !h.Open {
		return
	}
	if !strings.HasPrefix(composer, "!") {
		h.Close()
		return
	}
	if query := bashHistoryQuery(composer); query != h.Query {
		h.SetQuery(query)
	}
}

// atOldest reports whether the highlight is on the oldest loaded match, or
// nothing matches, while older durable history remains within the page
// bound. Up there loads older entries instead of wrapping.
func (h *bashHistoryController) atOldest() bool {
	if !h.Open || !h.HasMore || h.PagesLoaded >= bashHistoryMaxPages {
		return false
	}
	items := h.keys().Items(h.catalog())
	return len(items) == 0 || items[0].Key == h.Selection
}

// Exhausted reports whether navigation reached the oldest matching loaded
// entry (or found no matches) while older durable history remains and can be
// requested now.
func (h *bashHistoryController) Exhausted() bool {
	return !h.Loading && h.OnExhausted != nil && h.atOldest()
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
		h.Selection = h.newest()
	}
}

// SetQuery filters by query and highlights the newest match.
func (h *bashHistoryController) SetQuery(query string) {
	h.Query = query
	h.Selection = h.newest()
}

// newest is the key of the newest entry matching the query.
func (h *bashHistoryController) newest() string {
	return lastPickerKey(h.keys().Items(h.catalog()))
}

// catalog lists entries oldest first, as the picker shows them. Each row is
// the composer text it inserts, so "!!" marks output excluded from context;
// the command is matched.
func (h *bashHistoryController) catalog() []pickerItem {
	items := make([]pickerItem, 0, len(h.Entries))
	for index := len(h.Entries) - 1; index >= 0; index-- {
		entry := h.Entries[index]
		items = append(items, pickerItem{Key: entry.ID, Label: bashHistoryComposerText(entry), SearchText: entry.Command})
	}
	return items
}

// keys returns the picker's navigation key model.
func (h *bashHistoryController) keys() pickerKeyModel {
	return pickerKeyModel{Query: h.Query, Selection: h.Selection, Filter: filterPickerItemsInOrder}
}

// Selected returns the highlighted entry when it matches the query.
func (h *bashHistoryController) Selected() (bashHistoryEntry, bool) {
	if pickerItemIndex(h.keys().Items(h.catalog()), h.Selection) < 0 {
		return bashHistoryEntry{}, false
	}
	for _, entry := range h.Entries {
		if entry.ID == h.Selection {
			return entry, true
		}
	}
	return bashHistoryEntry{}, false
}

// HandleKey applies one key through the navigation-only picker key model.
// Up from the oldest loaded match requests the next older page instead of
// wrapping. Unhandled keys belong to the composer, which owns the query.
func (h *bashHistoryController) HandleKey(key ui.Key) pickerKeyResult {
	if !h.Open {
		return pickerKeyResult{}
	}
	atOldest, exhausted := h.atOldest(), h.Exhausted()
	model := h.keys()
	result := model.HandleNavigationKey(key, h.catalog())
	if result.Moved < 0 && atOldest {
		if request := h.OnExhausted; exhausted && request != nil {
			request()
		}
		return result
	}
	h.Selection = model.Selection
	return result
}

// bashHistorySurface maps bash history onto the inline picker.
type bashHistorySurface struct {
	Controller bashHistoryController
	Anchor     func(ui.Size) ui.Point
	OnSelect   func(ui.EventContext, string)
}

func (w bashHistorySurface) Build(ui.BuildContext) ui.Widget {
	model := w.Controller.keys()
	catalog := w.Controller.catalog()
	picker := inlinePicker{
		Query: model.Query, Catalog: catalog, Filter: model.Filter, Selection: model.Selection,
		OnActivate: w.OnSelect,
		Anchor:     w.Anchor,
	}
	// The first durable page is in flight, so an empty list is not yet an
	// answer and must not be presented as one.
	if w.Controller.Loading && len(model.Items(catalog)) == 0 {
		picker.Message, picker.MessageTone = "Loading history…", pickerToneLoading
	}
	return picker
}
