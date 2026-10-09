package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/subagent"
)

func TestSubagentChangedDoesNotWaitForParentRuntimeLock(t *testing.T) {
	t.Parallel()
	events, err := newEventLog()
	if err != nil {
		t.Fatal(err)
	}
	loaded := &runtime{events: events}
	owner := "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	manager := &Manager{runtimes: map[string]*runtime{owner: loaded}, deleting: make(map[string]bool)}
	loaded.mu.Lock()
	events.mu.Lock()
	done := make(chan struct{})
	go func() {
		manager.SubagentChanged(context.Background(), owner,
			subagent.ConversationID("subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			subagent.TaskID("task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		loaded.mu.Unlock()
		events.mu.Unlock()
		t.Fatal("SubagentChanged blocked on parent runtime or event-log work")
	}
	loaded.mu.Unlock()
	events.mu.Unlock()
	manager.ops.Wait()
	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.events) != 1 || events.events[0].Kind != EventSubagentChanged {
		t.Fatalf("subagent invalidations = %+v", events.events)
	}
}

func TestCollectChildTranscriptPagesCompleteTurns(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	messages := make([]droids.MessageEnvelope, 60)
	for index := range messages {
		messages[index] = droids.MessageEnvelope{
			ID: droids.MessageID(fmt.Sprintf("message_%02d", index)), ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TurnID: droids.TurnID(fmt.Sprintf("turn_%02d", index)), CreatedAt: now.Add(time.Duration(index) * time.Second),
			Message: droids.UserMessage{Content: []droids.InputContent{droids.TextInput{Text: fmt.Sprintf("message %d", index)}}},
		}
	}
	history := childTranscriptHistory(t, messages)
	page, err := collectChildTranscript(t.Context(), history, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMoreMessages || page.PreviousMessageCursor != 11 || len(page.Messages) != 50 || page.Messages[0].Sequence != 11 || page.Messages[49].Sequence != 60 {
		t.Fatalf("newest page = cursor %d more %t len %d first %d last %d", page.PreviousMessageCursor, page.HasMoreMessages, len(page.Messages), page.Messages[0].Sequence, page.Messages[len(page.Messages)-1].Sequence)
	}
	if page.Messages[0].Content[0].Text != "message 10" {
		t.Fatalf("oldest included text = %q", page.Messages[0].Content[0].Text)
	}

	older, err := collectChildTranscript(t.Context(), history, page.PreviousMessageCursor)
	if err != nil {
		t.Fatal(err)
	}
	if older.HasMoreMessages || older.PreviousMessageCursor != 0 || len(older.Messages) != 10 || older.Messages[0].Sequence != 1 || older.Messages[9].Sequence != 10 {
		t.Fatalf("older page = %+v", older)
	}

	large := append([]droids.MessageEnvelope(nil), messages[0])
	largeText := strings.Repeat("x", 9<<20)
	large[0].Message = droids.UserMessage{Content: []droids.InputContent{droids.TextInput{Text: largeText}}}
	page, err = collectChildTranscript(t.Context(), childTranscriptHistory(t, large), 0)
	if err != nil {
		t.Fatalf("large message page error = %v", err)
	}
	if page.HasMoreMessages || len(page.Messages) != 1 || len(page.Messages[0].Content) != 1 || page.Messages[0].Content[0].Text != largeText {
		t.Fatalf("large message page = more %t len %d", page.HasMoreMessages, len(page.Messages))
	}
}

func TestCollectChildTranscriptRejectsCursorInsideTurn(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	messages := []droids.MessageEnvelope{
		{ID: "message_00", ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TurnID: "turn_00", CreatedAt: now, Message: droids.UserMessage{Content: []droids.InputContent{droids.TextInput{Text: "older"}}}},
		{ID: "message_01", ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TurnID: "turn_01", CreatedAt: now.Add(time.Second), Message: droids.UserMessage{Content: []droids.InputContent{droids.TextInput{Text: "first"}}}},
		{ID: "message_02", ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TurnID: "turn_01", CreatedAt: now.Add(2 * time.Second), Message: droids.AssistantMessage{StopReason: droids.StopReasonStop, Content: []droids.AssistantContent{droids.TextContent{Text: "second"}}}},
	}
	history := childTranscriptHistory(t, messages)
	if err := validateChildTranscriptCursor(t.Context(), history, 3); !errors.Is(err, subagent.ErrTranscriptCursorUnavailable) {
		t.Fatalf("mid-turn cursor error = %v", err)
	}
	if err := validateChildTranscriptCursor(t.Context(), history, 2); err != nil {
		t.Fatalf("turn-boundary cursor error = %v", err)
	}
	page, err := collectChildTranscript(t.Context(), history, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Sequence != 1 || page.HasMoreMessages {
		t.Fatalf("boundary page = %+v", page)
	}
}

func childTranscriptHistory(t *testing.T, messages []droids.MessageEnvelope) func(context.Context, droids.HistoryQuery) (droids.MessagePage, error) {
	t.Helper()
	return func(_ context.Context, query droids.HistoryQuery) (droids.MessagePage, error) {
		if query.After > 0 {
			for index := range messages {
				sequence := uint64(index + 1)
				if sequence > query.After {
					envelope := messages[index]
					envelope.Sequence = sequence
					return droids.MessagePage{Messages: []droids.MessageEnvelope{envelope}}, nil
				}
			}
			return droids.MessagePage{}, nil
		}
		if !query.Descending {
			t.Fatal("child transcript must page newest-first")
		}
		var selected []droids.MessageEnvelope
		for index := len(messages) - 1; index >= 0; index-- {
			sequence := uint64(index + 1)
			if query.Before > 0 && sequence >= query.Before {
				continue
			}
			envelope := messages[index]
			envelope.Sequence = sequence
			selected = append(selected, envelope)
			if query.Limit > 0 && len(selected) == query.Limit {
				break
			}
		}
		page := droids.MessagePage{Messages: selected}
		if len(selected) > 0 && selected[len(selected)-1].Sequence > 1 {
			page.HasMore = true
			page.Next = selected[len(selected)-1].Sequence
		}
		return page, nil
	}
}

func TestProjectChildTranscriptMessageOmitsEmptyThinking(t *testing.T) {
	t.Parallel()
	message, err := projectChildTranscriptMessage(droids.MessageEnvelope{
		ID: "message_test", ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TurnID: "turn_test", CreatedAt: time.Now(),
		Message: droids.AssistantMessage{
			StopReason: droids.StopReasonToolUse,
			Content: []droids.AssistantContent{
				droids.TextContent{},
				droids.ThinkingContent{},
				droids.ToolCall{ID: "call_test", Name: "read"},
			},
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 1 || message.Content[0].Kind != "toolCall" || message.Content[0].Arguments != "{}" {
		t.Fatalf("projected content = %+v", message.Content)
	}
	transcript := protocol.SubagentTranscript{
		ConversationID: "subagent_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Messages: []protocol.TranscriptMessage{{
			ID: message.ID, TurnID: message.TurnID, Sequence: message.Sequence, Role: message.Role,
			StopReason: message.StopReason, CreatedAt: message.CreatedAt.Format(time.RFC3339Nano),
			Content: []protocol.TranscriptContent{protocol.NewTranscriptContent(protocol.ToolCallContent{ToolCallID: message.Content[0].ToolCallID, ToolName: message.Content[0].ToolName, Arguments: message.Content[0].Arguments})},
		}},
	}
	if err := transcript.Validate(); err != nil {
		t.Fatalf("projected transcript validation: %v", err)
	}
}
