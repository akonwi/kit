package session

import (
	"reflect"
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestDroidMessageCodecRoundTrip(t *testing.T) {
	t.Parallel()

	messages := []droids.Message{
		droids.UserMessage{
			Timestamp: 1000,
			Content: []droids.Content{
				droids.TextContent{Text: "hello", Signature: "text-signature"},
				droids.NewImageData("image/png", []byte("image")),
				droids.NewFileData("notes.txt", "text/plain", []byte("notes")),
			},
		},
		droids.AssistantMessage{
			Timestamp: 2000,
			Provider:  "test", Model: "echo", ResponseModel: "echo-1", ResponseID: "response-1",
			ProviderScope: "account:opaque",
			StopReason:    droids.StopReasonToolUse,
			ErrorKind:     droids.ErrorProtocol,
			Content: []droids.Content{
				droids.ThinkingContent{Thinking: "consider", Signature: "thinking-signature"},
				droids.TextContent{Text: "calling"},
				droids.ToolCall{ID: "call-1", Name: "read", Arguments: []byte(`{"path":"README.md"}`), Signature: "fc-1"},
			},
			Usage: droids.Usage{
				Input: 10, Output: 5, CacheRead: 2, Reasoning: 1, TotalTokens: 15,
				Cost: droids.UsageCost{Input: 0.1, Output: 0.2, Total: 0.3},
			},
		},
		droids.ToolResultMessage{
			Timestamp: 3000, ToolCallID: "call-1", ToolName: "read",
			Content: []droids.Content{droids.TextContent{Text: "contents"}},
			Details: map[string]any{"lines": float64(1)}, IsError: false,
		},
	}

	for _, original := range messages {
		role, body, createdAt, err := encodeDroidMessage(original)
		if err != nil {
			t.Fatalf("encodeDroidMessage(%T) error = %v", original, err)
		}
		if createdAt.UnixMilli() <= 0 {
			t.Errorf("createdAt = %v", createdAt)
		}
		decoded, err := decodeDroidMessage(role, body)
		if err != nil {
			t.Fatalf("decodeDroidMessage(%T) error = %v", original, err)
		}
		if !reflect.DeepEqual(decoded, original) {
			t.Errorf("round trip for %T:\n got  %#v\n want %#v", original, decoded, original)
		}
	}
}

func TestDroidMessageCodecRejectsRoleMismatch(t *testing.T) {
	t.Parallel()

	_, body, _, err := encodeDroidMessage(droids.UserMessage{
		Content: []droids.Content{droids.TextContent{Text: "hello"}}, Timestamp: 1,
	})
	if err != nil {
		t.Fatalf("encodeDroidMessage() error = %v", err)
	}
	if _, err := decodeDroidMessage("assistant", body); err == nil {
		t.Fatal("decodeDroidMessage() accepted a mismatched role")
	}
}
