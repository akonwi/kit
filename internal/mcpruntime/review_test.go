//go:build darwin || linux

package mcpruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/mcpconfig"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// go-sdk v1.7.0 returns from Client.Connect without closing the connection on
// some post-handshake failures, so the next connection attempt must reclaim the
// abandoned process rather than accumulate one server per failed call.
func TestStdioAbandonedProcessIsReclaimedOnNextAttempt(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "server.lock")
	t.Setenv(helperLock, lockPath)
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{stdioServer(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	// Simulate the SDK abandoning a connected transport without closing it.
	transport, err := servers[0].Transport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	requireLockHeld(t, lockPath)

	// The agent core retries by calling the factory again.
	if _, err := servers[0].Transport(ctx); err != nil {
		t.Fatal(err)
	}
	requireLockReleased(t, ctx, lockPath)
}

// A stdio server inherits Kit's environment, so anything it echoes to stderr
// could copy a provider credential into a log or UI error surface.
func TestStdioDiagnosticsRedactInheritedCredentials(t *testing.T) {
	environment := []string{
		"ANTHROPIC_API_KEY=sk-ant-supersecretvalue",
		"HOME=/Users/example",
		"SHELL=/bin/zsh",
	}
	raw := "failed to start: used sk-ant-supersecretvalue against /Users/example\n"

	got := sanitizeDiagnostic(raw, environment)

	if strings.Contains(got, "sk-ant-supersecretvalue") {
		t.Fatalf("diagnostic leaked a credential: %q", got)
	}
	want := "failed to start: used [redacted ANTHROPIC_API_KEY] against /Users/example"
	if got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
}

func TestSanitizeDiagnosticStripsControlCharacters(t *testing.T) {
	got := sanitizeDiagnostic("progress\x1b[31mred\x07\nsecond line\ttabbed", nil)

	want := "progress[31mred\nsecond line\ttabbed"
	if got != want {
		t.Fatalf("diagnostic = %q, want %q", got, want)
	}
}

func TestSanitizeDiagnosticKeepsShortValuesIntact(t *testing.T) {
	// A short value such as a port must not corrupt unrelated output.
	got := sanitizeDiagnostic("listening on 8080", []string{"SERVICE_TOKEN=8080"})

	if got != "listening on 8080" {
		t.Fatalf("diagnostic = %q, want the short value preserved", got)
	}
}

func TestSanitizeDiagnosticBoundsLength(t *testing.T) {
	got := sanitizeDiagnostic(strings.Repeat("x", maxDiagnosticBytes*2), nil)

	if len(got) > maxDiagnosticBytes+3 {
		t.Fatalf("diagnostic length = %d, want at most %d", len(got), maxDiagnosticBytes+3)
	}
	if !strings.HasPrefix(got, "...") {
		t.Fatalf("diagnostic = %q, want a truncation marker", got[:16])
	}
}

// Mutating configuration after Servers returns must not change what an already
// built factory launches.
func TestServersCloneCallerOwnedConfiguration(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	command, args := helperExecutable(t)
	configured := mcpconfig.Server{
		Name:      "mutable",
		Transport: mcpconfig.TransportStdio,
		Command:   command,
		Args:      args,
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{configured})
	if err != nil {
		t.Fatal(err)
	}
	configured.Args[0] = "-test.run=^TestDoesNotExist$"

	transport, err := servers[0].Transport(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stdio, ok := transport.(*stdioTransport)
	if !ok {
		t.Fatalf("transport type = %T", transport)
	}
	if stdio.spec.Args[0] != "-test.run=^TestMCPServerHelper$" {
		t.Fatalf("args = %v, want the value captured at projection time", stdio.spec.Args)
	}
}

func TestHTTPFactoryClonesHeadersAndAuth(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	auth := &mcpconfig.Auth{Kind: mcpconfig.AuthBearer, BearerToken: "original"}
	headers := map[string]string{"X-Tenant": "acme"}
	factory, err := launcher.httpFactory(mcpconfig.Server{
		Name:      "remote",
		Transport: mcpconfig.TransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   headers,
		Auth:      auth,
	})
	if err != nil {
		t.Fatal(err)
	}
	headers["X-Tenant"] = "mutated"
	auth.BearerToken = "mutated"

	transport, err := factory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	streamable, ok := transport.(*sdkmcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("transport type = %T", transport)
	}
	scoped, ok := streamable.HTTPClient.Transport.(*scopedRoundTripper)
	if !ok {
		t.Fatalf("round tripper type = %T", streamable.HTTPClient.Transport)
	}
	if scoped.headers["X-Tenant"] != "acme" {
		t.Errorf("header = %q, want the value captured at projection time", scoped.headers["X-Tenant"])
	}
	if scoped.auth.BearerToken != "original" {
		t.Errorf("token = %q, want the value captured at projection time", scoped.auth.BearerToken)
	}
}

func TestHTTPFactoryPreservesCallerRedirectPolicy(t *testing.T) {
	refused := errors.New("redirects are not permitted")
	launcher, err := NewLauncher(t.Context(), WithHTTPClient(&http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return refused },
	}))
	if err != nil {
		t.Fatal(err)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	defer target.Close()

	factory, err := launcher.httpFactory(mcpconfig.Server{Name: "remote", Transport: mcpconfig.TransportHTTP, URL: target.URL})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := factory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	client := transport.(*sdkmcp.StreamableClientTransport).HTTPClient
	if _, err := client.Get(target.URL); !strings.Contains(err.Error(), refused.Error()) {
		t.Fatalf("err = %v, want the caller's redirect policy to apply", err)
	}
}

// The Connection contract requires Close to be safe concurrently and to unblock
// a waiting Read.
func TestProcessConnectionCloseIsConcurrentAndUnblocksRead(t *testing.T) {
	session := connect(t, t.Context(), stdioServer(t), t.TempDir())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if got := probe(t, ctx, session, ""); got != helperReplyOK {
		t.Fatalf("probe = %q", got)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := session.Close(); err != nil {
				t.Errorf("close = %v", err)
			}
		})
	}
	done := make(chan struct{})
	go func() { defer close(done); wg.Wait() }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("concurrent Close did not settle")
	}
}

// requireLockHeld asserts the helper process is running and holding its lock.
func requireLockHeld(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := flockNonBlocking(file); err != nil {
			return
		}
		_ = unlock(file)
		time.Sleep(time.Millisecond)
	}
	t.Fatal("server never acquired its lock")
}

func flockNonBlocking(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
