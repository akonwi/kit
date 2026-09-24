package mcpconfig

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func TestLoadCandidateFilesFollowDocumentedPrecedence(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitProject, `{"mcpServers":{}}`)

	result := h.load()

	want := []File{
		{Source: SourceKitUser, Path: h.pathFor(SourceKitUser)},
		{Source: SourceSharedProject, Path: h.pathFor(SourceSharedProject)},
		{Source: SourceKitProject, Path: h.pathFor(SourceKitProject), Present: true, Loaded: true},
	}
	if !reflect.DeepEqual(result.Files, want) {
		t.Fatalf("files = %#v, want %#v", result.Files, want)
	}
	requireNoDiagnostics(t, result)
	if len(result.Servers) != 0 {
		t.Fatalf("expected no servers, got %v", serverNames(result))
	}
}

func TestLoadHigherPrecedenceFileOverridesScalarFields(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"docs":{
		"command":"user-docs","args":["--user"],"description":"user","disabled":true}}}`)
	h.write(SourceKitProject, `{"mcpServers":{"docs":{
		"command":"project-docs","disabled":false}}}`)

	server := h.mustServer(h.load(), "docs")

	if server.Command != "project-docs" {
		t.Errorf("command = %q, want %q", server.Command, "project-docs")
	}
	if server.Disabled {
		t.Error("disabled = true, want false from the higher-precedence file")
	}
	if got := []string{"--user"}; !reflect.DeepEqual(server.Args, got) {
		t.Errorf("args = %v, want %v retained from the lower-precedence file", server.Args, got)
	}
	if server.Description != "user" {
		t.Errorf("description = %q, want %q retained from the lower-precedence file", server.Description, "user")
	}
	if server.Source != SourceKitProject {
		t.Errorf("source = %q, want %q", server.Source, SourceKitProject)
	}
	if server.Path != h.pathFor(SourceKitProject) {
		t.Errorf("path = %q, want %q", server.Path, h.pathFor(SourceKitProject))
	}
}

func TestLoadDeepMergesEnvAndHeadersByKey(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"build":{
		"command":"server","env":{"SHARED":"user","USER_ONLY":"1"}}}}`)
	h.write(SourceSharedProject, `{"mcpServers":{"build":{
		"env":{"SHARED":"project","PROJECT_ONLY":"2"}}}}`)

	server := h.mustServer(h.load(), "build")

	want := map[string]string{"SHARED": "project", "USER_ONLY": "1", "PROJECT_ONLY": "2"}
	if !reflect.DeepEqual(server.Env, want) {
		t.Fatalf("env = %v, want %v", server.Env, want)
	}
}

func TestLoadResolvesStdioServer(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitProject, `{"mcpServers":{"everything":{
		"command":"npx","args":["-y","@modelcontextprotocol/server-everything"],
		"description":"demo","cwd":"~/work"}}}`)

	result := h.load()
	requireNoDiagnostics(t, result)
	server := h.mustServer(result, "everything")

	want := Server{
		Name:        "everything",
		Transport:   TransportStdio,
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-everything"},
		Env:         map[string]string{},
		Cwd:         filepath.Join(h.home, "work"),
		Description: "demo",
		Source:      SourceKitProject,
		Path:        h.pathFor(SourceKitProject),
	}
	if !reflect.DeepEqual(server, want) {
		t.Fatalf("server = %#v, want %#v", server, want)
	}
}

func TestLoadResolvesHTTPServerWithBearerHeader(t *testing.T) {
	h := newHarness(t)
	h.env["DOCS_TOKEN"] = "secret-value"
	h.write(SourceKitUser, `{"mcpServers":{"remote":{
		"url":"https://mcp.example.com/api","auth":"bearer","bearerToken":"${DOCS_TOKEN}",
		"headers":{"X-Tenant":"acme"}}}}`)

	result := h.load()
	requireNoDiagnostics(t, result)
	server := h.mustServer(result, "remote")

	if server.Transport != TransportHTTP {
		t.Fatalf("transport = %q, want %q", server.Transport, TransportHTTP)
	}
	if server.URL != "https://mcp.example.com/api" {
		t.Errorf("url = %q", server.URL)
	}
	wantHeaders := map[string]string{"X-Tenant": "acme", "Authorization": "Bearer secret-value"}
	if !reflect.DeepEqual(server.Headers, wantHeaders) {
		t.Errorf("headers = %v, want %v", server.Headers, wantHeaders)
	}
	wantAuth := &Auth{Kind: AuthBearer, BearerToken: "secret-value"}
	if !reflect.DeepEqual(server.Auth, wantAuth) {
		t.Errorf("auth = %#v, want %#v", server.Auth, wantAuth)
	}
}

func TestLoadAcceptsBaseURLAlias(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"remote":{"baseUrl":"https://example.com/mcp"}}}`)

	server := h.mustServer(h.load(), "remote")
	if server.URL != "https://example.com/mcp" {
		t.Fatalf("url = %q, want %q", server.URL, "https://example.com/mcp")
	}
}

func TestLoadExpandsVariableSyntaxes(t *testing.T) {
	h := newHarness(t)
	h.env["TOKEN"] = "abc"
	h.write(SourceKitUser, `{"mcpServers":{"x":{"command":"run","args":["$env:TOKEN","${TOKEN}","${MISSING:-fallback}"]}}}`)

	result := h.load()
	requireNoDiagnostics(t, result)
	server := h.mustServer(result, "x")

	want := []string{"abc", "abc", "fallback"}
	if !reflect.DeepEqual(server.Args, want) {
		t.Fatalf("args = %v, want %v", server.Args, want)
	}
}

func TestLoadReportsUnsetVariableWithoutFallback(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{"command":"run","env":{"KEY":"${MISSING}"}}}}`)

	result := h.load()
	server := h.mustServer(result, "x")

	if server.Env["KEY"] != "" {
		t.Errorf("env KEY = %q, want empty", server.Env["KEY"])
	}
	wantDiagnostic := Diagnostic{
		Source:  SourceKitUser,
		Path:    h.pathFor(SourceKitUser),
		Server:  "x",
		Field:   "env.KEY",
		Message: `environment variable "MISSING" is not set; expanded to an empty value`,
	}
	if !reflect.DeepEqual(result.Diagnostics, []Diagnostic{wantDiagnostic}) {
		t.Fatalf("diagnostics = %v, want %v", messages(result), wantDiagnostic.Error())
	}
}

func TestLoadDisabledServersAreRetainedButExcludedFromEnabled(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{
		"on":{"command":"a"},
		"off":{"command":"b","disabled":true}}}`)

	result := h.load()
	if got := serverNames(result); !reflect.DeepEqual(got, []string{"off", "on"}) {
		t.Fatalf("servers = %v, want sorted [off on]", got)
	}
	enabled := result.Enabled()
	if len(enabled) != 1 || enabled[0].Name != "on" {
		t.Fatalf("enabled = %v, want [on]", enabled)
	}
}

func TestLoadRequiresAbsoluteCwd(t *testing.T) {
	h := newHarness(t)
	loader, err := NewLoader(apphome.FromHome(h.kitHome), WithUserHome(h.home))
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	if _, err := loader.Load(context.Background(), "relative/path"); err == nil {
		t.Fatal("expected an error for a relative cwd")
	}
}

func TestLoadHonorsContextCancellation(t *testing.T) {
	h := newHarness(t)
	loader, err := NewLoader(apphome.FromHome(h.kitHome), WithUserHome(h.home))
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.Load(ctx, h.cwd); !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context canceled", err)
	}
}

func TestLoadIgnoresDirectoryAtConfigPath(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.pathFor(SourceSharedProject), 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	h.write(SourceKitProject, `{"mcpServers":{"x":{"command":"a"}}}`)

	result := h.load()
	h.mustServer(result, "x")
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "MCP config must be a regular file" {
		t.Fatalf("diagnostics = %v, want a regular-file diagnostic", messages(result))
	}
}
