package session_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
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
	})
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

type typedNilRuntimeBundleBuilder struct{}

func (*typedNilRuntimeBundleBuilder) Build(context.Context, session.SessionRecord) (session.RuntimeBundle, error) {
	panic("unexpected call")
}
