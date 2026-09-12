package session

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/promptcommands"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
)

// RuntimeBundle is one atomic prompt and tool configuration for a session
// runtime. Prompt provenance remains server-owned and is not conversation data.
type RuntimeBundle struct {
	Prompt         systemprompt.Result
	Tools          []droids.AnyTool
	PromptCommands *promptcommands.Registry
	Subagents      subagent.LoadResult
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
}

type defaultRuntimeBundleBuilder struct {
	composer            *systemprompt.Composer
	context             *systemprompt.ContextBuilder
	registry            *skills.Registry
	skillLoader         skills.Loader
	promptCommandLoader promptcommands.Loader
	subagentLoader      subagent.Loader
	subagentToolFactory subagent.ParentToolFactory
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
	composer, err := systemprompt.New(options.Core)
	if err != nil {
		return nil, err
	}
	builder := &defaultRuntimeBundleBuilder{
		composer: composer, registry: options.Registry, skillLoader: options.SkillLoader,
		promptCommandLoader: options.PromptCommandLoader,
		subagentLoader:      options.SubagentLoader, subagentToolFactory: options.SubagentToolFactory,
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
	tools = append(tools, registry.ActivateTool())
	if b.subagentLoader != nil {
		tool, toolErr := b.subagentToolFactory.Tool(record.ID, subagents.Catalog)
		if toolErr != nil {
			return RuntimeBundle{}, toolErr
		}
		tools = append(tools, tool)
	}
	return RuntimeBundle{
		Prompt: systemprompt.Result{
			Prompt:      result.Prompt,
			Sources:     append([]systemprompt.Source(nil), result.Sources...),
			Diagnostics: append([]systemprompt.Diagnostic(nil), result.Diagnostics...),
		},
		Tools: tools, PromptCommands: commands, Subagents: subagents,
	}, nil
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
	catalog, err := subagent.NewCatalog(loaded.bundle.Subagents.Catalog.Definitions()...)
	if err != nil {
		return subagent.LoadResult{}, err
	}
	return subagent.LoadResult{
		Catalog: catalog, Diagnostics: append([]subagent.Diagnostic(nil), loaded.bundle.Subagents.Diagnostics...),
	}, nil
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
