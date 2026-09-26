package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/mcp"
	"github.com/akonwi/kit/internal/inspectimage"
	"github.com/akonwi/kit/internal/mcpconfig"
	"github.com/akonwi/kit/internal/peer"
	"github.com/akonwi/kit/internal/promptcommands"
	"github.com/akonwi/kit/internal/sessiontool"
	"github.com/akonwi/kit/internal/showimage"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
)

// MCPServerInfo is the credential-free MCP configuration retained for status presentation.
type MCPServerInfo struct {
	Name, Description, Source, Path string
	Transport                       string
	Disabled, ManagedOAuth          bool
	OAuthSaved                      func(context.Context) (bool, error)
}

// RuntimeBundle is one atomic prompt and tool configuration for a session
// runtime. Prompt provenance remains server-owned and is not conversation data.
type RuntimeBundle struct {
	Prompt         systemprompt.Result
	Tools          []droids.AnyTool
	PromptCommands *promptcommands.Registry
	Subagents      subagent.LoadResult
	MCPServers     []MCPServerInfo
	MCPWarnings    []string
	// MCP is the session's configured MCP namespaces, or nil when none are
	// configured. The owning session runtime closes it; subagents of that session
	// borrow its tools and must not close it.
	MCP *mcp.Manager
}

// RuntimeBundleBuilder resolves the prompt and tools applicable to a session
// runtime. Build may be called concurrently for different sessions. One call
// must return a mutually consistent immutable snapshot. The cwd provider is
// session-scoped and may change independently after the bundle is built.
type RuntimeBundleBuilder interface {
	Build(context.Context, SessionRecord, codingtools.CWDProvider) (RuntimeBundle, error)
}

func cloneRuntimeBundle(bundle RuntimeBundle) RuntimeBundle {
	bundle.Prompt.Sources = append([]systemprompt.Source(nil), bundle.Prompt.Sources...)
	bundle.Prompt.Diagnostics = append([]systemprompt.Diagnostic(nil), bundle.Prompt.Diagnostics...)
	bundle.Tools = append([]droids.AnyTool(nil), bundle.Tools...)
	bundle.MCPServers = append([]MCPServerInfo(nil), bundle.MCPServers...)
	bundle.MCPWarnings = append([]string(nil), bundle.MCPWarnings...)
	// MCP is deliberately shared rather than copied. It owns processes and
	// connections, so duplicating the reference would double-close it.
	catalog, err := subagent.NewCatalog(bundle.Subagents.Catalog.Definitions()...)
	if err == nil {
		bundle.Subagents.Catalog = catalog
	}
	bundle.Subagents.Diagnostics = append([]subagent.Diagnostic(nil), bundle.Subagents.Diagnostics...)
	return bundle
}

// RuntimeBundleOptions configures Kit's standard session bundle builder.
type RuntimeBundleOptions struct {
	Core                string
	Context             *systemprompt.ContextBuilderOptions
	Registry            *skills.Registry
	SkillLoader         skills.Loader
	PromptCommandLoader promptcommands.Loader
	SubagentLoader      subagent.Loader
	SubagentToolFactory subagent.ParentToolFactory
	PeerToolFactory     peer.ToolFactory
	SessionToolFactory  sessiontool.ToolFactory
	AttachmentStore     attachment.Store
	PresentImageEnabled func(SessionRecord) bool
	InspectImageEnabled func(SessionRecord) bool
	// MCPLoader and MCPLauncher must be configured together and only for
	// session-owning builders. A child builder omits them and borrows its owner's
	// namespaces instead of starting duplicate server processes.
	MCPLoader   MCPConfigLoader
	MCPLauncher MCPLauncher
}

// MCPConfigLoader resolves the MCP servers applicable to one session cwd.
type MCPConfigLoader interface {
	Load(context.Context, string) (mcpconfig.Result, error)
}

// MCPLauncher projects validated servers into agent-core namespaces.
type MCPLauncher interface {
	Servers(string, []mcpconfig.Server) ([]mcp.Server, error)
	OAuthSaved(context.Context, string, string) (bool, error)
}

type defaultRuntimeBundleBuilder struct {
	composer            *systemprompt.Composer
	context             *systemprompt.ContextBuilder
	registry            *skills.Registry
	skillLoader         skills.Loader
	promptCommandLoader promptcommands.Loader
	subagentLoader      subagent.Loader
	subagentToolFactory subagent.ParentToolFactory
	peerToolFactory     peer.ToolFactory
	sessionToolFactory  sessiontool.ToolFactory
	attachmentStore     attachment.Store
	presentImageEnabled func(SessionRecord) bool
	inspectImageEnabled func(SessionRecord) bool
	mcpLoader           MCPConfigLoader
	mcpLauncher         MCPLauncher
}

// NewRuntimeBundleBuilder constructs Kit's standard atomic prompt/tool builder.
// Each Build obtains one registry snapshot, then derives both its catalog and
// activation tool from that same snapshot.
func NewRuntimeBundleBuilder(options RuntimeBundleOptions) (RuntimeBundleBuilder, error) {
	if (options.Registry == nil) == (options.SkillLoader == nil) {
		return nil, errors.New("session requires exactly one fixed skill registry or skill loader")
	}
	if (options.SubagentLoader == nil) != (options.SubagentToolFactory == nil) {
		return nil, errors.New("session subagent loader and tool factory must be configured together")
	}
	if options.AttachmentStore == nil && (options.PresentImageEnabled != nil || options.InspectImageEnabled != nil) ||
		options.AttachmentStore != nil && (options.PresentImageEnabled == nil || options.InspectImageEnabled == nil) {
		return nil, errors.New("session attachment store and image capability gates must be configured together")
	}
	if (options.MCPLoader == nil) != (options.MCPLauncher == nil) {
		return nil, errors.New("session MCP loader and launcher must be configured together")
	}
	composer, err := systemprompt.New(options.Core)
	if err != nil {
		return nil, err
	}
	builder := &defaultRuntimeBundleBuilder{
		composer: composer, registry: options.Registry, skillLoader: options.SkillLoader,
		promptCommandLoader: options.PromptCommandLoader,
		subagentLoader:      options.SubagentLoader, subagentToolFactory: options.SubagentToolFactory,
		peerToolFactory:     options.PeerToolFactory,
		sessionToolFactory:  options.SessionToolFactory,
		attachmentStore:     options.AttachmentStore,
		presentImageEnabled: options.PresentImageEnabled,
		inspectImageEnabled: options.InspectImageEnabled,
		mcpLoader:           options.MCPLoader,
		mcpLauncher:         options.MCPLauncher,
	}
	if options.Context != nil {
		builder.context, err = systemprompt.NewContextBuilder(composer, *options.Context)
		if err != nil {
			return nil, err
		}
	}
	return builder, nil
}

func (b *defaultRuntimeBundleBuilder) Build(ctx context.Context, record SessionRecord, currentCWD codingtools.CWDProvider) (RuntimeBundle, error) {
	if currentCWD == nil {
		cwd := filepath.Clean(record.CWD)
		currentCWD = func() string { return cwd }
	}
	registry := b.registry
	var discoveryDiagnostics []systemprompt.Diagnostic
	if b.skillLoader != nil {
		loaded, err := b.skillLoader.Load(ctx, record.CWD)
		if err != nil {
			return RuntimeBundle{}, err
		}
		registry = loaded.Registry
		discoveryDiagnostics = loaded.Diagnostics
	}
	catalog, err := registry.CatalogSection()
	if err != nil {
		return RuntimeBundle{}, err
	}
	catalog.Diagnostics = append(catalog.Diagnostics, discoveryDiagnostics...)
	sections := []systemprompt.Section{catalog}
	if record.ParentSessionID != "" {
		sections = append(sections, systemprompt.StaticSection("session-lineage", systemprompt.SectionFeature,
			"This session's parent session ID is "+record.ParentSessionID+". This session has independent state."))
	}
	var subagents subagent.LoadResult
	if b.subagentLoader != nil {
		subagents, err = b.subagentLoader.Load(ctx, record.CWD)
		if err != nil {
			return RuntimeBundle{}, err
		}
		subagentCatalog, catalogErr := subagents.Catalog.CatalogSection()
		if catalogErr != nil {
			return RuntimeBundle{}, catalogErr
		}
		sections = append(sections, subagentCatalog)
	}
	request := systemprompt.Request{SessionID: record.ID, CWD: record.CWD}
	var result systemprompt.Result
	if b.context == nil {
		result, err = b.composer.BuildWith(ctx, request, sections...)
	} else {
		result, err = b.context.BuildWith(ctx, request, sections...)
	}
	if err != nil {
		return RuntimeBundle{}, err
	}
	if strings.TrimSpace(result.Prompt) == "" {
		return RuntimeBundle{}, errors.New("session prompt builder returned an empty prompt")
	}
	commands, err := promptcommands.NewRegistry()
	if err != nil {
		return RuntimeBundle{}, err
	}
	if b.promptCommandLoader != nil {
		commands, err = b.promptCommandLoader.Load(ctx, record.CWD)
		if err != nil {
			return RuntimeBundle{}, err
		}
	}
	tools := codingtools.NewDynamic(currentCWD)
	if b.attachmentStore != nil && b.presentImageEnabled(record) {
		tool, toolErr := showimage.New(showimage.Options{SessionID: record.ID, CWD: showimage.CWDProvider(currentCWD), Store: b.attachmentStore})
		if toolErr != nil {
			return RuntimeBundle{}, toolErr
		}
		tools = append(tools, tool)
	}
	if b.attachmentStore != nil && b.inspectImageEnabled(record) {
		tool, toolErr := inspectimage.New(inspectimage.Options{SessionID: record.ID, CWD: inspectimage.CWDProvider(currentCWD), Store: b.attachmentStore})
		if toolErr != nil {
			return RuntimeBundle{}, toolErr
		}
		tools = append(tools, tool)
	}
	tools = append(tools, registry.ActivateTool())
	if b.subagentLoader != nil {
		tool, toolErr := b.subagentToolFactory.Tool(record.ID, subagents.Catalog)
		if toolErr != nil {
			return RuntimeBundle{}, toolErr
		}
		tools = append(tools, tool)
	}
	if record.Persistent {
		if b.peerToolFactory != nil {
			tool, toolErr := b.peerToolFactory.Tool(record.ID)
			if toolErr != nil {
				return RuntimeBundle{}, toolErr
			}
			tools = append(tools, tool)
		}
		if b.sessionToolFactory != nil && record.ParentSessionID == "" {
			tool, toolErr := b.sessionToolFactory.Tool(record.ID)
			if toolErr != nil {
				return RuntimeBundle{}, toolErr
			}
			tools = append(tools, tool)
		}
	}
	manager, mcpServers, mcpWarnings, diagnostics, err := b.buildMCP(ctx, record.CWD)
	if err != nil {
		return RuntimeBundle{}, err
	}
	if manager != nil {
		tools = append(tools, manager.Tools()...)
	}
	return RuntimeBundle{
		Prompt: systemprompt.Result{
			Prompt:      result.Prompt,
			Sources:     append([]systemprompt.Source(nil), result.Sources...),
			Diagnostics: append(append([]systemprompt.Diagnostic(nil), result.Diagnostics...), diagnostics...),
		},
		Tools: tools, PromptCommands: commands, Subagents: subagents, MCP: manager,
		MCPServers: mcpServers, MCPWarnings: mcpWarnings,
	}, nil
}

// buildMCP resolves configured MCP servers for one session cwd. Configuration
// problems are reported as prompt diagnostics rather than failing the session,
// so one malformed file cannot make a session unusable.
func (b *defaultRuntimeBundleBuilder) buildMCP(ctx context.Context, cwd string) (*mcp.Manager, []MCPServerInfo, []string, []systemprompt.Diagnostic, error) {
	if b.mcpLoader == nil || b.mcpLauncher == nil {
		return nil, nil, nil, nil, nil
	}
	resolved, err := b.mcpLoader.Load(ctx, cwd)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("load MCP configuration: %w", err)
	}
	servers := make([]MCPServerInfo, 0, len(resolved.Servers))
	for _, server := range resolved.Servers {
		managedOAuth := server.Auth != nil && server.Auth.Kind == mcpconfig.AuthOAuth
		var saved func(context.Context) (bool, error)
		if managedOAuth {
			name, endpoint := server.Name, server.URL
			saved = func(statusCtx context.Context) (bool, error) {
				return b.mcpLauncher.OAuthSaved(statusCtx, name, endpoint)
			}
		}
		servers = append(servers, MCPServerInfo{Name: server.Name, Description: server.Description,
			Source: string(server.Source), Path: server.Path, Transport: string(server.Transport), Disabled: server.Disabled,
			ManagedOAuth: managedOAuth, OAuthSaved: saved})
	}
	diagnostics := make([]systemprompt.Diagnostic, 0, len(resolved.Diagnostics))
	warnings := make([]string, 0, len(resolved.Diagnostics))
	for _, diagnostic := range resolved.Diagnostics {
		warnings = append(warnings, safeMCPDiagnostic(diagnostic))
		diagnostics = append(diagnostics, systemprompt.Diagnostic{
			Severity: systemprompt.DiagnosticWarning, Code: "mcp.configuration", Message: diagnostic.Error(),
		})
	}
	enabled := resolved.Enabled()
	if len(enabled) == 0 {
		return nil, servers, warnings, diagnostics, nil
	}
	launched, err := b.mcpLauncher.Servers(cwd, enabled)
	if err != nil {
		return nil, servers, append(warnings, "MCP server setup failed."), append(diagnostics, systemprompt.Diagnostic{
			Severity: systemprompt.DiagnosticWarning, Code: "mcp.configuration", Message: err.Error(),
		}), nil
	}
	manager, err := mcp.NewManager(launched...)
	if err != nil {
		return nil, servers, append(warnings, "MCP server setup failed."), append(diagnostics, systemprompt.Diagnostic{
			Severity: systemprompt.DiagnosticWarning, Code: "mcp.configuration", Message: err.Error(),
		}), nil
	}
	return manager, servers, warnings, diagnostics, nil
}

func safeMCPDiagnostic(diagnostic mcpconfig.Diagnostic) string {
	location := diagnostic.Path
	if location == "" {
		location = string(diagnostic.Source)
	}
	if diagnostic.Server != "" && diagnostic.Field != "" {
		return fmt.Sprintf("%s: server %q has invalid %s configuration", location, diagnostic.Server, diagnostic.Field)
	}
	if diagnostic.Server != "" {
		return fmt.Sprintf("%s: server %q has invalid configuration", location, diagnostic.Server)
	}
	return location + ": invalid MCP configuration"
}

// SubagentDefinitions returns the immutable definition snapshot applied to a
// session runtime. It lazily loads an absent runtime without rebuilding one.
func (m *Manager) SubagentDefinitions(ctx context.Context, sessionID string) (subagent.LoadResult, error) {
	if err := m.beginOperation(); err != nil {
		return subagent.LoadResult{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return subagent.LoadResult{}, err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	return loaded.pluginContributions.snapshot(), nil
}

// PromptMetadata is the provenance and non-fatal diagnostics for the prompt
// currently applied to one loaded runtime.
type PromptMetadata struct {
	Sources     []systemprompt.Source
	Diagnostics []systemprompt.Diagnostic
}

// PromptMetadata returns a copy of the prompt metadata applied to a session's
// authoritative runtime. It lazily loads an absent runtime but never rebuilds
// an already loaded one.
func (m *Manager) PromptMetadata(ctx context.Context, sessionID string) (PromptMetadata, error) {
	if err := m.beginOperation(); err != nil {
		return PromptMetadata{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return PromptMetadata{}, err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	return promptMetadata(loaded), nil
}

func nilRuntimeBundleBuilder(builder RuntimeBundleBuilder) bool {
	if builder == nil {
		return true
	}
	value := reflect.ValueOf(builder)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
