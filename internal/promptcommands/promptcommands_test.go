package promptcommands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
)

func TestFilesystemLoaderDiscoversGlobalThenProjectNonRecursively(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	globalReview := writeTemplate(t, paths.Prompts, "review.md", "---\ndescription: Global review\n---\nReview $1 with $@.")
	writeTemplate(t, filepath.Join(cwd, projectPromptsPath), "review.md", "Project review")
	projectSummary := writeTemplate(t, filepath.Join(cwd, projectPromptsPath), "summary.md", "\nSummarize $ARGUMENTS")
	writeTemplate(t, filepath.Join(cwd, projectPromptsPath, "nested"), "ignored.md", "Ignored")

	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	commands := registry.Commands()
	if got, want := commandNames(commands), []string{"review", "summary"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
	review, _ := registry.Lookup("review")
	if review.Source != SourceUser || review.Location != globalReview || review.Description != "Global review" {
		t.Fatalf("review command = %#v", review)
	}
	summary, _ := registry.Lookup("summary")
	if summary.Source != SourceProject || summary.Location != projectSummary || summary.Description != "Summarize $ARGUMENTS" {
		t.Fatalf("summary command = %#v", summary)
	}
	if _, ok := registry.Lookup("ignored"); ok {
		t.Fatal("prompt discovery recursed")
	}
}

func TestCommandExpandMatchesQuotedMainWorktreeBehavior(t *testing.T) {
	command := Command{Content: "Review $1 and $2. All: $@ / $ARGUMENTS. Tail: ${@:2}. Slice: ${@:1:2}. Missing: $9"}
	expanded, err := command.Expand(`"auth module" carefully extra`)
	if err != nil {
		t.Fatal(err)
	}
	want := "Review auth module and carefully. All: auth module carefully extra / auth module carefully extra. Tail: carefully extra. Slice: auth module carefully. Missing:"
	if expanded != want {
		t.Fatalf("expanded = %q, want %q", expanded, want)
	}
	if got, want := ParseArgs(`one 'two words' "three words"`), []string{"one", "two words", "three words"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseArgs() = %#v, want %#v", got, want)
	}
	command.Content = "$1"
	expanded, err = command.Expand(`'x$@' y`)
	if err != nil || expanded != "xx$@ y" {
		t.Fatalf("placeholder-like argument compatibility = %q, %v", expanded, err)
	}
}

func TestCommandExpandBoundsOutputAndHandlesLargeSliceNumbers(t *testing.T) {
	command := Command{Content: strings.Repeat("$1", maxTemplateBytes/2)}
	if _, err := command.Expand(strings.Repeat("x", maxTemplateBytes/2)); err == nil {
		t.Fatal("Expand() accepted oversized output")
	}
	command.Content = "Tail: ${@:2:9223372036854775807}"
	expanded, err := command.Expand("one two three")
	if err != nil || expanded != "Tail: two three" {
		t.Fatalf("large slice expansion = %q, %v", expanded, err)
	}
}

func TestRegistryRejectsRendererUnsafeMetadata(t *testing.T) {
	base := Command{Name: "review", Description: "Review", Content: "body", Source: SourceProject, Location: "/repo/review.md"}
	for _, mutate := range []func(*Command){
		func(command *Command) { command.Name = "re\u200bview" },
		func(command *Command) { command.Description = "Review\x1b[31m" },
		func(command *Command) { command.Location = "/repo/\x1b.md" },
	} {
		command := base
		mutate(&command)
		if _, err := NewRegistry(command); err == nil {
			t.Fatalf("NewRegistry() accepted unsafe command %#v", command)
		}
	}
}

func TestFilesystemLoaderRefreshesAddChangeAndRemove(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := loader.Load(t.Context(), cwd)
	if err != nil || len(registry.Commands()) != 0 {
		t.Fatalf("empty Load() = %#v, %v", registry.Commands(), err)
	}
	path := writeTemplate(t, filepath.Join(cwd, projectPromptsPath), "check.md", "First $@")
	registry, err = loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	command, ok := registry.Lookup("check")
	if !ok || command.Content != "First $@" {
		t.Fatalf("added command = %#v, %v", command, ok)
	}
	if err := os.WriteFile(path, []byte("Changed $@"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err = loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	command, _ = registry.Lookup("check")
	if command.Content != "Changed $@" {
		t.Fatalf("changed command = %#v", command)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	registry, err = loader.Load(t.Context(), cwd)
	if err != nil || len(registry.Commands()) != 0 {
		t.Fatalf("removed Load() = %#v, %v", registry.Commands(), err)
	}
}

func TestFilesystemLoaderHonorsCancellationAndRejectsEscapingRoots(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	writeTemplate(t, outside, "escaped.md", "Escape")
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, paths.Prompts); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cwd, projectPromptsPath)); err != nil {
		t.Fatal(err)
	}
	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := loader.Load(t.Context(), cwd)
	if err != nil || len(registry.Commands()) != 0 {
		t.Fatalf("escaped Load() = %#v, %v", registry.Commands(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.Load(ctx, cwd); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Load() = %v", err)
	}
}

func writeTemplate(t *testing.T, root, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func commandNames(commands []Command) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Name)
	}
	return names
}
