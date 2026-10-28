package session_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func newCompactionFailureManager(t *testing.T, base string, store *storage.Store, providers *configurationProviders) *session.Manager {
	t.Helper()
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func openCompactionFailureStore(t *testing.T, base string) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestExplicitCompactionFailureReportsItsReason(t *testing.T) {
	base := t.TempDir()
	store := openCompactionFailureStore(t, base)
	providers := &configurationProviders{}
	manager := newCompactionFailureManager(t, base, store, providers)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two", "three"} {
		if _, err := manager.RunPrompt(t.Context(), record.ID, text); err != nil {
			t.Fatal(err)
		}
	}
	providers.mu.Lock()
	providers.compactionStop = droids.StopReasonLength
	providers.mu.Unlock()

	watcher := watchSession(t, manager, record.ID)
	_, err = manager.CompactSession(t.Context(), record.ID, "compact_truncated")
	var failed *session.CompactionFailedError
	if !errors.Is(err, session.ErrCompactionFailed) || !errors.As(err, &failed) || failed.Reason != session.CompactionFailureSummaryTruncated {
		t.Fatalf("CompactSession() error = %v, want summary_truncated", err)
	}
	ended := watcher.next(session.EventCompactionFailed)
	if ended.CompactionID != "compact_truncated" || ended.CompactionFailure != session.CompactionFailureSummaryTruncated || ended.ErrorMessage != "" {
		t.Fatalf("failed compaction event = %+v, want summary_truncated without text", ended)
	}
}

func TestAModelChangeWhoseCompactionFailsReportsItsReason(t *testing.T) {
	base := t.TempDir()
	store := openCompactionFailureStore(t, base)
	providers := &configurationProviders{}
	manager := newCompactionFailureManager(t, base, store, providers)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large", ThinkingLevel: "low"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 12; index++ {
		if _, err := manager.RunPrompt(t.Context(), record.ID, fmt.Sprintf("history-%d %s", index, strings.Repeat("y", 800))); err != nil {
			t.Fatal(err)
		}
	}
	providers.mu.Lock()
	providers.compactionStop = droids.StopReasonLength
	providers.mu.Unlock()
	_, err = manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: record.ConfigurationRevision, Model: "test/small"})
	var failed *session.CompactionFailedError
	if !errors.As(err, &failed) || failed.Reason != session.CompactionFailureSummaryTruncated {
		t.Fatalf("ConfigureSession() error = %v, want summary_truncated", err)
	}
}

func TestATurnThatCompactionFailedKeepsItsReasonAcrossRestart(t *testing.T) {
	base := t.TempDir()
	store := openCompactionFailureStore(t, base)
	providers := &configurationProviders{compactionStop: droids.StopReasonLength}
	manager := newCompactionFailureManager(t, base, store, providers)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/small", ThinkingLevel: "low"})
	if err != nil {
		t.Fatal(err)
	}
	var failed session.PromptResult
	for index := 0; index < 50 && failed.RunID == ""; index++ {
		result, err := manager.RunPrompt(t.Context(), record.ID, fmt.Sprintf("automatic-%d %s", index, strings.Repeat("a", 600)))
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == session.RunStatusFailed {
			failed = result
		}
	}
	if failed.RunID == "" || failed.CompactionFailure != session.CompactionFailureSummaryTruncated || failed.ErrorMessage != "Context compaction failed" {
		t.Fatalf("failed turn = %+v, want a summary_truncated compaction failure", failed)
	}
	run, err := manager.GetRun(t.Context(), record.ID, failed.RunID)
	if err != nil || run.CompactionFailure != session.CompactionFailureSummaryTruncated {
		t.Fatalf("GetRun() = %+v, %v", run, err)
	}

	manager.Close()
	reopened := newCompactionFailureManager(t, base, store, providers)
	run, err = reopened.GetRun(t.Context(), record.ID, failed.RunID)
	if err != nil || run.Status != session.RunStatusFailed || run.CompactionFailure != session.CompactionFailureSummaryTruncated {
		t.Fatalf("GetRun() after restart = %+v, %v", run, err)
	}
}
