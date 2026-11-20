package session

import (
	"errors"
	"testing"
)

func pluginInteractionRequest() InteractionRequest {
	request := testInteractionRequest("interaction_0123456789abcdef0123456789abcdef")
	request.RunID = ""
	request.ToolCallID = ""
	request.Plugin = &PluginInteractionOwner{PluginID: "demo", Instance: "host:1"}
	return request
}

func TestInteractionOwnershipAndPluginEmptyInput(t *testing.T) {
	request := pluginInteractionRequest()
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
