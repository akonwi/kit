package session_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
	kitsession "github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestManagerPersistsDirectBashAndProjectsSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	manager, err := kitsession.NewManager(store, &echoProviders{}, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	executionID := newBashID(t)
	execution, err := manager.StartBash(ctx, created.ID, executionID, "printf 'direct output'", false)
	if err != nil {
		t.Fatalf("StartBash() error = %v", err)
	}
	if execution.Status != kitsession.BashExecutionRunning {
		t.Fatalf("start status = %q, want running", execution.Status)
	}
	if _, err := manager.StartBash(ctx, created.ID, executionID, "printf 'direct output'", false); err != nil {
		t.Fatalf("idempotent StartBash() error = %v", err)
	}
	if _, err := manager.StartBash(ctx, created.ID, executionID, "printf different", false); !errors.Is(err, kitsession.ErrInvalidInput) {
		t.Fatalf("reused StartBash() error = %v, want ErrInvalidInput", err)
	}
	completed := waitForBash(t, manager, created.ID, executionID)
	if completed.Status != kitsession.BashExecutionCompleted || completed.Output != "direct output" || completed.ExitCode == nil || *completed.ExitCode != 0 {
		t.Fatalf("completed execution = %+v", completed)
	}
	snapshot, err := manager.Snapshot(ctx, created.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.ActiveBashExecutionID != "" || len(snapshot.Messages) != 1 || snapshot.Messages[0].Role != "bash" || snapshot.Messages[0].Bash == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Messages[0].Bash.Output != "direct output" {
		t.Fatalf("snapshot bash = %+v", snapshot.Messages[0].Bash)
	}
}

func TestManagerShutdownCancelsBashAdmittedDuringShutdown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	gate := make(chan struct{})
	started := make(chan struct{})
	repository := &blockingBashCreateRepository{Repository: store, started: started, gate: gate}
	manager, err := kitsession.NewManager(repository, &echoProviders{}, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	bashID := newBashID(t)
	admitted := make(chan error, 1)
	go func() {
		_, err := manager.StartBash(ctx, created.ID, bashID, "sleep 5", false)
		admitted <- err
	}()
	<-started
	shutdown := make(chan error, 1)
	go func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdown <- manager.Shutdown(shutdownContext)
	}()
	close(gate)
	if err := <-admitted; err != nil {
		t.Fatalf("StartBash() error = %v", err)
	}
	if err := <-shutdown; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	record, err := store.GetBashExecution(ctx, created.ID, bashID)
	if err != nil {
		t.Fatalf("GetBashExecution() error = %v", err)
	}
	if !strings.Contains(string(record.PayloadJSON), `"status":"interrupted"`) {
		t.Fatalf("bash payload after shutdown = %s", record.PayloadJSON)
	}
}

func TestManagerShutdownInterruptsBashWithoutAddingContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	providers := &echoProviders{}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	bashID := newBashID(t)
	if _, err := manager.StartBash(ctx, created.ID, bashID, "sleep 5", false); err != nil {
		t.Fatalf("StartBash() error = %v", err)
	}
	manager.Close()

	resumed, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("resumed NewManager() error = %v", err)
	}
	defer resumed.Close()
	execution, err := resumed.GetBash(ctx, created.ID, bashID)
	if err != nil {
		t.Fatalf("GetBash() error = %v", err)
	}
	if execution.Status != kitsession.BashExecutionInterrupted {
		t.Fatalf("shutdown bash status = %q, want interrupted", execution.Status)
	}
	if _, err := runPrompt(resumed, ctx, created.ID, "after restart"); err != nil {
		t.Fatalf("RunPrompt() error = %v", err)
	}
	texts := strings.Join(requestUserTexts(providers.Requests()[0]), "\n")
	if strings.Contains(texts, "sleep 5") {
		t.Fatalf("provider context = %q, interrupted bash leaked into context", texts)
	}
}

func TestManagerAllowsParentAndBashOverlapButOnlyOneBash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	gate := make(chan struct{})
	providers := &echoProviders{gate: gate}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	runID := newRunID(t)
	if _, err := manager.StartPrompt(ctx, created.ID, runID, "first"); err != nil {
		t.Fatalf("StartPrompt() error = %v", err)
	}
	providers.WaitForRequestCount(t, 1)
	bashID := newBashID(t)
	if _, err := manager.StartBash(ctx, created.ID, bashID, "sleep 5", false); err != nil {
		t.Fatalf("StartBash() during parent run error = %v", err)
	}
	if _, err := manager.StartBash(ctx, created.ID, newBashID(t), "printf second", false); !errors.Is(err, kitsession.ErrBashBusy) {
		t.Fatalf("second StartBash() error = %v, want ErrBashBusy", err)
	}
	if err := manager.AbortBash(ctx, created.ID, bashID); err != nil {
		t.Fatalf("AbortBash() error = %v", err)
	}
	aborted := waitForBash(t, manager, created.ID, bashID)
	if aborted.Status != kitsession.BashExecutionAborted {
		t.Fatalf("bash status = %q, want aborted", aborted.Status)
	}
	close(gate)
	waitForParentRun(t, manager, created.ID, runID)
}

func TestBashCompletedDuringParentRunWaitsForNextContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	gate := make(chan struct{})
	providers := &echoProviders{gate: gate}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	firstRunID := newRunID(t)
	if _, err := manager.StartPrompt(ctx, created.ID, firstRunID, "first prompt"); err != nil {
		t.Fatalf("StartPrompt() error = %v", err)
	}
	providers.WaitForRequestCount(t, 1)
	bashID := newBashID(t)
	if _, err := manager.StartBash(ctx, created.ID, bashID, "printf between", false); err != nil {
		t.Fatalf("StartBash() error = %v", err)
	}
	completed := waitForBash(t, manager, created.ID, bashID)
	if completed.ContextBeforeTurnID != "" {
		t.Fatalf("bash context was claimed before the next parent: %q", completed.ContextBeforeTurnID)
	}
	firstTexts := strings.Join(requestUserTexts(providers.Requests()[0]), "\n")
	if strings.Contains(firstTexts, "between") {
		t.Fatalf("active parent context = %q, bash result leaked into admitted run", firstTexts)
	}
	close(gate)
	waitForParentRun(t, manager, created.ID, firstRunID)
	providers.SetGate(nil)
	second, err := runPrompt(manager, ctx, created.ID, "second prompt")
	if err != nil {
		t.Fatalf("second RunPrompt() error = %v", err)
	}
	claimed := waitForBash(t, manager, created.ID, bashID)
	if claimed.ContextBeforeTurnID != second.TurnID {
		t.Fatalf("bash context turn = %q, want %q", claimed.ContextBeforeTurnID, second.TurnID)
	}
	secondTexts := strings.Join(requestUserTexts(providers.Requests()[1]), "\n")
	if !strings.Contains(secondTexts, "[bash command: printf between]\nbetween") {
		t.Fatalf("next parent context = %q, want completed bash result", secondTexts)
	}
}

func TestIncludedBashReloadsDroidContextAndExcludedBashDoesNot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	defer store.Close()
	providers := &echoProviders{}
	manager, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer manager.Close()
	created, err := manager.Create(ctx, kitsession.CreateInput{CWD: t.TempDir(), Model: "test/echo"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := runPrompt(manager, ctx, created.ID, "first prompt"); err != nil {
		t.Fatalf("first RunPrompt() error = %v", err)
	}

	includedID := newBashID(t)
	if _, err := manager.StartBash(ctx, created.ID, includedID, "printf included", false); err != nil {
		t.Fatalf("included StartBash() error = %v", err)
	}
	waitForBash(t, manager, created.ID, includedID)
	excludedID := newBashID(t)
	if _, err := manager.StartBash(ctx, created.ID, excludedID, "printf excluded", true); err != nil {
		t.Fatalf("excluded StartBash() error = %v", err)
	}
	waitForBash(t, manager, created.ID, excludedID)

	if _, err := runPrompt(manager, ctx, created.ID, "second prompt"); err != nil {
		t.Fatalf("second RunPrompt() error = %v", err)
	}
	requests := providers.Requests()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	texts := requestUserTexts(requests[1])
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "[bash command: printf included]\nincluded") {
		t.Fatalf("second request user messages = %q, want included bash result", texts)
	}
	if strings.Contains(joined, "excluded") {
		t.Fatalf("second request user messages = %q, excluded bash leaked into context", texts)
	}

	manager.Close()
	resumed, err := kitsession.NewManager(store, providers, "test")
	if err != nil {
		t.Fatalf("resumed NewManager() error = %v", err)
	}
	defer resumed.Close()
	if _, err := runPrompt(resumed, ctx, created.ID, "third prompt"); err != nil {
		t.Fatalf("third RunPrompt() error = %v", err)
	}
	resumedTexts := requestUserTexts(providers.Requests()[2])
	includedIndex, secondPromptIndex := -1, -1
	for index, text := range resumedTexts {
		if strings.Contains(text, "[bash command: printf included]") {
			includedIndex = index
		}
		if text == "second prompt" {
			secondPromptIndex = index
		}
		if strings.Contains(text, "excluded") {
			t.Fatalf("resumed request user messages = %q, excluded bash leaked into context", resumedTexts)
		}
	}
	if includedIndex < 0 || secondPromptIndex < 0 || includedIndex > secondPromptIndex {
		t.Fatalf("resumed request user messages = %q, claimed bash moved after its target turn", resumedTexts)
	}
}

func requestUserTexts(request droids.Request) []string {
	var result []string
	for _, message := range request.Messages {
		var content []droids.InputContent
		switch typed := message.(type) {
		case droids.UserMessage:
			content = typed.Content
		case droids.ContextMessage:
			content = typed.Content
		default:
			continue
		}
		for _, block := range content {
			if text, ok := block.(droids.TextInput); ok {
				result = append(result, text.Text)
			}
		}
	}
	return result
}

func waitForBash(t testing.TB, manager *kitsession.Manager, sessionID, executionID string) kitsession.BashExecution {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		execution, err := manager.GetBash(context.Background(), sessionID, executionID)
		if err != nil {
			t.Fatalf("GetBash() error = %v", err)
		}
		if execution.Status != kitsession.BashExecutionRunning {
			return execution
		}
		if time.Now().After(deadline) {
			t.Fatalf("bash execution %q did not finish", executionID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForParentRun(t testing.TB, manager *kitsession.Manager, sessionID, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := manager.GetRun(context.Background(), sessionID, runID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if run.Status != kitsession.RunStatusQueued && run.Status != kitsession.RunStatusRunning {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("parent run %q did not finish", runID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type blockingBashCreateRepository struct {
	kitsession.Repository
	started chan struct{}
	gate    chan struct{}
}

func (r *blockingBashCreateRepository) CreateBashExecution(ctx context.Context, sessionID string, message kitsession.NewMessageRecord) (kitsession.MessageRecord, error) {
	close(r.started)
	select {
	case <-r.gate:
	case <-ctx.Done():
		return kitsession.MessageRecord{}, ctx.Err()
	}
	return r.Repository.CreateBashExecution(ctx, sessionID, message)
}

func newBashID(t testing.TB) string {
	t.Helper()
	id, err := identifier.New("bash_")
	if err != nil {
		t.Fatalf("identifier.New() error = %v", err)
	}
	return id
}
