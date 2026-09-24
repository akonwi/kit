// Package mcpruntime builds agent-core MCP namespaces from validated Kit
// configuration.
//
// It owns transport construction, process supervision, and credential policy,
// as decided in docs/adrs/0028-own-mcp-transport-construction.md. The agent core
// owns when and why a namespace connects; this package owns how the connection
// is physically made.
package mcpruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	kitauth "github.com/akonwi/kit/internal/auth"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/mcp"
	"github.com/akonwi/kit/internal/mcpconfig"
)

// Launcher builds MCP namespace definitions for one runtime. The lifetime
// context bounds every server process the launcher starts.
//
// A launcher is immutable after construction and safe for concurrent use.
type Launcher struct {
	lifetime       context.Context
	lookupEnv      func(string) (string, bool)
	httpClient     *http.Client
	httpCloseGrace time.Duration
	oauthStore     MCPAuthStore
	openBrowser    func(context.Context, string) error
	oauthPrompt    func(context.Context, string, string, error) error
	oauthTimeout   time.Duration
}

// MCPAuthStore is the private credential surface needed by HTTP OAuth.
type MCPAuthStore interface {
	LoadMCPOAuth(context.Context, string) (kitauth.MCPOAuthRecord, error)
	SaveMCPOAuth(context.Context, string, string, kitauth.MCPOAuthSession) (string, error)
	DeleteMCPOAuth(context.Context, string) error
	DeleteMCPOAuthRevision(context.Context, string, string) (string, error)
}

// Option customizes a Launcher so tests and headless callers can supply an
// environment and HTTP stack without mutating process-global state.
type Option func(*Launcher)

// WithLookupEnv overrides environment variable resolution for credentials that
// configuration deliberately defers to connect time.
func WithLookupEnv(lookup func(string) (string, bool)) Option {
	return func(l *Launcher) {
		if lookup != nil {
			l.lookupEnv = lookup
		}
	}
}

// WithHTTPClient overrides the base HTTP client used for HTTP servers. The
// launcher still installs its own credential-scoping round tripper.
func WithHTTPClient(client *http.Client) Option {
	return func(l *Launcher) {
		if client != nil {
			l.httpClient = client
		}
	}
}

// WithMCPAuthStore enables durable OAuth for configured HTTP servers.
func WithMCPAuthStore(store MCPAuthStore) Option {
	return func(l *Launcher) { l.oauthStore = store }
}

// WithOAuthBrowser and WithOAuthPrompt override interactive OAuth side effects.
// Prompt is called after browser launch is attempted and can surface a manual
// authorization URL while the callback listener remains active.
func WithOAuthBrowser(open func(context.Context, string) error) Option {
	return func(l *Launcher) {
		if open != nil {
			l.openBrowser = open
		}
	}
}

func WithOAuthPrompt(prompt func(context.Context, string, string, error) error) Option {
	return func(l *Launcher) {
		if prompt != nil {
			l.oauthPrompt = prompt
		}
	}
}

// NewLauncher constructs a launcher bound to a runtime lifetime.
//
// The lifetime must outlive individual tool calls. The agent core invokes a
// transport factory with a per-call context that is cancelled once the call
// settles, so a server process tied to that context would not survive the call
// that started it.
func NewLauncher(lifetime context.Context, opts ...Option) (*Launcher, error) {
	if lifetime == nil {
		return nil, errors.New("MCP launcher requires a runtime lifetime context")
	}
	launcher := &Launcher{
		lifetime:       lifetime,
		lookupEnv:      os.LookupEnv,
		httpCloseGrace: 4 * time.Second,
		oauthTimeout:   time.Minute,
		openBrowser:    openOAuthBrowser,
		oauthPrompt: func(ctx context.Context, server, authorizationURL string, openErr error) error {
			message := "Authorize the " + server + " MCP server in your browser: " + authorizationURL
			if openErr != nil {
				message = "Browser opening failed; open this MCP authorization URL manually: " + authorizationURL
			}
			if !mcp.PublishAuthorizationProgress(ctx, message) {
				slog.Warn("MCP OAuth authorization required", "server", server, "browser_error", openErr, "authorization_url", authorizationURL)
			}
			return nil
		},
	}
	for _, opt := range opts {
		opt(launcher)
	}
	return launcher, nil
}

// Servers projects validated configuration into agent-core namespaces for one
// session cwd. Disabled servers are omitted. The returned order follows the
// configuration order, which mcpconfig sorts by name.
func (l *Launcher) Servers(cwd string, configured []mcpconfig.Server) ([]mcp.Server, error) {
	if l == nil {
		return nil, errors.New("MCP launcher is nil")
	}
	if !filepath.IsAbs(cwd) {
		return nil, fmt.Errorf("MCP servers require an absolute session cwd: %q", cwd)
	}
	servers := make([]mcp.Server, 0, len(configured))
	for _, server := range configured {
		if server.Disabled {
			continue
		}
		factory, err := l.transport(server, filepath.Clean(cwd))
		if err != nil {
			return nil, err
		}
		projected := mcp.Server{
			Name:          server.Name,
			Description:   description(server),
			Transport:     factory,
			ExecutionMode: droids.ModeDefault,
		}
		if server.Auth != nil && server.Auth.Kind == mcpconfig.AuthOAuth {
			name, endpoint := server.Name, server.URL
			projected.Logout = func(ctx context.Context) error { return l.LogoutOAuth(ctx, name, endpoint) }
			projected.OAuthSaved = func(ctx context.Context) (bool, error) {
				if l.oauthStore == nil {
					return false, nil
				}
				record, err := l.oauthStore.LoadMCPOAuth(ctx, oauthServerKey(name, endpoint))
				return record.Found, err
			}
		}
		servers = append(servers, projected)
	}
	return servers, nil
}

// OAuthSaved reports whether managed credentials exist without exposing them.
func (l *Launcher) OAuthSaved(ctx context.Context, name, endpoint string) (bool, error) {
	if l == nil || l.oauthStore == nil {
		return false, nil
	}
	record, err := l.oauthStore.LoadMCPOAuth(ctx, oauthServerKey(name, endpoint))
	return record.Found, err
}

// transport selects the stdio or HTTP factory for one configured server.
func (l *Launcher) transport(server mcpconfig.Server, cwd string) (mcp.TransportFactory, error) {
	switch server.Transport {
	case mcpconfig.TransportStdio:
		return l.stdioFactory(server, cwd), nil
	case mcpconfig.TransportHTTP:
		return l.httpFactory(server)
	default:
		return nil, fmt.Errorf("MCP server %q has unsupported transport %q", server.Name, server.Transport)
	}
}

// description prefers application-authored text over anything a remote server
// supplies, so an untrusted server cannot author its own tool instructions.
func description(server mcpconfig.Server) string {
	if server.Description != "" {
		return server.Description
	}
	return fmt.Sprintf("Tools provided by the %q MCP server.", server.Name)
}

// osEnviron is replaced in tests to keep environment projection deterministic.
var osEnviron = os.Environ
