package subagent_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/subagent"
)

// Regression for #244: child runtime Close ran under the worker lock, so
// Shutdown's cancellation pass blocked before its bounded wait.
func TestSupervisorShutdownHonorsDeadlineWhileChildRuntimeCloses(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "shutdown-close")
	factory := newLifecycleChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		factory.releaseClose()
		_ = supervisor.Shutdown(context.Background())
	})
	_, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "close slowly", time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, factory.started)
	factory.release(task.ID)
	waitSignal(t, factory.closeEntered, "child runtime close did not start")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- supervisor.Shutdown(ctx) }()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Shutdown() = %v, want deadline exceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown blocked past its deadline behind child runtime Close")
	}
}

// Regression for #244: steering held the worker lock across runtime I/O, so
// cancellation waited for the steer to finish.
func TestSupervisorCancelDoesNotWaitForInFlightSteer(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "cancel-steer")
	factory := newLifecycleChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		factory.releaseSteer()
		factory.releaseClose()
		_ = supervisor.Shutdown(context.Background())
	})
	conversation, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "steer slowly", time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, factory.started)
	steered := make(chan error, 1)
	go func() {
		_, err := supervisor.Steer(context.Background(), conversation.ID, "block")
		steered <- err
	}()
	waitSignal(t, factory.steerEntered, "steer did not reach the child runtime")

	canceled := make(chan error, 1)
	go func() {
		_, err := supervisor.Cancel(t.Context(), task.ID, task.CancellationGeneration, "stop")
		canceled <- err
	}()
	select {
	case err := <-canceled:
		if err != nil {
			t.Fatalf("Cancel() = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel blocked behind an in-flight steer")
	}
	factory.releaseSteer()
	if err := <-steered; err != nil {
		t.Fatalf("Steer() = %v", err)
	}
	factory.releaseClose()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	settled, err := supervisor.WaitTask(ctx, task.ID)
	if err != nil || settled.State != subagent.TaskAborted {
		t.Fatalf("settled = %#v, %v", settled, err)
	}
}

// The runtime is closed only after in-flight runtime calls return, even though
// those calls no longer hold the worker lock.
func TestSupervisorClosesChildRuntimeAfterInFlightSteerReturns(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "close-after-steer")
	factory := newLifecycleChildFactory()
	factory.releaseClose()
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		factory.releaseSteer()
		_ = supervisor.Shutdown(context.Background())
	})
	conversation, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "finish while steering", time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, factory.started)
	steered := make(chan error, 1)
	go func() {
		_, err := supervisor.Steer(context.Background(), conversation.ID, "block")
		steered <- err
	}()
	waitSignal(t, factory.steerEntered, "steer did not reach the child runtime")
	factory.release(task.ID)
	// Once Run has returned, new steering is refused while the in-flight one
	// still holds the runtime open.
	deadline := time.Now().Add(5 * time.Second)
	for {
		probe := make(chan error, 1)
		go func() {
			_, err := supervisor.Steer(t.Context(), conversation.ID, "probe")
			probe <- err
		}()
		var err error
		select {
		case err = <-probe:
		case <-time.After(5 * time.Second):
			t.Fatal("probe steer blocked behind the in-flight steer")
		}
		if errors.Is(err, subagent.ErrConflict) {
			break
		}
		if err != nil {
			t.Fatalf("probe Steer() = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not stop accepting steering after Run returned")
		}
		time.Sleep(time.Millisecond)
	}
	factory.releaseSteer()
	if err := <-steered; err != nil {
		t.Fatalf("Steer() = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if settled, err := supervisor.WaitTask(ctx, task.ID); err != nil || settled.State != subagent.TaskCompleted {
		t.Fatalf("settled = %#v, %v", settled, err)
	}
	if events := factory.events(); !slices.Equal(events, []string{"steer:block:begin", "steer:block:end", "close"}) {
		t.Fatalf("runtime events = %q", events)
	}
}

// Regression for #245: claim reconciliation stored a generation read before a
// concurrent Cancel committed, regressing the worker's generation.
func TestSupervisorReconciliationDoesNotRegressCancellationGeneration(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "generation-regress")
	repository := &reconcileGateRepository{Repository: store, read: make(chan subagent.Task, 1), release: make(chan struct{}), completions: make(chan subagent.Completion, 8)}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(repository, factory, subagent.DefaultLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		repository.open()
		_ = supervisor.Shutdown(context.Background())
	})
	_, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "race", time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	var reconciled subagent.Task
	select {
	case reconciled = <-repository.read:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not reconcile the claimed task")
	}
	canceled, err := supervisor.Cancel(t.Context(), task.ID, reconciled.CancellationGeneration, "race cancel")
	if err != nil || canceled.CancellationGeneration != reconciled.CancellationGeneration+1 {
		t.Fatalf("Cancel() = %#v, %v", canceled, err)
	}
	repository.open()
	select {
	case completion := <-repository.completions:
		if completion.CancellationGeneration != canceled.CancellationGeneration {
			t.Fatalf("first completion generation = %d, want %d", completion.CancellationGeneration, canceled.CancellationGeneration)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not complete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if settled, err := supervisor.WaitTask(ctx, task.ID); err != nil || settled.State != subagent.TaskAborted {
		t.Fatalf("settled = %#v, %v", settled, err)
	}
}

// Regression for #246: the finished worker kept the conversation lock through
// parent notification, so a model change right after settling conflicted.
func TestSupervisorModelChangeSucceedsWhileParentNotificationRuns(t *testing.T) {
	store := openSupervisorStore(t)
	owner := createSupervisorOwner(t, store, "settled-model-change")
	sink := &blockingMailboxSink{entered: make(chan struct{}), release: make(chan struct{})}
	factory := newGatedChildFactory()
	supervisor, err := subagent.NewSupervisor(store, factory, subagent.DefaultLimits(), sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sink.open()
		_ = supervisor.Shutdown(context.Background())
	})
	conversation, task, err := supervisor.Admit(t.Context(), supervisorAdmission(owner, "initial", time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, factory.started)
	factory.release(task.ID)
	waitSignal(t, sink.entered, "parent notification did not start")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	conversation, err = supervisor.WaitConversation(ctx, conversation.ID)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	model := "test/other"
	configured, err := supervisor.Configure(t.Context(), conversation.ID, conversation.Generation, subagent.ConfigurationPatch{Model: &model}, nil)
	if err != nil {
		t.Fatalf("Configure() while parent notification runs = %v", err)
	}
	if configured.Conversation.Model != model || !configured.Compacted {
		t.Fatalf("configured = %#v", configured)
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
	}
}

// lifecycleChildFactory opens gated runtimes whose Steer("block") and Close
// wait for the test, recording the order of runtime calls.
type lifecycleChildFactory struct {
	*gatedChildFactory
	steerEntered chan struct{}
	steerGate    chan struct{}
	closeEntered chan struct{}
	closeGate    chan struct{}
	steerOnce    sync.Once
	closeOnce    sync.Once
	steerSeen    atomic.Bool
	closeSeen    atomic.Bool
	eventsMu     sync.Mutex
	recorded     []string
}

func newLifecycleChildFactory() *lifecycleChildFactory {
	return &lifecycleChildFactory{
		gatedChildFactory: newGatedChildFactory(),
		steerEntered:      make(chan struct{}), steerGate: make(chan struct{}),
		closeEntered: make(chan struct{}), closeGate: make(chan struct{}),
	}
}

func (f *lifecycleChildFactory) Open(_ context.Context, _ subagent.Conversation) (subagent.ChildRuntime, error) {
	return &lifecycleChildRuntime{gatedChildRuntime: &gatedChildRuntime{factory: f.gatedChildFactory}, factory: f}, nil
}

func (f *lifecycleChildFactory) releaseSteer() { f.steerOnce.Do(func() { close(f.steerGate) }) }
func (f *lifecycleChildFactory) releaseClose() { f.closeOnce.Do(func() { close(f.closeGate) }) }

func (f *lifecycleChildFactory) record(event string) {
	f.eventsMu.Lock()
	f.recorded = append(f.recorded, event)
	f.eventsMu.Unlock()
}

func (f *lifecycleChildFactory) events() []string {
	f.eventsMu.Lock()
	defer f.eventsMu.Unlock()
	return slices.Clone(f.recorded)
}

type lifecycleChildRuntime struct {
	*gatedChildRuntime
	factory *lifecycleChildFactory
}

func (r *lifecycleChildRuntime) Steer(_ context.Context, message string) error {
	if message != "block" {
		return nil
	}
	r.factory.record("steer:block:begin")
	if r.factory.steerSeen.CompareAndSwap(false, true) {
		close(r.factory.steerEntered)
	}
	<-r.factory.steerGate
	r.factory.record("steer:block:end")
	return nil
}

// Close ignores its context, like a droid shutdown waiting on in-flight work.
func (r *lifecycleChildRuntime) Close(context.Context) error {
	r.factory.record("close")
	if r.factory.closeSeen.CompareAndSwap(false, true) {
		close(r.factory.closeEntered)
	}
	<-r.factory.closeGate
	return nil
}

// reconcileGateRepository suspends the scheduler's first task read after a
// claim, so a test can commit a cancellation between that read and its use.
type reconcileGateRepository struct {
	subagent.Repository
	armed       atomic.Bool
	read        chan subagent.Task
	release     chan struct{}
	releaseOnce sync.Once
	completions chan subagent.Completion
}

func (r *reconcileGateRepository) open() { r.releaseOnce.Do(func() { close(r.release) }) }

func (r *reconcileGateRepository) ClaimNext(ctx context.Context, owner string, limits subagent.Limits, at time.Time) (subagent.Claim, error) {
	claim, err := r.Repository.ClaimNext(ctx, owner, limits, at)
	if err == nil {
		r.armed.Store(true)
	}
	return claim, err
}

func (r *reconcileGateRepository) Task(ctx context.Context, id subagent.TaskID) (subagent.Task, error) {
	task, err := r.Repository.Task(ctx, id)
	if err == nil && r.armed.CompareAndSwap(true, false) {
		r.read <- task
		<-r.release
	}
	return task, err
}

func (r *reconcileGateRepository) Complete(ctx context.Context, completion subagent.Completion) (subagent.Task, *subagent.MailboxItem, error) {
	select {
	case r.completions <- completion:
	default:
	}
	return r.Repository.Complete(ctx, completion)
}

// blockingMailboxSink holds parent notification open until released.
type blockingMailboxSink struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
}

func (s *blockingMailboxSink) open() { s.releaseOnce.Do(func() { close(s.release) }) }

func (*blockingMailboxSink) SubagentChanged(context.Context, string, subagent.ConversationID, subagent.TaskID) {
}

func (s *blockingMailboxSink) MailboxAdded(context.Context, subagent.MailboxItem) {
	s.enteredOnce.Do(func() { close(s.entered) })
	<-s.release
}
