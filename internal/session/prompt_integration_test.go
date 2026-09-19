package session_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kitannotation "github.com/akonwi/kit/internal/annotation"
	"github.com/akonwi/kit/internal/apphome"
	"github.com/akonwi/kit/internal/attachment"
	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/skills"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/systemprompt"
)

type annotationFileReader struct{ content string }

func (r annotationFileReader) ReadFile(context.Context, string, string, kitannotation.WorkspaceFileAnchor) (kitannotation.FileEvidence, error) {
	return kitannotation.FileEvidence{Content: r.content}, nil
}

func TestManagerSubmitsAndConsumesOrderedAnnotations(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	annotations, err := kitannotation.NewService(store, annotationFileReader{content: "first\nsecond\nthird"})
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")), session.WithAnnotationService(annotations))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo"})
	if err != nil {
		t.Fatal(err)
	}
	anchor := protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &kitannotation.WorkspaceFileAnchor{
		WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4", Path: "main.go",
		FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A", StartLine: 2, EndLine: 3,
	}}
	first, err := annotations.Create(t.Context(), record.ID, base, anchor, "First instruction")
	if err != nil {
		t.Fatal(err)
	}
	second, err := annotations.Create(t.Context(), record.ID, base, anchor, "Second instruction")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPromptInput(t.Context(), record.ID, session.PromptInput{Text: "Apply these", AnnotationIDs: []uint64{second.ID, first.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAnnotation(t.Context(), record.ID, first.ID); !errors.Is(err, kitannotation.ErrNotFound) {
		t.Fatalf("submitted annotation remains: %v", err)
	}
	pending, err := store.PendingAnnotationSubmissions(t.Context(), record.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending submissions = %v, %v", pending, err)
	}
	providers.mu.Lock()
	request := providers.requests[len(providers.requests)-1]
	providers.mu.Unlock()
	user := request.Messages[len(request.Messages)-1].(droids.UserMessage)
	if len(user.Content) != 2 {
		t.Fatalf("user content = %#v", user.Content)
	}
	bundle, ok := user.Content[1].(droids.AnnotationInput)
	if !ok || len(bundle.Annotations) != 2 || bundle.Annotations[0].ID != second.ID || bundle.Annotations[1].ID != first.ID || !strings.Contains(bundle.Text, "<kit_annotations version=\"1\">") {
		t.Fatalf("annotation bundle = %#v", user.Content[1])
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	var submitted session.TranscriptMessage
	for _, message := range snapshot.Messages {
		if message.Role == "user" {
			submitted = message
		}
	}
	if len(submitted.Content) != 2 || submitted.Content[1].Kind != session.TranscriptContentAnnotations || len(submitted.Content[1].Annotations) != 2 {
		t.Fatalf("transcript content = %#v", submitted.Content)
	}
	events, err := manager.Events(t.Context(), record.ID, snapshot.EventStreamID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var submission *session.Event
	for index := range events.Events {
		if events.Events[index].Kind == session.EventAnnotationSubmitted {
			submission = &events.Events[index]
		}
	}
	if submission == nil || !reflect.DeepEqual(submission.AnnotationIDs, []uint64{second.ID, first.ID}) || !strings.HasPrefix(submission.AcceptedMessageID, "message_") {
		t.Fatalf("submission event = %+v", submission)
	}
}

func TestManagerResolvesOrderedPromptAttachments(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	attachments, err := attachment.NewFilesystem(filepath.Join(base, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")), session.WithAttachmentStore(attachments))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := attachments.Put(t.Context(), attachment.PutInput{SessionID: record.ID, Filename: "first.txt", MediaType: "text/plain", Content: strings.NewReader("alpha"), MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	second, err := attachments.Put(t.Context(), attachment.PutInput{SessionID: record.ID, Filename: "second.txt", MediaType: "text/plain", Content: strings.NewReader("beta"), MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunPromptInput(t.Context(), record.ID, session.PromptInput{Text: "inspect", AttachmentIDs: []string{first.ID, second.ID}}); err != nil {
		t.Fatal(err)
	}
	providers.mu.Lock()
	request := providers.requests[len(providers.requests)-1]
	providers.mu.Unlock()
	user, ok := request.Messages[len(request.Messages)-1].(droids.UserMessage)
	if !ok || len(user.Content) != 3 {
		t.Fatalf("user content = %#v", request.Messages[len(request.Messages)-1])
	}
	for index, want := range []string{"inspect", "first.txt", "second.txt"} {
		text, ok := user.Content[index].(droids.TextInput)
		if !ok || !strings.Contains(text.Text, want) {
			t.Fatalf("content[%d] = %#v, want %q", index, user.Content[index], want)
		}
	}
}

func TestManagerRejectsUnsupportedImageAttachmentBeforeProviderCall(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	attachments, err := attachment.NewFilesystem(filepath.Join(base, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	providers := &authorityProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")), session.WithAttachmentStore(attachments))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	image, err := attachments.Put(t.Context(), attachment.PutInput{SessionID: record.ID, Filename: "image.png", MediaType: "image/png", Content: strings.NewReader("bytes"), MaxBytes: 16, Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartPromptInput(t.Context(), record.ID, session.PromptInput{AttachmentIDs: []string{image.ID}}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("StartPromptInput() error = %v, want invalid input", err)
	}
	providers.mu.Lock()
	calls := providers.calls
	providers.mu.Unlock()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

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
		result, err := manager.RunPrompt(t.Context(), record.ID, strings.Repeat(string(rune('a'+index)), 8_000))
		if err != nil || result.Status != session.RunStatusCompleted {
			t.Fatalf("prompt %d = %+v, %v", index, result, err)
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
func (p *sessionCompactionProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, errors.New("unknown model")
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
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
		// Leave room for Kit's normal system prompt as well as retained context.
		ContextWindow: 20_000, MaxInputTokens: 18_000, MaxOutputTokens: 64,
	}
}

type countingRuntimeBundleBuilder struct {
	mu       sync.Mutex
	builds   int
	delegate session.RuntimeBundleBuilder
}

func (b *countingRuntimeBundleBuilder) Build(ctx context.Context, record session.SessionRecord, currentCWD codingtools.CWDProvider) (session.RuntimeBundle, error) {
	b.mu.Lock()
	b.builds++
	b.mu.Unlock()
	return b.delegate.Build(ctx, record, currentCWD)
}

func (b *countingRuntimeBundleBuilder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.builds
}

type queuedAnnotationReader struct {
	stale   atomic.Bool
	pause   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (r *queuedAnnotationReader) ReadFile(ctx context.Context, _ string, _ string, _ kitannotation.WorkspaceFileAnchor) (kitannotation.FileEvidence, error) {
	if r.pause.Load() {
		r.entered <- struct{}{}
		select {
		case <-r.release:
		case <-ctx.Done():
			return kitannotation.FileEvidence{}, ctx.Err()
		}
	}
	if r.stale.Load() {
		return kitannotation.FileEvidence{}, kitannotation.ErrStale
	}
	return kitannotation.FileEvidence{Content: "first\nsecond\nthird"}, nil
}

func TestManagerQueuesStructuredFollowUps(t *testing.T) {
	for _, mode := range []string{"automatic", "promote", "shutdown", "shutdown-race", "dispose"} {
		for _, kind := range []string{"annotation", "attachment", "both"} {
			if mode == "shutdown-race" && kind == "attachment" {
				continue
			}
			t.Run(mode+"/"+kind, func(t *testing.T) {
				base := t.TempDir()
				store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = store.Close() })
				reader := &queuedAnnotationReader{entered: make(chan struct{}, 1), release: make(chan struct{})}
				annotations, err := kitannotation.NewService(store, reader)
				if err != nil {
					t.Fatal(err)
				}
				attachments, err := attachment.NewFilesystem(filepath.Join(base, "attachments"))
				if err != nil {
					t.Fatal(err)
				}
				providers := &authorityProviders{block: make(chan struct{}), started: make(chan struct{})}
				manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")), session.WithAnnotationService(annotations), session.WithAttachmentStore(attachments))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(manager.Close)
				record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/echo", Temporary: mode == "dispose"})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "dispose" {
					annotations.SetTemporary(record.ID)
				}
				input := session.PromptInput{}
				if kind != "attachment" {
					note, err := annotations.Create(t.Context(), record.ID, base, protocol.AnnotationAnchor{Kind: protocol.AnnotationAnchorWorkspaceFile, WorkspaceFile: &kitannotation.WorkspaceFileAnchor{WorkspaceID: "workspace_q910VG98LjAo2kcaf1zof8JyFwVkDF-ShNRhyZIDuC4", Path: "main.go", FileRevision: "file_H3T9powiSBvNvpOX7c0rfRXfUK9elSR5ymvVeBfsi7A", StartLine: 2, EndLine: 3}}, "Queued instruction")
					if err != nil {
						t.Fatal(err)
					}
					input.AnnotationIDs = []uint64{note.ID}
				}
				if kind != "annotation" {
					file, err := attachments.Put(t.Context(), attachment.PutInput{SessionID: record.ID, Filename: "queued.txt", MediaType: "text/plain", Content: strings.NewReader("queued attachment"), MaxBytes: 64})
					if err != nil {
						t.Fatal(err)
					}
					input.AttachmentIDs = []string{file.ID}
				}
				if _, err := manager.StartPrompt(t.Context(), record.ID, "initial"); err != nil {
					t.Fatal(err)
				}
				<-providers.started
				if mode == "shutdown-race" {
					reader.pause.Store(true)
					submitted := make(chan error, 1)
					go func() { _, err := manager.SubmitPromptInput(t.Context(), record.ID, input); submitted <- err }()
					select {
					case <-reader.entered:
					case <-time.After(5 * time.Second):
						t.Fatal("queue did not reach annotation preparation")
					}
					closed := make(chan struct{})
					go func() { manager.Close(); close(closed) }()
					deadline := time.Now().Add(5 * time.Second)
					for {
						if _, err := manager.Get(t.Context(), record.ID); errors.Is(err, session.ErrClosed) {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("shutdown did not begin")
						}
						time.Sleep(time.Millisecond)
					}
					close(reader.release)
					if err := <-submitted; err != nil {
						t.Fatal(err)
					}
					select {
					case <-closed:
					case <-time.After(5 * time.Second):
						t.Fatal("shutdown did not finish")
					}
					reader.pause.Store(false)
					if _, err := annotations.Update(t.Context(), record.ID, base, input.AnnotationIDs[0], "Recovered after racing shutdown"); err != nil {
						t.Fatalf("shutdown leaked queued ownership: %v", err)
					}
					return
				}

				result, err := manager.SubmitPromptInput(t.Context(), record.ID, input)
				if err != nil || !result.Queued {
					t.Fatalf("queue = %+v, %v", result, err)
				}
				if mode == "dispose" {
					if err := manager.DisposeTemporary(t.Context(), record.ID); err != nil {
						t.Fatal(err)
					}
					if len(input.AnnotationIDs) > 0 {
						if _, err := annotations.Update(t.Context(), record.ID, base, input.AnnotationIDs[0], "Released after disposal"); err != nil {
							t.Fatalf("disposal leaked queue ownership: %v", err)
						}
					}
					return
				}

				wantPreview := "Annotation"
				if len(input.AttachmentIDs) > 0 {
					wantPreview = "Attachment"
				}
				if !reflect.DeepEqual(result.Queue.Previews, []string{wantPreview}) || !reflect.DeepEqual(result.Queue.AnnotationIDs, input.AnnotationIDs) {
					t.Fatalf("queue = %+v", result.Queue)
				}
				if len(input.AnnotationIDs) > 0 {
					if _, err := manager.SubmitPromptInput(t.Context(), record.ID, input); !errors.Is(err, session.ErrInvalidInput) {
						t.Fatalf("duplicate annotation admission = %v", err)
					}
					if _, err := store.GetAnnotation(t.Context(), record.ID, input.AnnotationIDs[0]); err != nil {
						t.Fatalf("queued annotation must remain available: %v", err)
					}
				}
				if len(input.AnnotationIDs) > 0 {
					if _, err := annotations.Update(t.Context(), record.ID, base, input.AnnotationIDs[0], "Modified"); err == nil {
						t.Fatal("queued annotation was editable")
					}
					if err := annotations.Delete(t.Context(), record.ID, input.AnnotationIDs[0]); err == nil {
						t.Fatal("queued annotation was deletable")
					}
				}

				snapshot, err := manager.Snapshot(t.Context(), record.ID)
				if err != nil || !reflect.DeepEqual(snapshot.FollowUps, result.Queue) {
					t.Fatalf("snapshot queue = %+v, %v", snapshot.FollowUps, err)
				}
				restored, err := manager.RestoreFollowUps(t.Context(), record.ID)
				if err != nil || !reflect.DeepEqual(restored.Messages, []session.PromptInput{input}) || restored.Queue.Count != 0 || len(restored.Queue.AnnotationIDs) != 0 {
					t.Fatalf("restore = %+v, %v", restored, err)
				}
				if len(input.AnnotationIDs) > 0 {
					if _, err := annotations.Update(t.Context(), record.ID, base, input.AnnotationIDs[0], "Queued instruction"); err != nil {
						t.Fatalf("restored annotation was not editable: %v", err)
					}
				}

				if _, err := manager.SubmitPromptInput(t.Context(), record.ID, restored.Messages[0]); err != nil {
					t.Fatal(err)
				}
				if mode == "shutdown" {
					manager.Close()
					if len(input.AnnotationIDs) > 0 {
						if _, err := annotations.Update(t.Context(), record.ID, base, input.AnnotationIDs[0], "Recovered draft"); err != nil {
							t.Fatalf("shutdown retained ownership: %v", err)
						}
					}
					return
				}
				reader.stale.Store(true) // The active run changes the file after queue acceptance.

				if mode == "promote" {
					promoted, err := manager.PromoteFollowUps(t.Context(), record.ID)
					if err != nil || promoted.Promoted != 1 || promoted.Queue.Count != 0 || len(promoted.Queue.AnnotationIDs) != 0 {
						t.Fatalf("promotion = %+v, %v", promoted, err)
					}
				}
				close(providers.block)
				deadline := time.Now().Add(5 * time.Second)
				for {
					snapshot, err = manager.Snapshot(t.Context(), record.ID)
					if err != nil {
						t.Fatal(err)
					}
					if snapshot.ActiveRunID == "" && snapshot.FollowUps.Count == 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("queue did not settle: %+v", snapshot.FollowUps)
					}
					time.Sleep(10 * time.Millisecond)
				}
				var users []session.TranscriptMessage
				for _, message := range snapshot.Messages {
					if message.Role == "user" {
						users = append(users, message)
					}
				}
				if len(users) != 2 {
					t.Fatalf("user messages = %+v", users)
				}
				var gotAnnotations []uint64
				var gotAttachments []string
				for _, content := range users[1].Content {
					for _, note := range content.Annotations {
						gotAnnotations = append(gotAnnotations, note.ID)
						if note.Body != "Queued instruction" || note.Preview != "second\nthird" {
							t.Fatalf("captured annotation = %+v", note)
						}
					}
					if content.AttachmentID != "" {
						gotAttachments = append(gotAttachments, content.AttachmentID)
					}
				}
				if !reflect.DeepEqual(gotAnnotations, input.AnnotationIDs) || !reflect.DeepEqual(gotAttachments, input.AttachmentIDs) {
					t.Fatalf("accepted annotations=%v attachments=%v, want %+v", gotAnnotations, gotAttachments, input)
				}
				if len(input.AnnotationIDs) > 0 {
					if _, err := store.GetAnnotation(t.Context(), record.ID, input.AnnotationIDs[0]); !errors.Is(err, kitannotation.ErrNotFound) {
						t.Fatalf("accepted annotation not consumed: %v", err)
					}
				}
			})
		}
	}
}
