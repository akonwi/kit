package droids_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/droids/sqlitestore"
)

func TestSDKForkPreservesAncestryAndDivergesAcrossStores(t *testing.T) {
	for _, test := range []struct {
		name        string
		sourceStore func(*testing.T) droids.Store
		childStore  func(*testing.T) droids.Store
	}{
		{name: "memory to memory", sourceStore: memoryForkStore, childStore: memoryForkStore},
		{name: "memory to sqlite", sourceStore: memoryForkStore, childStore: sqliteForkStore},
		{name: "sqlite to memory", sourceStore: sqliteForkStore, childStore: memoryForkStore},
		{name: "sqlite to sqlite", sourceStore: sqliteForkStore, childStore: sqliteForkStore},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceStore := test.sourceStore(t)
			childStore := test.childStore(t)
			providers := newReadProviders()
			var toolRuns atomic.Int32
			source, err := droids.Spawn(t.Context(), "conversation_fork_source", droids.Config{
				Store: sourceStore, Model: resolvedTestModel(providers, "test/read"),
				Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = source.Close() })
			waitForCompleted(t, source, "establish ancestry")
			if got := toolRuns.Load(); got != 1 {
				t.Fatalf("ancestral tool runs = %d, want 1", got)
			}
			boundary := droids.BoundaryMessage{
				ID: "boundary_pending", ReceiptIDs: []string{"boundary_receipt"},
				Kind: "test", Source: "fork-test",
				Content: []droids.InputContent{droids.TextInput{Text: "pending inherited context"}},
			}
			if err := source.Inform(t.Context(), boundary); err != nil {
				t.Fatal(err)
			}
			sourceSnapshot, err := source.Snapshot(t.Context(), droids.SnapshotOptions{})
			if err != nil {
				t.Fatal(err)
			}
			sourceHistory, err := source.History(t.Context(), droids.HistoryQuery{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			sourceRecords := allForkRecords(t, sourceStore)

			forked, err := source.Fork(t.Context(), "conversation_fork_child", droids.ForkOptions{Store: childStore})
			if err != nil {
				t.Fatalf("Fork: %v", err)
			}
			if forked.Droid == nil {
				t.Fatal("Fork returned a nil child")
			}
			child := forked.Droid
			t.Cleanup(func() { _ = child.Close() })
			wantPoint := droids.ForkPoint{
				ConversationID: "conversation_fork_source",
				Revision:       sourceSnapshot.Conversation.Revision,
				LastEvent:      sourceSnapshot.LastEvent,
			}
			if forked.Point != wantPoint {
				t.Fatalf("fork point = %+v, want %+v", forked.Point, wantPoint)
			}
			childSnapshot, err := child.Snapshot(t.Context(), droids.SnapshotOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if childSnapshot.Conversation.ID != "conversation_fork_child" || childSnapshot.Conversation.Revision != 1 || childSnapshot.LastEvent != 2 {
				t.Fatalf("child snapshot = %+v", childSnapshot)
			}
			if childSnapshot.Conversation.ForkedFrom == nil || *childSnapshot.Conversation.ForkedFrom != wantPoint {
				t.Fatalf("child lineage = %+v, want %+v", childSnapshot.Conversation.ForkedFrom, wantPoint)
			}
			if childSnapshot.Active != nil || childSnapshot.Pending.Boundary != 1 || childSnapshot.Context.Messages != sourceSnapshot.Context.Messages {
				t.Fatalf("child ready state = %+v", childSnapshot)
			}
			if received, err := child.BoundaryReceived(t.Context(), "boundary_receipt"); err != nil || !received {
				t.Fatalf("inherited boundary receipt = %v, %v", received, err)
			}

			childHistory, err := child.History(t.Context(), droids.HistoryQuery{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			assertForkedMessages(t, sourceHistory, childHistory)
			childRecords := allForkRecords(t, childStore)
			if len(childRecords) != len(sourceRecords) {
				t.Fatalf("child records = %d, want %d", len(childRecords), len(sourceRecords))
			}
			for index := range sourceRecords {
				if childRecords[index].Kind != sourceRecords[index].Kind || childRecords[index].ID != sourceRecords[index].ID {
					t.Fatalf("child record %d = %s/%s, want %s/%s", index,
						childRecords[index].Kind, childRecords[index].ID,
						sourceRecords[index].Kind, sourceRecords[index].ID)
				}
			}
			events, err := childStore.Events(t.Context(), droids.EventQuery{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(events.Events) != 2 || events.Events[0].Kind != "conversation.created" || events.Events[1].Kind != "conversation.forked" {
				t.Fatalf("child initial events = %+v", events.Events)
			}
			afterFork, err := source.Snapshot(t.Context(), droids.SnapshotOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if afterFork.Conversation.Revision != sourceSnapshot.Conversation.Revision || afterFork.LastEvent != sourceSnapshot.LastEvent {
				t.Fatalf("source changed during fork: before=%+v after=%+v", sourceSnapshot, afterFork)
			}

			childHandle := promptDroid(t, child, "continue child")
			sourceHandle := promptDroid(t, source, "continue source")
			if childHandle.TurnID() == sourceHandle.TurnID() {
				t.Fatalf("divergent turns reused id %q", childHandle.TurnID())
			}
			waitHandleCompleted(t, childHandle)
			waitHandleCompleted(t, sourceHandle)
			if got := toolRuns.Load(); got != 1 {
				t.Fatalf("tool runs after divergence = %d, want inherited call not repeated", got)
			}
			childAfter, err := child.History(t.Context(), droids.HistoryQuery{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			sourceAfter, err := source.History(t.Context(), droids.HistoryQuery{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if childAfter.Messages[len(childAfter.Messages)-1].ID == sourceAfter.Messages[len(sourceAfter.Messages)-1].ID {
				t.Fatalf("divergent messages reused id %q", childAfter.Messages[len(childAfter.Messages)-1].ID)
			}
		})
	}
}

func TestSDKForkAcceptsFailedAndAbortedSources(t *testing.T) {
	t.Run("failed", func(t *testing.T) {
		providers := &retryProviders{}
		retry := droids.RetryPolicy{Enabled: false}
		source, err := droids.Spawn(t.Context(), "conversation_failed_source", droids.Config{
			Model: resolvedTestModel(providers, "test/retry"), Retry: &retry,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = source.Close() })
		handle := promptDroid(t, source, "fail")
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		outcome, err := handle.Wait(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Status != droids.ExecutionFailed {
			t.Fatalf("outcome = %+v, want failed", outcome)
		}
		forked, err := source.Fork(t.Context(), "conversation_failed_child", droids.ForkOptions{})
		if err != nil {
			t.Fatalf("Fork failed source: %v", err)
		}
		t.Cleanup(func() { _ = forked.Droid.Close() })
	})

	t.Run("aborted", func(t *testing.T) {
		providers := newSteeringProviders()
		source, err := droids.Spawn(t.Context(), "conversation_aborted_source", droids.Config{
			Model: resolvedTestModel(providers, "test/steer"),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = source.Close() })
		handle := promptDroid(t, source, "abort")
		select {
		case <-providers.started:
		case <-time.After(5 * time.Second):
			t.Fatal("provider did not start")
		}
		if err := source.Abort(t.Context()); err != nil {
			t.Fatal(err)
		}
		close(providers.release)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		outcome, err := handle.Wait(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Status != droids.ExecutionAborted {
			t.Fatalf("outcome = %+v, want aborted", outcome)
		}
		forked, err := source.Fork(t.Context(), "conversation_aborted_child", droids.ForkOptions{})
		if err != nil {
			t.Fatalf("Fork aborted source: %v", err)
		}
		t.Cleanup(func() { _ = forked.Droid.Close() })
		waitForCompleted(t, forked.Droid, "continue after inherited abort")
	})
}

func TestSDKForkRejectsOccupiedSource(t *testing.T) {
	providers := newSteeringProviders()
	source, err := droids.Spawn(t.Context(), "conversation_fork_busy", droids.Config{
		Model: resolvedTestModel(providers, "test/steer"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	handle := promptDroid(t, source, "stay busy")
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	destination := droids.NewMemoryStore()
	if _, err := source.Fork(t.Context(), "conversation_busy_child", droids.ForkOptions{Store: destination}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("Fork error = %v, want ErrBusy", err)
	}
	if _, err := destination.State(t.Context()); !errors.Is(err, droids.ErrStoreUninitialized) {
		t.Fatalf("destination State error = %v, want ErrStoreUninitialized", err)
	}
	close(providers.release)
	waitHandleCompleted(t, handle)
}

func TestSDKForkRejectsPausedSource(t *testing.T) {
	providers := newReadProviders()
	var toolRuns atomic.Int32
	budget := droids.ExecutionPolicy{Budget: droids.ExecutionBudget{MaxModelCycles: 1}}
	source, err := droids.Spawn(t.Context(), "conversation_fork_paused", droids.Config{
		Model: resolvedTestModel(providers, "test/read"), Execution: &budget,
		Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	handle := promptDroid(t, source, "pause after tools")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != droids.ExecutionPaused {
		t.Fatalf("outcome = %+v, want paused", outcome)
	}
	if _, err := source.Fork(t.Context(), "conversation_paused_child", droids.ForkOptions{}); !errors.Is(err, droids.ErrBusy) {
		t.Fatalf("Fork error = %v, want ErrBusy", err)
	}
	if err := source.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSDKForkSerializesWithPromptAdmission(t *testing.T) {
	providers := newSteeringProviders()
	source, err := droids.Spawn(t.Context(), "conversation_prompt_race_source", droids.Config{
		Model: resolvedTestModel(providers, "test/steer"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	destination := droids.NewMemoryStore()
	start := make(chan struct{})
	forkResult := make(chan struct {
		result droids.ForkResult
		err    error
	}, 1)
	promptResult := make(chan struct {
		handle droids.ExecutionHandle
		err    error
	}, 1)
	go func() {
		<-start
		result, err := source.Fork(t.Context(), "conversation_prompt_race_child", droids.ForkOptions{Store: destination})
		forkResult <- struct {
			result droids.ForkResult
			err    error
		}{result, err}
	}()
	go func() {
		<-start
		handle, err := source.Prompt(t.Context(), droids.Input{
			Content: []droids.InputContent{droids.TextInput{Text: "race"}},
		}, droids.PromptOptions{})
		promptResult <- struct {
			handle droids.ExecutionHandle
			err    error
		}{handle, err}
	}()
	close(start)
	prompted := <-promptResult
	if prompted.err != nil {
		t.Fatalf("racing Prompt: %v", prompted.err)
	}
	forked := <-forkResult
	switch {
	case forked.err == nil:
		if forked.result.Droid == nil || forked.result.Point.Revision != 1 {
			t.Fatalf("fork-before-prompt result = %+v", forked.result)
		}
		t.Cleanup(func() { _ = forked.result.Droid.Close() })
	case errors.Is(forked.err, droids.ErrBusy):
		if _, err := destination.State(t.Context()); !errors.Is(err, droids.ErrStoreUninitialized) {
			t.Fatalf("busy fork initialized destination: %v", err)
		}
	default:
		t.Fatalf("racing Fork error = %v", forked.err)
	}
	select {
	case <-providers.started:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}
	close(providers.release)
	waitHandleCompleted(t, prompted.handle)
}

func TestSDKForkSerializesWithBoundaryAdmission(t *testing.T) {
	providers := newReadProviders()
	source, err := droids.Spawn(t.Context(), "conversation_boundary_race_source", droids.Config{
		Model: resolvedTestModel(providers, "test/read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	destination := droids.NewMemoryStore()
	boundary := droids.BoundaryMessage{
		ID: "boundary_race", Kind: "test", Source: "race",
		Content: []droids.InputContent{droids.TextInput{Text: "boundary"}},
	}
	start := make(chan struct{})
	forkResult := make(chan struct {
		result droids.ForkResult
		err    error
	}, 1)
	informResult := make(chan error, 1)
	go func() {
		<-start
		result, err := source.Fork(t.Context(), "conversation_boundary_race_child", droids.ForkOptions{Store: destination})
		forkResult <- struct {
			result droids.ForkResult
			err    error
		}{result, err}
	}()
	go func() {
		<-start
		informResult <- source.Inform(t.Context(), boundary)
	}()
	close(start)
	forked := <-forkResult
	if forked.err != nil {
		t.Fatal(forked.err)
	}
	t.Cleanup(func() { _ = forked.result.Droid.Close() })
	if err := <-informResult; err != nil {
		t.Fatal(err)
	}
	snapshot, err := forked.result.Droid.Snapshot(t.Context(), droids.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	received, err := forked.result.Droid.BoundaryReceived(t.Context(), boundary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if (snapshot.Pending.Boundary == 1) != received {
		t.Fatalf("partial boundary fork: pending=%d received=%v", snapshot.Pending.Boundary, received)
	}
	if snapshot.Pending.Boundary != 0 && snapshot.Pending.Boundary != 1 {
		t.Fatalf("pending boundaries = %d", snapshot.Pending.Boundary)
	}
}

func TestSDKConcurrentForksAttachOneDestinationDroid(t *testing.T) {
	providers := newReadProviders()
	source, err := droids.Spawn(t.Context(), "conversation_concurrent_source", droids.Config{
		Model: resolvedTestModel(providers, "test/read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	waitForCompleted(t, source, "seed")
	destination := droids.NewMemoryStore()
	start := make(chan struct{})
	results := make(chan struct {
		result droids.ForkResult
		err    error
	}, 2)
	for range 2 {
		go func() {
			<-start
			result, err := source.Fork(t.Context(), "conversation_concurrent_child", droids.ForkOptions{Store: destination})
			results <- struct {
				result droids.ForkResult
				err    error
			}{result, err}
		}()
	}
	close(start)
	var attached, existing int
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			attached++
			if result.result.Droid == nil {
				t.Fatal("successful fork returned nil droid")
			}
			t.Cleanup(func() { _ = result.result.Droid.Close() })
		case errors.Is(result.err, droids.ErrForkAlreadyInitialized):
			existing++
		default:
			t.Fatalf("concurrent Fork error = %v", result.err)
		}
	}
	if attached != 1 || existing != 1 {
		t.Fatalf("attached=%d existing=%d, want one each", attached, existing)
	}
}

func TestSDKForkReopensAndContinues(t *testing.T) {
	providers := newReadProviders()
	var toolRuns atomic.Int32
	source, err := droids.Spawn(t.Context(), "conversation_reopen_source", droids.Config{
		Model: resolvedTestModel(providers, "test/read"), Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	waitForCompleted(t, source, "seed")
	destination := droids.NewMemoryStore()
	forked, err := source.Fork(t.Context(), "conversation_reopen_child", droids.ForkOptions{Store: destination})
	if err != nil {
		t.Fatal(err)
	}
	if err := forked.Droid.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := droids.Spawn(t.Context(), "conversation_reopen_child", droids.Config{
		Store: destination, Model: resolvedTestModel(providers, "test/read"), Tools: []droids.AnyTool{readOnlyTool(&toolRuns)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	snapshot, err := reopened.Snapshot(t.Context(), droids.SnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Conversation.ForkedFrom == nil || *snapshot.Conversation.ForkedFrom != forked.Point {
		t.Fatalf("reopened lineage = %+v, want %+v", snapshot.Conversation.ForkedFrom, forked.Point)
	}
	waitForCompleted(t, reopened, "continue after reopen")
	if got := toolRuns.Load(); got != 1 {
		t.Fatalf("tool runs after reopen = %d, want ancestral tool not repeated", got)
	}
}

func TestSDKForkDetectsExistingDestinationWithoutAttaching(t *testing.T) {
	providers := newReadProviders()
	source, err := droids.Spawn(t.Context(), "conversation_existing_source", droids.Config{
		Model: resolvedTestModel(providers, "test/read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	waitForCompleted(t, source, "seed")
	destination := droids.NewMemoryStore()
	first, err := source.Fork(t.Context(), "conversation_existing_child", droids.ForkOptions{Store: destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Droid.Close() })
	second, err := source.Fork(t.Context(), "conversation_existing_child", droids.ForkOptions{Store: destination})
	if !errors.Is(err, droids.ErrForkAlreadyInitialized) {
		t.Fatalf("second Fork error = %v, want ErrForkAlreadyInitialized", err)
	}
	if second.Droid != nil || second.Point != first.Point {
		t.Fatalf("second Fork result = %+v, want point without attached droid", second)
	}
	var typed *droids.ForkAlreadyInitializedError
	if !errors.As(err, &typed) || typed.Point != first.Point {
		t.Fatalf("typed existing fork error = %+v", typed)
	}
}

func TestSDKForkReconcilesCanceledDestinationInitialization(t *testing.T) {
	providers := newReadProviders()
	source, err := droids.Spawn(t.Context(), "conversation_canceled_source", droids.Config{
		Model: resolvedTestModel(providers, "test/read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	waitForCompleted(t, source, "seed")
	ctx, cancel := context.WithCancel(t.Context())
	destination := &cancelingOpenStore{Store: droids.NewMemoryStore(), cancel: cancel}
	forked, err := source.Fork(ctx, "conversation_canceled_child", droids.ForkOptions{Store: destination})
	if err != nil {
		t.Fatalf("Fork after canceled committed Open: %v", err)
	}
	if forked.Droid == nil {
		t.Fatal("canceled Fork returned nil droid")
	}
	t.Cleanup(func() { _ = forked.Droid.Close() })
}

func TestSDKForkReconcilesAmbiguousDestinationInitialization(t *testing.T) {
	providers := newReadProviders()
	source, err := droids.Spawn(t.Context(), "conversation_ambiguous_source", droids.Config{
		Model: resolvedTestModel(providers, "test/read"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	waitForCompleted(t, source, "seed")
	destination := &ambiguousOpenStore{Store: droids.NewMemoryStore()}
	destination.failNext.Store(true)
	forked, err := source.Fork(t.Context(), "conversation_ambiguous_child", droids.ForkOptions{Store: destination})
	if err != nil {
		t.Fatalf("Fork after ambiguous Open: %v", err)
	}
	if forked.Droid == nil {
		t.Fatal("ambiguous Fork returned nil droid")
	}
	t.Cleanup(func() { _ = forked.Droid.Close() })
}

type cancelingOpenStore struct {
	droids.Store
	cancel context.CancelFunc
	done   atomic.Bool
}

func (s *cancelingOpenStore) Open(ctx context.Context, request droids.OpenConversation) (droids.OpenConversationResult, error) {
	result, err := s.Store.Open(ctx, request)
	if err == nil && result.Created && s.done.CompareAndSwap(false, true) {
		s.cancel()
		return droids.OpenConversationResult{}, context.Canceled
	}
	return result, err
}

type ambiguousOpenStore struct {
	droids.Store
	failNext atomic.Bool
}

func (s *ambiguousOpenStore) Open(ctx context.Context, request droids.OpenConversation) (droids.OpenConversationResult, error) {
	result, err := s.Store.Open(ctx, request)
	if err == nil && result.Created && s.failNext.CompareAndSwap(true, false) {
		return droids.OpenConversationResult{}, errors.New("simulated response loss after fork initialization")
	}
	return result, err
}

func memoryForkStore(*testing.T) droids.Store {
	return droids.NewMemoryStore()
}

func sqliteForkStore(t *testing.T) droids.Store {
	t.Helper()
	store, err := sqlitestore.Open(t.Context(), sqlitestore.Options{Path: filepath.Join(t.TempDir(), "fork.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func waitForCompleted(t *testing.T, droid *droids.Droid, prompt string) {
	t.Helper()
	waitHandleCompleted(t, promptDroid(t, droid, prompt))
}

func promptDroid(t *testing.T, droid *droids.Droid, prompt string) droids.ExecutionHandle {
	t.Helper()
	handle, err := droid.Prompt(t.Context(), droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: prompt}},
	}, droids.PromptOptions{})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	return handle
}

func waitHandleCompleted(t *testing.T, handle droids.ExecutionHandle) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	outcome, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if outcome.Status != droids.ExecutionCompleted {
		t.Fatalf("outcome = %+v, want completed", outcome)
	}
}

func allForkRecords(t *testing.T, store droids.Store) []droids.EncodedRecord {
	t.Helper()
	var records []droids.EncodedRecord
	var after uint64
	for {
		page, err := store.Records(t.Context(), droids.RecordQuery{After: after, Limit: 1000})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, page.Records...)
		after = page.Next
		if !page.HasMore {
			return records
		}
	}
}

func assertForkedMessages(t *testing.T, source, child droids.MessagePage) {
	t.Helper()
	if len(child.Messages) != len(source.Messages) {
		t.Fatalf("child messages = %d, want %d", len(child.Messages), len(source.Messages))
	}
	for index := range source.Messages {
		if child.Messages[index].ID != source.Messages[index].ID || child.Messages[index].TurnID != source.Messages[index].TurnID {
			t.Fatalf("child message %d identity = %+v, want ancestry %+v", index, child.Messages[index], source.Messages[index])
		}
		if child.Messages[index].ConversationID != "conversation_fork_child" {
			t.Fatalf("child message %d conversation = %q", index, child.Messages[index].ConversationID)
		}
		if source.Messages[index].ConversationID != "conversation_fork_source" {
			t.Fatalf("source message %d conversation = %q", index, source.Messages[index].ConversationID)
		}
	}
}
