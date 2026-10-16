package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akonwi/kit/droids"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var ownerCall = droids.ToolContext{ConversationID: "conversation_1", TurnID: "turn_1", ToolCallID: "call_1"}

// A transport's OAuth handler reaches the authorizer of the tool call that
// is connecting, with that call's identity.
func TestAuthorizeAsksTheToolCallsAuthorizer(t *testing.T) {
	var asked droids.ToolContext
	var request AuthorizationRequest
	var stateWhileAsking string
	var progress []string
	authorized := make(chan string, 1)
	manager, err := NewManager(Server{Name: "docs", Transport: func(ctx context.Context) (sdkmcp.Transport, error) {
		value, err := Authorize(ctx, AuthorizationRequest{Server: "docs", URL: "https://auth.example.com/authorize", RedirectURI: "http://127.0.0.1:53173/mcp/callback"})
		if err != nil {
			return nil, err
		}
		authorized <- value
		return nil, errors.New("stop after authorizing")
	}})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetAuthorizer("conversation_1", func(_ context.Context, call droids.ToolContext, req AuthorizationRequest) (string, error) {
		asked, request = call, req
		stateWhileAsking = manager.Statuses(t.Context())[0].State
		return "http://127.0.0.1:53173/mcp/callback?code=c&state=s", nil
	})
	_, _ = manager.namespaces[0].execute(manager.callContext(t.Context(), ownerCall, func(delta droids.ToolResultDelta) {
		for _, content := range delta.Content {
			progress = append(progress, content.(droids.TextContent).Text)
		}
	}, nil), namespaceRequest{Action: "list"})
	if got := <-authorized; got != "http://127.0.0.1:53173/mcp/callback?code=c&state=s" {
		t.Fatalf("authorized = %q", got)
	}
	if asked != ownerCall || request.Server != "docs" || request.URL != "https://auth.example.com/authorize" {
		t.Fatalf("asked %+v with %+v", asked, request)
	}
	if stateWhileAsking != "authorizing" || len(progress) != 1 || progress[0] != "Waiting for authorization of the docs MCP server" {
		t.Fatalf("while asking: state %q, progress %q", stateWhileAsking, progress)
	}
}

func TestOnlyALiveCallInTheOwnerConversationCanAuthorize(t *testing.T) {
	manager, err := NewManager(Server{Name: "docs", Transport: func(context.Context) (sdkmcp.Transport, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	refused := func(name string, ctx context.Context) {
		t.Helper()
		if CanAuthorize(ctx) {
			t.Fatalf("%s: CanAuthorize", name)
		}
		if _, err := Authorize(ctx, AuthorizationRequest{Server: "docs"}); !errors.Is(err, ErrNoAuthorizer) {
			t.Fatalf("%s: Authorize() = %v", name, err)
		}
	}
	refused("outside a tool call", t.Context())
	refused("without an authorizer", manager.callContext(t.Context(), ownerCall, nil, nil))

	manager.SetAuthorizer("conversation_1", func(context.Context, droids.ToolContext, AuthorizationRequest) (string, error) {
		t.Fatal("asked")
		return "", nil
	})
	// A subagent borrowing the namespace runs in another conversation.
	borrowed := ownerCall
	borrowed.ConversationID = "subagent_1"
	refused("another conversation", manager.callContext(t.Context(), borrowed, nil, nil))
	done := make(chan struct{})
	ctx := manager.callContext(t.Context(), ownerCall, nil, done)
	if !CanAuthorize(ctx) {
		t.Fatal("the owner's running call cannot authorize")
	}
	close(done)
	refused("a finished call", ctx)
}

// Logout cancels a connection waiting for its user to authorize rather than
// waiting minutes for it.
func TestLogoutCancelsAPendingAuthorization(t *testing.T) {
	asking := make(chan struct{})
	manager, err := NewManager(Server{
		Name:   "docs",
		Logout: func(context.Context) error { return nil },
		Transport: func(ctx context.Context) (sdkmcp.Transport, error) {
			_, err := Authorize(ctx, AuthorizationRequest{Server: "docs"})
			return nil, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetAuthorizer("conversation_1", func(ctx context.Context, _ droids.ToolContext, _ AuthorizationRequest) (string, error) {
		close(asking)
		<-ctx.Done()
		return "", ctx.Err()
	})
	executed := make(chan error, 1)
	go func() {
		_, err := manager.namespaces[0].execute(manager.callContext(t.Context(), ownerCall, nil, nil), namespaceRequest{Action: "list"})
		executed <- err
	}()
	<-asking
	logout, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := manager.Logout(logout, "docs"); err != nil {
		t.Fatalf("Logout() = %v", err)
	}
	if err := <-executed; !errors.Is(err, context.Canceled) {
		t.Fatalf("pending call = %v", err)
	}
}
