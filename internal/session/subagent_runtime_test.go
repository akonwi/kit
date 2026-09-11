package session

import (
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/protocol"
)

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
