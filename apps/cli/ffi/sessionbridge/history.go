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
		text := strings.TrimSpace(userText(message))
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

// userText is the recalled text of a user message: its text blocks and a
// prompt command's invocation, in content order. Submitting a recalled
// invocation runs the command again with its current template.
func userText(message protocol.TranscriptMessage) string {
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
