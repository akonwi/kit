// Package mcpconfig loads and merges Kit's Model Context Protocol server
// configuration from the supported locations with deterministic precedence.
//
// Configuration is advisory input, not a trust boundary: an unreadable or
// malformed file produces diagnostics and never prevents the remaining files
// from contributing servers.
package mcpconfig

import "fmt"

// Source identifies one supported configuration location.
type Source string

const (
	// SourceKitUser is the Kit-owned user configuration file.
	SourceKitUser Source = "kit-user"
	// SourceSharedProject is <cwd>/.mcp.json, shared with other MCP clients.
	SourceSharedProject Source = "shared-project"
	// SourceKitProject is <cwd>/.agents/mcp.json.
	SourceKitProject Source = "kit-project"
)

// Precedence lists sources from lowest to highest priority. A server defined in
// a later source overrides same-named fields from earlier sources.
var Precedence = [...]Source{SourceKitUser, SourceSharedProject, SourceKitProject}

// Transport identifies how Kit connects to a configured server.
type Transport string

const (
	// TransportStdio launches a child process and speaks MCP over its pipes.
	TransportStdio Transport = "stdio"
	// TransportHTTP connects to a remote streamable HTTP endpoint.
	TransportHTTP Transport = "http"
)

// AuthKind identifies how Kit authenticates to an HTTP server.
type AuthKind string

const (
	// AuthOAuth performs the browser-based authorization code flow on first use.
	AuthOAuth AuthKind = "oauth"
	// AuthBearer sends a static or environment-sourced bearer token.
	AuthBearer AuthKind = "bearer"
)

// Auth describes the authentication configured for one server. BearerToken is
// resolved configuration material; BearerTokenEnv is resolved at connect time
// so tokens are not retained in configuration snapshots.
type Auth struct {
	Kind           AuthKind
	BearerToken    string
	BearerTokenEnv string
}

// Server is one fully resolved, validated MCP server definition.
type Server struct {
	// Name is the stable namespace identifier used by the proxy tool.
	Name string
	// Transport selects the stdio or HTTP fields below.
	Transport Transport

	// Command, Args, Env, and Cwd apply when Transport is TransportStdio.
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string

	// URL and Headers apply when Transport is TransportHTTP.
	URL     string
	Headers map[string]string

	// Description is application-facing help text for the namespace.
	Description string
	// Disabled records that the server is configured but must not be connected.
	Disabled bool
	// Auth is nil when the server needs no Kit-managed authentication.
	Auth *Auth

	// Source and Path record the highest-precedence file that defined the server.
	Source Source
	Path   string
}

// Diagnostic reports configuration that could not be used. Server is empty when
// the problem applies to the whole file.
type Diagnostic struct {
	Source  Source
	Path    string
	Server  string
	Field   string
	Message string
}

func (d Diagnostic) Error() string {
	switch {
	case d.Server != "" && d.Field != "":
		return fmt.Sprintf("%s: server %q: %s: %s", d.Path, d.Server, d.Field, d.Message)
	case d.Server != "":
		return fmt.Sprintf("%s: server %q: %s", d.Path, d.Server, d.Message)
	default:
		return fmt.Sprintf("%s: %s", d.Path, d.Message)
	}
}

// File records one candidate configuration location and whether it contributed.
type File struct {
	Source Source
	Path   string
	// Present is true when a regular file existed at Path.
	Present bool
	// Loaded is true when the file parsed into a usable document.
	Loaded bool
}

// Result is one immutable configuration snapshot for a single session cwd.
type Result struct {
	// Servers are sorted by name and contain both enabled and disabled entries.
	Servers []Server
	// Files preserves precedence order, lowest priority first.
	Files []File
	// Diagnostics are ordered by the file that produced them.
	Diagnostics []Diagnostic
}

// Enabled returns the servers that are not disabled, preserving name order.
func (r Result) Enabled() []Server {
	enabled := make([]Server, 0, len(r.Servers))
	for _, server := range r.Servers {
		if !server.Disabled {
			enabled = append(enabled, server)
		}
	}
	return enabled
}

// Lookup returns the resolved server with the given name.
func (r Result) Lookup(name string) (Server, bool) {
	for _, server := range r.Servers {
		if server.Name == name {
			return server, true
		}
	}
	return Server{}, false
}
