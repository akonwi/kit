// Package mcp adapts Model Context Protocol servers into progressively
// disclosed droids tools. Each configured server is exposed as one namespace
// tool; connections and remote tool schemas are loaded only when used.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/akonwi/kit/internal/droids"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMaxResults     = 20
	defaultMaxTools       = 1000
	defaultMaxSchemaBytes = 256 << 10
	defaultMaxResultBytes = 4 << 20
	maxToolNameLength     = 64
)

// TransportFactory creates a fresh MCP transport. MCP transports are
// single-use, so the factory may be called again after a failed connection.
type TransportFactory func(context.Context) (sdkmcp.Transport, error)

// Server configures one progressively disclosed MCP namespace.
type Server struct {
	// Name is the namespace's stable, user-facing identifier.
	Name string
	// Description tells the model when this namespace is useful. It should be
	// application-authored rather than copied from an untrusted remote server.
	Description string
	// Transport creates a fresh transport when the namespace is first used.
	// Configure HTTP authentication, including an OAuthHandler, here.
	Transport TransportFactory
	// Filter optionally limits the remote tools visible and callable through
	// this namespace.
	Filter func(*sdkmcp.Tool) bool
	// ExecutionMode controls concurrent calls to this namespace proxy.
	ExecutionMode droids.ExecutionMode
	// MaxResults limits list/search output. Defaults to 20.
	MaxResults int
	// MaxTools bounds discovered tools retained for this namespace. Defaults to 1000.
	MaxTools int
	// MaxSchemaBytes bounds a schema returned by describe. Defaults to 256 KiB.
	MaxSchemaBytes int
	// MaxResultBytes bounds converted model-visible content and structured output
	// from one remote call. Defaults to 4 MiB.
	MaxResultBytes int
}

// Manager owns lazy MCP connections and exposes one droids tool per server.
type Manager struct {
	mu         sync.Mutex
	closed     bool
	namespaces []*namespace
}

type namespace struct {
	config   Server
	toolName string

	mu              sync.Mutex
	connectMu       sync.Mutex
	listMu          sync.Mutex
	closed          bool
	lifetime        context.Context
	cancel          context.CancelFunc
	session         *sdkmcp.ClientSession
	tools           map[string]*sdkmcp.Tool
	toolGeneration  uint64
	cacheGeneration uint64
}

// NewManager validates servers without connecting to them.
func NewManager(servers ...Server) (*Manager, error) {
	m := &Manager{}
	seenServers := make(map[string]struct{}, len(servers))
	seenTools := make(map[string]string, len(servers))

	for _, server := range servers {
		server.Name = strings.TrimSpace(server.Name)
		if server.Name == "" {
			return nil, fmt.Errorf("droids/mcp: server name is required")
		}
		if server.Transport == nil {
			return nil, fmt.Errorf("droids/mcp: server %q requires a transport factory", server.Name)
		}
		if _, exists := seenServers[server.Name]; exists {
			return nil, fmt.Errorf("droids/mcp: duplicate server name %q", server.Name)
		}
		seenServers[server.Name] = struct{}{}

		toolName, err := namespaceToolName(server.Name)
		if err != nil {
			return nil, err
		}
		if previous, exists := seenTools[toolName]; exists {
			return nil, fmt.Errorf("droids/mcp: server names %q and %q both map to tool %q", previous, server.Name, toolName)
		}
		seenTools[toolName] = server.Name
		if server.MaxResults <= 0 {
			server.MaxResults = defaultMaxResults
		}
		if server.MaxTools <= 0 {
			server.MaxTools = defaultMaxTools
		}
		if server.MaxSchemaBytes <= 0 {
			server.MaxSchemaBytes = defaultMaxSchemaBytes
		}
		if server.MaxResultBytes <= 0 {
			server.MaxResultBytes = defaultMaxResultBytes
		}
		lifetime, cancel := context.WithCancel(context.Background())
		m.namespaces = append(m.namespaces, &namespace{
			config:         server,
			toolName:       toolName,
			lifetime:       lifetime,
			cancel:         cancel,
			toolGeneration: 1,
		})
	}
	return m, nil
}

// Tools returns one local proxy tool per configured MCP server. It performs no
// network I/O; each namespace connects when the proxy is first executed.
func (m *Manager) Tools() []droids.AnyTool {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]droids.AnyTool, 0, len(m.namespaces))
	for _, ns := range m.namespaces {
		ns := ns
		out = append(out, droids.MustTool(droids.Tool[namespaceRequest]{
			Name:        ns.toolName,
			Description: namespaceDescription(ns.config),
			Parameters:  namespaceSchema(),
			Mode:        ns.config.ExecutionMode,
			Execute: func(ctx context.Context, _ droids.ToolContext, request namespaceRequest, _ droids.ToolUpdate) (droids.ToolResult, error) {
				return ns.execute(ctx, request)
			},
		}))
	}
	return out
}

// Close closes all connected MCP sessions. It is safe to call more than once.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	namespaces := append([]*namespace(nil), m.namespaces...)
	m.mu.Unlock()

	var errs []error
	for _, ns := range namespaces {
		if err := ns.close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ns.config.Name, err))
		}
	}
	return errors.Join(errs...)
}

type namespaceRequest struct {
	Action    string         `json:"action"`
	Query     string         `json:"query,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func namespaceSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "search", "describe", "call"},
				"description": "List tools, search tools, inspect one tool's schema, or call one tool.",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Search query. Required when action is search.",
			},
			"tool": map[string]any{
				"type":        "string",
				"description": "Remote tool name. Required for describe and call.",
			},
			"arguments": map[string]any{
				"type":                 "object",
				"description":          "Remote tool arguments. Used when action is call.",
				"additionalProperties": true,
			},
		},
		"required": []string{"action"},
	}
}

func namespaceDescription(server Server) string {
	description := fmt.Sprintf("Explore and call tools in the %s MCP namespace.", server.Name)
	if strings.TrimSpace(server.Description) != "" {
		description += " " + strings.TrimSpace(server.Description) + "."
	}
	return description + " Use list or search to discover tools, describe to inspect a tool's input schema, and call to run it."
}

func namespaceToolName(name string) (string, error) {
	var b strings.Builder
	b.WriteString("mcp_")
	underscore := false
	for _, r := range strings.ToLower(name) {
		if r <= 127 && ((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			b.WriteRune(r)
			underscore = false
			continue
		}
		if !underscore {
			b.WriteByte('_')
			underscore = true
		}
	}
	toolName := strings.Trim(b.String(), "_-")
	if toolName == "mcp" || toolName == "mcp_" {
		return "", fmt.Errorf("droids/mcp: server name %q has no usable tool-name characters", name)
	}
	if len(toolName) > maxToolNameLength {
		return "", fmt.Errorf("droids/mcp: server name %q produces tool name longer than %d characters", name, maxToolNameLength)
	}
	return toolName, nil
}

func (ns *namespace) execute(ctx context.Context, req namespaceRequest) (droids.ToolResult, error) {
	ctx, cancel := ns.operationContext(ctx)
	defer cancel()
	switch req.Action {
	case "list":
		return ns.list(ctx)
	case "search":
		if strings.TrimSpace(req.Query) == "" {
			return droids.ToolResult{}, fmt.Errorf("query is required when action is search")
		}
		return ns.search(ctx, req.Query)
	case "describe":
		if strings.TrimSpace(req.Tool) == "" {
			return droids.ToolResult{}, fmt.Errorf("tool is required when action is describe")
		}
		return ns.describe(ctx, req.Tool)
	case "call":
		if strings.TrimSpace(req.Tool) == "" {
			return droids.ToolResult{}, fmt.Errorf("tool is required when action is call")
		}
		return ns.call(ctx, req.Tool, req.Arguments)
	default:
		return droids.ToolResult{}, fmt.Errorf("unknown action %q; use list, search, describe, or call", req.Action)
	}
}

func (ns *namespace) connect(ctx context.Context) (*sdkmcp.ClientSession, error) {
	ns.connectMu.Lock()
	defer ns.connectMu.Unlock()

	ns.mu.Lock()
	if ns.closed {
		ns.mu.Unlock()
		return nil, fmt.Errorf("MCP namespace %q is closed", ns.config.Name)
	}
	if ns.session != nil {
		session := ns.session
		ns.mu.Unlock()
		return session, nil
	}
	ns.mu.Unlock()

	transport, err := ns.config.Transport(ctx)
	if err != nil {
		return nil, fmt.Errorf("create transport for MCP namespace %q: %w", ns.config.Name, err)
	}
	if transport == nil {
		return nil, fmt.Errorf("create transport for MCP namespace %q: factory returned nil", ns.config.Name)
	}

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "droids-mcp-" + ns.config.Name, Version: "0.1.0"}, &sdkmcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *sdkmcp.ToolListChangedRequest) {
			ns.mu.Lock()
			ns.toolGeneration++
			ns.mu.Unlock()
		},
	})
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect MCP namespace %q: %w", ns.config.Name, err)
	}

	ns.mu.Lock()
	if ns.closed {
		ns.mu.Unlock()
		_ = session.Close()
		return nil, fmt.Errorf("MCP namespace %q is closed", ns.config.Name)
	}
	ns.session = session
	ns.mu.Unlock()
	go func() {
		_ = session.Wait()
		ns.invalidateSession(session)
	}()
	return session, nil
}

func (ns *namespace) loadTools(ctx context.Context) (map[string]*sdkmcp.Tool, error) {
	session, err := ns.connect(ctx)
	if err != nil {
		return nil, err
	}

	ns.listMu.Lock()
	defer ns.listMu.Unlock()
	ns.mu.Lock()
	if ns.closed {
		ns.mu.Unlock()
		return nil, fmt.Errorf("MCP namespace %q is closed", ns.config.Name)
	}
	if ns.tools != nil && ns.cacheGeneration == ns.toolGeneration {
		tools := copyTools(ns.tools)
		ns.mu.Unlock()
		return tools, nil
	}
	generation := ns.toolGeneration
	ns.mu.Unlock()

	tools := make(map[string]*sdkmcp.Tool)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			if isConnectionError(err) {
				ns.invalidateSession(session)
			}
			return nil, fmt.Errorf("list tools for MCP namespace %q: %w", ns.config.Name, err)
		}
		if tool == nil || (ns.config.Filter != nil && !ns.config.Filter(tool)) {
			continue
		}
		if strings.TrimSpace(tool.Name) == "" || strings.IndexFunc(tool.Name, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("MCP namespace %q returned an invalid tool name", ns.config.Name)
		}
		if _, exists := tools[tool.Name]; exists {
			return nil, fmt.Errorf("MCP namespace %q returned duplicate tool %q", ns.config.Name, tool.Name)
		}
		if len(tools) >= ns.config.MaxTools {
			return nil, fmt.Errorf("MCP namespace %q exceeds the %d tool discovery limit", ns.config.Name, ns.config.MaxTools)
		}
		tools[tool.Name] = tool
	}
	ns.mu.Lock()
	if ns.closed {
		ns.mu.Unlock()
		return nil, fmt.Errorf("MCP namespace %q is closed", ns.config.Name)
	}
	if ns.toolGeneration == generation {
		ns.tools = tools
		ns.cacheGeneration = generation
	}
	ns.mu.Unlock()
	return copyTools(tools), nil
}

func copyTools(in map[string]*sdkmcp.Tool) map[string]*sdkmcp.Tool {
	out := make(map[string]*sdkmcp.Tool, len(in))
	for name, tool := range in {
		out[name] = tool
	}
	return out
}

func sortedTools(tools map[string]*sdkmcp.Tool) []*sdkmcp.Tool {
	out := make([]*sdkmcp.Tool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (ns *namespace) list(ctx context.Context) (droids.ToolResult, error) {
	tools, err := ns.loadTools(ctx)
	if err != nil {
		return droids.ToolResult{}, err
	}
	all := sortedTools(tools)
	shown := all
	if len(shown) > ns.config.MaxResults {
		shown = shown[:ns.config.MaxResults]
	}

	lines := []string{fmt.Sprintf("%s MCP namespace: %d tool(s)", ns.config.Name, len(all)), ""}
	for _, tool := range shown {
		lines = append(lines, formatToolSummary(tool))
	}
	if len(shown) < len(all) {
		lines = append(lines, "", fmt.Sprintf("Showing %d of %d tools. Use search to narrow the list.", len(shown), len(all)))
	}
	return discoveryResult(ns.config.Name, "list", "", strings.Join(lines, "\n"), len(shown)), nil
}

func (ns *namespace) search(ctx context.Context, query string) (droids.ToolResult, error) {
	tools, err := ns.loadTools(ctx)
	if err != nil {
		return droids.ToolResult{}, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var matches []*sdkmcp.Tool
	for _, tool := range tools {
		haystack := strings.ToLower(tool.Name + " " + tool.Description)
		if strings.Contains(haystack, query) {
			matches = append(matches, tool)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Name < matches[j].Name })
	if len(matches) > ns.config.MaxResults {
		matches = matches[:ns.config.MaxResults]
	}
	if len(matches) == 0 {
		return discoveryResult(ns.config.Name, "search", "", fmt.Sprintf("No tools in %s matched %q.", ns.config.Name, query), 0), nil
	}
	lines := []string{fmt.Sprintf("Tools in %s matching %q:", ns.config.Name, query), ""}
	for _, tool := range matches {
		lines = append(lines, formatToolSummary(tool))
	}
	return discoveryResult(ns.config.Name, "search", "", strings.Join(lines, "\n"), len(matches)), nil
}

func (ns *namespace) describe(ctx context.Context, name string) (droids.ToolResult, error) {
	tools, err := ns.loadTools(ctx)
	if err != nil {
		return droids.ToolResult{}, err
	}
	tool, ok := tools[name]
	if !ok {
		return droids.ToolResult{}, fmt.Errorf("tool %q was not found in MCP namespace %q", name, ns.config.Name)
	}
	input, err := json.MarshalIndent(tool.InputSchema, "", "  ")
	if err != nil {
		return droids.ToolResult{}, fmt.Errorf("encode input schema for %s.%s: %w", ns.config.Name, name, err)
	}
	if len(input) > ns.config.MaxSchemaBytes {
		return droids.ToolResult{}, fmt.Errorf("input schema for %s.%s exceeds the %d byte limit", ns.config.Name, name, ns.config.MaxSchemaBytes)
	}
	lines := []string{name}
	if tool.Description != "" {
		lines = append(lines, cleanMetadata(tool.Description, 2000))
	}
	lines = append(lines, "", "Input schema:", string(input))
	if tool.OutputSchema != nil {
		output, err := json.MarshalIndent(tool.OutputSchema, "", "  ")
		if err != nil {
			return droids.ToolResult{}, fmt.Errorf("encode output schema for %s.%s: %w", ns.config.Name, name, err)
		}
		if len(output) > ns.config.MaxSchemaBytes {
			return droids.ToolResult{}, fmt.Errorf("output schema for %s.%s exceeds the %d byte limit", ns.config.Name, name, ns.config.MaxSchemaBytes)
		}
		lines = append(lines, "", "Output schema:", string(output))
	}
	return discoveryResult(ns.config.Name, "describe", name, strings.Join(lines, "\n"), 1), nil
}

func (ns *namespace) call(ctx context.Context, name string, arguments map[string]any) (droids.ToolResult, error) {
	tools, err := ns.loadTools(ctx)
	if err != nil {
		return droids.ToolResult{}, err
	}
	if _, ok := tools[name]; !ok {
		return droids.ToolResult{}, fmt.Errorf("tool %q was not found in MCP namespace %q", name, ns.config.Name)
	}
	session, err := ns.connect(ctx)
	if err != nil {
		return droids.ToolResult{}, err
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		if isConnectionError(err) {
			ns.invalidateSession(session)
		}
		return droids.ToolResult{}, fmt.Errorf("call MCP tool %s.%s: %w", ns.config.Name, name, err)
	}
	return convertCallResult(ns.config.Name, name, result, ns.config.MaxResultBytes)
}

func (ns *namespace) close() error {
	ns.mu.Lock()
	if ns.closed {
		ns.mu.Unlock()
		return nil
	}
	ns.closed = true
	ns.cancel()
	ns.mu.Unlock()

	// Wait for a connection handshake to observe cancellation before taking
	// ownership of the final session. All tool operations derive from lifetime,
	// so ClientSession.Close does not wait on uncancelled calls.
	ns.connectMu.Lock()
	defer ns.connectMu.Unlock()
	ns.mu.Lock()
	session := ns.session
	ns.session = nil
	ns.tools = nil
	ns.mu.Unlock()
	if session != nil {
		return session.Close()
	}
	return nil
}

func (ns *namespace) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(ns.lifetime, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func isConnectionError(err error) bool {
	return errors.Is(err, sdkmcp.ErrConnectionClosed) || errors.Is(err, sdkmcp.ErrSessionMissing)
}

func (ns *namespace) invalidateSession(session *sdkmcp.ClientSession) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	if ns.session != session {
		return
	}
	ns.session = nil
	ns.tools = nil
	ns.toolGeneration++
}

func formatToolSummary(tool *sdkmcp.Tool) string {
	if strings.TrimSpace(tool.Description) == "" {
		return "- " + tool.Name
	}
	return fmt.Sprintf("- %s — %s", tool.Name, cleanMetadata(tool.Description, 200))
}

func cleanMetadata(text string, maxRunes int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes-3]) + "..."
	}
	return text
}

// DiscoveryDetails is persisted with namespace discovery results for UI and
// observability without adding metadata to the model-visible text.
type DiscoveryDetails struct {
	Namespace string
	Action    string
	Tool      string
	Count     int
}

func discoveryResult(namespace, action, tool, text string, count int) droids.ToolResult {
	details, _ := droids.EncodeDetails(DiscoveryDetails{Namespace: namespace, Action: action, Tool: tool, Count: count})
	return droids.ToolResult{
		Content: []droids.ResultContent{droids.TextContent{Text: text}},
		Details: details,
	}
}
