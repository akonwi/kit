package session

import (
	"errors"
	"testing"

	"github.com/akonwi/kit/droids/mcp"
)

func testAuthorizationRequest() mcp.AuthorizationRequest {
	return mcp.AuthorizationRequest{Server: "docs", URL: "https://auth.example.com/authorize?state=s", RedirectURI: "http://127.0.0.1:53173/mcp/callback"}
}

func TestAuthorizeInteractionValidation(t *testing.T) {
	t.Parallel()
	valid := testInteractionRequest("interaction_0123456789abcdef0123456789abcdef")
	valid.Kind, valid.URL, valid.RedirectURI = InteractionAuthorize, "https://auth.example.com/authorize", "http://127.0.0.1:53173/mcp/callback"
	if err := validateInteractionRequest(valid); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	for _, redirect := range []string{"http://localhost:53173/cb", "http://[::1]:53173/cb"} {
		request := valid
		request.RedirectURI = redirect
		if err := validateInteractionRequest(request); err != nil {
			t.Fatalf("redirect %q: %v", redirect, err)
		}
	}
	for name, change := range map[string]func(*InteractionRequest){
		"page over http":        func(r *InteractionRequest) { r.URL = "http://auth.example.com/authorize" },
		"page with a scheme":    func(r *InteractionRequest) { r.URL = "javascript:alert(1)" },
		"redirect elsewhere":    func(r *InteractionRequest) { r.RedirectURI = "http://example.com:53173/cb" },
		"redirect over https":   func(r *InteractionRequest) { r.RedirectURI = "https://127.0.0.1:53173/cb" },
		"redirect with no port": func(r *InteractionRequest) { r.RedirectURI = "http://127.0.0.1/cb" },
		"plugin owned": func(r *InteractionRequest) {
			r.RunID, r.ToolCallID, r.Plugin = "", "", &PluginInteractionOwner{PluginID: "demo", Instance: "one"}
		},
		"fields on input": func(r *InteractionRequest) { r.Kind = InteractionInput },
	} {
		request := valid
		change(&request)
		if err := validateInteractionRequest(request); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
	empty := ""
	if err := validateInteractionResponse(valid, InteractionResponse{RequestID: valid.ID, Value: &empty}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty answer error = %v", err)
	}
}
