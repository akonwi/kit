package session

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/droids/mcp"
)

const testAuthorizeSession = "session_0123456789abcdef0123456789abcdef"

func testAuthorizeCall() droids.ToolContext {
	return droids.ToolContext{ConversationID: testAuthorizeSession, TurnID: "turn_0123456789abcdef0123456789abcdef", ToolCallID: "call_1"}
}

func testAuthorizationRequest() mcp.AuthorizationRequest {
	return mcp.AuthorizationRequest{Server: "docs", URL: "https://auth.example.com/authorize?state=s", RedirectURI: "http://127.0.0.1:53173/mcp/callback"}
}

func TestMCPAuthorizerAsksTheUserAndReturnsTheirAnswer(t *testing.T) {
	t.Parallel()
	broker := newInteractionBroker(func(NewEvent) {})
	answer := make(chan string, 1)
	go func() {
		value, err := mcpAuthorizer(testAuthorizeSession, broker)(context.Background(), testAuthorizeCall(), testAuthorizationRequest())
		if err != nil {
			t.Errorf("authorize: %v", err)
		}
		answer <- value
	}()
	pending := waitForOnePending(t, broker)
	want := InteractionRequest{ID: pending.ID, SessionID: testAuthorizeSession, RunID: "turn_0123456789abcdef0123456789abcdef", ToolCallID: "call_1",
		Kind: InteractionAuthorize, Title: "Authorize the docs MCP server",
		URL: "https://auth.example.com/authorize?state=s", RedirectURI: "http://127.0.0.1:53173/mcp/callback", CreatedAt: pending.CreatedAt}
	if !reflect.DeepEqual(pending, want) {
		t.Fatalf("pending = %#v", pending)
	}
	redirect := "http://127.0.0.1:53173/mcp/callback?code=c&state=s"
	if err := broker.respond(InteractionResponse{RequestID: pending.ID, Value: &redirect}); err != nil {
		t.Fatal(err)
	}
	if got := <-answer; got != redirect {
		t.Fatalf("answer = %q", got)
	}
}

func TestMCPAuthorizerReportsCancellationAndTimeout(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var resolutions []string
	broker := newInteractionBroker(func(event NewEvent) {
		if event.Kind == EventInteractionResolved {
			mu.Lock()
			resolutions = append(resolutions, event.InteractionResolution)
			mu.Unlock()
		}
	})
	failed := make(chan error, 1)
	go func() {
		_, err := mcpAuthorizer(testAuthorizeSession, broker)(context.Background(), testAuthorizeCall(), testAuthorizationRequest())
		failed <- err
	}()
	pending := waitForOnePending(t, broker)
	if err := broker.respond(InteractionResponse{RequestID: pending.ID, Cancelled: true}); err != nil {
		t.Fatal(err)
	}
	if err := <-failed; !errors.Is(err, ErrAuthorizationCancelled) {
		t.Fatalf("cancelled error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := mcpAuthorizer(testAuthorizeSession, broker)(ctx, testAuthorizeCall(), testAuthorizationRequest()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed out error = %v", err)
	}
	if pending := broker.snapshot(); len(pending) != 0 {
		t.Fatalf("pending after timeout = %#v", pending)
	}
	mu.Lock()
	defer mu.Unlock()
	// Waiting too long ends the request without ending the run.
	if !reflect.DeepEqual(resolutions, []string{"user_cancelled", "unavailable"}) {
		t.Fatalf("resolutions = %q", resolutions)
	}
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

func waitForOnePending(t *testing.T, broker *interactionBroker) InteractionRequest {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pending := broker.snapshot(); len(pending) == 1 {
			return pending[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no interaction became pending")
	return InteractionRequest{}
}
