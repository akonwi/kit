package session_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestRuntimeBundleBuilderOwnsMatchingSkillCatalogAndTool(t *testing.T) {
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	builder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{
		Core: "core", Registry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := builder.Build(t.Context(), session.SessionRecord{
		ID: "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CWD: t.TempDir(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.Prompt.Prompt, "<name>kit-customization</name>") {
		t.Fatalf("bundle prompt has no built-in skill catalog:\n%s", bundle.Prompt.Prompt)
	}
	if len(bundle.Tools) != 8 {
		t.Fatalf("bundle tools = %d, want 8", len(bundle.Tools))
	}
	var catalogSources int
	for _, source := range bundle.Prompt.Sources {
		if source.Kind == systemprompt.SectionSkillCatalog && source.ID == "skill:kit-customization" {
			catalogSources++
		}
	}
	if catalogSources != 1 {
		t.Fatalf("catalog sources = %#v", bundle.Prompt.Sources)
	}
}

func TestRuntimeBundleBuilderOwnsMatchingSubagentCatalogAndTool(t *testing.T) {
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	definition := subagent.Definition{
		Name: "scout", Description: "finds things", Instructions: "Inspect.",
		Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/scout.md"},
	}
	catalog, err := subagent.NewCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	loader := &fixedSubagentLoader{result: subagent.LoadResult{Catalog: catalog}}
	factory := &recordingSubagentToolFactory{}
	builder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{
		Core: "core", Registry: registry, SubagentLoader: loader, SubagentToolFactory: factory,
	})
	if err != nil {
		t.Fatal(err)
	}
	record := session.SessionRecord{ID: "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CWD: t.TempDir()}
	bundle, err := builder.Build(t.Context(), record, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Tools) != 9 || bundle.Subagents.Catalog.Len() != 1 || factory.owner != record.ID || factory.catalog.Len() != 1 {
		t.Fatalf("bundle/factory mismatch: tools=%d definitions=%d owner=%q factory definitions=%d", len(bundle.Tools), bundle.Subagents.Catalog.Len(), factory.owner, factory.catalog.Len())
	}
	copy := bundle.Subagents.Catalog.Definitions()
	copy[0].Name = "mutated"
	if definition, ok := bundle.Subagents.Catalog.Lookup("scout"); !ok || definition.Name != "scout" {
		t.Fatalf("catalog was mutable: %#v, %v", definition, ok)
	}
}

func TestRuntimeBundleBuilderRejectsInvalidDependencies(t *testing.T) {
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: "core"}); err == nil {
		t.Fatal("NewRuntimeBundleBuilder() accepted a nil registry")
	}
	if _, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Registry: registry}); err == nil {
		t.Fatal("NewRuntimeBundleBuilder() accepted an empty core")
	}
	if _, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{Core: "core", Registry: registry, SubagentLoader: &fixedSubagentLoader{}}); err == nil {
		t.Fatal("NewRuntimeBundleBuilder() accepted a subagent loader without a tool factory")
	}
}

func TestManagerRejectsNilRuntimeBundleBuilder(t *testing.T) {
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := session.NewManager(store, &authorityProviders{}, nil); err == nil {
		t.Fatal("NewManager() accepted a nil runtime bundle builder")
	}
	var typedNil *typedNilRuntimeBundleBuilder
	if _, err := session.NewManager(store, &authorityProviders{}, typedNil); err == nil {
		t.Fatal("NewManager() accepted a typed-nil runtime bundle builder")
	}
}

type fixedSubagentLoader struct{ result subagent.LoadResult }

func (l *fixedSubagentLoader) Load(context.Context, string) (subagent.LoadResult, error) {
	return l.result, nil
}

type recordingSubagentToolFactory struct {
	owner   string
	catalog subagent.Catalog
}

func (f *recordingSubagentToolFactory) Tool(owner string, catalog subagent.Catalog) (droids.AnyTool, error) {
	f.owner, f.catalog = owner, catalog
	return droids.NewTool(droids.Tool[struct{}]{
		Name: "subagent", Description: "delegate", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, droids.ToolContext, struct{}, droids.ToolUpdate) (droids.ToolResult, error) {
			return droids.ToolText("ok"), nil
		},
	})
}

type typedNilRuntimeBundleBuilder struct{}

func (*typedNilRuntimeBundleBuilder) Build(context.Context, session.SessionRecord, codingtools.CWDProvider) (session.RuntimeBundle, error) {
	panic("unexpected call")
}
