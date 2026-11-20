package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/droids/mcp"
)

const maxRedirectURIBytes = 2 << 10

// ErrAuthorizationCancelled reports that the user declined to authorize.
var ErrAuthorizationCancelled = errors.New("the user cancelled authorization")

// attachInteractions gives a session runtime bundle the session's interaction
// tools and its MCP authorizer.
func attachInteractions(bundle *RuntimeBundle, sessionID string, interactions *sessionInteractions) {
	bundle.Prompt.Prompt += interactionPromptGuidance
	bundle.Tools = append(bundle.Tools, interactionTools()...)
	if bundle.MCP != nil {
		// Subagents borrow these namespaces but run in other conversations,
		// which no user is watching, so only the session's own can ask.
		bundle.MCP.SetAuthorizer(droids.ConversationID(sessionID), mcpAuthorizer(interactions))
	}
}

// mcpAuthorizer asks the session's user, through an authorize question on
// the tool call, to authorize an MCP server. The question belongs to the
// call's turn: steering declines it and the turn ending cancels it.
func mcpAuthorizer(interactions *sessionInteractions) mcp.Authorizer {
	return func(ctx context.Context, call droids.ToolContext, request mcp.AuthorizationRequest) (string, error) {
		if !validAuthorizationURL(request.URL) || !validRedirectURI(request.RedirectURI) {
			return "", fmt.Errorf("%w: authorization request is invalid", ErrInvalidInput)
		}
		settlement, err := interactions.ask(ctx, droids.Question{
			Kind:        droids.QuestionAuthorize,
			Title:       "Authorize the " + request.Server + " MCP server",
			URL:         request.URL,
			RedirectURI: request.RedirectURI,
		}, droids.AskOptions{Call: &call})
		if err != nil {
			return "", err
		}
		if settlement.Status == droids.InteractionAnswered && settlement.Answer.Value != nil {
			return *settlement.Answer.Value, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", ErrAuthorizationCancelled
	}
}

// validAuthorizationURL accepts an absolute https page.
func validAuthorizationURL(raw string) bool {
	if !boundedInteractionText(raw, maxInteractionDetailBytes, true) {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

// validRedirectURI accepts an http address on this machine's loopback
// interface with an explicit port, which a client can listen on.
func validRedirectURI(raw string) bool {
	if !boundedInteractionText(raw, maxRedirectURIBytes, true) {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
