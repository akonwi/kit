package sessionbridge

import (
	"testing"

	protocol "github.com/akonwi/kit/api/contract"
)

func TestEventsProjectsScratchpadChanges(t *testing.T) {
	record := &protocol.Scratchpad{OwnerSessionID: "session_test", Content: "remote notes", Revision: 2}
	projected := Events([]protocol.SessionEvent{{
		StreamID:  "stream_test",
		Sequence:  1,
		SessionID: "session_test",
		Payload:   protocol.ScratchpadChangedEvent{Scratchpad: record},
	}})
	if len(projected) != 1 || projected[0].Scratchpad != record {
		t.Fatalf("Events() = %+v, want projected scratchpad", projected)
	}
}

func TestEventsProjectsInteractionLifecycle(t *testing.T) {
	request := &protocol.InteractionRequest{ID: "interaction_1", Kind: protocol.InteractionConfirm, Title: "Proceed?"}
	projected := Events([]protocol.SessionEvent{
		{Sequence: 1, TurnID: "turn_1", Payload: protocol.InteractionRequestedEvent{Interaction: request}},
		{Sequence: 2, TurnID: "turn_1", Payload: protocol.InteractionResolvedEvent{InteractionID: "interaction_1", InteractionResolution: "answered"}},
	})
	if len(projected) != 2 {
		t.Fatalf("Events() returned %d events, want 2", len(projected))
	}
	if projected[0].Kind != "interaction.requested" || projected[0].Interaction != request || projected[0].InteractionID != "interaction_1" {
		t.Fatalf("requested = %+v", projected[0])
	}
	if projected[1].Kind != "interaction.resolved" || projected[1].Interaction != nil || projected[1].InteractionID != "interaction_1" {
		t.Fatalf("resolved = %+v", projected[1])
	}
}
