package peer

import (
	"context"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
)

type fakeService struct{ call droids.ToolContext }

func (f *fakeService) DiscoverPeers(context.Context, string, int) ([]Session, error) {
	return []Session{{ID: "session_b", Name: "B", Availability: "idle"}}, nil
}
func (f *fakeService) SendPeerQuery(_ context.Context, _ string, call droids.ToolContext, recipient, message, thread, preceding string) (Request, error) {
	f.call = call
	return Request{ID: "peer_one", RecipientSessionID: recipient, Message: message, State: StateQueued}, nil
}
func (*fakeService) InspectPeerQuery(context.Context, string, string) (Request, error) {
	return Request{ID: "peer_one", State: StateCompleted, Result: "done"}, nil
}
func (*fakeService) WaitPeerQuery(context.Context, string, string, time.Duration) (Request, bool, error) {
	return Request{ID: "peer_one", State: StateCompleted}, false, nil
}

func TestToolPassesDurableToolCallIdentityToSend(t *testing.T) {
	service := &fakeService{}
	tool := &ToolService{Service: service}
	call := droids.ToolContext{TurnID: "turn_one", ToolCallID: "call_one"}
	result := tool.execute(t.Context(), "session_a", call, toolArguments{Action: "send", SessionID: "session_b", Message: "question"})
	if service.call != call {
		t.Fatalf("tool context = %+v, want %+v", service.call, call)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestToolRejectsExcessiveWait(t *testing.T) {
	tool := &ToolService{Service: &fakeService{}}
	result := tool.execute(t.Context(), "session_a", droids.ToolContext{}, toolArguments{Action: "wait", RequestID: "peer_one", TimeoutSeconds: 31})
	if !result.IsError {
		t.Fatalf("result = %+v", result)
	}
}
