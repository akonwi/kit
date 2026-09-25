package session

import (
	"testing"

	"github.com/akonwi/kit/internal/droids"
)

func TestProjectChildMessageCompletionKeepsMessageIdentity(t *testing.T) {
	t.Parallel()
	event, ok := projectChildLiveEvent(droids.EventEnvelope{
		TurnID: "turn_1",
		Event: droids.MessageEnd{Message: droids.AssistantMessage{
			ID: "message_1", Content: []droids.AssistantContent{droids.TextContent{Text: "done"}},
		}},
	})
	if !ok || event.Kind != "message.completed" || event.MessageID != "message_1" {
		t.Fatalf("projected completion = %+v, %t", event, ok)
	}
}
