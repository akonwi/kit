package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInteractionBrokerPublishesResolutionBeforeUnblockingAndRejectsDuplicate(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var events []NewEvent
	broker := newInteractionBroker(func(event NewEvent) { mu.Lock(); events = append(events, event); mu.Unlock() })
	request := testInteractionRequest("interaction_0123456789abcdef0123456789abcdef")
	result := make(chan interactionSettlement, 1)
	go func() {
		settlement, err := broker.request(context.Background(), request)
		if err != nil {
			t.Errorf("request: %v", err)
			return
		}
		result <- settlement
	}()
	waitForPendingInteraction(t, broker, request.ID)
	confirmed := true
	if err := broker.respond(InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); err != nil {
		t.Fatalf("respond: %v", err)
	}
	settlement := <-result
	if settlement.reason != "answered" || settlement.response.Confirmed == nil || !*settlement.response.Confirmed {
		t.Fatalf("settlement = %#v", settlement)
	}
	mu.Lock()
	if len(events) != 2 || events[1].Kind != EventInteractionResolved || events[1].InteractionResolution != "answered" {
		t.Fatalf("events = %#v", events)
	}
	mu.Unlock()
	if err := broker.respond(InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); !errors.Is(err, ErrInteractionSettled) {
		t.Fatalf("duplicate response error = %v", err)
	}
}

func TestInteractionBrokerRecordsNegativeConfirmationReason(t *testing.T) {
	t.Parallel()
	broker := newInteractionBroker(func(NewEvent) {})
	request := testInteractionRequest("interaction_abcdef0123456789abcdef0123456789")
	result := make(chan interactionSettlement, 1)
	go func() { settlement, _ := broker.request(context.Background(), request); result <- settlement }()
	waitForPendingInteraction(t, broker, request.ID)
	confirmed := false
	if err := broker.respond(InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if settlement := <-result; settlement.reason != "negative_confirmation" {
		t.Fatalf("reason = %q", settlement.reason)
	}
}

func TestInteractionBrokerContextCancellationPublishesReason(t *testing.T) {
	t.Parallel()
	var resolved NewEvent
	broker := newInteractionBroker(func(event NewEvent) {
		if event.Kind == EventInteractionResolved {
			resolved = event
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	request := testInteractionRequest("interaction_0123456789abcdef0123456789abcdef")
	result := make(chan interactionSettlement, 1)
	go func() { settlement, _ := broker.request(ctx, request); result <- settlement }()
	waitForPendingInteraction(t, broker, request.ID)
	cancel()
	settlement := <-result
	if settlement.reason != "run_abort" || !settlement.response.Cancelled {
		t.Fatalf("settlement = %#v", settlement)
	}
	if resolved.InteractionID != request.ID || resolved.InteractionResolution != "run_abort" {
		t.Fatalf("resolved event = %#v", resolved)
	}
}

func testInteractionRequest(id string) InteractionRequest {
	return InteractionRequest{ID: id, SessionID: "session_0123456789abcdef0123456789abcdef", RunID: "run_0123456789abcdef0123456789abcdef", ToolCallID: "tool_0123456789abcdef0123456789abcdef", Kind: InteractionConfirm, Title: "Continue?", CreatedAt: time.Now().UTC()}
}

func waitForPendingInteraction(t *testing.T, broker *interactionBroker, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		pending := broker.snapshot()
		if len(pending) == 1 && pending[0].ID == id {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("interaction %q did not become pending", id)
}
