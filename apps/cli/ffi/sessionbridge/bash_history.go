package sessionbridge

import (
	"context"
	"strconv"
	"strings"

	kit "github.com/akonwi/kit/api"
	protocol "github.com/akonwi/kit/api/contract"
)

// BashHistoryItem is one command run earlier in a session.
type BashHistoryItem struct {
	ID      string
	Command string
	// Exclude reports that the run kept its result out of the model's context.
	Exclude bool
}

// BashHistoryPage is one newest-first page of a session's command history.
type BashHistoryPage struct {
	Items []BashHistoryItem
	// More reports that older commands remain; Before is the cursor for them.
	More   bool
	Before int64
}

type bashHistorian interface {
	BashHistory(context.Context, uint64, int) (protocol.BashHistoryPage, error)
}

// BashHistory reads up to limit commands run in the session before the cursor
// before, newest first; a zero cursor starts at the newest. Blank commands are
// skipped.
func BashHistory(ctx context.Context, session *kit.Session, before int64, limit int) (BashHistoryPage, error) {
	return bashHistory(ctx, session, before, limit)
}

func bashHistory(ctx context.Context, history bashHistorian, before int64, limit int) (BashHistoryPage, error) {
	var cursor uint64
	if before > 0 {
		cursor = uint64(before)
	}
	page, err := history.BashHistory(ctx, cursor, limit)
	if err != nil {
		return BashHistoryPage{}, err
	}
	result := BashHistoryPage{Items: make([]BashHistoryItem, 0, len(page.Entries))}
	for _, entry := range page.Entries {
		command := strings.TrimSpace(entry.Command)
		if command == "" {
			continue
		}
		result.Items = append(result.Items, BashHistoryItem{ID: entry.ID, Command: command, Exclude: entry.ExcludeFromContext})
	}
	if page.HasMore && page.NextCursor != "" {
		if next, err := strconv.ParseUint(page.NextCursor, 10, 63); err == nil && next > 0 && next != cursor {
			result.More, result.Before = true, int64(next)
		}
	}
	return result, nil
}
