package protocol

import (
	"encoding/json"
	"testing"
)

func TestTranscriptPageValidatesCursorAndMessages(t *testing.T) {
	t.Parallel()

	page := TranscriptPage{
		SessionID: "session_0123456789abcdef0123456789abcdef",
		Messages: []TranscriptMessage{{
			ID: "message_1", TurnID: "turn_1", Sequence: 10, Role: "user",
			Content:   []TranscriptContent{{Kind: TranscriptContentText, Text: "hello"}},
			CreatedAt: "2026-01-01T00:00:00Z",
		}},
		PreviousMessageCursor: "10", HasMoreMessages: true,
	}
	if err := page.ValidateBefore("20"); err != nil {
		t.Fatalf("ValidateBefore() error = %v", err)
	}
	page.PreviousMessageCursor = "11"
	if err := page.Validate(); err == nil {
		t.Fatal("Validate() accepted a mismatched cursor")
	}
	page.PreviousMessageCursor = "10"
	if err := page.ValidateBefore("10"); err == nil {
		t.Fatal("ValidateBefore() accepted a message at the exclusive cursor")
	}
}

func TestTranscriptPageRejectsReopenedTurnsAndOrphanedToolResults(t *testing.T) {
	t.Parallel()

	message := func(id, turn string, sequence int64) TranscriptMessage {
		return TranscriptMessage{
			ID: id, TurnID: turn, Sequence: sequence, Role: "user",
			Content: []TranscriptContent{{Kind: TranscriptContentText, Text: "hello"}}, CreatedAt: "2026-01-01T00:00:00Z",
		}
	}
	page := TranscriptPage{SessionID: "session_0123456789abcdef0123456789abcdef", Messages: []TranscriptMessage{
		message("message_1", "turn_1", 1), message("message_2", "turn_2", 2), message("message_3", "turn_1", 3),
	}}
	if err := page.Validate(); err == nil {
		t.Fatal("Validate() accepted a reopened turn")
	}

	page.Messages = []TranscriptMessage{{
		ID: "message_1", TurnID: "turn_1", Sequence: 1, Role: "tool",
		Content:    []TranscriptContent{{Kind: TranscriptContentText, Text: "contents"}},
		ToolCallID: "call_1", ToolName: "read", Details: json.RawMessage(`{"lines":1}`), CreatedAt: "2026-01-01T00:00:00Z",
	}}
	if err := page.Validate(); err == nil {
		t.Fatal("Validate() accepted a tool result without a preceding call")
	}
}
