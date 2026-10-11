package sessionbridge

import (
	"context"
	"errors"
	"reflect"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

type scriptedHistorian struct {
	page    protocol.BashHistoryPage
	err     error
	before  uint64
	limit   int
	queried bool
}

func (h *scriptedHistorian) BashHistory(_ context.Context, before uint64, limit int) (protocol.BashHistoryPage, error) {
	h.before, h.limit, h.queried = before, limit, true
	return h.page, h.err
}

func TestBashHistoryProjectsCommandsAndTheNextCursor(t *testing.T) {
	historian := &scriptedHistorian{page: protocol.BashHistoryPage{
		Entries: []protocol.BashHistoryEntry{
			{ID: "bash_3", Command: " git status "},
			{ID: "bash_2", Command: "  "},
			{ID: "bash_1", Command: "ls", ExcludeFromContext: true},
		},
		HasMore: true, NextCursor: "7",
	}}
	page, err := bashHistory(context.Background(), historian, 12, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := BashHistoryPage{
		Items:  []BashHistoryItem{{ID: "bash_3", Command: "git status"}, {ID: "bash_1", Command: "ls", Exclude: true}},
		More:   true,
		Before: 7,
	}
	if !reflect.DeepEqual(page, want) || historian.before != 12 || historian.limit != 10 {
		t.Fatalf("page = %+v (before %d, limit %d), want %+v", page, historian.before, historian.limit, want)
	}
}

func TestBashHistoryEndsWithoutAUsableCursor(t *testing.T) {
	for _, cursor := range []string{"", "0", "x", "12"} {
		historian := &scriptedHistorian{page: protocol.BashHistoryPage{HasMore: true, NextCursor: cursor}}
		page, err := bashHistory(context.Background(), historian, 12, 100)
		if err != nil || page.More || page.Before != 0 {
			t.Fatalf("cursor %q: page = %+v, err = %v", cursor, page, err)
		}
	}
	failure := errors.New("connection refused")
	if _, err := bashHistory(context.Background(), &scriptedHistorian{err: failure}, 0, 10); !errors.Is(err, failure) {
		t.Fatalf("err = %v", err)
	}
}
