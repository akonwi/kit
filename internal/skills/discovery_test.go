package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
)

func TestFilesystemLoaderDiscoversGlobalAndProjectSkillsWithPrecedence(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	globalPath := writeSkill(t, paths.Skills, "review", "Review globally", "global body", false)
	projectPath := writeSkill(t, filepath.Join(cwd, ".agents", "skills"), "review", "Review locally", "project body", false)
	debugPath := writeSkill(t, filepath.Join(cwd, ".agents", "skills"), "debug", "Debug failures", "debug body", false)

	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := skillNames(result.Registry.Skills()), []string{"debug", KitCustomizationName, "review"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skill names = %#v, want %#v", got, want)
	}
	review, _ := result.Registry.Lookup("review")
	if review.Source != SourceUser || review.Location != globalPath || !strings.Contains(review.Content, "global body") {
		t.Fatalf("precedence winner = %#v", review)
	}
	debug, _ := result.Registry.Lookup("debug")
	if debug.Source != SourceProject || debug.Location != debugPath {
		t.Fatalf("project skill = %#v", debug)
	}
	if !hasSkillDiagnostic(result, "skills.duplicate_name", projectPath) {
		t.Fatalf("discovery diagnostics = %#v", result.Diagnostics)
	}
	activation, err := result.Registry.newActivateTool().Execute(t.Context(), droids.ToolContext{}, activateArgs{Name: "debug"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := toolResultText(t, activation); !strings.Contains(got, "debug body") || !strings.Contains(got, "name: debug") {
		t.Fatalf("project skill activation = %q", got)
	}
}

func TestFilesystemLoaderStopsAtSkillRootAndHidesDisabledSkillFromCatalog(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	root := filepath.Join(cwd, ".agents", "skills")
	parentPath := writeSkill(t, root, "parent", "Parent skill", "parent body", true)
	writeSkill(t, filepath.Join(root, "parent"), "child", "Child skill", "child body", false)

	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	parent, ok := result.Registry.Lookup("parent")
	if !ok || parent.Location != parentPath || !parent.DisableModelInvocation {
		t.Fatalf("parent skill = %#v, found=%v", parent, ok)
	}
	if _, ok := result.Registry.Lookup("child"); ok {
		t.Fatal("discovery recursed below a skill root")
	}
	catalog, err := result.Registry.CatalogSection()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(catalog.Text, "<name>parent</name>") || !strings.Contains(catalog.Text, "<name>kit-customization</name>") {
		t.Fatalf("disabled skill catalog =\n%s", catalog.Text)
	}
	unknown, err := result.Registry.newActivateTool().Execute(t.Context(), droids.ToolContext{}, activateArgs{Name: "missing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text := toolResultText(t, unknown); strings.Contains(text, "parent") {
		t.Fatalf("disabled skill leaked through unknown response: %q", text)
	}
}

func TestFilesystemLoaderReportsInvalidAndReservedDefinitions(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	root := filepath.Join(cwd, ".agents", "skills")
	reserved := writeSkill(t, root, KitCustomizationName, "Shadow built-in", "shadow", false)
	mismatchDir := filepath.Join(root, "actual-name")
	if err := os.MkdirAll(mismatchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mismatch := filepath.Join(mismatchDir, "SKILL.md")
	if err := os.WriteFile(mismatch, []byte("---\nname: other-name\ndescription: mismatch\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidDir := filepath.Join(root, "invalid")
	if err := os.MkdirAll(invalidDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(invalidDir, "SKILL.md")
	if err := os.WriteFile(invalid, []byte("---\nname: [\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Registry.Skills()) != 1 || !hasSkillDiagnostic(result, "skills.reserved_name", reserved) ||
		!hasSkillDiagnostic(result, "skills.name_mismatch", mismatch) || !hasSkillDiagnostic(result, "skills.invalid_frontmatter", invalid) {
		t.Fatalf("invalid discovery result = skills:%#v diagnostics:%#v", result.Registry.Skills(), result.Diagnostics)
	}
	state := discoveryState{}
	state.warn("skills.test", strings.Repeat("message", 1024), "/"+string([]byte{'b', 'a', 'd', 0xff})+strings.Repeat("x", maxDiagnosticMessage))
	diagnostic := state.diagnostics[0]
	if !utf8.ValidString(diagnostic.Source.Path) || len(diagnostic.Source.Path) > maxDiagnosticMessage || len(diagnostic.Message) > maxDiagnosticMessage {
		t.Fatalf("unbounded diagnostic = %#v", diagnostic)
	}
}

func TestFilesystemLoaderRejectsReplacedGlobalRoot(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	for _, directory := range []string{paths.Home, cwd} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(paths.Home, paths.Home+"-original"); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, paths.Skills, "replacement", "Replacement skill", "replacement", false)
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Registry.Lookup("replacement"); ok || !hasSkillDiagnostic(result, "skills.root_replaced", paths.Skills) {
		t.Fatalf("replaced-root result = skills:%#v diagnostics:%#v", result.Registry.Skills(), result.Diagnostics)
	}
}

func TestFilesystemLoaderBoundsDirectoryEntries(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	root := filepath.Join(cwd, ".agents", "skills")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= maxDirectoryEntries; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("entry-%04d", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSkillDiagnostic(result, "skills.entry_limit", root) {
		t.Fatalf("entry-limit diagnostics = %#v", result.Diagnostics)
	}
}

func TestFilesystemLoaderFollowsSymlinkedSkillRoot(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	writeSkill(t, outside, "shared", "Shared skill", "outside", false)
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, paths.Skills); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	canonicalHome, err := filepath.EvalSymlinks(paths.Home)
	if err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(canonicalHome, "skills", "shared", "SKILL.md")
	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	skill, ok := result.Registry.Lookup("shared")
	if !ok || skill.Location != skillPath || !strings.Contains(skill.Content, "outside") {
		t.Fatalf("symlinked-root skill = %#v, found=%v diagnostics=%#v", skill, ok, result.Diagnostics)
	}
}

func TestFilesystemLoaderFollowsSymlinkedSkillDirectoryAndFile(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	root := filepath.Join(cwd, ".agents", "skills")
	outside := filepath.Join(base, "outside")
	originalDirectoryTarget := filepath.Join(outside, "linked-directory")
	writeSkill(t, outside, "linked-directory", "Linked directory", "directory body", false)
	directoryTarget := filepath.Join(outside, "physical-target")
	if err := os.Rename(originalDirectoryTarget, directoryTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	directoryLink := filepath.Join(root, "linked-directory")
	if err := os.Symlink(directoryTarget, directoryLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	canonicalCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	directorySkillPath := filepath.Join(canonicalCWD, ".agents", "skills", "linked-directory", "SKILL.md")
	fileDirectory := filepath.Join(root, "linked-file")
	if err := os.MkdirAll(fileDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	fileTarget := writeSkill(t, outside, "linked-file", "Linked file", "file body", false)
	if err := os.Symlink(fileTarget, filepath.Join(fileDirectory, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	loader, err := NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.Load(t.Context(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"linked-directory": "directory body", "linked-file": "file body"} {
		skill, ok := result.Registry.Lookup(name)
		if !ok || !strings.Contains(skill.Content, content) {
			t.Fatalf("skill %q = %#v, found=%v diagnostics=%#v", name, skill, ok, result.Diagnostics)
		}
	}
	if skill, _ := result.Registry.Lookup("linked-directory"); skill.Location != directorySkillPath {
		t.Fatalf("linked directory location = %q, want %q", skill.Location, directorySkillPath)
	}
}

func TestFilesystemLoaderHonorsCancellation(t *testing.T) {
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.Load(ctx, cwd); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context canceled", err)
	}
}

func writeSkill(t *testing.T, root, name, description, body string, disabled bool) string {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "SKILL.md")
	content := "---\nname: " + name + "\ndescription: " + description + "\n"
	if disabled {
		content += "disable-model-invocation: true\n"
	}
	content += "---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func skillNames(skills []Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}

func hasSkillDiagnostic(result LoadResult, code, path string) bool {
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		path = canonical
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code && diagnostic.Source.Path == path {
			return true
		}
	}
	return false
}
