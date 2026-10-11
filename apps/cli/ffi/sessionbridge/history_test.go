package sessionbridge

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

type scriptedPager struct {
	pages   []protocol.MessagePage
	queries []protocol.MessagePageQuery
	err     error
}

func (p *scriptedPager) MessagePage(_ context.Context, query protocol.MessagePageQuery) (protocol.MessagePage, error) {
	p.queries = append(p.queries, query)
	if p.err != nil {
		return protocol.MessagePage{}, p.err
	}
	page := p.pages[0]
	p.pages = p.pages[1:]
	return page, nil
}

func userMessage(id, text string) protocol.TranscriptMessage {
	return protocol.TranscriptMessage{ID: id, Role: "user", Content: []protocol.TranscriptContent{protocol.TextBlock(text)}}
}

func TestMessageHistoryKeepsTheNewestOfEachPromptAndSkipsBlankOnes(t *testing.T) {
	pager := &scriptedPager{pages: []protocol.MessagePage{
		{Messages: []protocol.TranscriptMessage{
			userMessage("4", "  run the tests \n"),
			userMessage("3", "   "),
			{ID: "x", Role: "assistant", Content: []protocol.TranscriptContent{protocol.TextBlock("reply")}},
		}, HasMore: true, NextCursor: "3"},
		{Messages: []protocol.TranscriptMessage{
			userMessage("2", "run the tests"),
			userMessage("1", "first"),
		}},
	}}
	entries, err := messageHistory(context.Background(), pager)
	if err != nil {
		t.Fatal(err)
	}
	want := []HistoryEntry{{ID: "4", Text: "run the tests"}, {ID: "1", Text: "first"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
	wantQueries := []protocol.MessagePageQuery{
		{Limit: 100, Roles: []string{"user"}},
		{Limit: 100, Roles: []string{"user"}, Before: 3},
	}
	if !reflect.DeepEqual(pager.queries, wantQueries) {
		t.Fatalf("queries = %+v, want %+v", pager.queries, wantQueries)
	}
}

func TestMessageHistoryReadsAtMostFivePages(t *testing.T) {
	pager := &scriptedPager{}
	for page := range 6 {
		pager.pages = append(pager.pages, protocol.MessagePage{
			Messages:   []protocol.TranscriptMessage{userMessage(strconv.Itoa(page), "prompt "+strconv.Itoa(page))},
			HasMore:    true,
			NextCursor: strconv.Itoa(100 - page),
		})
	}
	entries, err := messageHistory(context.Background(), pager)
	if err != nil {
		t.Fatal(err)
	}
	if len(pager.queries) != 5 || len(entries) != 5 {
		t.Fatalf("queries = %d, entries = %d, want 5 of each", len(pager.queries), len(entries))
	}
}

func TestMessageHistoryReportsAFailedPage(t *testing.T) {
	failure := errors.New("connection refused")
	if _, err := messageHistory(context.Background(), &scriptedPager{err: failure}); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want %v", err, failure)
	}
}

func TestMessageHistoryRecallsPromptCommandInvocations(t *testing.T) {
	command := protocol.TranscriptMessage{ID: "2", Role: "user", Content: []protocol.TranscriptContent{
		protocol.NewTranscriptContent(protocol.PromptCommandContent{
			Name: "claude-fix", Arguments: "123 high", Source: protocol.PromptCommandSourceClaudeProject,
			Text: "Fix issue #123 at high priority.",
		}),
	}}
	pager := &scriptedPager{pages: []protocol.MessagePage{{Messages: []protocol.TranscriptMessage{command, userMessage("1", "typed prompt")}}}}
	entries, err := messageHistory(context.Background(), pager)
	if err != nil {
		t.Fatal(err)
	}
	want := []HistoryEntry{{ID: "2", Text: "/claude-fix 123 high"}, {ID: "1", Text: "typed prompt"}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}
