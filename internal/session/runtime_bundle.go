package session

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/systemprompt"
)

// RuntimeBundle is one atomic prompt and tool configuration for a session
// runtime. Prompt provenance remains server-owned and is not conversation data.
type RuntimeBundle struct {
	Prompt systemprompt.Result
	Tools  []droids.AnyTool
}

// RuntimeBundleBuilder resolves the prompt and cwd-bound tools applicable to a
// session runtime. Build may be called concurrently for different sessions. One
// call must return a mutually consistent immutable snapshot.
type RuntimeBundleBuilder interface {
	Build(context.Context, SessionRecord) (RuntimeBundle, error)
}

func cloneRuntimeBundle(bundle RuntimeBundle) RuntimeBundle {
	bundle.Prompt.Sources = append([]systemprompt.Source(nil), bundle.Prompt.Sources...)
	bundle.Prompt.Diagnostics = append([]systemprompt.Diagnostic(nil), bundle.Prompt.Diagnostics...)
	bundle.Tools = append([]droids.AnyTool(nil), bundle.Tools...)
	return bundle
}

// RuntimeBundleOptions configures Kit's standard session bundle builder.
type RuntimeBundleOptions struct {
	Core     string
	Context  *systemprompt.ContextBuilderOptions
	Registry *skills.Registry
}

type defaultRuntimeBundleBuilder struct {
	promptBuilder systemprompt.Builder
	registry      *skills.Registry
}

// NewRuntimeBundleBuilder constructs Kit's standard atomic prompt/tool builder.
// It installs the registry's catalog itself so the advertised skills and the
// activation tool cannot be configured independently.
func NewRuntimeBundleBuilder(options RuntimeBundleOptions) (RuntimeBundleBuilder, error) {
	if options.Registry == nil {
		return nil, errors.New("session skill registry is required")
	}
	composer, err := systemprompt.New(options.Core)
	if err != nil {
		return nil, err
	}
	catalog, err := options.Registry.CatalogSection()
	if err != nil {
		return nil, err
	}
	if _, err := composer.Set(catalog); err != nil {
		return nil, err
	}
	var promptBuilder systemprompt.Builder = composer
	if options.Context != nil {
		promptBuilder, err = systemprompt.NewContextBuilder(composer, *options.Context)
		if err != nil {
			return nil, err
		}
	}
	return &defaultRuntimeBundleBuilder{promptBuilder: promptBuilder, registry: options.Registry}, nil
}

func (b *defaultRuntimeBundleBuilder) Build(ctx context.Context, record SessionRecord) (RuntimeBundle, error) {
	result, err := b.promptBuilder.Build(ctx, systemprompt.Request{SessionID: record.ID, CWD: record.CWD})
	if err != nil {
		return RuntimeBundle{}, err
	}
	if strings.TrimSpace(result.Prompt) == "" {
		return RuntimeBundle{}, errors.New("session prompt builder returned an empty prompt")
	}
	tools := codingtools.New(record.CWD)
	tools = append(tools, b.registry.ActivateTool())
	return RuntimeBundle{
		Prompt: systemprompt.Result{
			Prompt:      result.Prompt,
			Sources:     append([]systemprompt.Source(nil), result.Sources...),
			Diagnostics: append([]systemprompt.Diagnostic(nil), result.Diagnostics...),
		},
		Tools: tools,
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
	loaded.controlMu.Lock()
	defer loaded.controlMu.Unlock()
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
