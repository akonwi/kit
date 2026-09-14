package session_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
)

func TestConfigureSessionAdaptsContextClampsThinkingAndReplacesRuntime(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	if record.ThinkingLevel != "medium" || record.ConfigurationRevision != 1 {
		t.Fatalf("created configuration = %+v", record)
	}
	for index := 0; index < 20; index++ {
		if _, err := manager.RunPrompt(t.Context(), record.ID, fmt.Sprintf("history-%d %s", index, strings.Repeat("x", 800))); err != nil {
			t.Fatal(err)
		}
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeIDs := transcriptIDs(before)

	configured, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: before.Session.ConfigurationRevision,
		Model:            "test/small",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.Session.ModelProvider != "test" || configured.Session.ModelID != "small" ||
		configured.Session.ThinkingLevel != "low" || configured.Session.ConfigurationRevision != 2 {
		t.Fatalf("configured session = %+v", configured.Session)
	}
	if !configured.Compacted || configured.CheckpointID == "" {
		t.Fatalf("configuration did not adapt context: %+v", configured)
	}
	if len(configured.Warnings) != 1 || !strings.Contains(configured.Warnings[0], `Thinking level "medium"`) || !strings.Contains(configured.Warnings[0], `using "low"`) {
		t.Fatalf("configuration warnings = %#v", configured.Warnings)
	}
	if configured.EventStreamID == before.EventStreamID {
		t.Fatalf("configuration retained event stream %q", before.EventStreamID)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameSessionConfiguration(after.Session, configured.Session) || after.EventStreamID != configured.EventStreamID {
		t.Fatalf("snapshot after configuration = %+v, result=%+v", after, configured)
	}
	for _, id := range beforeIDs {
		if !containsString(transcriptIDs(after), id) {
			t.Fatalf("configuration lost historical message %q: before=%#v after=%#v", id, beforeIDs, transcriptIDs(after))
		}
	}
	oldPage, err := manager.Events(t.Context(), record.ID, before.EventStreamID, before.EventCursor)
	if err != nil || !oldPage.ResyncRequired || oldPage.StreamID != configured.EventStreamID {
		t.Fatalf("old event stream page = %+v, %v", oldPage, err)
	}
	persisted, err := store.GetSession(t.Context(), record.ID)
	if err != nil || !sameSessionConfiguration(persisted, configured.Session) {
		t.Fatalf("persisted configuration = %+v, %v", persisted, err)
	}

	callsBeforeStale := providers.callCount()
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: 1, Model: "test/large",
	}); !errors.Is(err, session.ErrConfigurationConflict) {
		t.Fatalf("stale ConfigureSession() error = %v", err)
	}
	if providers.callCount() != callsBeforeStale {
		t.Fatal("stale configuration performed provider work")
	}
	noChange, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: 2, Model: "test/small",
	})
	if err != nil {
		t.Fatal(err)
	}
	if noChange.Session.ConfigurationRevision != 2 || noChange.EventStreamID != configured.EventStreamID {
		t.Fatalf("no-op configuration = %+v", noChange)
	}

	high := "high"
	thinkingChanged, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: 2, Model: "test/small", ThinkingLevel: &high,
	})
	if err != nil {
		t.Fatal(err)
	}
	if thinkingChanged.Session.ThinkingLevel != "high" || thinkingChanged.Session.ConfigurationRevision != 3 ||
		thinkingChanged.EventStreamID != configured.EventStreamID {
		t.Fatalf("thinking configuration = %+v", thinkingChanged)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "after configuration"); err != nil {
		t.Fatal(err)
	}
	last := providers.lastCall()
	if last.model != "small" || last.reasoning != "high" || last.compaction {
		t.Fatalf("next provider request = %+v", last)
	}
	manager.Close()
	reopened, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	reopenedSnapshot, err := reopened.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopenedSnapshot.Session.ModelID != "small" || reopenedSnapshot.Session.ThinkingLevel != "high" || reopenedSnapshot.Session.ConfigurationRevision != 3 {
		t.Fatalf("reopened configuration = %+v", reopenedSnapshot.Session)
	}
	if _, err := reopened.RunPrompt(t.Context(), record.ID, "after restart"); err != nil {
		t.Fatal(err)
	}
	if last := providers.lastCall(); last.model != "small" || last.reasoning != "high" || last.compaction {
		t.Fatalf("provider after restart = %+v", last)
	}
}

func TestConfigureSessionUpdatesTemporaryRegistryAndRuntime(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large", Temporary: true})
	if err != nil {
		t.Fatal(err)
	}
	high := "high"
	configured, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: 1, Model: "test/small", ThinkingLevel: &high,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.Session.Persistent || configured.Session.ModelID != "small" || configured.Session.ThinkingLevel != "high" || configured.Session.ConfigurationRevision != 2 {
		t.Fatalf("temporary configuration = %+v", configured)
	}
	snapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil || !sameSessionConfiguration(snapshot.Session, configured.Session) {
		t.Fatalf("temporary snapshot = %+v, %v", snapshot, err)
	}
}

func TestConfigureSessionRejectsAliasesAndActiveWork(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "small"}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("alias ConfigureSession() error = %v", err)
	}
	unsupported := "medium"
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: 1, Model: "test/small", ThinkingLevel: &unsupported,
	}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("unsupported thinking ConfigureSession() error = %v", err)
	}

	providers.setBlock(make(chan struct{}), make(chan struct{}))
	runDone := make(chan error, 1)
	go func() {
		_, err := manager.RunPrompt(context.Background(), record.ID, "blocked")
		runDone <- err
	}()
	providers.waitStarted(t)
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/small"}); !errors.Is(err, session.ErrConfigureBusy) {
		t.Fatalf("active ConfigureSession() error = %v", err)
	}
	if _, err := manager.CompactSession(t.Context(), record.ID, "compact_active_test"); !errors.Is(err, session.ErrConfigureBusy) {
		t.Fatalf("active CompactSession() error = %v", err)
	}
	high := "high"
	thinking, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: 1, Model: "test/large", ThinkingLevel: &high,
	})
	if err != nil {
		t.Fatalf("thinking change during active run: %v", err)
	}
	if thinking.Session.ThinkingLevel != high || thinking.Session.ConfigurationRevision != 2 {
		t.Fatalf("live thinking configuration = %+v", thinking.Session)
	}
	providers.releaseBlock()
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	if call := providers.lastCall(); call.reasoning != "medium" {
		t.Fatalf("in-flight request reasoning = %q, want medium", call.reasoning)
	}
	providers.setBlock(nil, nil)
	if _, err := manager.RunPrompt(t.Context(), record.ID, "after live thinking change"); err != nil {
		t.Fatal(err)
	}
	if call := providers.lastCall(); call.reasoning != high {
		t.Fatalf("next request reasoning = %q, want %q", call.reasoning, high)
	}

	bash, err := manager.StartBash(t.Context(), record.ID, "bash_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "sleep 30", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/small"}); !errors.Is(err, session.ErrConfigureBusy) {
		t.Fatalf("bash ConfigureSession() error = %v", err)
	}
	if _, err := manager.CompactSession(t.Context(), record.ID, "compact_bash_test"); !errors.Is(err, session.ErrConfigureBusy) {
		t.Fatalf("bash CompactSession() error = %v", err)
	}
	if err := manager.AbortBash(t.Context(), record.ID, bash.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConfigureSessionRejectsReloadAndDeleteTransitions(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &blockingArchiveRepository{Repository: store}
	builder := &blockingReloadBundleBuilder{
		delegate: staticRuntimeBundleBuilder("system"), started: make(chan struct{}), release: make(chan struct{}),
	}
	manager, err := session.NewManager(repository, &configurationProviders{}, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
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
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/small"}); !errors.Is(err, session.ErrConfigureBusy) {
		t.Fatalf("reloading ConfigureSession() error = %v", err)
	}
	close(builder.release)
	if err := <-reloadDone; err != nil {
		t.Fatal(err)
	}

	repository.startBlocking()
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- manager.Delete(context.Background(), record.ID) }()
	repository.waitStarted(t)
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/small"}); !errors.Is(err, session.ErrDeleteBusy) {
		t.Fatalf("deleting ConfigureSession() error = %v", err)
	}
	repository.releaseBlock()
	if err := <-deleteDone; err != nil {
		t.Fatal(err)
	}
}

type blockingArchiveRepository struct {
	session.Repository
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
}

func (repository *blockingArchiveRepository) startBlocking() {
	repository.mu.Lock()
	repository.started, repository.release = make(chan struct{}), make(chan struct{})
	repository.mu.Unlock()
}

func (repository *blockingArchiveRepository) ArchiveSession(ctx context.Context, id string, archivedAt time.Time) error {
	repository.mu.Lock()
	started, release := repository.started, repository.release
	repository.mu.Unlock()
	if started != nil {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return repository.Repository.ArchiveSession(ctx, id, archivedAt)
}

func (repository *blockingArchiveRepository) waitStarted(t *testing.T) {
	t.Helper()
	repository.mu.Lock()
	started := repository.started
	repository.mu.Unlock()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not reach archive")
	}
}

func (repository *blockingArchiveRepository) releaseBlock() {
	repository.mu.Lock()
	release := repository.release
	repository.started, repository.release = nil, nil
	repository.mu.Unlock()
	close(release)
}

func TestConfigureSessionPreparationFailuresKeepOldConfiguration(t *testing.T) {
	for _, test := range []struct {
		name         string
		failAt       int
		invalidateAt int
	}{
		{name: "bundle build", failAt: 2},
		{name: "replacement open", invalidateAt: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			providers := &configurationProviders{}
			builder := &scriptedRuntimeBundleBuilder{
				delegate: staticRuntimeBundleBuilder("system"), failAt: test.failAt, invalidateAt: test.invalidateAt,
			}
			manager, err := session.NewManager(store, providers, builder, session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(manager.Close)
			record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
			if err != nil {
				t.Fatal(err)
			}
			before, err := manager.Snapshot(t.Context(), record.ID)
			if err != nil {
				t.Fatal(err)
			}
			high := "high"
			if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
				ExpectedRevision: 1, Model: "test/small", ThinkingLevel: &high,
			}); err == nil {
				t.Fatal("ConfigureSession() accepted invalid replacement preparation")
			}
			after, err := manager.Snapshot(t.Context(), record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Session.ModelID != "large" || after.Session.ThinkingLevel != "medium" || after.Session.ConfigurationRevision != 1 || after.EventStreamID != before.EventStreamID {
				t.Fatalf("precommit preparation changed session = %+v", after)
			}
			if _, err := manager.RunPrompt(t.Context(), record.ID, "old runtime"); err != nil {
				t.Fatal(err)
			}
			if call := providers.lastCall(); call.model != "large" || call.reasoning != "medium" {
				t.Fatalf("provider after preparation failure = %+v", call)
			}
		})
	}
}

func TestConfigureSessionReconcilesCommittedWriteAndRetainsRuntimeOnPrecommitFailure(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := &controlledConfigurationRepository{Repository: store}
	providers := &configurationProviders{}
	manager, err := session.NewManager(repository, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	repository.setFailure(true, false, false)
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/small"}); !errors.Is(err, errConfigurationWrite) {
		t.Fatalf("precommit ConfigureSession() error = %v", err)
	}
	afterFailure, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.Session.ModelID != "large" || afterFailure.Session.ConfigurationRevision != 1 || afterFailure.EventStreamID != before.EventStreamID {
		t.Fatalf("precommit failure changed session = %+v", afterFailure)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "old runtime remains"); err != nil {
		t.Fatal(err)
	}
	if call := providers.lastCall(); call.model != "large" {
		t.Fatalf("provider after precommit failure = %+v", call)
	}

	repository.setFailure(false, true, false)
	configured, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{ExpectedRevision: 1, Model: "test/small"})
	if err != nil {
		t.Fatalf("committed ambiguous ConfigureSession() error = %v", err)
	}
	if configured.Session.ModelID != "small" || configured.Session.ConfigurationRevision != 2 || configured.EventStreamID == before.EventStreamID {
		t.Fatalf("reconciled configuration = %+v", configured)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "new runtime published"); err != nil {
		t.Fatal(err)
	}
	if call := providers.lastCall(); call.model != "small" || call.reasoning != "low" {
		t.Fatalf("provider after reconciled commit = %+v", call)
	}

	repository.setFailure(false, true, true)
	high := "high"
	if _, err := manager.ConfigureSession(t.Context(), record.ID, session.ConfigureSessionInput{
		ExpectedRevision: 2, Model: "test/small", ThinkingLevel: &high,
	}); err == nil {
		t.Fatal("ConfigureSession() resolved an intentionally unreadable write outcome")
	}
	resynchronized, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resynchronized.Session.ModelID != "small" || resynchronized.Session.ThinkingLevel != "high" ||
		resynchronized.Session.ConfigurationRevision != 3 || resynchronized.EventStreamID == configured.EventStreamID {
		t.Fatalf("snapshot after unknown write outcome = %+v", resynchronized)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "runtime after unknown outcome"); err != nil {
		t.Fatal(err)
	}
	if call := providers.lastCall(); call.model != "small" || call.reasoning != "high" {
		t.Fatalf("provider after unknown-outcome resync = %+v", call)
	}
}

var errConfigurationWrite = errors.New("simulated configuration write failure")

type controlledConfigurationRepository struct {
	session.Repository
	mu                       sync.Mutex
	failBefore               bool
	failAfter                bool
	failReconcileAfterCommit bool
	failNextGet              bool
}

func (repository *controlledConfigurationRepository) setFailure(before, after, reconcile bool) {
	repository.mu.Lock()
	repository.failBefore, repository.failAfter, repository.failReconcileAfterCommit = before, after, reconcile
	repository.mu.Unlock()
}

func (repository *controlledConfigurationRepository) UpdateSessionConfiguration(ctx context.Context, update session.ConfigurationUpdate) (session.SessionRecord, error) {
	repository.mu.Lock()
	before, after, reconcile := repository.failBefore, repository.failAfter, repository.failReconcileAfterCommit
	repository.failBefore, repository.failAfter, repository.failReconcileAfterCommit = false, false, false
	repository.mu.Unlock()
	if before {
		return session.SessionRecord{}, errConfigurationWrite
	}
	record, err := repository.Repository.UpdateSessionConfiguration(ctx, update)
	if err == nil && after {
		if reconcile {
			repository.mu.Lock()
			repository.failNextGet = true
			repository.mu.Unlock()
		}
		return session.SessionRecord{}, errConfigurationWrite
	}
	return record, err
}

func (repository *controlledConfigurationRepository) GetSession(ctx context.Context, id string) (session.SessionRecord, error) {
	repository.mu.Lock()
	fail := repository.failNextGet
	repository.failNextGet = false
	repository.mu.Unlock()
	if fail {
		return session.SessionRecord{}, errors.New("simulated configuration reconciliation read failure")
	}
	return repository.Repository.GetSession(ctx, id)
}

func TestCompactSessionCreatesCheckpointPreservesHistoryAndReopens(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	providers.setSmallContextWindow(128_000)
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/small", ThinkingLevel: "low"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 20; index++ {
		if _, err := manager.RunPrompt(t.Context(), record.ID, fmt.Sprintf("threshold-%d %s", index, strings.Repeat("z", 800))); err != nil {
			t.Fatal(err)
		}
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	providers.setSmallContextWindow(8_000)
	beforeIDs := transcriptIDs(before)
	result, err := manager.CompactSession(t.Context(), record.ID, "compact_checkpoint_configuration_test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.CheckpointID == "" || result.EventStreamID == before.EventStreamID {
		t.Fatalf("CompactSession() = %+v", result)
	}
	after, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range beforeIDs {
		if !containsString(transcriptIDs(after), id) {
			t.Fatalf("explicit compaction lost diagnostic message %q", id)
		}
	}
	manager.Close()
	reopened, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if _, err := reopened.RunPrompt(t.Context(), record.ID, "continue after reopened compact"); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticCompactionLifecycleReachesSessionEventStream(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/small", ThinkingLevel: "low"})
	if err != nil {
		t.Fatal(err)
	}
	compactions := providers.compactionCount()
	compactedRunID := ""
	for index := 0; index < 50 && compactedRunID == ""; index++ {
		result, runErr := manager.RunPrompt(t.Context(), record.ID, fmt.Sprintf("automatic-%d %s", index, strings.Repeat("a", 600)))
		if runErr != nil {
			t.Fatal(runErr)
		}
		if providers.compactionCount() > compactions {
			compactedRunID = result.RunID
		}
	}
	if compactedRunID == "" {
		t.Fatal("automatic compaction did not run")
	}
	compactedSnapshot, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundStarted, foundCompleted, foundContext := false, false, false
	var compactedRunEvents []session.EventKind
	after := int64(0)
	for {
		page, err := manager.Events(t.Context(), record.ID, compactedSnapshot.EventStreamID, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Events {
			if event.RunID != compactedRunID {
				continue
			}
			compactedRunEvents = append(compactedRunEvents, event.Kind)
			foundStarted = foundStarted || event.Kind == session.EventCompactionStarted
			foundCompleted = foundCompleted || event.Kind == session.EventCompactionCompleted
			if event.Kind == session.EventContextUpdated {
				foundContext = event.ContextTokens >= 0 && event.ContextWindow > 0
			}
		}
		if len(page.Events) == 0 || page.Events[len(page.Events)-1].Sequence >= page.LastSequence {
			break
		}
		after = page.Events[len(page.Events)-1].Sequence
	}
	if !foundStarted || !foundCompleted || !foundContext {
		t.Fatalf("automatic compaction lifecycle started=%v completed=%v context=%v events=%v", foundStarted, foundCompleted, foundContext, compactedRunEvents)
	}
}

func TestCompactSessionForcesBelowThresholdContext(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 6; index++ {
		if _, err := manager.RunPrompt(t.Context(), record.ID, fmt.Sprintf("compact-%d %s", index, strings.Repeat("y", 500))); err != nil {
			t.Fatal(err)
		}
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.ContextTokens*100 >= before.ContextWindow*80 {
		t.Fatalf("test context unexpectedly reached automatic threshold: %d/%d", before.ContextTokens, before.ContextWindow)
	}
	result, err := manager.CompactSession(t.Context(), record.ID, "compact_forced_configuration_test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.CheckpointID == "" || result.EventStreamID == before.EventStreamID {
		t.Fatalf("forced CompactSession() = %+v", result)
	}
}

func TestCompactSessionEmptyContextNoOpIsIdempotentAndPreservesStream(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	record, err := manager.Create(t.Context(), session.CreateInput{CWD: base, Model: "test/large"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.CompactSession(t.Context(), record.ID, "compact_explicit_configuration_test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Compacted || result.CheckpointID != "" || result.EventStreamID != before.EventStreamID {
		t.Fatalf("no-op CompactSession() = %+v", result)
	}
	afterFirst, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	compactions := providers.compactionCount()
	time.Sleep(time.Millisecond)
	replayed, err := manager.CompactSession(t.Context(), record.ID, "compact_explicit_configuration_test")
	if err != nil {
		t.Fatal(err)
	}
	if replayed != result || providers.compactionCount() != compactions {
		t.Fatalf("replayed CompactSession() = %+v, first=%+v compactions=%d/%d", replayed, result, providers.compactionCount(), compactions)
	}
	afterReplay, err := manager.Snapshot(t.Context(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !afterReplay.Session.UpdatedAt.Equal(afterFirst.Session.UpdatedAt) {
		t.Fatalf("compaction replay advanced activity from %v to %v", afterFirst.Session.UpdatedAt, afterReplay.Session.UpdatedAt)
	}
	if _, err := manager.RunPrompt(t.Context(), record.ID, "continue after compact"); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRestoresAndPersistsMissingOrUnsupportedThinking(t *testing.T) {
	base := t.TempDir()
	store, err := storage.Open(t.Context(), filepath.Join(base, "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	providers := &configurationProviders{}
	stale, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CWD: base, Persistent: true,
		ModelProvider: "test", ModelID: "small", ThinkingLevel: "xhigh",
	})
	if err != nil {
		t.Fatal(err)
	}
	missing, err := store.CreateSession(t.Context(), session.NewSession{
		ID: "session_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CWD: base, Persistent: true,
		ModelProvider: "test", ModelID: "small",
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	staleSnapshot, err := manager.Snapshot(t.Context(), stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if staleSnapshot.Session.ThinkingLevel != "high" || staleSnapshot.Session.ConfigurationRevision != 2 ||
		len(staleSnapshot.Warnings) != 1 {
		t.Fatalf("restored stale thinking = %+v", staleSnapshot)
	}
	missingSnapshot, err := manager.Snapshot(t.Context(), missing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if missingSnapshot.Session.ThinkingLevel != "off" || missingSnapshot.Session.ConfigurationRevision != 2 || len(missingSnapshot.Warnings) != 0 {
		t.Fatalf("restored missing thinking = %+v", missingSnapshot)
	}
	manager.Close()

	reopened, err := session.NewManager(store, providers, staticRuntimeBundleBuilder("system"), session.WithDroidStoreDirectory(filepath.Join(base, "droids")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	reopenedSnapshot, err := reopened.Snapshot(t.Context(), stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopenedSnapshot.Session.ThinkingLevel != "high" || reopenedSnapshot.Session.ConfigurationRevision != 2 || len(reopenedSnapshot.Warnings) != 0 {
		t.Fatalf("reopened restored thinking = %+v", reopenedSnapshot)
	}
}

type configurationProviderCall struct {
	model      string
	reasoning  string
	compaction bool
}

type configurationProviders struct {
	mu           sync.Mutex
	calls        []configurationProviderCall
	compactions  int
	block        chan struct{}
	started      chan struct{}
	smallContext int
}

func (p *configurationProviders) Models() []droids.Model {
	return []droids.Model{p.largeModel(), p.smallModel()}
}
func (p *configurationProviders) Model(id string) (droids.Model, bool) {
	for _, model := range p.Models() {
		if id == model.ID || id == model.Provider+"/"+model.ID {
			return model, true
		}
	}
	return droids.Model{}, false
}
func (p *configurationProviders) Resolve(id string) (droids.Model, error) {
	model, ok := p.Model(id)
	if !ok {
		return droids.Model{}, fmt.Errorf("unknown model %q", id)
	}
	return droids.BindModel(droids.AdaptProvider("test", p.Models(), p.Stream), model)
}
func (*configurationProviders) RefreshModels(context.Context) error { return nil }
func (p *configurationProviders) Stream(ctx context.Context, model droids.Model, request droids.Request) droids.Stream {
	compaction := strings.HasPrefix(request.SystemPrompt, "Summarize the supplied conversation")
	p.mu.Lock()
	p.calls = append(p.calls, configurationProviderCall{model: model.ID, reasoning: request.Reasoning, compaction: compaction})
	if compaction {
		p.compactions++
	}
	block, started := p.block, p.started
	p.mu.Unlock()
	if block != nil && !compaction {
		if started != nil {
			select {
			case <-started:
			default:
				close(started)
			}
		}
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	text := "ok"
	if compaction {
		text = "compact summary"
	}
	return &configurationStream{message: droids.AssistantMessage{
		Provider: "test", Model: model.ID, StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: text}},
	}}
}
func (*configurationProviders) largeModel() droids.Model {
	return droids.Model{
		ID: "large", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: 128_000, MaxOutputTokens: 2_048,
		Reasoning: true, ReasoningLevels: []string{"none", "low", "medium", "high", "xhigh"},
	}
}
func (p *configurationProviders) smallModel() droids.Model {
	p.mu.Lock()
	contextWindow := p.smallContext
	p.mu.Unlock()
	if contextWindow == 0 {
		contextWindow = 8_000
	}
	return droids.Model{
		ID: "small", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		ContextWindow: contextWindow, MaxOutputTokens: 1_024,
		Reasoning: true, ReasoningLevels: []string{"none", "low", "high"},
	}
}
func (p *configurationProviders) setSmallContextWindow(contextWindow int) {
	p.mu.Lock()
	p.smallContext = contextWindow
	p.mu.Unlock()
}

func (p *configurationProviders) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}
func (p *configurationProviders) compactionCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.compactions
}
func (p *configurationProviders) lastCall() configurationProviderCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[len(p.calls)-1]
}
func (p *configurationProviders) setBlock(block, started chan struct{}) {
	p.mu.Lock()
	p.block, p.started = block, started
	p.mu.Unlock()
}
func (p *configurationProviders) waitStarted(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
}
func (p *configurationProviders) releaseBlock() {
	p.mu.Lock()
	block := p.block
	p.block, p.started = nil, nil
	p.mu.Unlock()
	close(block)
}

type configurationStream struct{ message droids.AssistantMessage }

func (stream *configurationStream) Events() <-chan droids.StreamEvent {
	events := make(chan droids.StreamEvent, 1)
	events <- droids.StreamDone{Message: stream.message}
	close(events)
	return events
}
func (stream *configurationStream) Result() droids.AssistantMessage { return stream.message }

func sameSessionConfiguration(left, right session.SessionRecord) bool {
	return left.ID == right.ID && left.ModelProvider == right.ModelProvider && left.ModelID == right.ModelID &&
		left.ThinkingLevel == right.ThinkingLevel && left.ConfigurationRevision == right.ConfigurationRevision &&
		left.UpdatedAt.Equal(right.UpdatedAt)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
