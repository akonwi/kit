package subagent_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/storage"
	"github.com/akonwi/kit/internal/subagent"
)

func TestSupervisorSchedulesSessionFairFIFOAndSurvivesAdmissionContext(t *testing.T) {
	store := openSupervisorStore(t)
	ownerA := createSupervisorOwner(t, store, "a")
	ownerB := createSupervisorOwner(t, store, "b")
	limits := subagent.Limits{GlobalRunning: 1, PerSessionRunning: 1, PerConversationQueue: 8, PerSessionQueue: 8, GlobalQueue: 16}
	base := time.Now().Add(-time.Minute)
	conversationA, a1, err := store.Admit(t.Context(), supervisorAdmission(ownerA, "a1", base), limits)
	if err != nil {
		t.Fatal(err)
	}
	_, a2, err := store.Admit(t.Context(), supervisorAdmission(ownerA, "a2", base.Add(time.Second)), limits)
	if err != nil {
		t.Fatal(err)
	}
	_, b1, err := store.Admit(t.Context(), supervisorAdmission(ownerB, "b1", base.Add(2*time.Second)), limits)
	if err != nil {
		t.Fatal(err)
	}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	first := waitStarted(t, factory.started)
	if first != a1.ID {
		t.Fatalf("first task = %s, want %s", first, a1.ID)
	}
	live, err := supervisor.LiveEvents(t.Context(), conversationA.ID, "", 0)
	if err != nil || len(live.Events) != 1 || live.Events[0].Delta != "working" || live.StreamID == "" {
		t.Fatalf("live child events = %#v, %v", live, err)
	}
	factory.release(a1.ID)
	second := waitStarted(t, factory.started)
	if second != b1.ID {
		t.Fatalf("second task = %s, want fair owner task %s", second, b1.ID)
	}
	factory.release(b1.ID)
	third := waitStarted(t, factory.started)
	if third != a2.ID {
		t.Fatalf("third task = %s, want FIFO task %s", third, a2.ID)
	}
	factory.release(a2.ID)
	for _, taskID := range []subagent.TaskID{a1.ID, b1.ID, a2.ID} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		task, err := supervisor.WaitTask(ctx, taskID)
		cancel()
		if err != nil || task.State != subagent.TaskCompleted {
			t.Fatalf("task %s = %#v, %v", taskID, task, err)
		}
	}
}

func TestSupervisorCancelRunningTaskIsGenerationSafe(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "cancel")
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })

	admissionContext, cancelAdmission := context.WithCancel(context.Background())
	conversation, task, err := supervisor.Admit(admissionContext, supervisorAdmission(owner, "cancel me", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	cancelAdmission()
	if started := waitStarted(t, factory.started); started != task.ID {
		t.Fatalf("started task = %s, want %s", started, task.ID)
	}
	canceled, err := supervisor.Cancel(t.Context(), task.ID, task.CancellationGeneration, "stop")
	if err != nil || canceled.CancellationGeneration != task.CancellationGeneration+1 {
		t.Fatalf("Cancel() = %#v, %v", canceled, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	settled, err := supervisor.WaitTask(ctx, task.ID)
	if err != nil || settled.State != subagent.TaskAborted {
		t.Fatalf("settled = %#v, %v", settled, err)
	}
	pending, err := store.PendingMailbox(t.Context(), owner, 10)
	if err != nil || len(pending) != 1 || pending[0].ConversationID != conversation.ID || pending[0].State != subagent.TaskAborted {
		t.Fatalf("mailbox = %#v, %v", pending, err)
	}
	if _, err := supervisor.Cancel(t.Context(), task.ID, task.CancellationGeneration, "stale"); err == nil {
		t.Fatal("stale cancellation succeeded")
	}
}

func TestDismissSerializesChildStoreDeletionWithTranscriptInspection(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "dismiss-inspection")
	baseFactory := newGatedChildFactory()
	factory := &inspectionGateFactory{gatedChildFactory: baseFactory, openStarted: make(chan struct{}), releaseOpen: make(chan struct{})}
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	conversation, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "complete first", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if started := waitStarted(t, factory.started); started != task.ID {
		t.Fatalf("started = %s", started)
	}
	factory.release(task.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if _, err := supervisor.WaitTask(ctx, task.ID); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	conversation, err = supervisor.Conversation(t.Context(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	factory.inspect.Store(true)
	transcriptDone := make(chan error, 1)
	go func() {
		_, err := supervisor.Transcript(context.Background(), conversation.ID)
		transcriptDone <- err
	}()
	select {
	case <-factory.openStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("transcript inspection did not reach open gate")
	}
	dismissDone := make(chan error, 1)
	go func() {
		_, err := supervisor.Dismiss(context.Background(), conversation.ID, conversation.Generation, "dismiss")
		dismissDone <- err
	}()
	select {
	case err := <-dismissDone:
		t.Fatalf("dismiss returned before inspection released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(factory.releaseOpen)
	if err := <-transcriptDone; err != nil {
		t.Fatal(err)
	}
	if err := <-dismissDone; err != nil {
		t.Fatal(err)
	}
	if factory.deletes.Load() != 1 {
		t.Fatalf("child store deletions = %d, want 1", factory.deletes.Load())
	}
}

func TestSupervisorRetainsWorkerUntilCompletionIsDurable(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "completion-retry")
	repository := &failingCompletionRepository{Repository: store}
	repository.remaining.Store(2)
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(repository, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	_, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "persist", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if started := waitStarted(t, factory.started); started != task.ID {
		t.Fatalf("started = %s", started)
	}
	factory.release(task.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	settled, err := supervisor.WaitTask(ctx, task.ID)
	if err != nil || settled.State != subagent.TaskCompleted || repository.calls.Load() < 3 {
		t.Fatalf("settled = %#v, calls=%d, error=%v", settled, repository.calls.Load(), err)
	}
}

func TestSupervisorReconcilesCancellationBetweenClaimAndWorkerPublication(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "claim-race")
	repository := &claimGateRepository{Repository: store, claimed: make(chan subagent.Claim, 1), release: make(chan struct{})}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(repository, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = supervisor.Shutdown(context.Background()) })
	_, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "race", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case claim := <-repository.claimed:
		if claim.Task.ID != task.ID {
			t.Fatalf("claim = %#v", claim)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("claim did not reach publication gate")
	}
	canceled, err := supervisor.Cancel(t.Context(), task.ID, task.CancellationGeneration, "race cancel")
	if err != nil || canceled.CancellationGeneration != task.CancellationGeneration+1 {
		t.Fatalf("Cancel() = %#v, %v", canceled, err)
	}
	close(repository.release)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	settled, err := supervisor.WaitTask(ctx, task.ID)
	if err != nil || settled.State != subagent.TaskAborted {
		t.Fatalf("settled = %#v, %v", settled, err)
	}
}

func TestSupervisorShutdownInterruptsRunningAndLeavesQueuedDurable(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "shutdown")
	limits := subagent.Limits{GlobalRunning: 1, PerSessionRunning: 1, PerConversationQueue: 8, PerSessionQueue: 8, GlobalQueue: 16}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, running, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "running", time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	_, queued, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "queued", time.Now().Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if started := waitStarted(t, factory.started); started != running.ID {
		t.Fatalf("started task = %s", started)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := supervisor.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	runningState, err := store.Task(t.Context(), running.ID)
	if err != nil || runningState.State != subagent.TaskInterrupted {
		t.Fatalf("running after shutdown = %#v, %v", runningState, err)
	}
	queuedState, err := store.Task(t.Context(), queued.ID)
	if err != nil || queuedState.State != subagent.TaskQueued {
		t.Fatalf("queued after shutdown = %#v, %v", queuedState, err)
	}
}

type inspectionGateFactory struct {
	*gatedChildFactory
	inspect     atomic.Bool
	openStarted chan struct{}
	releaseOpen chan struct{}
	deletes     atomic.Int32
}

func (f *inspectionGateFactory) Open(ctx context.Context, conversation subagent.Conversation) (subagent.ChildRuntime, error) {
	if f.inspect.Load() {
		select {
		case <-f.openStarted:
		default:
			close(f.openStarted)
		}
		select {
		case <-f.releaseOpen:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.gatedChildFactory.Open(ctx, conversation)
}

func (f *inspectionGateFactory) Delete(context.Context, subagent.Conversation) error {
	f.deletes.Add(1)
	return nil
}

type failingCompletionRepository struct {
	subagent.Repository
	remaining atomic.Int32
	calls     atomic.Int32
}

func (r *failingCompletionRepository) Complete(ctx context.Context, completion subagent.Completion) (subagent.Task, *subagent.MailboxItem, error) {
	r.calls.Add(1)
	if r.remaining.Add(-1) >= 0 {
		return subagent.Task{}, nil, context.DeadlineExceeded
	}
	return r.Repository.Complete(ctx, completion)
}

type claimGateRepository struct {
	subagent.Repository
	claimed chan subagent.Claim
	release chan struct{}
}

func (r *claimGateRepository) ClaimNext(ctx context.Context, owner string, limits subagent.Limits, at time.Time) (subagent.Claim, error) {
	claim, err := r.Repository.ClaimNext(ctx, owner, limits, at)
	if err != nil {
		return claim, err
	}
	r.claimed <- claim
	select {
	case <-r.release:
		return claim, nil
	case <-ctx.Done():
		return subagent.Claim{}, ctx.Err()
	}
}

type gatedChildFactory struct {
	started chan subagent.TaskID
	mu      sync.Mutex
	gates   map[subagent.TaskID]chan struct{}
}

func newGatedChildFactory() *gatedChildFactory {
	return &gatedChildFactory{started: make(chan subagent.TaskID, 32), gates: make(map[subagent.TaskID]chan struct{})}
}

func (f *gatedChildFactory) Open(_ context.Context, _ subagent.Conversation) (subagent.ChildRuntime, error) {
	return &gatedChildRuntime{factory: f}, nil
}
func (*gatedChildFactory) Delete(context.Context, subagent.Conversation) error { return nil }

func (f *gatedChildFactory) release(taskID subagent.TaskID) {
	f.mu.Lock()
	gate := f.gates[taskID]
	f.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}

type gatedChildRuntime struct{ factory *gatedChildFactory }

func (r *gatedChildRuntime) Run(ctx context.Context, task subagent.Task, admitted func(string) error, emit func(subagent.LiveEvent)) (subagent.ChildOutcome, error) {
	if err := admitted("turn_" + string(task.ID)); err != nil {
		return subagent.ChildOutcome{}, err
	}
	gate := make(chan struct{})
	r.factory.mu.Lock()
	r.factory.gates[task.ID] = gate
	r.factory.mu.Unlock()
	if emit != nil {
		emit(subagent.LiveEvent{Kind: "message.text.delta", TurnID: "turn_" + string(task.ID), MessageID: "message_test", Delta: "working"})
	}
	r.factory.started <- task.ID
	select {
	case <-gate:
		return subagent.ChildOutcome{TurnID: "turn_" + string(task.ID), State: subagent.TaskCompleted, Text: "done " + task.Message}, nil
	case <-ctx.Done():
		return subagent.ChildOutcome{TurnID: "turn_" + string(task.ID), State: subagent.TaskAborted}, ctx.Err()
	}
}

func (*gatedChildRuntime) Abort(context.Context) error { return nil }
func (*gatedChildRuntime) Transcript(context.Context) (subagent.Transcript, error) {
	return subagent.Transcript{}, nil
}
func (*gatedChildRuntime) Close(context.Context) error { return nil }

func waitStarted(t *testing.T, started <-chan subagent.TaskID) subagent.TaskID {
	t.Helper()
	select {
	case taskID := <-started:
		return taskID
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for child start")
		return ""
	}
}

func openSupervisorStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.Context(), filepath.Join(t.TempDir(), "kit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createSupervisorOwner(t *testing.T, store *storage.Store, suffix string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(suffix))
	id := "session_" + hex.EncodeToString(digest[:16])
	_, err := store.CreateSession(t.Context(), session.NewSession{
		ID: id, CWD: t.TempDir(), Persistent: true,
		ModelProvider: "test", ModelID: "model", ThinkingLevel: "medium",
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func supervisorAdmission(owner, message string, at time.Time) subagent.Admission {
	return subagent.Admission{
		OwnerSessionID: owner,
		Definition: subagent.Definition{
			Name: "scout", Description: "finds things", Instructions: "Inspect carefully.",
			Source: subagent.Source{Kind: subagent.SourceUser, Path: "/tmp/scout.md"},
		},
		CWD: "/tmp", Model: "test/model", ThinkingLevel: "medium", Message: message, Now: at,
	}
}
