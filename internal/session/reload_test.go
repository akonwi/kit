package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestReloadSessionRefreshesContextAndPreservesRuntimeStream(t *testing.T) {
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
	contextWindow := 100_000
	manager, err := session.NewManager(
		store, providers, newContextRuntimeBundleBuilder(t, home, resolver),
		session.WithDroidStoreDirectory(filepath.Join(base, "droids")),
		session.WithModelContextWindow(func(string) int { return contextWindow }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: cwd, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	contextPath := filepath.Join(cwd, "AGENTS.md")
	writeContext(t, contextPath, "reload-context-one")
	if _, err := manager.RunPrompt(t.Context(), record.ID, "first"); err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeIDs := transcriptIDs(before)
	if before.ContextWindow != 100_000 {
		t.Fatalf("initial context window = %d", before.ContextWindow)
	}

	contextWindow = 1_000_000
	writeContext(t, contextPath, "reload-context-two")
	metadata, err := manager.ReloadSession(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := contextSourcePaths(session.PromptMetadata{Sources: metadata.Sources, Diagnostics: metadata.Diagnostics}); len(got) != 1 || got[0] != canonicalPath(t, contextPath) {
		t.Fatalf("reloaded context sources = %#v", got)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflectStringsEqual(transcriptIDs(after), beforeIDs) {
		t.Fatalf("temporary history changed across reload: before=%#v after=%#v", beforeIDs, transcriptIDs(after))
	}
	if after.EventStreamID != before.EventStreamID {
		t.Fatalf("event stream changed across live reload: before=%q after=%q", before.EventStreamID, after.EventStreamID)
	}
	if after.ContextWindow != 1_000_000 {
		t.Fatalf("reloaded context window = %d", after.ContextWindow)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "second"); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(contextPath); err != nil {
		t.Fatal(err)
	}
	contextWindow = 0
	metadata, err = manager.ReloadSession(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paths := contextSourcePaths(session.PromptMetadata{Sources: metadata.Sources, Diagnostics: metadata.Diagnostics}); len(paths) != 0 {
		t.Fatalf("removed context remains sourced: %#v", paths)
	}
	cleared, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ContextWindow != 128_000 {
		t.Fatalf("cleared context window = %d", cleared.ContextWindow)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "third"); err != nil {
		t.Fatal(err)
	}

	providers.mu.Lock()
	prompts := requestPrompts(providers.requests)
	providers.mu.Unlock()
	if len(prompts) != 3 || !strings.Contains(prompts[0], "reload-context-one") ||
		!strings.Contains(prompts[1], "reload-context-two") || strings.Contains(prompts[1], "reload-context-one") ||
		strings.Contains(prompts[2], "reload-context-one") || strings.Contains(prompts[2], "reload-context-two") || strings.Contains(prompts[2], "<context-files>") {
		t.Fatalf("provider prompts across reload = %#v", prompts)
	}
}

func TestReloadSessionPreservesPersistentPendingBoundaries(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(base, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	bash, err := manager.StartBash(t.Context(), record.ID, "bash_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "printf boundary", false)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for bash.Status == session.BashExecutionRunning {
		bash, err = manager.GetBash(t.Context(), record.ID, bash.ID)
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("bash boundary did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Boundaries) != 1 || before.Boundaries[0].ID != bash.ID {
		t.Fatalf("pending boundaries before reload = %+v", before.Boundaries)
	}
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Boundaries) != 1 || after.Boundaries[0].ID != before.Boundaries[0].ID || !reflectStringsEqual(transcriptIDs(after), transcriptIDs(before)) {
		t.Fatalf("persistent state changed across reload: before=%+v after=%+v", before, after)
	}
}

func TestReloadSessionAppliesDuringActiveParentRun(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
	builder := &countingRuntimeBundleBuilder{delegate: staticRuntimeBundleBuilder("system")}
	manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := manager.RunPrompt(context.Background(), record.ID, "blocked")
		done <- err
	}()
	<-providers.started
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatalf("ReloadSession() during active run: %v", err)
	}
	if builds := builder.count(); builds != 2 {
		t.Fatalf("bundle builds after live reload = %d, want 2", builds)
	}
	close(providers.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReloadSessionAppliesDuringActiveDirectBash(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manager, err := session.NewManager(
		store, &authorityProviders{}, staticRuntimeBundleBuilder("system"),
		session.WithDroidStoreDirectory(filepath.Join(base, "droids")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	bash, err := manager.StartBash(t.Context(), record.ID, "bash_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sleep 30", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ReloadSession(t.Context(), record.ID); err != nil {
		t.Fatalf("ReloadSession() during active bash: %v", err)
	}
	if err := manager.AbortBash(t.Context(), record.ID, bash.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReloadSessionSerializesWithCanceledManagerShutdown(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	builder := &blockingReloadBundleBuilder{
		delegate: staticRuntimeBundleBuilder("system"), started: make(chan struct{}), release: make(chan struct{}),
	}
	manager, err := session.NewManager(store, &authorityProviders{}, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	reloadDone := make(chan error, 1)
	go func() {
		_, err := manager.ReloadSession(context.Background(), record.ID)
		reloadDone <- err
	}()
	<-builder.started
	shutdownContext, cancelShutdown := context.WithCancel(context.Background())
	cancelShutdown()
	if err := manager.Shutdown(shutdownContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Shutdown() error = %v, want context canceled", err)
	}
	close(builder.release)
	if err := <-reloadDone; !errors.Is(err, session.ErrClosed) {
		t.Fatalf("ReloadSession() error = %v, want ErrClosed", err)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("detached shutdown cleanup error = %v", err)
	}
}

func TestUserCWDChangeSerializesAfterReload(t *testing.T) {
	base := t.TempDir()
	next := filepath.Join(base, "next")
	if err := os.Mkdir(next, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	builder := &blockingReloadBundleBuilder{
		delegate: staticRuntimeBundleBuilder("system"), started: make(chan struct{}), release: make(chan struct{}),
	}
	manager, err := session.NewManager(store, &authorityProviders{}, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	reloadDone := make(chan error, 1)
	go func() {
		_, err := manager.ReloadSession(context.Background(), record.ID)
		reloadDone <- err
	}()
	<-builder.started
	cwdDone := make(chan error, 1)
	go func() {
		_, err := manager.ChangeCWD(context.Background(), record.ID, "next")
		cwdDone <- err
	}()
	close(builder.release)
	if err := <-reloadDone; err != nil {
		t.Fatalf("ReloadSession() error = %v", err)
	}
	if err := <-cwdDone; err != nil {
		t.Fatal(err)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Session.CWD != next || after.EventStreamID != before.EventStreamID {
		t.Fatalf("snapshot after serialized reload and cwd change = %+v, before = %+v", after, before)
	}
}

func TestReloadSessionCancellationBeforeTransitionLeavesRuntimeUntouched(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	builder := &blockingReloadBundleBuilder{
		delegate: staticRuntimeBundleBuilder("system"), started: make(chan struct{}), release: make(chan struct{}),
	}
	manager, err := session.NewManager(store, &authorityProviders{}, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	reloadContext, cancelReload := context.WithCancel(context.Background())
	reloadDone := make(chan error, 1)
	go func() {
		_, err := manager.ReloadSession(reloadContext, record.ID)
		reloadDone <- err
	}()
	<-builder.started
	cancelReload()
	if err := <-reloadDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("ReloadSession() error = %v, want context canceled", err)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.EventStreamID != before.EventStreamID || !reflectStringsEqual(transcriptIDs(after), transcriptIDs(before)) {
		t.Fatalf("canceled reload changed runtime: before=%+v after=%+v", before, after)
	}
}

func TestReloadSessionLeavesRuntimeUntouchedWhenBundleBuildFails(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	builder := &scriptedRuntimeBundleBuilder{delegate: staticRuntimeBundleBuilder("stable-system"), failAt: 2}
	manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
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
	if _, err := manager.ReloadSession(t.Context(), record.ID); err == nil || !strings.Contains(err.Error(), "simulated bundle failure") {
		t.Fatalf("ReloadSession() error = %v", err)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.EventStreamID != before.EventStreamID || !reflectStringsEqual(transcriptIDs(after), transcriptIDs(before)) {
		t.Fatalf("failed preflight changed runtime: before=%+v after=%+v", before, after)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "second"); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	prompts := requestPrompts(providers.requests)
	providers.mu.Unlock()
	if len(prompts) != 2 || prompts[0] != prompts[1] || !strings.HasPrefix(prompts[1], "stable-system\n\n") {
		t.Fatalf("prompts after failed preflight = %#v", prompts)
	}
}

func TestReloadSessionRollsBackWhenReplacementDroidCannotOpen(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &authorityProviders{}
	builder := &scriptedRuntimeBundleBuilder{delegate: staticRuntimeBundleBuilder("rollback-system"), invalidateAt: 2}
	manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
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
	if _, err := manager.ReloadSession(t.Context(), record.ID); err == nil || !strings.Contains(err.Error(), "duplicate tool name") {
		t.Fatalf("ReloadSession() error = %v", err)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatalf("Snapshot() after rollback error = %v", err)
	}
	if after.EventStreamID != before.EventStreamID || !reflectStringsEqual(transcriptIDs(after), transcriptIDs(before)) {
		t.Fatalf("rollback changed runtime: before=%+v after=%+v", before, after)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "second"); err != nil {
		t.Fatalf("prompt after rollback: %v", err)
	}
	providers.mu.Lock()
	prompts := requestPrompts(providers.requests)
	providers.mu.Unlock()
	if len(prompts) != 2 || prompts[0] != prompts[1] || !strings.HasPrefix(prompts[1], "rollback-system\n\n") {
		t.Fatalf("prompts after rollback = %#v", prompts)
	}
}

func transcriptIDs(snapshot session.Snapshot) []string {
	ids := make([]string, 0, len(snapshot.Messages))
	for _, message := range snapshot.Messages {
		ids = append(ids, message.ID)
	}
	return ids
}

func reflectStringsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type blockingReloadBundleBuilder struct {
	mu       sync.Mutex
	builds   int
	delegate session.RuntimeBundleBuilder
	started  chan struct{}
	release  chan struct{}
}

func (b *blockingReloadBundleBuilder) Build(ctx context.Context, record session.SessionRecord, currentCWD codingtools.CWDProvider) (session.RuntimeBundle, error) {
	if currentCWD != nil && currentCWD() == "" {
		return session.RuntimeBundle{}, errors.New("empty workspace cwd")
	}
	b.mu.Lock()
	b.builds++
	build := b.builds
	b.mu.Unlock()
	if build == 2 {
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
			return session.RuntimeBundle{}, ctx.Err()
		}
	}
	return b.delegate.Build(ctx, record, currentCWD)
}

type scriptedRuntimeBundleBuilder struct {
	mu           sync.Mutex
	builds       int
	failAt       int
	invalidateAt int
	delegate     session.RuntimeBundleBuilder
}

func (b *scriptedRuntimeBundleBuilder) Build(ctx context.Context, record session.SessionRecord, currentCWD codingtools.CWDProvider) (session.RuntimeBundle, error) {
	b.mu.Lock()
	b.builds++
	build := b.builds
	b.mu.Unlock()
	if build == b.failAt {
		return session.RuntimeBundle{}, errors.New("simulated bundle failure")
	}
	bundle, err := b.delegate.Build(ctx, record, currentCWD)
	if err == nil && build == b.invalidateAt {
		bundle.Tools = append(bundle.Tools, bundle.Tools[0])
	}
	return bundle, err
}
