package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func pluginBrokerRequest() InteractionRequest {
	request := testInteractionRequest("interaction_0123456789abcdef0123456789abcdef")
	request.RunID = ""
	request.ToolCallID = ""
	request.Plugin = &PluginInteractionOwner{PluginID: "demo", Instance: "host:1"}
	return request
}

func TestPluginBrokerCancellationDuringAdmissionDoesNotNeedRuntimeAuthority(t *testing.T) {
	broker := newInteractionBroker(nil)
	broker.authority = &sync.Mutex{}
	broker.authority.Lock()
	defer broker.authority.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := broker.requestOwned(ctx, pluginBrokerRequest(), func() bool { return true })
		result <- err
	}()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("admission = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled admission waited for authority")
	}
}

func TestPluginBrokerShutdownUnblocksCallbackWhileRuntimeAuthorityIsHeld(t *testing.T) {
	broker := newInteractionBroker(nil)
	broker.authority = &sync.Mutex{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := pluginBrokerRequest()
	result := make(chan interactionSettlement, 1)
	go func() {
		settlement, err := broker.requestOwned(ctx, request, func() bool { return true })
		if err != nil {
			t.Error(err)
		}
		result <- settlement
	}()
	waitForPendingInteraction(t, broker, request.ID)
	broker.authority.Lock()
	defer broker.authority.Unlock()
	cancel()
	broker.cancelAll("session_closed")
	select {
	case settlement := <-result:
		if !settlement.response.Cancelled || settlement.reason != "session_closed" {
			t.Fatalf("shutdown settlement = %#v", settlement)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown callback deadlocked waiting for authority")
	}
}

func TestPluginBrokerFencesRevokedOwnerAtSnapshotAndResponse(t *testing.T) {
	broker := newInteractionBroker(nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var available atomic.Bool
	available.Store(true)
	request := pluginBrokerRequest()
	result := make(chan interactionSettlement, 1)
	go func() {
		settlement, err := broker.requestOwned(ctx, request, available.Load)
		if err != nil {
			t.Error(err)
		}
		result <- settlement
	}()
	waitForPendingInteraction(t, broker, request.ID)
	available.Store(false)
	if got := broker.snapshot(); len(got) != 0 {
		t.Fatalf("revoked snapshot = %#v", got)
	}
	confirmed := true
	if err := broker.respond(InteractionResponse{RequestID: request.ID, Confirmed: &confirmed}); !errors.Is(err, ErrInteractionSettled) {
		t.Fatalf("revoked answer = %v", err)
	}
	cancel() // Host revocation also cancels the generation-owned callback context.
	select {
	case settlement := <-result:
		if !settlement.response.Cancelled || settlement.reason != "unavailable" {
			t.Fatalf("revoked settlement = %#v", settlement)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked response did not release callback")
	}
}

func TestPluginBrokerMapsOptionMetadataToOriginalIndex(t *testing.T) {
	broker := newInteractionBroker(nil)
	result := make(chan PluginInteractionResult, 1)
	go func() {
		answer, err := broker.requestPlugin(t.Context(), "session_0123456789abcdef0123456789abcdef", PluginInteractionInput{Owner: PluginInteractionOwner{PluginID: "demo", Instance: "host:1"}, Kind: InteractionSelect, Title: "Select", Options: []PluginInteractionOption{{Label: "First"}, {Label: "Second"}}}, func() bool { return true })
		if err != nil {
			t.Error(err)
		}
		result <- answer
	}()
	deadline := time.Now().Add(time.Second)
	var request InteractionRequest
	for {
		pending := broker.snapshot()
		if len(pending) == 1 {
			request = pending[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no select request")
		}
		time.Sleep(time.Millisecond)
	}
	if err := broker.respond(InteractionResponse{RequestID: request.ID, SelectedOptionID: request.Options[1].ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case answer := <-result:
		if answer.OptionIndex != 1 || answer.Cancelled {
			t.Fatalf("selected answer = %#v", answer)
		}
	case <-time.After(time.Second):
		t.Fatal("selection remained blocked")
	}
}

func TestInteractionOwnershipAndPluginEmptyInput(t *testing.T) {
	request := pluginBrokerRequest()
	if err := validateInteractionRequest(request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*InteractionRequest){func(r *InteractionRequest) { r.RunID = "run" }, func(r *InteractionRequest) { r.ToolCallID = "tool" }, func(r *InteractionRequest) { r.Plugin = nil }, func(r *InteractionRequest) { r.Kind = InteractionGuided }, func(r *InteractionRequest) {
		r.Plugin = &PluginInteractionOwner{PluginID: "bad.plugin", Instance: "host:1"}
	}} {
		invalid := cloneInteractionRequest(request)
		mutate(&invalid)
		if err := validateInteractionRequest(invalid); err == nil {
			t.Fatalf("accepted ownership = %#v", invalid)
		}
	}
	request.Kind = InteractionInput
	empty := ""
	response := InteractionResponse{RequestID: request.ID, Value: &empty}
	if err := validateInteractionResponse(request, response); err != nil {
		t.Fatalf("empty plugin answer = %v", err)
	}
	request.Plugin = nil
	request.RunID = "run"
	request.ToolCallID = "tool"
	if err := validateInteractionResponse(request, response); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty model answer = %v", err)
	}
}
