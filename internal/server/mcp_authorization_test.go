//go:build darwin || linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	protocol "github.com/akonwi/kit/api/contract"
	"github.com/akonwi/kit/internal/apphome"
)

// oauthMCPServer is an MCP server behind a minimal OAuth authorization
// server: it registers a client, accepts code "granted", and serves the
// lookup tool to the token it issues.
func oauthMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	remote := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "docs", Version: "0.1.0"}, nil)
	sdkmcp.AddTool(remote, &sdkmcp.Tool{Name: "lookup", Description: "Look something up."},
		func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "looked up"}}}, nil, nil
		})
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return remote }, nil)
	var server *httptest.Server
	writeJSON := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp":
			writeJSON(w, map[string]any{"resource": server.URL + "/mcp", "authorization_servers": []string{server.URL}})
		case "/.well-known/oauth-authorization-server":
			writeJSON(w, map[string]any{
				"issuer": server.URL,
				// The person's browser opens this page; Kit never fetches it.
				"authorization_endpoint":                "https://auth.example.com/authorize",
				"token_endpoint":                        server.URL + "/token",
				"registration_endpoint":                 server.URL + "/register",
				"response_types_supported":              []string{"code"},
				"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
		case "/register":
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{"client_id": "kit-test", "redirect_uris": []string{"http://127.0.0.1:53173/mcp/callback"}, "token_endpoint_auth_method": "none"})
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != "granted" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{"access_token": "issued", "token_type": "Bearer", "expires_in": 3600})
		case "/mcp":
			if r.Header.Get("Authorization") != "Bearer issued" {
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+server.URL+`/.well-known/oauth-protected-resource/mcp"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			mcpHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestAClientAuthorizesAnMCPServerOverHTTP answers an MCP server's
// authorization the way clients do, with the redirect the browser reached.
func TestAClientAuthorizesAnMCPServerOverHTTP(t *testing.T) {
	remote := oauthMCPServer(t)
	paths := apphome.FromHome(filepath.Join(t.TempDir(), "kit"))
	providers := &deletionProviders{}
	client, stop := startDeletionDaemon(t, paths, providers)
	defer stop()
	cwd := t.TempDir()
	config := `{"mcpServers":{"docs":{"url":"` + remote.URL + `/mcp","auth":"oauth"}}}`
	if err := os.WriteFile(filepath.Join(cwd, ".mcp.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err := client.CreateSession(t.Context(), protocol.CreateSessionInput{CWD: cwd, Name: "MCP", Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	providers.toolName, providers.deleteArguments = "docs", `{"action":"call","tool":"lookup"}`
	providers.mu.Unlock()
	reservation, err := client.StartPrompt(t.Context(), owner.ID, "look it up")
	if err != nil {
		t.Fatal(err)
	}
	pending := waitForApproval(t, client, owner.ID)
	if pending.Kind != protocol.InteractionAuthorize || pending.Title != "Authorize the docs MCP server" ||
		pending.RedirectURI != "http://127.0.0.1:53173/mcp/callback" || pending.TurnID != reservation.TurnID || pending.ToolCallID == "" {
		t.Fatalf("authorization = %#v", pending)
	}
	page, err := url.Parse(pending.URL)
	if err != nil {
		t.Fatal(err)
	}
	redirect := "http://127.0.0.1:53173/mcp/callback?code=granted&state=" + url.QueryEscape(page.Query().Get("state"))
	if err := client.RespondInteraction(t.Context(), owner.ID, protocol.InteractionResponse{RequestID: pending.ID, Value: &redirect}); err != nil {
		t.Fatal(err)
	}
	waitForTurnStatus(t, client, owner.ID, reservation.TurnID, protocol.TurnStatusCompleted)
	providers.mu.Lock()
	result := providers.result
	providers.mu.Unlock()
	if result != "looked up" {
		t.Fatalf("tool result = %q", result)
	}
}
