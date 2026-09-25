package mcpconfig

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

// A higher-precedence file must be able to drop an inherited credential rather
// than silently forwarding it to a newly configured endpoint.
func TestLoadNullClearsInheritedBearerCredential(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"remote":{
		"url":"https://user.example.com","auth":"bearer","bearerToken":"user-secret"}}}`)
	h.write(SourceKitProject, `{"mcpServers":{"remote":{
		"url":"https://project.example.com","auth":null,"bearerToken":null}}}`)

	result := h.load()
	requireNoDiagnostics(t, result)
	server := h.mustServer(result, "remote")

	if server.URL != "https://project.example.com" {
		t.Fatalf("url = %q", server.URL)
	}
	if server.Auth != nil {
		t.Fatalf("auth = %#v, want nil after an explicit null", server.Auth)
	}
	if got, ok := server.Headers["Authorization"]; ok {
		t.Fatalf("Authorization = %q, want the inherited credential to be dropped", got)
	}
	if len(server.Headers) != 0 {
		t.Fatalf("headers = %v, want empty", server.Headers)
	}
}

func TestLoadNullClearsInheritedScalarsAndMaps(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{
		"command":"server","args":["--verbose"],"description":"user","disabled":true,
		"env":{"A":"1","B":"2"}}}}`)
	h.write(SourceKitProject, `{"mcpServers":{"x":{
		"args":null,"description":null,"disabled":null,"env":null}}}`)

	server := h.mustServer(h.load(), "x")

	if len(server.Args) != 0 {
		t.Errorf("args = %v, want empty", server.Args)
	}
	if server.Description != "" {
		t.Errorf("description = %q, want empty", server.Description)
	}
	if server.Disabled {
		t.Error("disabled = true, want false")
	}
	if len(server.Env) != 0 {
		t.Errorf("env = %v, want empty", server.Env)
	}
	if server.Command != "server" {
		t.Errorf("command = %q, want the uncleared inherited value", server.Command)
	}
}

func TestLoadNullEnvClearsThenAcceptsNewKeys(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{"command":"a","env":{"OLD":"1"}}}}`)
	h.write(SourceSharedProject, `{"mcpServers":{"x":{"env":null}}}`)
	h.write(SourceKitProject, `{"mcpServers":{"x":{"env":{"NEW":"2"}}}}`)

	server := h.mustServer(h.load(), "x")

	want := map[string]string{"NEW": "2"}
	if !reflect.DeepEqual(server.Env, want) {
		t.Fatalf("env = %v, want %v", server.Env, want)
	}
}

func TestLoadRejectsCommandThatExpandsToNothing(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{"command":"${MISSING}"}}}`)

	result := h.load()

	if _, ok := result.Lookup("x"); ok {
		t.Fatal("expected no server for a command that expands to empty")
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %v, want an unset-variable and an empty-command diagnostic", messages(result))
	}
	if got := result.Diagnostics[1].Message; got != "ignored: resolved to an empty command" {
		t.Fatalf("message = %q", got)
	}
}

func TestLoadRejectsURLThatExpandsToNothing(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{"url":"${MISSING:-}"}}}`)

	result := h.load()

	if _, ok := result.Lookup("x"); ok {
		t.Fatal("expected no server for a url that expands to empty")
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "ignored: resolved to an empty URL" {
		t.Fatalf("diagnostics = %v, want an empty-URL diagnostic", messages(result))
	}
}

func TestNewLoaderRejectsRelativeUserHome(t *testing.T) {
	h := newHarness(t)
	_, err := NewLoader(apphome.FromHome(h.kitHome), WithUserHome("relative/home"))
	if err == nil {
		t.Fatal("expected an error for a relative user home")
	}
	if !strings.Contains(err.Error(), "user home must be absolute") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewLoaderRejectsRelativeConfigPath(t *testing.T) {
	if _, err := NewLoader(apphome.Paths{MCPConfig: "mcp.json"}); err == nil {
		t.Fatal("expected an error for a relative MCP config path")
	}
}

// A large low-precedence file must not prevent a project file from contributing.
func TestLoadProjectServersDisplaceLowerPrecedenceAtTheCeiling(t *testing.T) {
	h := newHarness(t)
	entries := make([]string, 0, MaxServers)
	for index := range MaxServers {
		entries = append(entries, fmt.Sprintf(`"user-%03d":{"command":"a"}`, index))
	}
	h.write(SourceKitUser, `{"mcpServers":{`+strings.Join(entries, ",")+`}}`)
	h.write(SourceKitProject, `{"mcpServers":{"project":{"command":"b"}}}`)

	result := h.load()

	if len(result.Servers) != MaxServers {
		t.Fatalf("server count = %d, want %d", len(result.Servers), MaxServers)
	}
	if _, ok := result.Lookup("project"); !ok {
		t.Fatal("project server was starved by the user file")
	}
	// The last user server in lexical order is the deterministic victim.
	evicted := fmt.Sprintf("user-%03d", MaxServers-1)
	if _, ok := result.Lookup(evicted); ok {
		t.Fatalf("%s should have been evicted", evicted)
	}
	want := fmt.Sprintf("ignored: replaced by higher-precedence server %q at the %d server limit", "project", MaxServers)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Server != evicted ||
		result.Diagnostics[0].Message != want {
		t.Fatalf("diagnostics = %v, want an eviction diagnostic for %s", messages(result), evicted)
	}
}

func TestLoadCeilingRejectsWithinOneFile(t *testing.T) {
	h := newHarness(t)
	entries := make([]string, 0, MaxServers+1)
	for index := range MaxServers + 1 {
		entries = append(entries, fmt.Sprintf(`"s-%03d":{"command":"a"}`, index))
	}
	h.write(SourceKitUser, `{"mcpServers":{`+strings.Join(entries, ",")+`}}`)

	result := h.load()

	if len(result.Servers) != MaxServers {
		t.Fatalf("server count = %d, want %d", len(result.Servers), MaxServers)
	}
	want := fmt.Sprintf("ignored: more than %d servers are configured", MaxServers)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != want {
		t.Fatalf("diagnostics = %v, want %q", messages(result), want)
	}
}

func TestLoadRecordsDiagnosticTruncation(t *testing.T) {
	h := newHarness(t)
	entries := make([]string, 0, MaxDiagnostics+10)
	for index := range MaxDiagnostics + 10 {
		entries = append(entries, fmt.Sprintf(`"s-%03d":{"description":%d}`, index, index))
	}
	h.write(SourceKitUser, `{"mcpServers":{`+strings.Join(entries, ",")+`}}`)

	result := h.load()

	if len(result.Diagnostics) != MaxDiagnostics {
		t.Fatalf("diagnostic count = %d, want %d", len(result.Diagnostics), MaxDiagnostics)
	}
	last := result.Diagnostics[MaxDiagnostics-1]
	want := fmt.Sprintf("additional MCP configuration problems were omitted after %d diagnostics", MaxDiagnostics-1)
	if last.Message != want {
		t.Fatalf("final diagnostic = %q, want %q", last.Message, want)
	}
}

func TestLoadIsSafeForConcurrentSessions(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{"command":"a","env":{"K":"v"}}}}`)
	loader, err := NewLoader(apphome.FromHome(h.kitHome), WithUserHome(h.home))
	if err != nil {
		t.Fatalf("new loader: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]Result, 8)
	for index := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := loader.Load(context.Background(), h.cwd)
			if err != nil {
				t.Errorf("load: %v", err)
				return
			}
			results[index] = result
		}()
	}
	wg.Wait()

	for index, result := range results {
		if !reflect.DeepEqual(result.Servers, results[0].Servers) {
			t.Fatalf("result %d servers = %#v, want %#v", index, result.Servers, results[0].Servers)
		}
	}
}
