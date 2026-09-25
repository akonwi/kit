package systemprompt

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/apphome"
)

func TestContextBuilderLoadsGlobalAndGitRootToCWD(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	repository := canonicalTestDirectory(t, t.TempDir())
	service := filepath.Join(repository, "apps", "service")
	if err := os.MkdirAll(service, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		path    string
		content string
	}{
		{filepath.Join(home, agentsFilename), "global guidance"},
		{filepath.Join(repository, agentsFilename), "repository guidance\n"},
		{filepath.Join(repository, "apps", agentsFilename), "apps guidance"},
		{filepath.Join(service, agentsFilename), "service guidance"},
	}
	for _, file := range files {
		writeContextFile(t, file.path, []byte(file.content))
	}

	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(repository), ContextLimits{})
	result, err := builder.Build(context.Background(), Request{SessionID: "session_1", CWD: service})
	if err != nil {
		t.Fatal(err)
	}

	wantContext := contextPreamble + `

<context-files>
<context-file path="` + files[0].path + `">
global guidance
</context-file>

<context-file path="` + files[1].path + `">
repository guidance
</context-file>

<context-file path="` + files[2].path + `">
apps guidance
</context-file>

<context-file path="` + files[3].path + `">
service guidance
</context-file>
</context-files>`
	wantPrompt := DefaultCore + "\n\n" + wantContext
	if result.Prompt != wantPrompt {
		t.Fatalf("Prompt:\n%s\n\nwant:\n%s", result.Prompt, wantPrompt)
	}
	wantSources := []Source{{SectionID: "kit.core", ID: "kit.core", Kind: SectionCore}}
	for _, file := range files {
		source := contextSource(file.path)
		source.SectionID = contextSectionID
		source.Kind = SectionContext
		wantSources = append(wantSources, source)
	}
	if !reflect.DeepEqual(result.Sources, wantSources) {
		t.Fatalf("Sources = %#v, want %#v", result.Sources, wantSources)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestContextBuilderOutsideWorktreeLoadsOnlyCWD(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	parent := canonicalTestDirectory(t, t.TempDir())
	cwd := filepath.Join(parent, "project")
	child := filepath.Join(cwd, "child")
	sibling := filepath.Join(parent, "sibling")
	for _, directory := range []string{cwd, child, sibling} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeContextFile(t, filepath.Join(parent, agentsFilename), []byte("parent"))
	writeContextFile(t, filepath.Join(cwd, agentsFilename), []byte("cwd"))
	writeContextFile(t, filepath.Join(child, agentsFilename), []byte("child"))
	writeContextFile(t, filepath.Join(sibling, agentsFilename), []byte("sibling"))

	builder := newTestContextBuilder(t, home, noWorktreeRoot(), ContextLimits{})
	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore+"\n\n"+renderedContext(filepath.Join(cwd, agentsFilename), "cwd") {
		t.Fatalf("Prompt:\n%s", result.Prompt)
	}
	if len(result.Sources) != 2 || result.Sources[1].Path != filepath.Join(cwd, agentsFilename) {
		t.Fatalf("Sources = %#v, want cwd only", result.Sources)
	}
}

func TestContextBuilderIgnoresUnsupportedNames(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	cwd := canonicalTestDirectory(t, t.TempDir())
	for name, content := range map[string]string{
		"CLAUDE.md":          "claude",
		"AGENTS.override.md": "override",
	} {
		writeContextFile(t, filepath.Join(cwd, name), []byte(content))
	}

	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(cwd), ContextLimits{})
	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore || len(result.Sources) != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("Result = %#v, want core without context", result)
	}
}

func TestContextBuilderIgnoresCasingVariantsOnCaseSensitiveFilesystems(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	cwd := canonicalTestDirectory(t, t.TempDir())
	writeContextFile(t, filepath.Join(cwd, "AGENTS.MD"), []byte("other casing"))
	if _, err := os.Stat(filepath.Join(cwd, agentsFilename)); err == nil {
		t.Skip("filesystem is case-insensitive")
	}

	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(cwd), ContextLimits{})
	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore || len(result.Sources) != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("Result = %#v, want core without context", result)
	}
}

func TestContextBuilderFallsBackToCWDWhenGitRootFails(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	cwd := canonicalTestDirectory(t, t.TempDir())
	path := filepath.Join(cwd, agentsFilename)
	writeContextFile(t, path, []byte("cwd guidance"))
	resolverFailure := errors.New("git unavailable")
	builder := newTestContextBuilder(t, home, WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", resolverFailure
	}), ContextLimits{})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 2 || result.Sources[1].Path != path {
		t.Fatalf("Sources = %#v, want cwd context", result.Sources)
	}
	if !hasDiagnostic(result.Diagnostics, "context.git_root_unavailable", cwd) {
		t.Fatalf("Diagnostics = %#v, want Git root warning", result.Diagnostics)
	}
}

func TestContextBuilderRejectsResolvedRootOutsideCWD(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	cwd := canonicalTestDirectory(t, t.TempDir())
	other := canonicalTestDirectory(t, t.TempDir())
	path := filepath.Join(cwd, agentsFilename)
	writeContextFile(t, path, []byte("cwd guidance"))
	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(other), ContextLimits{})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 2 || result.Sources[1].Path != path {
		t.Fatalf("Sources = %#v, want cwd context", result.Sources)
	}
	if !hasDiagnostic(result.Diagnostics, "context.git_root_outside_cwd", other) {
		t.Fatalf("Diagnostics = %#v, want invalid root warning", result.Diagnostics)
	}
}

func TestContextBuilderDeduplicatesGlobalAndLocalIdentity(t *testing.T) {
	cwd := canonicalTestDirectory(t, t.TempDir())
	path := filepath.Join(cwd, agentsFilename)
	writeContextFile(t, path, []byte("shared guidance"))
	builder := newTestContextBuilder(t, cwd, fixedWorktreeRoot(cwd), ContextLimits{})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 2 || result.Sources[1].Path != path {
		t.Fatalf("Sources = %#v, want one context source", result.Sources)
	}
	if !hasDiagnostic(result.Diagnostics, "context.duplicate", path) {
		t.Fatalf("Diagnostics = %#v, want duplicate source", result.Diagnostics)
	}
}

func TestContextBuilderRejectsContextFileSymlinks(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	cwd := canonicalTestDirectory(t, t.TempDir())
	target := filepath.Join(t.TempDir(), "private")
	writeContextFile(t, target, []byte("must not load"))
	path := filepath.Join(cwd, agentsFilename)
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(cwd), ContextLimits{})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if result.Prompt != DefaultCore || len(result.Sources) != 1 {
		t.Fatalf("Result = %#v, want no context", result)
	}
	if !hasDiagnostic(result.Diagnostics, "context.non_regular", path) {
		t.Fatalf("Diagnostics = %#v, want non-regular source", result.Diagnostics)
	}
}

func TestContextBuilderSourceLimitPreservesLocalFiles(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	root := canonicalTestDirectory(t, t.TempDir())
	cwd := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(home, agentsFilename),
		filepath.Join(root, agentsFilename),
		filepath.Join(root, "a", agentsFilename),
		filepath.Join(cwd, agentsFilename),
	}
	for index, path := range paths {
		writeContextFile(t, path, []byte(string(rune('0'+index))))
	}
	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(root), ContextLimits{
		MaxCandidates: 2, MaxFileBytes: 16, MaxTotalBytes: 32,
	})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{paths[2], paths[3]}
	if got := sourcePaths(result.Sources); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("context source paths = %#v, want %#v", got, wantPaths)
	}
	for _, omitted := range paths[:2] {
		if !hasDiagnostic(result.Diagnostics, "context.candidate_limit", omitted) {
			t.Fatalf("Diagnostics = %#v, want source limit for %s", result.Diagnostics, omitted)
		}
	}
}

func TestContextBuilderFileValidationAndAggregateLimit(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	root := canonicalTestDirectory(t, t.TempDir())
	middle := filepath.Join(root, "middle")
	cwd := filepath.Join(middle, "cwd")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(home, agentsFilename)
	rootPath := filepath.Join(root, agentsFilename)
	middlePath := filepath.Join(middle, agentsFilename)
	cwdPath := filepath.Join(cwd, agentsFilename)
	writeContextFile(t, globalPath, []byte("123456"))
	writeContextFile(t, rootPath, []byte{0xff, 0xfe})
	if err := os.Mkdir(middlePath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeContextFile(t, cwdPath, []byte("local"))
	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(root), ContextLimits{
		MaxCandidates: 8, MaxFileBytes: 5, MaxTotalBytes: 5,
	})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if got := sourcePaths(result.Sources); !reflect.DeepEqual(got, []string{cwdPath}) {
		t.Fatalf("context source paths = %#v, want cwd", got)
	}
	for _, expected := range []struct {
		code string
		path string
	}{
		{"context.file_too_large", globalPath},
		{"context.invalid_utf8", rootPath},
		{"context.non_regular", middlePath},
	} {
		if !hasDiagnostic(result.Diagnostics, expected.code, expected.path) {
			t.Fatalf("Diagnostics = %#v, want %s for %s", result.Diagnostics, expected.code, expected.path)
		}
	}
}

func TestContextBuilderAggregateLimitPreservesMostLocalContent(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	root := canonicalTestDirectory(t, t.TempDir())
	cwd := filepath.Join(root, "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(root, agentsFilename)
	cwdPath := filepath.Join(cwd, agentsFilename)
	writeContextFile(t, rootPath, []byte("broad"))
	writeContextFile(t, cwdPath, []byte("local"))
	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(root), ContextLimits{
		MaxCandidates: 8, MaxFileBytes: 8, MaxTotalBytes: 5,
	})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if got := sourcePaths(result.Sources); !reflect.DeepEqual(got, []string{cwdPath}) {
		t.Fatalf("context source paths = %#v, want cwd", got)
	}
	if !hasDiagnostic(result.Diagnostics, "context.aggregate_limit", rootPath) {
		t.Fatalf("Diagnostics = %#v, want aggregate omission", result.Diagnostics)
	}
}

func TestContextBuilderEscapesContextFilePath(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	root := filepath.Join(canonicalTestDirectory(t, t.TempDir()), `repo"&<>`)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, agentsFilename)
	writeContextFile(t, path, []byte("guidance"))
	builder := newTestContextBuilder(t, home, fixedWorktreeRoot(root), ContextLimits{})

	result, err := builder.Build(context.Background(), Request{CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	wantOpeningTag := `<context-file path="` + escapeXMLAttribute(path) + `">`
	if !strings.Contains(result.Prompt, wantOpeningTag+"\nguidance\n</context-file>") {
		t.Fatalf("Prompt does not contain escaped wrapper %q:\n%s", wantOpeningTag, result.Prompt)
	}
}

func TestContextBuilderBuildsConcurrentSessionContextIndependently(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	first := canonicalTestDirectory(t, t.TempDir())
	second := canonicalTestDirectory(t, t.TempDir())
	writeContextFile(t, filepath.Join(first, agentsFilename), []byte("first guidance"))
	writeContextFile(t, filepath.Join(second, agentsFilename), []byte("second guidance"))
	resolver := WorktreeRootResolverFunc(func(_ context.Context, cwd string) (string, error) {
		return cwd, nil
	})
	builder := newTestContextBuilder(t, home, resolver, ContextLimits{})

	type buildResult struct {
		result Result
		err    error
	}
	results := make(chan buildResult, 2)
	for _, cwd := range []string{first, second} {
		cwd := cwd
		go func() {
			result, err := builder.Build(context.Background(), Request{CWD: cwd})
			results <- buildResult{result: result, err: err}
		}()
	}
	seen := make(map[string]Result)
	for range 2 {
		built := <-results
		if built.err != nil {
			t.Fatal(built.err)
		}
		paths := sourcePaths(built.result.Sources)
		if len(paths) != 1 {
			t.Fatalf("context source paths = %#v, want one", paths)
		}
		seen[paths[0]] = built.result
	}
	if !strings.Contains(seen[filepath.Join(first, agentsFilename)].Prompt, "first guidance") ||
		strings.Contains(seen[filepath.Join(first, agentsFilename)].Prompt, "second guidance") {
		t.Fatalf("first session prompt = %q", seen[filepath.Join(first, agentsFilename)].Prompt)
	}
	if !strings.Contains(seen[filepath.Join(second, agentsFilename)].Prompt, "second guidance") ||
		strings.Contains(seen[filepath.Join(second, agentsFilename)].Prompt, "first guidance") {
		t.Fatalf("second session prompt = %q", seen[filepath.Join(second, agentsFilename)].Prompt)
	}
}

func TestContextBuilderDiagnosticHandlesCWDWithTrailingWhitespace(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	cwd := filepath.Join(canonicalTestDirectory(t, t.TempDir()), "cwd ")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cwd, agentsFilename)
	writeContextFile(t, path, []byte("guidance"))
	builder := newTestContextBuilder(t, home, WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", errors.New("git unavailable")
	}), ContextLimits{})

	result, err := builder.Build(context.Background(), Request{CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 2 || result.Sources[1].Path != path {
		t.Fatalf("Sources = %#v, want cwd context", result.Sources)
	}
	if !hasDiagnostic(result.Diagnostics, "context.git_root_unavailable", cwd) {
		t.Fatalf("Diagnostics = %#v, want Git root warning", result.Diagnostics)
	}
}

func TestContextBuilderRequiresAbsolutePaths(t *testing.T) {
	composer := NewDefault()
	if _, err := NewContextBuilder(composer, ContextBuilderOptions{Paths: apphome.FromHome("relative")}); err == nil {
		t.Fatal("NewContextBuilder() accepted a relative Kit home")
	}

	home := canonicalTestDirectory(t, t.TempDir())
	builder := newTestContextBuilder(t, home, noWorktreeRoot(), ContextLimits{})
	if _, err := builder.Build(context.Background(), Request{CWD: "relative"}); err == nil {
		t.Fatal("Build() accepted a relative cwd")
	}
}

func TestContextBuilderRejectsNegativeLimits(t *testing.T) {
	_, err := NewContextBuilder(NewDefault(), ContextBuilderOptions{
		Paths:  apphome.FromHome(t.TempDir()),
		Limits: ContextLimits{MaxCandidates: -1},
	})
	if err == nil {
		t.Fatal("NewContextBuilder() accepted negative limits")
	}
	_, err = NewContextBuilder(NewDefault(), ContextBuilderOptions{
		Paths:  apphome.FromHome(t.TempDir()),
		Limits: ContextLimits{MaxFileBytes: int64(^uint64(0) >> 1)},
	})
	if err == nil {
		t.Fatal("NewContextBuilder() accepted an overflowing per-file limit")
	}
}

func TestBoundDiagnosticsIsDeterministicAndBounded(t *testing.T) {
	diagnostics := []Diagnostic{
		contextDiagnostic(DiagnosticWarning, "z", "z", "/z"),
		contextDiagnostic(DiagnosticWarning, "b", "b", "/a"),
		contextDiagnostic(DiagnosticWarning, "a", "a", "/a"),
		contextDiagnostic(DiagnosticWarning, "c", "c", "/c"),
	}
	got := boundDiagnostics(diagnostics, 3)
	want := []Diagnostic{
		contextDiagnostic(DiagnosticWarning, "a", "a", "/a"),
		contextDiagnostic(DiagnosticWarning, "b", "b", "/a"),
		contextDiagnostic(DiagnosticWarning, "context.diagnostics_suppressed", "Suppressed 2 additional context diagnostics", ""),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("boundDiagnostics() = %#v, want %#v", got, want)
	}
}

func TestXMLAttributeEncoding(t *testing.T) {
	const input = "a&<>\"'\t\n\rb"
	const want = "a&amp;&lt;&gt;&quot;&apos;&#x9;&#xA;&#xD;b"
	if got := escapeXMLAttribute(input); got != want {
		t.Fatalf("escapeXMLAttribute() = %q, want %q", got, want)
	}
	if validXMLString("bad\x00path") {
		t.Fatal("validXMLString() accepted an XML-forbidden control character")
	}
}

func TestGitDiscoveryEnvironmentRemovesRepositorySelectors(t *testing.T) {
	input := []string{
		"PATH=/bin",
		"GIT_DIR=/other/.git",
		"GIT_WORK_TREE=/other",
		"GIT_COMMON_DIR=/other/common",
		"GIT_CEILING_DIRECTORIES=/",
		"HOME=/home/test",
	}
	want := []string{"PATH=/bin", "HOME=/home/test", "LC_ALL=C"}
	if got := gitDiscoveryEnvironment(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("gitDiscoveryEnvironment() = %#v, want %#v", got, want)
	}
}

func TestContextBuilderHonorsCancellation(t *testing.T) {
	home := canonicalTestDirectory(t, t.TempDir())
	cwd := canonicalTestDirectory(t, t.TempDir())
	builder := newTestContextBuilder(t, home, WorktreeRootResolverFunc(func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}), ContextLimits{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := builder.Build(ctx, Request{CWD: cwd}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Build() error = %v, want context canceled", err)
	}
}

func TestGitWorktreeRootResolver(t *testing.T) {
	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	root := canonicalTestDirectory(t, t.TempDir())
	command := exec.Command(git, "init", "--quiet", root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	resolver := GitWorktreeRootResolver{Timeout: time.Second}
	got, err := resolver.ResolveWorktreeRoot(context.Background(), nested)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonicalRoot {
		t.Fatalf("ResolveWorktreeRoot() = %q, want %q", got, canonicalRoot)
	}
	if currentCWD, err := os.Getwd(); err != nil || currentCWD != originalCWD {
		t.Fatalf("process cwd = %q, %v; want %q", currentCWD, err, originalCWD)
	}

	outside := canonicalTestDirectory(t, t.TempDir())
	if _, err := resolver.ResolveWorktreeRoot(context.Background(), outside); !errors.Is(err, ErrNoGitWorktree) {
		t.Fatalf("outside worktree error = %v, want ErrNoGitWorktree", err)
	}

	rootWithSpace := filepath.Join(canonicalTestDirectory(t, t.TempDir()), "repo ")
	if output, err := exec.Command(git, "init", "--quiet", rootWithSpace).CombinedOutput(); err != nil {
		t.Fatalf("git init whitespace root: %v: %s", err, output)
	}
	got, err = resolver.ResolveWorktreeRoot(context.Background(), rootWithSpace)
	if err != nil {
		t.Fatal(err)
	}
	if got != rootWithSpace {
		t.Fatalf("ResolveWorktreeRoot() whitespace root = %q, want %q", got, rootWithSpace)
	}
}

func TestGitWorktreeRootResolverTimesOut(t *testing.T) {
	script := filepath.Join(t.TempDir(), "slow-git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolver := GitWorktreeRootResolver{Command: script, Timeout: 10 * time.Millisecond}
	started := time.Now()
	_, err := resolver.ResolveWorktreeRoot(context.Background(), canonicalTestDirectory(t, t.TempDir()))
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ResolveWorktreeRoot() error = %v, want deadline exceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("ResolveWorktreeRoot() took %v after timeout", elapsed)
	}
}

func canonicalTestDirectory(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func newTestContextBuilder(t *testing.T, home string, resolver WorktreeRootResolver, limits ContextLimits) *ContextBuilder {
	t.Helper()
	builder, err := NewContextBuilder(NewDefault(), ContextBuilderOptions{
		Paths:    apphome.FromHome(home),
		Resolver: resolver,
		Limits:   limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func fixedWorktreeRoot(root string) WorktreeRootResolver {
	return WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return root, nil
	})
}

func noWorktreeRoot() WorktreeRootResolver {
	return WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", ErrNoGitWorktree
	})
}

func writeContextFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func renderedContext(path, content string) string {
	return contextPreamble + `

<context-files>
<context-file path="` + escapeXMLAttribute(path) + `">
` + content + `
</context-file>
</context-files>`
}

func sourcePaths(sources []Source) []string {
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Kind == SectionContext {
			paths = append(paths, source.Path)
		}
	}
	return paths
}

func hasDiagnostic(diagnostics []Diagnostic, code, path string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Source.Path == path {
			return true
		}
	}
	return false
}
