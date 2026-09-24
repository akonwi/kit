package mcpconfig

import (
	"reflect"
	"strings"
	"testing"
)

// diagnosticCase asserts the actionable message produced for invalid config.
type diagnosticCase struct {
	name     string
	contents string
	server   string
	field    string
	message  string
	// follows is a second diagnostic expected after message, if any.
	follows *Diagnostic
	// prefix matches message as a prefix, for wrapped decoder errors.
	prefix bool
	// resolves is true when the entry still produces a usable server.
	resolves bool
}

func TestLoadReportsActionableValidationDiagnostics(t *testing.T) {
	cases := []diagnosticCase{
		{
			name:     "missing transport",
			contents: `{"mcpServers":{"x":{"description":"no transport"}}}`,
			server:   "x",
			message:  `ignored: set "command" for a stdio server or "url" for an HTTP server`,
		},
		{
			name:     "entry is not an object",
			contents: `{"mcpServers":{"x":"npx server"}}`,
			server:   "x",
			message:  "must be an object: ",
			prefix:   true,
		},
		{
			name:     "entry is null",
			contents: `{"mcpServers":{"x":null}}`,
			server:   "x",
			message:  "must be an object, not null",
		},
		{
			name:     "command is not a string",
			contents: `{"mcpServers":{"x":{"command":["npx"]}}}`,
			server:   "x",
			field:    "command",
			message:  "ignored: must be a string",
			follows: &Diagnostic{
				Server:  "x",
				Message: `ignored: set "command" for a stdio server or "url" for an HTTP server`,
			},
		},
		{
			name:     "args is not an array of strings",
			contents: `{"mcpServers":{"x":{"command":"a","args":"--flag"}}}`,
			server:   "x",
			field:    "args",
			message:  "ignored: must be an array of strings",
			resolves: true,
		},
		{
			name:     "env value is not a string",
			contents: `{"mcpServers":{"x":{"command":"a","env":{"PORT":8080}}}}`,
			server:   "x",
			field:    "env.PORT",
			message:  "ignored: must be a string",
			resolves: true,
		},
		{
			name:     "disabled is not a boolean",
			contents: `{"mcpServers":{"x":{"command":"a","disabled":"yes"}}}`,
			server:   "x",
			field:    "disabled",
			message:  "ignored: must be true or false",
			resolves: true,
		},
		{
			name:     "url scheme is unsupported",
			contents: `{"mcpServers":{"x":{"url":"ftp://example.com"}}}`,
			server:   "x",
			field:    "url",
			message:  `ignored: must use http or https, got "ftp"`,
		},
		{
			name:     "url has no host",
			contents: `{"mcpServers":{"x":{"url":"https:///mcp"}}}`,
			server:   "x",
			field:    "url",
			message:  "ignored: must include a host",
		},
		{
			name:     "cwd is relative",
			contents: `{"mcpServers":{"x":{"command":"a","cwd":"relative/dir"}}}`,
			server:   "x",
			field:    "cwd",
			message:  "ignored: must be an absolute path",
			resolves: true,
		},
		{
			name:     "auth value is unknown",
			contents: `{"mcpServers":{"x":{"url":"https://example.com","auth":"basic"}}}`,
			server:   "x",
			field:    "auth",
			message:  `ignored: must be "oauth" or "bearer", got "basic"`,
			resolves: true,
		},
		{
			name:     "bearer auth without a token",
			contents: `{"mcpServers":{"x":{"url":"https://example.com","auth":"bearer"}}}`,
			server:   "x",
			field:    "auth",
			message:  `"bearer" requires "bearerToken" or "bearerTokenEnv"`,
			resolves: true,
		},
		{
			name:     "oauth on a stdio server",
			contents: `{"mcpServers":{"x":{"command":"a","auth":"oauth"}}}`,
			server:   "x",
			field:    "auth",
			message:  `ignored: "oauth" applies to HTTP servers only`,
			resolves: true,
		},
		{
			name:     "blank server name",
			contents: `{"mcpServers":{"   ":{"command":"a"}}}`,
			server:   "   ",
			message:  "ignored: server name must not be blank",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newHarness(t)
			h.write(SourceKitUser, testCase.contents)

			result := h.load()

			want := []Diagnostic{{
				Source:  SourceKitUser,
				Path:    h.pathFor(SourceKitUser),
				Server:  testCase.server,
				Field:   testCase.field,
				Message: testCase.message,
			}}
			if testCase.follows != nil {
				follows := *testCase.follows
				follows.Source = SourceKitUser
				follows.Path = h.pathFor(SourceKitUser)
				want = append(want, follows)
			}
			if len(result.Diagnostics) != len(want) {
				t.Fatalf("diagnostics = %v, want %d", messages(result), len(want))
			}
			for index, expected := range want {
				actual := result.Diagnostics[index]
				if testCase.prefix && index == 0 {
					if !strings.HasPrefix(actual.Message, expected.Message) {
						t.Fatalf("message = %q, want prefix %q", actual.Message, expected.Message)
					}
					expected.Message = actual.Message
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("diagnostic %d = %#v, want %#v", index, actual, expected)
				}
			}
			_, resolved := result.Lookup(testCase.server)
			if resolved != testCase.resolves {
				t.Fatalf("server resolved = %v, want %v", resolved, testCase.resolves)
			}
		})
	}
}

func TestLoadMalformedFileDoesNotDisableOtherFiles(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{`)
	h.write(SourceKitProject, `{"mcpServers":{"good":{"command":"a"}}}`)

	result := h.load()

	h.mustServer(result, "good")
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v, want one", messages(result))
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Source != SourceKitUser || diagnostic.Server != "" {
		t.Errorf("diagnostic = %#v, want a file-level kit-user diagnostic", diagnostic)
	}
	if !strings.HasPrefix(diagnostic.Message, "MCP config must be a JSON object:") {
		t.Errorf("message = %q, want a JSON object prefix", diagnostic.Message)
	}
	kitUser := fileFor(t, result, SourceKitUser)
	if !kitUser.Present || kitUser.Loaded {
		t.Errorf("kit-user file = %#v, want present but not loaded", kitUser)
	}
}

func TestLoadRejectsNonObjectMcpServers(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":[{"name":"x"}]}`)

	result := h.load()

	if len(result.Servers) != 0 {
		t.Fatalf("servers = %v, want none", serverNames(result))
	}
	if len(result.Diagnostics) != 1 ||
		!strings.HasPrefix(result.Diagnostics[0].Message, `"mcpServers" must be an object of server name to definition:`) {
		t.Fatalf("diagnostics = %v, want an mcpServers shape diagnostic", messages(result))
	}
}

func TestLoadTreatsMissingMcpServersAsEmpty(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"$schema":"https://example.com/mcp.json"}`)

	result := h.load()

	requireNoDiagnostics(t, result)
	if len(result.Servers) != 0 {
		t.Fatalf("servers = %v, want none", serverNames(result))
	}
	if !fileFor(t, result, SourceKitUser).Loaded {
		t.Error("kit-user file should count as loaded")
	}
}

func TestLoadIgnoresUnknownServerFields(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{"command":"a","timeout":30,"experimental":{"k":1}}}}`)

	result := h.load()

	requireNoDiagnostics(t, result)
	if server := h.mustServer(result, "x"); server.Command != "a" {
		t.Fatalf("command = %q, want %q", server.Command, "a")
	}
}

func TestLoadPrefersHigherPrecedenceTransportWhenBothDefined(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitUser, `{"mcpServers":{"x":{"command":"local"}}}`)
	h.write(SourceKitProject, `{"mcpServers":{"x":{"url":"https://example.com/mcp"}}}`)

	result := h.load()
	server := h.mustServer(result, "x")

	if server.Transport != TransportHTTP {
		t.Fatalf("transport = %q, want %q from the higher-precedence file", server.Transport, TransportHTTP)
	}
	if len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Message != "defines both command and url; using the http transport" {
		t.Fatalf("diagnostics = %v, want an ambiguous-transport diagnostic", messages(result))
	}
}

func TestLoadPrefersCommandWhenOneFileDefinesBoth(t *testing.T) {
	h := newHarness(t)
	h.write(SourceKitProject, `{"mcpServers":{"x":{"command":"local","url":"https://example.com/mcp"}}}`)

	server := h.mustServer(h.load(), "x")
	if server.Transport != TransportStdio {
		t.Fatalf("transport = %q, want %q", server.Transport, TransportStdio)
	}
}
