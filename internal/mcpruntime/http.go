package mcpruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/mcpconfig"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// httpFactory returns a factory that mints one fresh transport per call.
func (l *Launcher) httpFactory(server mcpconfig.Server) (func(context.Context) (sdkmcp.Transport, error), error) {
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		return nil, fmt.Errorf("MCP server %q has an unusable URL: %w", server.Name, err)
	}
	base := l.httpClient
	headers := maps.Clone(server.Headers)
	auth := cloneAuth(server.Auth)
	return func(ctx context.Context) (sdkmcp.Transport, error) {
		// Streamable transports are single-use. Giving each one its own round
		// tripper also gives its graceful DELETE one deadline shared by every
		// redirect hop.
		client := &http.Client{
			Transport: &scopedRoundTripper{
				base:       baseRoundTripper(base),
				origin:     endpoint,
				headers:    headers,
				auth:       auth,
				lookupEnv:  l.lookupEnv,
				closeGrace: l.httpCloseGrace,
			},
		}
		if base != nil {
			// Timeout is deliberately not copied: it bounds the whole exchange
			// including the response body, which would sever a streamable HTTP
			// session's long-lived SSE stream.
			client.Jar = base.Jar
			client.CheckRedirect = base.CheckRedirect
		}
		transport := &sdkmcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: client}
		if server.Auth != nil && server.Auth.Kind == mcpconfig.AuthOAuth {
			handler, err := l.oauthHandler(ctx, server.Name, server.URL, oauthHTTPClient(base))
			if err != nil {
				return nil, err
			}
			transport.OAuthHandler = handler
		}
		return transport, nil
	}, nil
}

// cloneAuth copies caller-owned credentials so a later mutation cannot change
// what an already-built factory sends.
func cloneAuth(auth *mcpconfig.Auth) *mcpconfig.Auth {
	if auth == nil {
		return nil
	}
	copied := *auth
	return &copied
}

func oauthHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{}
	}
	return &http.Client{
		Transport: base.Transport, CheckRedirect: base.CheckRedirect,
		Jar: base.Jar, Timeout: base.Timeout,
	}
}

func baseRoundTripper(client *http.Client) http.RoundTripper {
	if client != nil && client.Transport != nil {
		return client.Transport
	}
	return http.DefaultTransport
}

// scopedRoundTripper attaches configured headers and credentials only to the
// configured origin. It also bounds the SDK's graceful session DELETE. go-sdk
// cancels the rest of a streamable connection only after that request returns,
// so an unresponsive DELETE must not be allowed to deadlock runtime shutdown.
//
// Applying credentials per hop rather than per request is what makes this
// necessary: net/http strips sensitive headers when it follows a cross-origin
// redirect, but a round tripper runs again for the redirected request and would
// otherwise re-attach them to the new host.
type scopedRoundTripper struct {
	base       http.RoundTripper
	origin     *url.URL
	headers    map[string]string
	auth       *mcpconfig.Auth
	lookupEnv  func(string) (string, bool)
	closeGrace time.Duration

	closeMu       sync.Mutex
	closeDeadline time.Time
}

func (t *scopedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	var cleanup context.CancelFunc
	if request.Method == http.MethodDelete && t.closeGrace > 0 {
		ctx, cleanup = t.gracefulCloseContext(ctx)
	}

	// RoundTrip must not modify the caller's request.
	scoped := request.Clone(ctx)
	if sameOrigin(t.origin, request.URL) {
		for key, value := range t.headers {
			scoped.Header.Set(key, value)
		}
		if token := t.bearerToken(); token != "" && scoped.Header.Get("Authorization") == "" {
			scoped.Header.Set("Authorization", "Bearer "+token)
		}
	} else {
		// Go may retain sensitive headers when redirecting from a parent domain to
		// its subdomain. MCP resource credentials are stricter: never cross the
		// configured scheme+host+effective-port boundary.
		scoped.Header.Del("Authorization")
		for key := range t.headers {
			scoped.Header.Del(key)
		}
	}
	response, err := t.base.RoundTrip(scoped)
	if err != nil {
		if cleanup != nil {
			cleanup()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				// The remote consumed its entire graceful-close allowance. Treat
				// forced cancellation as successful local teardown so the SDK can
				// continue closing its reader and active requests.
				return &http.Response{
					Status:     "204 No Content",
					StatusCode: http.StatusNoContent,
					Header:     make(http.Header),
					Body:       http.NoBody,
					Request:    scoped,
				}, nil
			}
		}
		return nil, err
	}
	if cleanup == nil {
		return response, nil
	}
	if response.Body == nil {
		cleanup()
		return response, nil
	}
	// RoundTrip returns after response headers, not after the body is consumed.
	// Retain the derived deadline until the caller closes or finishes the body.
	response.Body = &cleanupBody{ReadCloser: response.Body, cleanup: cleanup}
	return response, nil
}

func (t *scopedRoundTripper) gracefulCloseContext(parent context.Context) (context.Context, context.CancelFunc) {
	t.closeMu.Lock()
	if t.closeDeadline.IsZero() {
		t.closeDeadline = time.Now().Add(t.closeGrace)
	}
	deadline := t.closeDeadline
	t.closeMu.Unlock()
	return context.WithDeadline(parent, deadline)
}

type cleanupBody struct {
	io.ReadCloser
	cleanup func()
	once    sync.Once
}

func (b *cleanupBody) Read(buffer []byte) (int, error) {
	count, err := b.ReadCloser.Read(buffer)
	if err != nil {
		b.once.Do(b.cleanup)
	}
	return count, err
}

func (b *cleanupBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cleanup)
	return err
}

// bearerToken resolves an environment-sourced token at request time so a token
// rotated after startup is picked up and is never retained in configuration.
func (t *scopedRoundTripper) bearerToken() string {
	if t.auth == nil || t.auth.Kind != mcpconfig.AuthBearer {
		return ""
	}
	if t.auth.BearerToken != "" {
		return t.auth.BearerToken
	}
	if t.auth.BearerTokenEnv == "" || t.lookupEnv == nil {
		return ""
	}
	value, _ := t.lookupEnv(t.auth.BearerTokenEnv)
	return value
}

// sameOrigin compares scheme, host, and effective port. A redirect to a
// different origin, including a downgrade to http, is not the configured server.
func sameOrigin(origin, candidate *url.URL) bool {
	if origin == nil || candidate == nil {
		return false
	}
	if !strings.EqualFold(origin.Scheme, candidate.Scheme) {
		return false
	}
	return strings.EqualFold(effectiveHost(origin), effectiveHost(candidate))
}

func effectiveHost(value *url.URL) string {
	host, port := value.Hostname(), value.Port()
	if port == "" {
		switch strings.ToLower(value.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return host + ":" + port
}
