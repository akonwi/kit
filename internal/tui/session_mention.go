package tui

import (
	"github.com/akonwi/kit/internal/protocol"
	"go.rockorager.dev/vaxis"
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
func (s *sessionMentionController) filtered(entries []protocol.SessionInfo) []protocol.SessionInfo {
	return ui.DefaultFuzzySelectFilter(s.Query, entries, func(entry protocol.SessionInfo) ui.FuzzySelectItem {
		return ui.FuzzySelectItem{Title: sessionMentionName(entry), Aliases: []string{entry.ID, entry.CWD}}
	})
}
func (s *sessionMentionController) ensureSelection(entries []protocol.SessionInfo) {
	entries = s.filtered(entries)
	for _, entry := range entries {
		if entry.ID == s.Selection {
			return
		}
	}
	s.Selection = ""
	if len(entries) > 0 {
		s.Selection = entries[0].ID
	}
}
func (s *sessionMentionController) Move(entries []protocol.SessionInfo, delta int) {
	entries = s.filtered(entries)
	if len(entries) == 0 {
		return
	}
	index := 0
	for i, entry := range entries {
		if entry.ID == s.Selection {
			index = i
			break
		}
	}
	index = (index + delta) % len(entries)
	if index < 0 {
		index += len(entries)
	}
	s.Selection = entries[index].ID
}
func (s *sessionMentionController) Selected(entries []protocol.SessionInfo) (protocol.SessionInfo, bool) {
	for _, entry := range s.filtered(entries) {
		if entry.ID == s.Selection {
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
func (s *sessionMentionController) HandleKey(entries []protocol.SessionInfo, key ui.Key) (protocol.SessionInfo, bool, bool) {
	if !s.Open || key.EventType == ui.EventRelease || key.EventType == vaxis.EventPaste {
		return protocol.SessionInfo{}, false, false
	}
	switch {
	case key.MatchString("Up"):
		s.Move(entries, -1)
		return protocol.SessionInfo{}, false, true
	case key.MatchString("Down"):
		s.Move(entries, 1)
		return protocol.SessionInfo{}, false, true
	case key.MatchString("Enter"):
		entry, ok := s.Selected(entries)
		return entry, ok, true
	default:
		return protocol.SessionInfo{}, false, false
	}
}

type sessionMentionSource struct {
	Entries    []protocol.SessionInfo
	Loading    bool
	Error      string
	generation uint64
}
