package droids_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

func TestSDKConfigOverridesActiveModelContextWindow(t *testing.T) {
	droid, err := droids.Spawn(t.Context(), "conversation_context_override", droids.Config{
		Providers: newAdaptationProviders(), Model: "test/active", ContextWindow: 1_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Context.Usage.ContextWindow != 1_000_000 || snapshot.Context.Usage.MaxInputTokens != 1_000_000 {
		t.Fatalf("context usage = %+v", snapshot.Context.Usage)
	}
}

func TestSDKAssessesAndIdempotentlyCompactsSettledContextForTargetModel(t *testing.T) {
	providers := newAdaptationProviders()
	providers.activeSummaryFails = true
	store := droids.NewMemoryStore()
	droid, err := droids.Spawn(t.Context(), "conversation_adapt", droids.Config{
		Store: store, Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedAdaptationHistory(t, droid, 4)

	target := droids.ContextTarget{Model: "test/small", Reasoning: "off"}
	assessment, err := droid.AssessContext(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Target.Model != "test/small" || !assessment.ReplayCompatible || !assessment.RequiresCompaction {
		t.Fatalf("assessment = %+v", assessment)
	}
	beforeHistory, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	beforeSnapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := droid.Subscribe(t.Context(), droids.SubscribeOptions{After: beforeSnapshot.LastEvent, Buffer: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	result, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_model_switch_1", Target: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.CheckpointID == "" || result.Target.Model != "test/small" || sdkShouldCompactForTest(result.After) {
		t.Fatalf("compaction result = %+v", result)
	}
	if result.After.EstimatedInput >= result.Before.EstimatedInput {
		t.Fatalf("context did not shrink: before=%+v after=%+v", result.Before, result.After)
	}
	for index, wantKind := range []string{"compaction.started", "compaction.completed", "context.updated"} {
		select {
		case envelope := <-subscription.Events():
			event, ok := envelope.Event.(droids.LifecycleEvent)
			if !ok || event.Kind != wantKind || envelope.Sequence != beforeSnapshot.LastEvent+droids.EventSequence(index+1) || !strings.Contains(string(event.Data), "context_model_switch_1") {
				t.Fatalf("adaptation event %d = %+v, want %q", index, envelope, wantKind)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %q", wantKind)
		}
	}
	afterSnapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if afterSnapshot.Context.CheckpointID != result.CheckpointID || afterSnapshot.Context.Messages >= len(beforeHistory.Messages) {
		t.Fatalf("snapshot context = %+v, history=%d", afterSnapshot.Context, len(beforeHistory.Messages))
	}
	afterHistory, err := droid.History(t.Context(), droids.HistoryQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(afterHistory.Messages) != len(beforeHistory.Messages) {
		t.Fatalf("diagnostic history changed from %d to %d messages", len(beforeHistory.Messages), len(afterHistory.Messages))
	}
	compactions := providers.compactions.Load()
	replayed, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_model_switch_1", Target: droids.ContextTarget{Model: "test/small", Reasoning: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replayed, result) || providers.compactions.Load() != compactions {
		t.Fatalf("replayed result = %+v, want %+v; compactions=%d", replayed, result, providers.compactions.Load())
	}
	if _, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_model_switch_1", Target: droids.ContextTarget{Model: "test/active"},
	}); !errors.Is(err, droids.ErrConflict) {
		t.Fatalf("reused operation target error = %v", err)
	}
	forked, err := droid.Fork(t.Context(), "conversation_adapt_receipt_fork", droids.ForkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inherited, err := forked.Droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_model_switch_1", Target: target,
	})
	if err != nil || !reflect.DeepEqual(inherited, result) || providers.compactions.Load() != compactions {
		t.Fatalf("inherited receipt = %+v, %v; compactions=%d", inherited, err, providers.compactions.Load())
	}
	if err := forked.Droid.Close(); err != nil {
		t.Fatal(err)
	}
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}
	providers.smallUnavailable = true

	reopened, err := droids.Spawn(t.Context(), "conversation_adapt", droids.Config{
		Store: store, Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	afterRestart, err := reopened.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_model_switch_1", Target: target,
	})
	if err != nil || !reflect.DeepEqual(afterRestart, result) || providers.compactions.Load() != compactions {
		t.Fatalf("receipt after restart = %+v, %v; compactions=%d", afterRestart, err, providers.compactions.Load())
	}
}

func TestSDKForcedCompactionIgnoresAutomaticThreshold(t *testing.T) {
	providers := newAdaptationProviders()
	droid, err := droids.Spawn(t.Context(), "conversation_force_compact", droids.Config{
		Store: droids.NewMemoryStore(), Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	seedAdaptationHistory(t, droid, 4)
	target := droids.ContextTarget{Model: "test/active", Reasoning: "off"}
	assessment, err := droid.AssessContext(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.RequiresCompaction {
		t.Fatalf("test context unexpectedly required automatic compaction: %+v", assessment)
	}
	options := droids.CompactContextOptions{OperationID: "context_force_1", Target: target, Force: true}
	result, err := droid.CompactContext(t.Context(), options)
	if err != nil || !result.Compacted || !result.Forced || result.CheckpointID == "" {
		t.Fatalf("forced CompactContext() = %+v, %v", result, err)
	}
	replayed, err := droid.CompactContext(t.Context(), options)
	if err != nil || !reflect.DeepEqual(replayed, result) || providers.compactions.Load() != 1 {
		t.Fatalf("forced receipt replay = %+v, %v; summaries=%d", replayed, err, providers.compactions.Load())
	}
	options.Force = false
	if _, err := droid.CompactContext(t.Context(), options); !errors.Is(err, droids.ErrConflict) {
		t.Fatalf("operation force reuse error = %v, want conflict", err)
	}
}

func TestSDKCompactionReceiptPersistsInSQLite(t *testing.T) {
	providers := newAdaptationProviders()
	path := filepath.Join(t.TempDir(), "droid.db")
	store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	droid, err := droids.Spawn(t.Context(), "conversation_adapt_sqlite", droids.Config{
		Store: store, Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedAdaptationHistory(t, droid, 4)
	options := droids.CompactContextOptions{
		OperationID: "context_sqlite", Target: droids.ContextTarget{Model: "test/small", Reasoning: "off"},
	}
	original, err := droid.CompactContext(t.Context(), options)
	if err != nil || !original.Compacted {
		t.Fatalf("CompactContext() = %+v, %v", original, err)
	}
	compactions := providers.compactions.Load()
	if err := droid.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedStore.Close() })
	reopened, err := droids.Spawn(t.Context(), "conversation_adapt_sqlite", droids.Config{
		Store: reopenedStore, Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	replayed, err := reopened.CompactContext(t.Context(), options)
	if err != nil || !reflect.DeepEqual(replayed, original) || providers.compactions.Load() != compactions {
		t.Fatalf("SQLite receipt replay = %+v, %v; compactions=%d", replayed, err, providers.compactions.Load())
	}
}

func TestSDKAmbiguousCompactionCommitReconcilesReceiptAndCheckpoint(t *testing.T) {
	providers := newAdaptationProviders()
	store := &ambiguousCompactionStore{Store: droids.NewMemoryStore()}
	droid, err := droids.Spawn(t.Context(), "conversation_adapt_ambiguous", droids.Config{
		Store: store, Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	seedAdaptationHistory(t, droid, 4)
	store.failCompleted.Store(true)
	options := droids.CompactContextOptions{
		OperationID: "context_ambiguous", Target: droids.ContextTarget{Model: "test/small", Reasoning: "off"},
	}
	result, err := droid.CompactContext(t.Context(), options)
	if err != nil || !result.Compacted || result.CheckpointID == "" {
		t.Fatalf("CompactContext() = %+v, %v", result, err)
	}
	if providers.compactions.Load() != 1 {
		t.Fatalf("summary requests = %d, want 1", providers.compactions.Load())
	}
	replayed, err := droid.CompactContext(t.Context(), options)
	if err != nil || !reflect.DeepEqual(replayed, result) || providers.compactions.Load() != 1 {
		t.Fatalf("replayed ambiguous result = %+v, %v; summaries=%d", replayed, err, providers.compactions.Load())
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Context.CheckpointID != result.CheckpointID {
		t.Fatalf("checkpoint = %q, want %q", snapshot.Context.CheckpointID, result.CheckpointID)
	}
}

type ambiguousCompactionStore struct {
	droids.Store
	failCompleted atomic.Bool
}

func (store *ambiguousCompactionStore) Commit(ctx context.Context, request droids.CommitRequest) (droids.CommitResult, error) {
	result, err := store.Store.Commit(ctx, request)
	if err != nil {
		return result, err
	}
	for _, event := range request.Events {
		if event.Kind == "compaction.completed" && store.failCompleted.CompareAndSwap(true, false) {
			return droids.CommitResult{}, errors.New("simulated response loss after compaction commit")
		}
	}
	return result, nil
}

func TestSDKQuiescentCompactionUsesInheritedMessageProvenanceInReadyFork(t *testing.T) {
	providers := newAdaptationProviders()
	providers.rejectActiveForSmall = true
	source, err := droids.Spawn(t.Context(), "conversation_adapt_source", droids.Config{
		Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	seedAdaptationHistory(t, source, 3)
	forked, err := source.Fork(t.Context(), "conversation_adapt_child", droids.ForkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = forked.Droid.Close() })

	assessment, err := forked.Droid.AssessContext(t.Context(), droids.ContextTarget{Model: "test/small", Reasoning: "off"})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.ReplayCompatible || !assessment.RequiresCompaction {
		t.Fatalf("fork assessment = %+v", assessment)
	}
	result, err := forked.Droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_fork_switch", Target: droids.ContextTarget{Model: "test/small", Reasoning: "off"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted {
		t.Fatalf("fork compaction result = %+v", result)
	}
	snapshot, err := forked.Droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Context.Messages != 1 || snapshot.Context.CheckpointID != result.CheckpointID {
		t.Fatalf("fork compacted context = %+v", snapshot.Context)
	}
}

func TestSDKCompactionRejectsReplacementUnsafeForCurrentModel(t *testing.T) {
	providers := newAdaptationProviders()
	providers.divergentMeasure = true
	droid, err := droids.Spawn(t.Context(), "conversation_adapt_current_safety", droids.Config{
		Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	seedAdaptationHistory(t, droid, 3)
	before, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_current_unsafe", Target: droids.ContextTarget{Model: "test/small", Reasoning: "off"},
	}); !errors.Is(err, droids.ErrContextNotAdaptable) {
		t.Fatalf("CompactContext() error = %v", err)
	}
	if _, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_current_unsafe", Target: droids.ContextTarget{Model: "test/active"},
	}); !errors.Is(err, droids.ErrConflict) {
		t.Fatalf("failed operation target reuse error = %v", err)
	}
	after, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if after.Context.CheckpointID != before.Context.CheckpointID || after.Context.Messages != before.Context.Messages {
		t.Fatalf("failed adaptation changed context from %+v to %+v", before.Context, after.Context)
	}
}

func TestSDKCompactionReceiptWinsOverLaterBusyState(t *testing.T) {
	providers := newAdaptationProviders()
	droid, err := droids.Spawn(t.Context(), "conversation_adapt_receipt_busy", droids.Config{
		Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	options := droids.CompactContextOptions{
		OperationID: "context_noop", Target: droids.ContextTarget{Model: "test/active"},
	}
	before, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := droid.Subscribe(t.Context(), droids.SubscribeOptions{After: before.LastEvent, Buffer: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	original, err := droid.CompactContext(t.Context(), options)
	if err != nil || original.Compacted {
		t.Fatalf("initial no-op = %+v, %v", original, err)
	}
	for _, wantKind := range []string{"compaction.started", "compaction.completed"} {
		select {
		case envelope := <-subscription.Events():
			event, ok := envelope.Event.(droids.LifecycleEvent)
			if !ok || event.Kind != wantKind {
				t.Fatalf("no-op adaptation event = %+v, want %q", envelope, wantKind)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %q", wantKind)
		}
	}
	providers.blockNormal = make(chan struct{})
	providers.normalStarted = make(chan struct{})
	handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "block"},
	}}, droids.PromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-providers.normalStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	replayed, err := droid.CompactContext(t.Context(), options)
	if err != nil || !reflect.DeepEqual(replayed, original) {
		t.Fatalf("busy receipt replay = %+v, %v", replayed, err)
	}
	if _, err := droid.CompactContext(t.Context(), droids.CompactContextOptions{
		OperationID: "context_busy_new", Target: options.Target,
	}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("new operation while busy error = %v", err)
	}
	close(providers.blockNormal)
	waitContext, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := handle.Wait(waitContext); err != nil {
		t.Fatal(err)
	}
}

type compactCallResult struct {
	result droids.CompactContextResult
	err    error
}

func TestSDKQuiescentCompactionPreservesBoundaryAcceptedDuringSummary(t *testing.T) {
	providers := newAdaptationProviders()
	providers.blockSummary = make(chan struct{})
	providers.summaryStarted = make(chan struct{})
	droid, err := droids.Spawn(t.Context(), "conversation_adapt_boundary", droids.Config{
		Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	seedAdaptationHistory(t, droid, 4)
	options := droids.CompactContextOptions{
		OperationID: "context_boundary", Target: droids.ContextTarget{Model: "test/small", Reasoning: "off"},
	}
	result := make(chan compactCallResult, 1)
	go func() {
		value, err := droid.CompactContext(context.Background(), options)
		result <- compactCallResult{result: value, err: err}
	}()
	select {
	case <-providers.summaryStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("compaction summary did not start")
	}
	if _, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
		droids.TextInput{Text: "must wait"},
	}}, droids.PromptOptions{}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("prompt during compaction error = %v", err)
	}
	if _, err := droid.Fork(t.Context(), "conversation_adapt_racing_fork", droids.ForkOptions{}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("fork during compaction error = %v", err)
	}
	quiescent := make(chan error, 1)
	go func() {
		_, err := droid.WaitQuiescent(context.Background())
		quiescent <- err
	}()
	select {
	case err := <-quiescent:
		t.Fatalf("WaitQuiescent returned during compaction: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	joined := make(chan compactCallResult, 1)
	go func() {
		value, err := droid.CompactContext(context.Background(), options)
		joined <- compactCallResult{result: value, err: err}
	}()
	if err := droid.Inform(t.Context(), droids.BoundaryMessage{
		ID: "boundary_during_compaction", Kind: "test", Source: "test",
		Content: []droids.InputContent{droids.TextInput{Text: "preserve me"}},
	}); err != nil {
		t.Fatal(err)
	}
	close(providers.blockSummary)
	var first compactCallResult
	select {
	case first = <-result:
		if first.err != nil {
			t.Fatal(first.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("compaction did not finish")
	}
	select {
	case second := <-joined:
		if second.err != nil || !reflect.DeepEqual(second.result, first.result) {
			t.Fatalf("joined compaction = %+v, %v; want %+v", second.result, second.err, first.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("joined compaction did not finish")
	}
	select {
	case err := <-quiescent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitQuiescent did not resume after compaction")
	}
	snapshot, err := droid.Snapshot(t.Context(), droids.SnapshotOptions{RecentMessageLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Pending.Boundaries) != 1 || snapshot.Pending.Boundaries[0].Message.ID != "boundary_during_compaction" {
		t.Fatalf("pending boundaries = %+v", snapshot.Pending.Boundaries)
	}
}

func TestSDKAssessContextPropagatesReplayValidationCancellation(t *testing.T) {
	providers := newAdaptationProviders()
	providers.blockValidation = make(chan struct{})
	providers.validationStarted = make(chan struct{})
	droid, err := droids.Spawn(t.Context(), "conversation_adapt_assess_cancel", droids.Config{
		Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = droid.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := droid.AssessContext(ctx, droids.ContextTarget{Model: "test/small", Reasoning: "off"})
		result <- err
	}()
	select {
	case <-providers.validationStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("target replay validation did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("AssessContext() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("assessment did not observe cancellation")
	}
}

func TestSDKShutdownCancelsQuiescentCompaction(t *testing.T) {
	providers := newAdaptationProviders()
	providers.blockSummary = make(chan struct{})
	providers.summaryStarted = make(chan struct{})
	droid, err := droids.Spawn(t.Context(), "conversation_adapt_shutdown", droids.Config{
		Providers: providers, Model: "test/active",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedAdaptationHistory(t, droid, 4)
	compactDone := make(chan error, 1)
	go func() {
		_, err := droid.CompactContext(context.Background(), droids.CompactContextOptions{
			OperationID: "context_shutdown", Target: droids.ContextTarget{Model: "test/small", Reasoning: "off"},
		})
		compactDone <- err
	}()
	select {
	case <-providers.summaryStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("compaction summary did not start")
	}
	shutdownContext, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := droid.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-compactDone:
		if !errors.Is(err, droids.ErrClosed) {
			t.Fatalf("CompactContext() shutdown error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("compaction did not stop during shutdown")
	}
}

func seedAdaptationHistory(t *testing.T, droid *droids.Droid, turns int) {
	t.Helper()
	for index := 0; index < turns; index++ {
		handle, err := droid.Prompt(t.Context(), droids.Input{Content: []droids.InputContent{
			droids.TextInput{Text: strings.Repeat(string(rune('a'+index)), 500)},
		}}, droids.PromptOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		outcome, waitErr := handle.Wait(ctx)
		cancel()
		if waitErr != nil || outcome.Status != droids.ExecutionCompleted {
			t.Fatalf("seed turn %d = %+v, %v", index, outcome, waitErr)
		}
	}
}

func sdkShouldCompactForTest(usage droids.ContextUsage) bool {
	if usage.ContextWindow > 0 && usage.EstimatedInput+usage.ReservedOutput >= int(float64(usage.ContextWindow)*0.8) {
		return true
	}
	return usage.MaxInputTokens > 0 && usage.EstimatedInput >= int(float64(usage.MaxInputTokens)*0.8)
}

type adaptationProviders struct {
	compactions          atomic.Int32
	normalUsage          droids.Usage
	summaryUsage         droids.Usage
	summaryStopReason    droids.StopReason
	activeSummaryFails   bool
	rejectActiveForSmall bool
	smallUnavailable     bool
	divergentMeasure     bool

	mu                sync.Mutex
	blockNormal       chan struct{}
	normalStarted     chan struct{}
	blockSummary      chan struct{}
	summaryStarted    chan struct{}
	blockValidation   chan struct{}
	validationStarted chan struct{}
	summaryOnce       sync.Once
	normalOnce        sync.Once
	validationOnce    sync.Once
}

func newAdaptationProviders() *adaptationProviders { return &adaptationProviders{} }

func (p *adaptationProviders) Models() []droids.Model {
	return []droids.Model{p.activeModel(), p.smallModel()}
}

func (p *adaptationProviders) Resolve(selector string) (droids.Provider, droids.Model, error) {
	model, ok := p.Model(selector)
	if !ok {
		return nil, droids.Model{}, fmt.Errorf("unknown model %q", selector)
	}
	provider := &adaptationProvider{owner: p}
	if p.divergentMeasure {
		return &measuredAdaptationProvider{adaptationProvider: provider}, model, nil
	}
	return provider, model, nil
}

func (p *adaptationProviders) Model(selector string) (droids.Model, bool) {
	switch selector {
	case "active", "test/active":
		return p.activeModel(), true
	case "small", "test/small":
		if p.smallUnavailable {
			return droids.Model{}, false
		}
		return p.smallModel(), true
	default:
		return droids.Model{}, false
	}
}

func (*adaptationProviders) RefreshModels(context.Context) error { return nil }

func (p *adaptationProviders) Stream(ctx context.Context, model droids.Model, request droids.Request) droids.Stream {
	return p.stream(ctx, model, request)
}

func (p *adaptationProviders) activeModel() droids.Model {
	return droids.Model{
		ID: "active", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		Reasoning: true, ReasoningLevels: []string{"none", "low", "medium", "high"},
		ContextWindow: 100_000, MaxOutputTokens: 64,
	}
}

func (p *adaptationProviders) smallModel() droids.Model {
	return droids.Model{
		ID: "small", Provider: "test", API: droids.ModelAPIOpenAIResponses,
		Reasoning: true, ReasoningLevels: []string{"none", "low", "medium", "high"},
		ContextWindow: 1_000, MaxOutputTokens: 64,
	}
}

func (p *adaptationProviders) stream(ctx context.Context, model droids.Model, request droids.Request) droids.Stream {
	if strings.Contains(request.SystemPrompt, "Summarize the supplied conversation") {
		p.compactions.Add(1)
		if p.activeSummaryFails && model.ID == "active" {
			return sdkStaticStream(droids.AssistantMessage{
				Provider: "test", Model: model.ID, StopReason: droids.StopReasonError,
				ErrorKind: droids.ProviderRateLimit, ErrorMessage: "active model rate limited",
				Error: &droids.ProviderError{Kind: droids.ProviderRateLimit, Message: "active model rate limited", Retryable: true},
			})
		}
		p.mu.Lock()
		started, release := p.summaryStarted, p.blockSummary
		p.mu.Unlock()
		if started != nil {
			p.summaryOnce.Do(func() { close(started) })
		}
		if release != nil {
			select {
			case <-release:
			case <-ctx.Done():
				return sdkStaticStream(droids.AssistantMessage{
					Provider: "test", Model: model.ID, StopReason: droids.StopReasonAborted,
					ErrorMessage: ctx.Err().Error(),
				})
			}
		}
		stopReason := p.summaryStopReason
		if stopReason == "" {
			stopReason = droids.StopReasonStop
		}
		message := droids.AssistantMessage{
			Provider: "test", Model: model.ID, StopReason: stopReason,
			Content: []droids.AssistantContent{droids.TextContent{Text: "compact summary"}},
			Usage:   p.summaryUsage,
		}
		if stopReason == droids.StopReasonError {
			message.ErrorKind = droids.ProviderTransport
			message.ErrorMessage = "summary failed"
			message.Error = &droids.ProviderError{Kind: droids.ProviderTransport, Message: "summary failed", Retryable: true}
		}
		return sdkStaticStream(message)
	}
	p.mu.Lock()
	started, release := p.normalStarted, p.blockNormal
	p.mu.Unlock()
	if started != nil {
		p.normalOnce.Do(func() { close(started) })
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	return sdkStaticStream(droids.AssistantMessage{
		Provider: "test", Model: model.ID, StopReason: droids.StopReasonStop,
		Content: []droids.AssistantContent{droids.TextContent{Text: "ok"}},
		Usage:   p.normalUsage,
	})
}

type adaptationProvider struct {
	owner *adaptationProviders
}

func (*adaptationProvider) ID() string               { return "test" }
func (p *adaptationProvider) Models() []droids.Model { return p.owner.Models() }
func (p *adaptationProvider) Stream(ctx context.Context, model droids.Model, request droids.Request) (droids.AssistantStream, error) {
	return &adaptationAssistantStream{stream: p.owner.stream(ctx, model, request)}, nil
}
func (p *adaptationProvider) ValidateReplay(ctx context.Context, model droids.Model, messages []droids.Message) error {
	if model.ID == "small" {
		p.owner.mu.Lock()
		started, release := p.owner.validationStarted, p.owner.blockValidation
		p.owner.mu.Unlock()
		if started != nil {
			p.owner.validationOnce.Do(func() { close(started) })
		}
		if release != nil {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if model.ID != "small" || !p.owner.rejectActiveForSmall {
		return nil
	}
	for _, message := range messages {
		if assistant, ok := message.(droids.AssistantMessage); ok && assistant.Model == "active" {
			return errors.New("small model rejects active-model assistant metadata")
		}
	}
	return nil
}

type measuredAdaptationProvider struct {
	*adaptationProvider
}

func (*measuredAdaptationProvider) MeasureContext(_ context.Context, model droids.Model, request droids.Request) (droids.ContextUsage, error) {
	estimated := 900
	if len(request.Messages) > 0 {
		if _, summary := request.Messages[0].(droids.ContextMessage); summary {
			if model.ID == "active" {
				estimated = model.ContextWindow
			} else {
				estimated = 100
			}
		}
	}
	return droids.ContextUsage{
		Model: model, EstimatedInput: estimated, ReservedOutput: request.MaxTokens,
		ContextWindow: model.ContextWindow, MaxInputTokens: model.MaxInputTokens,
		Remaining: model.ContextWindow - estimated - request.MaxTokens, Exact: true,
	}, nil
}

type adaptationAssistantStream struct {
	stream droids.Stream
}

func (s *adaptationAssistantStream) Events() <-chan droids.StreamEvent { return s.stream.Events() }
func (s *adaptationAssistantStream) Result() (droids.AssistantMessage, error) {
	return s.stream.Result(), nil
}
func (*adaptationAssistantStream) Close() error { return nil }
