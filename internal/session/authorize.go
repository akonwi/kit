package session

import (
	"context"
	"errors"
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
// tools and lets its MCP namespaces ask the session's user to authorize.
func attachInteractions(bundle *RuntimeBundle, sessionID string, broker *interactionBroker) {
	bundle.Prompt.Prompt += interactionPromptGuidance
	bundle.Tools = append(bundle.Tools, interactionTools(sessionID, broker)...)
	if bundle.MCP != nil {
		// Subagents borrow these namespaces but run in other conversations,
		// which no user is watching, so only the session's own can ask.
		bundle.MCP.SetAuthorizer(droids.ConversationID(sessionID), mcpAuthorizer(sessionID, broker))
	}
}

// mcpAuthorizer asks the session's user, through an authorize interaction
// owned by the tool call, to authorize an MCP server.
func mcpAuthorizer(sessionID string, broker *interactionBroker) mcp.Authorizer {
	return func(ctx context.Context, call droids.ToolContext, request mcp.AuthorizationRequest) (string, error) {
		settlement, err := executeInteraction(ctx, sessionID, broker, call, InteractionRequest{
			Kind:        InteractionAuthorize,
			Title:       "Authorize the " + request.Server + " MCP server",
			URL:         request.URL,
			RedirectURI: request.RedirectURI,
		})
		if err != nil {
			return "", err
		}
		if settlement.response.Cancelled {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "", ErrAuthorizationCancelled
		}
		return *settlement.response.Value, nil
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
