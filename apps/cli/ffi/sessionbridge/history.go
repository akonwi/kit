package sessionbridge

import (
	"context"
	"strconv"
	"strings"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

const (
	historyPageLimit = 100
	historyMaxPages  = 5
)

// HistoryEntry is one prompt the user sent in a session.
type HistoryEntry struct {
	ID   string
	Text string
}

type messagePager interface {
	MessagePage(context.Context, protocol.MessagePageQuery) (protocol.MessagePage, error)
}

// MessageHistory returns the session's recent prompts, newest first, without
// blank prompts or repeats of a newer one. It reads up to five pages of user
// messages, as the vaxis client does.
func MessageHistory(ctx context.Context, session *kit.Session) ([]HistoryEntry, error) {
	return messageHistory(ctx, session)
}

func messageHistory(ctx context.Context, pager messagePager) ([]HistoryEntry, error) {
	var messages []protocol.TranscriptMessage
	var before uint64
	for range historyMaxPages {
		query := protocol.MessagePageQuery{Limit: historyPageLimit, Roles: []string{"user"}, Before: before}
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
	return historyEntries(messages), nil
}

func historyEntries(messages []protocol.TranscriptMessage) []HistoryEntry {
	entries := make([]HistoryEntry, 0, len(messages))
	seen := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(message.TextContent())
		if text == "" {
			continue
		}
		if _, repeated := seen[text]; repeated {
			continue
		}
		seen[text] = struct{}{}
		entries = append(entries, HistoryEntry{ID: message.ID, Text: text})
	}
	return entries
}
