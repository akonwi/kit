package subagent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/subagent"
)

func TestFilesystemLoaderUsesUserThenProjectFirstLoadedWins(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeAgent(t, filepath.Join(home, "agents", "z-user.md"), "scout", "user scout", "openai/gpt-5", "Follow user instructions.")
	writeAgent(t, filepath.Join(home, "agents", "a-reviewer.md"), "reviewer", "reviews code", "", "Review carefully.")
	writeAgent(t, filepath.Join(cwd, ".kit", "agents", "a-project.md"), "scout", "project scout", "", "Project instructions.")
	writeAgent(t, filepath.Join(cwd, ".kit", "agents", "z-builder.md"), "builder", "builds code", "", "Build carefully.")

	loader := newLoader(t, home)
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	definitions := result.Catalog.Definitions()
	gotNames := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		gotNames = append(gotNames, definition.Name)
	}
	if want := []string{"builder", "reviewer", "scout"}; !slices.Equal(gotNames, want) {
		t.Fatalf("names = %#v, want %#v", gotNames, want)
	}
	scout, ok := result.Catalog.Lookup("scout")
	if !ok || scout.Description != "user scout" || scout.Model != "openai/gpt-5" || scout.Source.Kind != subagent.SourceUser {
		t.Fatalf("scout = %#v, true", scout)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "subagents.duplicate_name" {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}

func TestFilesystemLoaderReportsMalformedDefinitionsAndContinues(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	directory := filepath.Join(home, "agents")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"missing-frontmatter.md": "just instructions",
		"missing-name.md":        "---\ndescription: missing\n---\nDo work.",
		"missing-description.md": "---\nname: no-description\n---\nDo work.",
		"missing-body.md":        "---\nname: no-body\ndescription: empty body\n---\n",
		"unknown-field.md":       "---\nname: unknown\ndescription: unknown\nextra: no\n---\nDo work.",
		"production-model.md":    "---\nname: model\ndescription: production model\nmodel: gpt-5\n---\nDo work.",
		"valid.md":               "---\nname: valid\ndescription: valid agent\n---\nDo work.",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "invalid-utf8.md"), []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nul.md"), []byte("---\nname: nul\ndescription: nul\n---\nA\x00B"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := newLoader(t, home).Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	definitions := result.Catalog.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "model" || definitions[0].Model != "gpt-5" || definitions[1].Name != "valid" {
		t.Fatalf("definitions = %#v", definitions)
	}
	if len(result.Diagnostics) != 7 {
		t.Fatalf("diagnostic count = %d, want 7: %#v", len(result.Diagnostics), result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity != subagent.DiagnosticWarning || diagnostic.Code == "" || diagnostic.Message == "" || diagnostic.Source.Path == "" {
			t.Fatalf("invalid diagnostic %#v", diagnostic)
		}
	}
}

func TestFilesystemLoaderAcceptsReadableFileSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is Unix-specific")
	}
	home := t.TempDir()
	cwd := t.TempDir()
	target := filepath.Join(home, "definition-target")
	writeAgent(t, target, "scout", "finds things", "", "Inspect the repository.")
	link := filepath.Join(home, "agents", "scout.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	result, err := newLoader(t, home).Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := result.Catalog.Lookup("scout")
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	expectedLink := filepath.Join(canonicalHome, "agents", "scout.md")
	if !ok || definition.Source.Path != expectedLink {
		t.Fatalf("definition = %#v, found %v", definition, ok)
	}
}

func TestFilesystemLoaderReportsBrokenSymlinkAndIgnoresNestedFiles(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	directory := filepath.Join(home, "agents")
	if err := os.MkdirAll(filepath.Join(directory, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeAgent(t, filepath.Join(directory, "nested", "hidden.md"), "hidden", "hidden", "", "Do hidden work.")
	if err := os.Symlink(filepath.Join(directory, "missing"), filepath.Join(directory, "broken.md")); err != nil {
		t.Fatal(err)
	}
	result, err := newLoader(t, home).Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if result.Catalog.Len() != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "subagents.unreadable" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFilesystemLoaderHonorsCancellationAndBounds(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newLoader(t, home).Load(ctx, cwd)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want canceled", err)
	}

	directory := filepath.Join(home, "agents")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	oversized := "---\nname: huge\ndescription: huge\n---\n" + strings.Repeat("x", 128<<10)
	if err := os.WriteFile(filepath.Join(directory, "huge.md"), []byte(oversized), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := newLoader(t, home).Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if result.Catalog.Len() != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "subagents.oversized" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogValidationAndStateTransitions(t *testing.T) {
	definition := subagent.Definition{
		Name: "scout", Description: "finds things", Instructions: "Inspect.",
		Source: subagent.Source{Kind: subagent.SourcePlugin, Path: "plugin://example/scout", PluginID: "example"},
	}
	catalog, err := subagent.NewCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Len() != 1 || !subagent.ValidTaskTransition(subagent.TaskQueued, subagent.TaskRunning) ||
		!subagent.ValidTaskTransition(subagent.TaskRunning, subagent.TaskInterrupted) ||
		subagent.ValidTaskTransition(subagent.TaskCompleted, subagent.TaskRunning) ||
		!subagent.TerminalTask(subagent.TaskAborted) {
		t.Fatal("unexpected catalog or state transition behavior")
	}
	if err := subagent.DefaultLimits().Validate(); err != nil {
		t.Fatal(err)
	}
}

func newLoader(t *testing.T, home string) *subagent.FilesystemLoader {
	t.Helper()
	loader, err := subagent.NewFilesystemLoader(apphome.FromHome(home))
	if err != nil {
		t.Fatal(err)
	}
	return loader
}

func writeAgent(t *testing.T, path, name, description, model, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n"
	if model != "" {
		content += "model: " + model + "\n"
	}
	content += "---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
