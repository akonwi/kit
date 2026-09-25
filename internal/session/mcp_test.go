//go:build darwin || linux

package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/mcpconfig"
	"github.com/akonwi/kit/internal/mcpruntime"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/systemprompt"
)

// mcpProject writes one MCP configuration file into a session cwd.
func mcpProject(t *testing.T, contents string) string {
	t.Helper()
	cwd := t.TempDir()
	path := filepath.Join(cwd, ".mcp.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return cwd
}

func mcpBundleBuilder(t *testing.T, cwd string) RuntimeBundleBuilder {
	t.Helper()
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	loader, err := mcpconfig.NewLoader(testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := mcpruntime.NewLauncher(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewRuntimeBundleBuilder(RuntimeBundleOptions{
		Core:        "core prompt",
		Registry:    registry,
		MCPLoader:   loader,
		MCPLauncher: launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func buildMCPBundle(t *testing.T, cwd string) RuntimeBundle {
	t.Helper()
	builder := mcpBundleBuilder(t, cwd)
	bundle, err := builder.Build(t.Context(), SessionRecord{ID: "session_1", CWD: cwd}, func() string { return cwd })
	if err != nil {
		t.Fatalf("build bundle: %v", err)
	}
	t.Cleanup(func() {
		if bundle.MCP != nil {
			_ = mcpruntime.CloseManager(context.Background(), bundle.MCP)
		}
	})
	return bundle
}

// baselineToolCount builds the same session without MCP configured, so a test
// can assert exactly how many tools the MCP namespaces contributed.
func baselineToolCount(t *testing.T, cwd string) int {
	t.Helper()
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewRuntimeBundleBuilder(RuntimeBundleOptions{Core: "core prompt", Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := builder.Build(t.Context(), SessionRecord{ID: "session_1", CWD: cwd}, func() string { return cwd })
	if err != nil {
		t.Fatal(err)
	}
	return len(bundle.Tools)
}

func testPaths(t *testing.T) apphome.Paths {
	t.Helper()
	return apphome.FromHome(t.TempDir())
}

func TestRuntimeBundleExposesConfiguredMCPNamespaceAsATool(t *testing.T) {
	cwd := mcpProject(t, `{"mcpServers":{"docs":{"command":"docs-server"}}}`)

	bundle := buildMCPBundle(t, cwd)

	if bundle.MCP == nil {
		t.Fatal("bundle has no MCP manager")
	}
	if got := len(bundle.MCP.Tools()); got != 1 {
		t.Fatalf("namespace tools = %d, want 1", got)
	}
	baseline := baselineToolCount(t, cwd)
	if got := len(bundle.Tools); got != baseline+1 {
		t.Fatalf("bundle tools = %d, want %d including the docs namespace", got, baseline+1)
	}
}

func TestRuntimeBundleOmitsMCPWhenNoServersAreEnabled(t *testing.T) {
	cwd := mcpProject(t, `{"mcpServers":{"docs":{"command":"docs-server","disabled":true}}}`)

	bundle := buildMCPBundle(t, cwd)

	if bundle.MCP != nil {
		t.Fatal("bundle created a manager with no enabled servers")
	}
}

// A malformed configuration file must degrade to a diagnostic rather than
// making the session unusable.
func TestRuntimeBundleReportsMCPConfigurationDiagnostics(t *testing.T) {
	cwd := mcpProject(t, `{"mcpServers":{"broken":{"description":"no transport"}}}`)

	bundle := buildMCPBundle(t, cwd)

	if bundle.MCP != nil {
		t.Fatal("an unusable server produced a manager")
	}
	var found *systemprompt.Diagnostic
	for index, diagnostic := range bundle.Prompt.Diagnostics {
		if diagnostic.Code == "mcp.configuration" {
			found = &bundle.Prompt.Diagnostics[index]
		}
	}
	if found == nil {
		t.Fatalf("diagnostics = %#v, want an mcp.configuration diagnostic", bundle.Prompt.Diagnostics)
	}
	if found.Severity != systemprompt.DiagnosticWarning {
		t.Errorf("severity = %v, want a warning", found.Severity)
	}
	if !strings.Contains(found.Message, `set "command" for a stdio server`) {
		t.Errorf("message = %q, want the loader's actionable text", found.Message)
	}
}

func TestRuntimeBundleWithoutMCPConfigurationBuildsNoManager(t *testing.T) {
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewRuntimeBundleBuilder(RuntimeBundleOptions{Core: "core prompt", Registry: registry})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	bundle, err := builder.Build(t.Context(), SessionRecord{ID: "session_1", CWD: cwd}, func() string { return cwd })
	if err != nil {
		t.Fatal(err)
	}
	if bundle.MCP != nil {
		t.Fatal("an unconfigured builder created a manager")
	}
}

func TestRuntimeBundleOptionsRequireLoaderAndLauncherTogether(t *testing.T) {
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	loader, err := mcpconfig.NewLoader(testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewRuntimeBundleBuilder(RuntimeBundleOptions{Core: "core", Registry: registry, MCPLoader: loader})
	if err == nil || !strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("err = %v, want a paired-configuration error", err)
	}
}

// The manager owns processes, so a cloned bundle must reference the same
// manager rather than a copy that a second runtime would close independently.
func TestCloneRuntimeBundleSharesTheMCPManager(t *testing.T) {
	cwd := mcpProject(t, `{"mcpServers":{"docs":{"command":"docs-server"}}}`)
	bundle := buildMCPBundle(t, cwd)

	cloned := cloneRuntimeBundle(bundle)

	if cloned.MCP != bundle.MCP {
		t.Fatal("clone produced a different manager reference")
	}
}
