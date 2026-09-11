package session

import (
	"context"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/protocol"
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
			Content: []protocol.TranscriptContent{{
				Kind: protocol.TranscriptContentKind(message.Content[0].Kind), ToolCallID: message.Content[0].ToolCallID,
				ToolName: message.Content[0].ToolName, Arguments: message.Content[0].Arguments,
			}},
		}},
	}
	if err := transcript.Validate(); err != nil {
		t.Fatalf("projected transcript validation: %v", err)
	}
}
