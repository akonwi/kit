package systemprompt

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultCoreExact(t *testing.T) {
	const want = `You are Kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.`
	if DefaultCore != want {
		t.Fatalf("DefaultCore = %q, want %q", DefaultCore, want)
	}
}

func TestNewRequiresCore(t *testing.T) {
	if _, err := New(" \n\t"); err == nil {
		t.Fatal("New() accepted an empty core prompt")
	}
}

func TestComposerRejectsUninitializedValue(t *testing.T) {
	var composer Composer
	if _, err := composer.Set(StaticSection("policy", SectionFeature, "text")); err == nil {
		t.Fatal("Set() accepted an uninitialized composer")
	}
	if _, err := composer.Build(context.Background(), Request{}); err == nil {
		t.Fatal("Build() accepted an uninitialized composer")
	}
}

func TestComposerBuildsDefaultCore(t *testing.T) {
	result, err := NewDefault().Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore {
		t.Fatalf("Prompt = %q, want %q", result.Prompt, DefaultCore)
	}
	wantSources := []Source{{SectionID: "kit.core", ID: "kit.core", Kind: SectionCore}}
	if !reflect.DeepEqual(result.Sources, wantSources) {
		t.Fatalf("Sources = %#v, want %#v", result.Sources, wantSources)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestComposerOrdersNamedSectionsAndSeparatesNonEmptyText(t *testing.T) {
	composer, err := New(" core \n")
	if err != nil {
		t.Fatal(err)
	}
	sections := []Section{
		StaticSection("z-plugin", SectionPlugin, " plugin "),
		StaticSection("z-feature", SectionFeature, " feature z "),
		StaticSection("context", SectionContext, " context "),
		StaticSection("skills", SectionSkillCatalog, " skills "),
		StaticSection("a-feature", SectionFeature, " feature a "),
		StaticSection("empty", SectionFeature, " \n "),
	}
	for _, section := range sections {
		if _, err := composer.Set(section); err != nil {
			t.Fatal(err)
		}
	}

	result, err := composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	wantPrompt := strings.Join([]string{
		"core",
		"feature a",
		"feature z",
		"skills",
		"plugin",
		"context",
	}, "\n\n")
	if result.Prompt != wantPrompt {
		t.Fatalf("Prompt = %q, want %q", result.Prompt, wantPrompt)
	}
	wantSources := []Source{
		{SectionID: "kit.core", ID: "kit.core", Kind: SectionCore},
		{SectionID: "a-feature", ID: "a-feature", Kind: SectionFeature},
		{SectionID: "z-feature", ID: "z-feature", Kind: SectionFeature},
		{SectionID: "skills", ID: "skills", Kind: SectionSkillCatalog},
		{SectionID: "z-plugin", ID: "z-plugin", Kind: SectionPlugin},
		{SectionID: "context", ID: "context", Kind: SectionContext},
	}
	if !reflect.DeepEqual(result.Sources, wantSources) {
		t.Fatalf("Sources = %#v, want %#v", result.Sources, wantSources)
	}
}

func TestComposerCarriesStampedSourcesAndDiagnostics(t *testing.T) {
	composer := NewDefault()
	section := Section{
		ID:   "context",
		Kind: SectionContext,
		Text: " project guidance ",
		Sources: []Source{{
			ID:   "project-agents",
			Kind: SectionCore,
			Path: "/repo/AGENTS.md",
		}},
		Diagnostics: []Diagnostic{{
			Severity: DiagnosticWarning,
			Code:     "context.unreadable",
			Message:  " could not read nested guidance ",
			Source: Source{
				ID:   "nested-agents",
				Kind: SectionCore,
				Path: "/repo/app/AGENTS.md",
			},
		}},
	}
	if _, err := composer.Set(section); err != nil {
		t.Fatal(err)
	}

	result, err := composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	wantSource := Source{
		SectionID: "context",
		ID:        "project-agents",
		Kind:      SectionContext,
		Path:      "/repo/AGENTS.md",
	}
	if got := result.Sources[len(result.Sources)-1]; got != wantSource {
		t.Fatalf("last source = %#v, want %#v", got, wantSource)
	}
	wantDiagnostic := Diagnostic{
		Severity: DiagnosticWarning,
		Code:     "context.unreadable",
		Message:  "could not read nested guidance",
		Source: Source{
			SectionID: "context",
			ID:        "nested-agents",
			Kind:      SectionContext,
			Path:      "/repo/app/AGENTS.md",
		},
	}
	if !reflect.DeepEqual(result.Diagnostics, []Diagnostic{wantDiagnostic}) {
		t.Fatalf("Diagnostics = %#v, want %#v", result.Diagnostics, []Diagnostic{wantDiagnostic})
	}
}

func TestComposerKeepsDiagnosticsFromEmptySection(t *testing.T) {
	composer := NewDefault()
	section := Section{
		ID:   "context",
		Kind: SectionContext,
		Diagnostics: []Diagnostic{{
			Severity: DiagnosticInfo,
			Code:     "context.none",
			Message:  "no project guidance",
		}},
	}
	if _, err := composer.Set(section); err != nil {
		t.Fatal(err)
	}
	result, err := composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore {
		t.Fatalf("Prompt = %q, want default core", result.Prompt)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("Sources = %#v, want core only", result.Sources)
	}
	want := Diagnostic{
		Severity: DiagnosticInfo,
		Code:     "context.none",
		Message:  "no project guidance",
		Source: Source{
			SectionID: "context",
			ID:        "context",
			Kind:      SectionContext,
		},
	}
	if !reflect.DeepEqual(result.Diagnostics, []Diagnostic{want}) {
		t.Fatalf("Diagnostics = %#v, want %#v", result.Diagnostics, []Diagnostic{want})
	}
}

func TestComposerReplacementHasGenerationBoundRemoval(t *testing.T) {
	composer := NewDefault()
	removeOld, err := composer.Set(StaticSection("policy", SectionFeature, "old"))
	if err != nil {
		t.Fatal(err)
	}
	removeCurrent, err := composer.Set(StaticSection("policy", SectionFeature, "current"))
	if err != nil {
		t.Fatal(err)
	}

	removeOld()
	removeOld()
	result, err := composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore+"\n\ncurrent" {
		t.Fatalf("Prompt after stale removal = %q", result.Prompt)
	}

	removeCurrent()
	removeCurrent()
	result, err = composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore {
		t.Fatalf("Prompt after current removal = %q", result.Prompt)
	}
}

func TestComposerKeepsSameIDInDifferentKinds(t *testing.T) {
	composer := NewDefault()
	for _, section := range []Section{
		StaticSection("policy", SectionPlugin, "plugin"),
		StaticSection("policy", SectionFeature, "feature"),
	} {
		if _, err := composer.Set(section); err != nil {
			t.Fatal(err)
		}
	}
	result, err := composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultCore + "\n\nfeature\n\nplugin"
	if result.Prompt != want {
		t.Fatalf("Prompt = %q, want %q", result.Prompt, want)
	}
}

func TestComposerBuildWithOverlaysRequestSectionWithoutMutation(t *testing.T) {
	composer := NewDefault()
	if _, err := composer.Set(StaticSection("context", SectionContext, "registered")); err != nil {
		t.Fatal(err)
	}

	result, err := composer.BuildWith(
		context.Background(),
		Request{SessionID: "session_1", CWD: "/repo"},
		StaticSection("context", SectionContext, "request context"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore+"\n\nrequest context" {
		t.Fatalf("overlaid Prompt = %q", result.Prompt)
	}

	result, err = composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore+"\n\nregistered" {
		t.Fatalf("registered Prompt = %q", result.Prompt)
	}
}

func TestComposerCopiesRegisteredSectionData(t *testing.T) {
	composer := NewDefault()
	section := Section{
		ID:      "context",
		Kind:    SectionContext,
		Text:    "first",
		Sources: []Source{{ID: "source", Path: "/first"}},
	}
	if _, err := composer.Set(section); err != nil {
		t.Fatal(err)
	}
	section.Text = "second"
	section.Sources[0].Path = "/second"

	result, err := composer.Build(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore+"\n\nfirst" {
		t.Fatalf("Prompt = %q", result.Prompt)
	}
	if got := result.Sources[len(result.Sources)-1].Path; got != "/first" {
		t.Fatalf("source path = %q, want /first", got)
	}
}

func TestComposerRejectsInvalidSectionsAndMetadata(t *testing.T) {
	composer := NewDefault()
	tests := []Section{
		StaticSection("", SectionFeature, "text"),
		StaticSection(" spaced ", SectionFeature, "text"),
		StaticSection("core", SectionCore, "text"),
		{ID: "source", Kind: SectionContext, Text: "text", Sources: []Source{{}}},
		{ID: "duplicate", Kind: SectionContext, Text: "text", Sources: []Source{{ID: "same"}, {ID: "same"}}},
		{ID: "severity", Kind: SectionContext, Diagnostics: []Diagnostic{{Severity: "fatal", Code: "bad", Message: "bad"}}},
		{ID: "code", Kind: SectionContext, Diagnostics: []Diagnostic{{Severity: DiagnosticWarning, Message: "bad"}}},
		{ID: "message", Kind: SectionContext, Diagnostics: []Diagnostic{{Severity: DiagnosticWarning, Code: "bad"}}},
	}
	for _, section := range tests {
		if _, err := composer.Set(section); err == nil {
			t.Fatalf("Set(%#v) succeeded", section)
		}
	}
}

func TestComposerHonorsCanceledContext(t *testing.T) {
	composer := NewDefault()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := composer.Build(ctx, Request{}); err != context.Canceled {
		t.Fatalf("Build() error = %v, want context canceled", err)
	}
}
