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
