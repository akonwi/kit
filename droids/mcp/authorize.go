package mcp

import (
	"context"
	"errors"

	"github.com/akonwi/kit/droids"
)

// AuthorizationRequest asks the user to authorize an MCP server in a browser.
type AuthorizationRequest struct {
	// Server is the namespace's name.
	Server string
	// URL is the authorization page to open.
	URL string
	// RedirectURI is where the authorization server sends the browser after
	// approval.
	RedirectURI string
}

// Authorizer asks the user who owns a tool call to authorize an MCP server
// and returns the URL the browser was redirected to.
type Authorizer func(ctx context.Context, call droids.ToolContext, request AuthorizationRequest) (string, error)

// ErrNoAuthorizer reports an authorization requested where no user can be
// asked: outside a running namespace tool call, from a conversation other
// than the authorizer's owner, or by a manager with no authorizer.
var ErrNoAuthorizer = errors.New("no user can authorize this MCP server here")

type authorizerContextKey struct{}

type callAuthorizer struct {
	call       droids.ToolContext
	owner      droids.ConversationID
	authorizer Authorizer
	// done closes when the tool call returns. Work the call started may
	// outlive it, but can no longer ask its user.
	done <-chan struct{}
}

// SetAuthorizer sets how the manager's namespace tool calls ask their user to
// authorize a server. Only calls in the owner conversation may ask; other
// conversations borrowing the manager's tools cannot. Calls already running
// keep the authorizer they started with.
func (m *Manager) SetAuthorizer(owner droids.ConversationID, authorizer Authorizer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.owner, m.authorizer = owner, authorizer
}

// callContext carries a namespace tool call's progress updates and its
// authorizer, bound to the call until done closes, to the namespace's
// transport.
func (m *Manager) callContext(ctx context.Context, call droids.ToolContext, update droids.ToolUpdate, done <-chan struct{}) context.Context {
	m.mu.Lock()
	owner, authorizer := m.owner, m.authorizer
	m.mu.Unlock()
	ctx = context.WithValue(ctx, progressContextKey{}, update)
	return context.WithValue(ctx, authorizerContextKey{}, callAuthorizer{call: call, owner: owner, authorizer: authorizer, done: done})
}

func callerOf(ctx context.Context) (callAuthorizer, bool) {
	caller, ok := ctx.Value(authorizerContextKey{}).(callAuthorizer)
	if !ok || caller.authorizer == nil || caller.call.ConversationID != caller.owner ||
		caller.call.TurnID == "" || caller.call.ToolCallID == "" {
		return callAuthorizer{}, false
	}
	select {
	case <-caller.done:
		return callAuthorizer{}, false
	default:
		return caller, true
	}
}

// CanAuthorize reports whether Authorize could ask a user in ctx. A
// transport checks it before discarding credentials it cannot replace.
func CanAuthorize(ctx context.Context) bool {
	_, ok := callerOf(ctx)
	return ok
}

// Authorize asks the user who owns the namespace tool call running in ctx to
// authorize a server, marking the namespace as authorizing. A transport's
// OAuth handler calls it. It returns ErrNoAuthorizer when no user can be
// asked.
func Authorize(ctx context.Context, request AuthorizationRequest) (string, error) {
	caller, ok := callerOf(ctx)
	if !ok {
		return "", ErrNoAuthorizer
	}
	PublishAuthorizationProgress(ctx, "Waiting for authorization of the "+request.Server+" MCP server")
	return caller.authorizer(ctx, caller.call, request)
}
