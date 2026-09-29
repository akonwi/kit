package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

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

// fileMentionCatalog maps indexed files onto inline picker rows labelled with
// their full paths; directory paths end in "/". The path is also the row key.
func fileMentionCatalog(entries []protocol.FileIndexEntry) []pickerItem {
	items := make([]pickerItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, pickerItem{Key: entry.Path, Label: entry.Path})
	}
	return items
}

// keys returns the mention's navigation key model.
func (f *fileMentionController) keys() pickerKeyModel {
	return pickerKeyModel{Query: f.Query, Selection: f.Selection}
}

// ensureSelection keeps a visible selection, or highlights the first match.
func (f *fileMentionController) ensureSelection(entries []protocol.FileIndexEntry) {
	items := f.keys().Items(fileMentionCatalog(entries))
	if pickerItemIndex(items, f.Selection) < 0 {
		f.Selection = firstEnabledPickerKey(items)
	}
}

// Selected returns the highlighted entry when it matches the query.
func (f *fileMentionController) Selected(entries []protocol.FileIndexEntry) (protocol.FileIndexEntry, bool) {
	if pickerItemIndex(f.keys().Items(fileMentionCatalog(entries)), f.Selection) < 0 {
		return protocol.FileIndexEntry{}, false
	}
	for _, entry := range entries {
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

// HandleKey applies one key through the navigation-only picker key model.
// Unhandled keys belong to the composer, which owns the query.
func (f *fileMentionController) HandleKey(entries []protocol.FileIndexEntry, key ui.Key) pickerKeyResult {
	if !f.Open {
		return pickerKeyResult{}
	}
	model := f.keys()
	result := model.HandleNavigationKey(key, fileMentionCatalog(entries))
	f.Selection = model.Selection
	return result
}

// fileMentionSurface maps the file mention and the shared file index onto the
// inline picker.
type fileMentionSurface struct {
	Controller fileMentionController
	Source     indexedFileSource
	Anchor     func(ui.Size) ui.Point
	OnSelect   func(ui.EventContext, string)
}

func (w fileMentionSurface) Build(ui.BuildContext) ui.Widget {
	catalog := fileMentionCatalog(w.Source.Entries)
	model := w.Controller.keys()
	if items := model.Items(catalog); pickerItemIndex(items, model.Selection) < 0 {
		model.Selection = firstEnabledPickerKey(items)
	}
	picker := inlinePicker{
		Query: model.Query, Catalog: catalog, Selection: model.Selection,
		OnActivate: w.OnSelect,
		Anchor:     w.Anchor,
	}
	switch {
	case len(catalog) == 0 && w.Source.Loading:
		picker.Message, picker.MessageTone = "Loading files…", pickerToneLoading
	case len(catalog) == 0 && w.Source.Error != "":
		picker.Message, picker.MessageTone = "Could not load files: "+w.Source.Error, pickerToneDanger
	case w.Source.Error != "":
		picker.Status, picker.StatusTone = "Refresh failed: "+w.Source.Error, pickerToneDanger
	case w.Source.Truncated:
		picker.Status = "Showing first 4,000 indexed paths"
	}
	return picker
}
