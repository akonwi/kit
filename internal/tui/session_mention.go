package tui

import (
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis/ui"
)

type sessionMentionController inlineMentionController

func (s *sessionMentionController) Close() { (*inlineMentionController)(s).Close() }
func (s *sessionMentionController) Observe(previous, next string, pasted bool) bool {
	return (*inlineMentionController)(s).Observe(previous, next, pasted, '#')
}
func sessionMentionName(entry protocol.SessionInfo) string {
	if entry.Name != "" {
		return entry.Name
	}
	return "Unnamed session"
}

// sessionMentionCatalog maps mentionable sessions onto inline picker rows:
// the name, then the working directory, then the updated time. The session
// ID and full working directory are matched too.
func sessionMentionCatalog(entries []protocol.SessionInfo, now time.Time) []pickerItem {
	items := make([]pickerItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, pickerItem{
			Key: entry.ID, Label: sessionMentionName(entry),
			Description:   sessionDisplayCWD(entry.CWD, sessionCWDMaxWidth),
			Meta:          formatSessionUpdated(entry.UpdatedAt, now),
			SearchAliases: []string{entry.ID, entry.CWD},
		})
	}
	return items
}

// keys returns the mention's navigation key model.
func (s *sessionMentionController) keys() pickerKeyModel {
	return pickerKeyModel{Query: s.Query, Selection: s.Selection}
}

// ensureSelection keeps a visible selection, or highlights the first match.
func (s *sessionMentionController) ensureSelection(entries []protocol.SessionInfo) {
	items := s.keys().Items(sessionMentionCatalog(entries, time.Time{}))
	if pickerItemIndex(items, s.Selection) < 0 {
		s.Selection = firstEnabledPickerKey(items)
	}
}

// Selected returns the session with id when it matches the query.
func (s *sessionMentionController) Selected(entries []protocol.SessionInfo, id string) (protocol.SessionInfo, bool) {
	if pickerItemIndex(s.keys().Items(sessionMentionCatalog(entries, time.Time{})), id) < 0 {
		return protocol.SessionInfo{}, false
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return protocol.SessionInfo{}, false
}

func (s *sessionMentionController) Insert(text string, entry protocol.SessionInfo) (string, int, bool) {
	if !s.Open || s.Anchor < 0 || s.QueryEnd <= s.Anchor || s.QueryEnd > len(text) || text[s.Anchor] != '#' || entry.ID == "" {
		return text, 0, false
	}
	token := "#[session:" + entry.ID + "] "
	next := text[:s.Anchor] + token + text[s.QueryEnd:]
	cursor := s.Anchor + len(token)
	s.Close()
	return next, cursor, true
}

// HandleKey applies one key through the navigation-only picker key model.
// Unhandled keys belong to the composer, which owns the query.
func (s *sessionMentionController) HandleKey(entries []protocol.SessionInfo, key ui.Key) pickerKeyResult {
	if !s.Open {
		return pickerKeyResult{}
	}
	model := s.keys()
	result := model.HandleNavigationKey(key, sessionMentionCatalog(entries, time.Time{}))
	s.Selection = model.Selection
	return result
}

type sessionMentionSource struct {
	Entries    []protocol.SessionInfo
	Loading    bool
	Error      string
	generation uint64
}
