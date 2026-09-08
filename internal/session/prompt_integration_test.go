package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestManagerBuildsIndependentContextForConcurrentSessions(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "kit-home")
	rootA, cwdA := filepath.Join(base, "repo-a"), filepath.Join(base, "repo-a", "nested")
	rootB, cwdB := filepath.Join(base, "repo-b"), filepath.Join(base, "repo-b", "nested")
	for _, directory := range []string{home, cwdA, cwdB} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeContext(t, filepath.Join(home, "AGENTS.md"), "global-guidance")
	writeContext(t, filepath.Join(rootA, "AGENTS.md"), "root-a-guidance")
	writeContext(t, filepath.Join(cwdA, "AGENTS.md"), "local-a-guidance")
	writeContext(t, filepath.Join(rootB, "AGENTS.md"), "root-b-guidance")
	writeContext(t, filepath.Join(cwdB, "AGENTS.md"), "local-b-guidance")

	resolver := systemprompt.WorktreeRootResolverFunc(func(_ context.Context, cwd string) (string, error) {
		return filepath.Dir(cwd), nil
	})
	builder := newContextRuntimeBundleBuilder(t, home, resolver)
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	recordA, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CWD: cwdA, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	recordB, err := manager.Create(t.Context(), session.CreateInput{
		ID: "session_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CWD: cwdB, Model: "test/echo", Temporary: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errors := make(chan error, 2)
	for _, record := range []session.SessionRecord{recordA, recordB} {
		record := record
		go func() {
			<-start
			_, err := manager.RunPrompt(context.Background(), record.ID, "hello")
			errors <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}

	providers.mu.Lock()
	requests := append([]string(nil), requestPrompts(providers.requests)...)
	providers.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	for _, request := range requests {
		switch {
		case strings.Contains(request, "local-a-guidance"):
			if !strings.Contains(request, "global-guidance") || !strings.Contains(request, "root-a-guidance") || strings.Contains(request, "root-b-guidance") || strings.Contains(request, "local-b-guidance") {
				t.Fatalf("session A received mixed context:\n%s", request)
			}
		case strings.Contains(request, "local-b-guidance"):
			if !strings.Contains(request, "global-guidance") || !strings.Contains(request, "root-b-guidance") || strings.Contains(request, "root-a-guidance") || strings.Contains(request, "local-a-guidance") {
				t.Fatalf("session B received mixed context:\n%s", request)
			}
		default:
			t.Fatalf("request has no local session guidance:\n%s", request)
		}
	}

	metadata, err := manager.PromptMetadata(t.Context(), recordA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := contextSourcePaths(metadata), []string{
		canonicalPath(t, filepath.Join(home, "AGENTS.md")), canonicalPath(t, filepath.Join(rootA, "AGENTS.md")), canonicalPath(t, filepath.Join(cwdA, "AGENTS.md")),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("session A context sources = %#v, want %#v", got, want)
	}
	snapshot, err := manager.Snapshot(t.Context(), recordA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if transcriptContains(snapshot, "global-guidance") || transcriptContains(snapshot, "root-a-guidance") || transcriptContains(snapshot, "local-a-guidance") {
		t.Fatalf("context guidance entered conversation history: %+v", snapshot.Messages)
	}
}

func TestReloadRefreshesProjectSkillCatalogForOneSession(t *testing.T) {
	base := t.TempDir()
	paths := apphome.FromHome(filepath.Join(base, "kit-home"))
	cwd := filepath.Join(base, "project")
	for _, directory := range []string{paths.Skills, cwd} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	skillDirectory := filepath.Join(cwd, ".agents", "skills", "workspace-review")
	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDirectory, "SKILL.md")
	writeContext(t, skillPath, "---\nname: workspace-review\ndescription: Review this workspace\n---\nInitial instructions.\n")
	loader, err := skills.NewFilesystemLoader(paths)
	if err != nil {
		t.Fatal(err)
	}
	bundleBuilder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{
		Core: systemprompt.DefaultCore, SkillLoader: loader,
		Context: &systemprompt.ContextBuilderOptions{Paths: paths, Resolver: systemprompt.WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
			return "", systemprompt.ErrNoGitWorktree
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, bundleBuilder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "first"); err != nil {
		t.Fatal(err)
	}
	writeContext(t, skillPath, "---\nname: workspace-review\ndescription: Updated workspace review\n---\nUpdated instructions.\n")
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "second"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(skillDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "third"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	prompts := requestPrompts(providers.requests)
	providers.mu.Unlock()
	if len(prompts) != 3 || !strings.Contains(prompts[0], "Review this workspace") || strings.Contains(prompts[0], "Updated workspace review") ||
		!strings.Contains(prompts[1], "Updated workspace review") || strings.Contains(prompts[1], "Review this workspace") ||
		strings.Contains(prompts[2], "workspace-review") {
		t.Fatalf("project skill catalogs across reload = %#v", prompts)
	}
}

func TestLoadedRuntimeDoesNotRebuildContextOnClientAttachment(t *testing.T) {
	base := t.TempDir()
	home, cwd := filepath.Join(base, "kit-home"), filepath.Join(base, "project")
	for _, directory := range []string{home, cwd} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resolver := systemprompt.WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", systemprompt.ErrNoGitWorktree
	})
	counter := &countingRuntimeBundleBuilder{delegate: newContextRuntimeBundleBuilder(t, home, resolver)}
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, counter, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}

	writeContext(t, filepath.Join(cwd, "AGENTS.md"), "guidance-before-attachment")
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	writeContext(t, filepath.Join(cwd, "AGENTS.md"), "guidance-after-attachment")
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.PromptMetadata(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	if builds := counter.count(); builds != 1 {
		t.Fatalf("runtime bundle builds = %d, want 1", builds)
	}
	providers.mu.Lock()
	prompt := providers.requests[0].SystemPrompt
	providers.mu.Unlock()
	if !strings.Contains(prompt, "guidance-before-attachment") || strings.Contains(prompt, "guidance-after-attachment") {
		t.Fatalf("loaded runtime rebuilt its context:\n%s", prompt)
	}
}

func TestDaemonStyleRestartReadsCurrentContextAndPreservesHistory(t *testing.T) {
	base := t.TempDir()
	home, cwd := filepath.Join(base, "kit-home"), filepath.Join(base, "project")
	for _, directory := range []string{home, cwd} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resolver := systemprompt.WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", systemprompt.ErrNoGitWorktree
	})
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	newManager := func() *session.Manager {
		manager, err := session.NewManager(
			store, providers, newContextRuntimeBundleBuilder(t, home, resolver),
			session.WithDroidStoreDirectory(filepath.Join(base, "droids")),
		)
		if err != nil {
			t.Fatal(err)
		}
		return manager
	}

	writeContext(t, filepath.Join(cwd, "AGENTS.md"), "context-version-one")
	manager := newManager()
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "first"); err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	writeContext(t, filepath.Join(cwd, "AGENTS.md"), "context-version-two")
	reopened := newManager()
	t.Cleanup(reopened.Close)
	afterRestart, err := reopened.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterRestart.Messages) != len(before.Messages) || afterRestart.Messages[0].ID != before.Messages[0].ID {
		t.Fatalf("history changed across restart: before=%+v after=%+v", before.Messages, afterRestart.Messages)
	}
	if _, err := reopened.RunPrompt(t.Context(), record.ID, "second"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	requests := append([]string(nil), requestPrompts(providers.requests)...)
	providers.mu.Unlock()
	if len(requests) != 2 || !strings.Contains(requests[0], "context-version-one") || strings.Contains(requests[0], "context-version-two") ||
		!strings.Contains(requests[1], "context-version-two") || strings.Contains(requests[1], "context-version-one") {
		t.Fatalf("restart prompts = %#v", requests)
	}
}

func TestNormalAndCompactionRequestsKeepSeparateSystemPrompts(t *testing.T) {
	base := t.TempDir()
	home, cwd := filepath.Join(base, "kit-home"), filepath.Join(base, "project")
	for _, directory := range []string{home, cwd} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeContext(t, filepath.Join(cwd, "AGENTS.md"), "normal-request-context-guidance")
	resolver := systemprompt.WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", systemprompt.ErrNoGitWorktree
	})
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &sessionCompactionProviders{}
	manager, err := session.NewManager(
		store, providers, newContextRuntimeBundleBuilder(t, home, resolver),
		session.WithDroidStoreDirectory(filepath.Join(base, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/compact", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	for index := range 4 {
		if _, err := manager.RunPrompt(t.Context(), record.ID, strings.Repeat(string(rune('a'+index)), 450)); err != nil {
			t.Fatal(err)
		}
	}
	providers.mu.Lock()
	prompts := append([]string(nil), providers.prompts...)
	providers.mu.Unlock()
	var normal, compaction int
	for _, prompt := range prompts {
		if strings.Contains(prompt, "Summarize the supplied conversation") {
			compaction++
			if strings.Contains(prompt, "normal-request-context-guidance") || strings.Contains(prompt, "kit-customization") {
				t.Fatalf("compaction request received the normal session prompt:\n%s", prompt)
			}
			continue
		}
		normal++
		if !strings.Contains(prompt, "normal-request-context-guidance") || !strings.Contains(prompt, "kit-customization") {
			t.Fatalf("normal request did not receive the assembled prompt:\n%s", prompt)
		}
	}
	if normal == 0 || compaction == 0 {
		t.Fatalf("normal requests = %d, compaction requests = %d", normal, compaction)
	}
}

func TestPromptMetadataRetainsContextDiagnostics(t *testing.T) {
	base := t.TempDir()
	home, cwd := filepath.Join(base, "kit-home"), filepath.Join(base, "project")
	for _, directory := range []string{home, cwd, filepath.Join(cwd, "AGENTS.md")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	resolver := systemprompt.WorktreeRootResolverFunc(func(context.Context, string) (string, error) {
		return "", systemprompt.ErrNoGitWorktree
	})
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, newContextRuntimeBundleBuilder(t, home, resolver),
		session.WithDroidStoreDirectory(filepath.Join(base, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := manager.PromptMetadata(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Diagnostics) != 1 || metadata.Diagnostics[0].Code != "context.non_regular" || metadata.Diagnostics[0].Source.Path != canonicalPath(t, filepath.Join(cwd, "AGENTS.md")) {
		t.Fatalf("prompt diagnostics = %#v", metadata.Diagnostics)
	}
	metadata.Diagnostics[0].Code = "mutated"
	again, err := manager.PromptMetadata(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Diagnostics[0].Code != "context.non_regular" {
		t.Fatalf("runtime diagnostics were externally mutated: %#v", again.Diagnostics)
	}
}

func newContextRuntimeBundleBuilder(t *testing.T, home string, resolver systemprompt.WorktreeRootResolver) session.RuntimeBundleBuilder {
	t.Helper()
	registry, err := skills.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	bundleBuilder, err := session.NewRuntimeBundleBuilder(session.RuntimeBundleOptions{
		Core: systemprompt.DefaultCore, Registry: registry,
		Context: &systemprompt.ContextBuilderOptions{Paths: apphome.FromHome(home), Resolver: resolver},
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundleBuilder
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func writeContext(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requestPrompts(requests []droids.Request) []string {
	prompts := make([]string, 0, len(requests))
	for _, request := range requests {
		prompts = append(prompts, request.SystemPrompt)
	}
	return prompts
}

func contextSourcePaths(metadata session.PromptMetadata) []string {
	var paths []string
	for _, source := range metadata.Sources {
		if source.Kind == systemprompt.SectionContext {
			paths = append(paths, source.Path)
		}
	}
	return paths
}

func transcriptContains(snapshot session.Snapshot, text string) bool {
	for _, message := range snapshot.Messages {
		for _, content := range message.Content {
			if strings.Contains(content.Text, text) {
				return true
			}
		}
	}
	return false
}

type sessionCompactionProviders struct {
	mu      sync.Mutex
	prompts []string
}

func (p *sessionCompactionProviders) ID() string { return "test" }
func (p *sessionCompactionProviders) Models() []droids.Model {
	return []droids.Model{p.model()}
}
func (p *sessionCompactionProviders) ValidateReplay(context.Context, droids.Model, []droids.Message) error {
	return nil
}
func (p *sessionCompactionProviders) Resolve(id string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return nil, droids.Model{}, errors.New("unknown model")
	}
	return droids.AdaptProvider("test", p.Models(), p.Stream), model, nil
}
func (p *sessionCompactionProviders) Model(id string) (droids.Model, bool) {
	return p.model(), id == "compact" || id == "test/compact"
}
func (p *sessionCompactionProviders) RefreshModels(context.Context) error { return nil }
func (p *sessionCompactionProviders) Stream(_ context.Context, _ droids.Model, request droids.Request) droids.Stream {
	p.mu.Lock()
	p.prompts = append(p.prompts, request.SystemPrompt)
	p.mu.Unlock()
	if strings.Contains(request.SystemPrompt, "Summarize the supplied conversation") {
		return &authorityStream{message: droids.AssistantMessage{
			Provider: "test", Model: "compact", StopReason: droids.StopReasonStop,
			Content: []droids.AssistantContent{droids.TextContent{Text: "Earlier prompts contained repeated test data."}},
		}}
	}
	return &authorityStream{message: droids.AssistantMessage{
		Provider: "test", Model: "compact", StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "ok"}},
	}}
}
func (*sessionCompactionProviders) model() droids.Model {
	return droids.Model{
		ID: "compact", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 2_000, MaxInputTokens: 1_800, MaxOutputTokens: 64,
	}
}

type countingRuntimeBundleBuilder struct {
	mu       sync.Mutex
	builds   int
	delegate session.RuntimeBundleBuilder
}

func (b *countingRuntimeBundleBuilder) Build(ctx context.Context, record session.SessionRecord) (session.RuntimeBundle, error) {
	b.mu.Lock()
	b.builds++
	b.mu.Unlock()
	return b.delegate.Build(ctx, record)
}

func (b *countingRuntimeBundleBuilder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.builds
}
