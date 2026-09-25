//go:build darwin || linux

package mcpruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/mcpconfig"
)

func TestNewLauncherRequiresLifetime(t *testing.T) {
	//lint:ignore SA1012 the nil lifetime is the condition under test.
	if _, err := NewLauncher(nil); err == nil {
		t.Fatal("expected an error for a nil lifetime context")
	}
}

func TestServersRequiresAbsoluteCwd(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := launcher.Servers("relative", nil); err == nil {
		t.Fatal("expected an error for a relative cwd")
	}
}

func TestServersOmitsDisabledServers(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{
		{Name: "on", Transport: mcpconfig.TransportStdio, Command: "a"},
		{Name: "off", Transport: mcpconfig.TransportStdio, Command: "b", Disabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Name != "on" {
		t.Fatalf("servers = %#v, want only the enabled server", servers)
	}
	if servers[0].ExecutionMode != droids.ModeDefault {
		t.Errorf("execution mode = %q, want the default", servers[0].ExecutionMode)
	}
	if servers[0].Transport == nil {
		t.Error("transport factory is nil")
	}
}

// A remote server must not author its own tool instructions, so an unconfigured
// description falls back to application-authored text.
func TestServersUseApplicationAuthoredDescriptions(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	servers, err := launcher.Servers(t.TempDir(), []mcpconfig.Server{
		{Name: "configured", Transport: mcpconfig.TransportStdio, Command: "a", Description: "Search the design docs."},
		{Name: "bare", Transport: mcpconfig.TransportStdio, Command: "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := servers[0].Description; got != "Search the design docs." {
		t.Errorf("configured description = %q", got)
	}
	if got := servers[1].Description; got != `Tools provided by the "bare" MCP server.` {
		t.Errorf("fallback description = %q", got)
	}
}

func TestServersRejectUnsupportedTransport(t *testing.T) {
	launcher, err := NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = launcher.Servers(t.TempDir(), []mcpconfig.Server{{Name: "odd", Transport: "carrier-pigeon"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported transport") {
		t.Fatalf("err = %v, want an unsupported transport error", err)
	}
}

func TestStdioServerDefaultsToSessionCwd(t *testing.T) {
	cwd := t.TempDir()
	session := connect(t, t.Context(), stdioServer(t), cwd)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	got := probe(t, ctx, session, "cwd")

	// macOS commonly resolves /var to /private/var in getcwd.
	physical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got != physical && got != cwd {
		t.Fatalf("server cwd = %q, want %q", got, cwd)
	}
}

func TestStdioServerUsesConfiguredCwdOverSessionCwd(t *testing.T) {
	configured := t.TempDir()
	server := stdioServer(t)
	server.Cwd = configured
	session := connect(t, t.Context(), server, t.TempDir())
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	got := probe(t, ctx, session, "cwd")

	physical, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatal(err)
	}
	if got != physical && got != configured {
		t.Fatalf("server cwd = %q, want the configured %q", got, configured)
	}
}

// ADR 0028 records that a stdio server inherits Kit's environment, overlaid
// with its configured env.
func TestStdioServerEnvironmentInheritanceAndOverlay(t *testing.T) {
	t.Setenv(helperProbe, "inherited-value")
	for _, testCase := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "inherits when unconfigured", want: "inherited-value"},
		{name: "configured value overrides", env: map[string]string{helperProbe: "configured-value"}, want: "configured-value"},
		{name: "unrelated keys keep inheritance", env: map[string]string{"KIT_MCPRUNTIME_OTHER": "x"}, want: "inherited-value"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := stdioServer(t)
			server.Env = testCase.env
			session := connect(t, t.Context(), server, t.TempDir())
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			if got := probe(t, ctx, session, "env"); got != testCase.want {
				t.Fatalf("environment = %q, want %q", got, testCase.want)
			}
		})
	}
}
