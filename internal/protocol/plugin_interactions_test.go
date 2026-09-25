package protocol

import (
	"encoding/json"
	"testing"
)

func TestPluginInteractionWireOwnershipAndPresentation(t *testing.T) {
	yes := true
	request := InteractionRequest{ID: "interaction_0123456789abcdef0123456789abcdef", SessionID: "session_0123456789abcdef0123456789abcdef", Plugin: &PluginInteractionOwner{PluginID: "demo", Instance: "host:1"}, Kind: InteractionConfirm, Title: "Continue?", ConfirmLabel: "Proceed", CancelLabel: "Stop", DefaultValue: &yes, CreatedAt: "2025-01-01T00:00:00Z"}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded InteractionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	if decoded.Plugin == nil || *decoded.Plugin != *request.Plugin || decoded.DefaultValue == nil || !*decoded.DefaultValue || decoded.ConfirmLabel != "Proceed" || decoded.CancelLabel != "Stop" {
		t.Fatalf("wire projection = %s", data)
	}
	for _, mutate := range []func(*InteractionRequest){func(r *InteractionRequest) { r.RunID = "run_0123456789abcdef0123456789abcdef" }, func(r *InteractionRequest) { r.ToolCallID = "tool_0123456789abcdef0123456789abcdef" }, func(r *InteractionRequest) { r.Plugin = nil }, func(r *InteractionRequest) { r.Kind = InteractionGuided }, func(r *InteractionRequest) { r.InitialValue = "misplaced" }, func(r *InteractionRequest) { r.Filterable = &yes }, func(r *InteractionRequest) { r.Placeholder = "misplaced" }} {
		invalid := request
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted invalid request = %#v", invalid)
		}
	}
	empty := ""
	if err := (InteractionResponse{RequestID: request.ID, Value: &empty}).Validate(); err != nil {
		t.Fatalf("empty answer wire = %v", err)
	}
}
