package subagent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akonwi/kit/internal/identifier"
)

var (
	errTaskCanceled     = errors.New("subagent task canceled")
	errConversationGone = errors.New("subagent conversation dismissed")
	errSupervisorStop   = errors.New("subagent supervisor shutting down")
)

// Supervisor owns daemon-wide fair scheduling and independently cancelable workers.
type Supervisor struct {
	repository Repository
	factory    ChildRuntimeFactory
	limits     Limits
	sink       EventSink

	ctx    context.Context
	cancel context.CancelCauseFunc
	wake   chan struct{}
	done   chan struct{}

	mu                sync.Mutex
	started           bool
	closed            bool
	cursor            string
	running           map[TaskID]*worker
	live              map[ConversationID]*liveEventJournal
	conversationLocks map[ConversationID]*sync.Mutex
	liveClock         uint64
	changed           chan struct{}
	startErr          error
	workerWG          sync.WaitGroup
}

type liveEventJournal struct {
	streamID string
	next     int64
	events   []LiveEvent
	bytes    int
	updated  uint64
}

type worker struct {
	claim      Claim
	ctx        context.Context
	cancel     context.CancelCauseFunc
	generation atomic.Uint64

	mu             sync.Mutex
	runtime        ChildRuntime
	conversationMu *sync.Mutex
}

// NewSupervisor constructs one daemon-wide supervisor. Start performs recovery
// synchronously before queued work can be claimed.
func NewSupervisor(repository Repository, factory ChildRuntimeFactory, limits Limits, sink EventSink) (*Supervisor, error) {
	if repository == nil {
		return nil, errors.New("subagent repository is required")
	}
	if factory == nil {
		return nil, errors.New("subagent child runtime factory is required")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	return &Supervisor{
		repository: repository, factory: factory, limits: limits, sink: sink,
		ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}),
		running: make(map[TaskID]*worker), live: make(map[ConversationID]*liveEventJournal),
		conversationLocks: make(map[ConversationID]*sync.Mutex), changed: make(chan struct{}),
	}, nil
}

// SetEventSink installs the lifecycle sink before Start.
func (s *Supervisor) SetEventSink(sink EventSink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return ErrConflict
	}
	s.sink = sink
	return nil
}

// Start commits restart recovery, then begins scheduling preserved queued work.
func (s *Supervisor) Start(ctx context.Context) (Recovery, error) {
	s.mu.Lock()
	if s.started {
		err := s.startErr
		s.mu.Unlock()
		return Recovery{}, err
	}
	if s.closed {
		s.mu.Unlock()
		return Recovery{}, ErrClosed
	}
	s.started = true
	s.mu.Unlock()

	recovery, err := s.repository.RecoverRunning(ctx, time.Now().UTC())
	if err == nil {
		var tombstones []Conversation
		tombstones, err = s.repository.AllDismissedConversations(ctx)
		if err == nil {
			for _, conversation := range tombstones {
				if deleteErr := s.factory.Delete(ctx, conversation); deleteErr != nil {
					err = fmt.Errorf("clean dismissed child %s: %w", conversation.ID, deleteErr)
					break
				}
			}
		}
	}
	s.mu.Lock()
	s.startErr = err
	if err != nil {
		s.closed = true
		s.cancel(err)
		close(s.done)
		s.signalChangedLocked()
		s.mu.Unlock()
		return Recovery{}, err
	}
	for _, task := range recovery.InterruptedTasks {
		s.emitChanged(task.OwnerSessionID, task.ConversationID, task.ID)
	}
	s.mu.Unlock()
	go s.scheduleLoop()
	s.Wake()
	return recovery, nil
}

// Admit durably queues one task and wakes the scheduler. Request cancellation
// after this returns does not cancel the admitted work.
func (s *Supervisor) Admit(ctx context.Context, admission Admission) (Conversation, Task, error) {
	if err := s.available(); err != nil {
		return Conversation{}, Task{}, err
	}
	conversation, task, err := s.repository.Admit(ctx, admission, s.limits)
	if err != nil {
		return Conversation{}, Task{}, err
	}
	s.emitChanged(task.OwnerSessionID, task.ConversationID, task.ID)
	s.signalChanged()
	s.Wake()
	return conversation, task, nil
}

// Wake hints that authoritative queue state may now be schedulable.
func (s *Supervisor) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Supervisor) scheduleLoop() {
	defer close(s.done)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			s.schedule()
		}
	}
}

func (s *Supervisor) schedule() {
	for {
		s.mu.Lock()
		if s.closed || len(s.running) >= s.limits.GlobalRunning {
			s.mu.Unlock()
			return
		}
		cursor := s.cursor
		s.mu.Unlock()

		owners, err := s.repository.QueuedSessions(s.ctx)
		if err != nil || len(owners) == 0 {
			return
		}
		owners = rotateOwners(owners, cursor)
		claimed := false
		for _, owner := range owners {
			s.mu.Lock()
			if s.closed || len(s.running) >= s.limits.GlobalRunning {
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			claim, err := s.repository.ClaimNext(s.ctx, owner, s.limits, time.Now().UTC())
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return
			}
			workerContext, cancel := context.WithCancelCause(s.ctx)
			owned := &worker{claim: claim, ctx: workerContext, cancel: cancel}
			owned.generation.Store(claim.Task.CancellationGeneration)
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				cancel(errSupervisorStop)
				_, _, _ = s.repository.Complete(context.Background(), Completion{
					TaskID: claim.Task.ID, ConversationID: claim.Task.ConversationID,
					CancellationGeneration: claim.Task.CancellationGeneration,
					State:                  TaskInterrupted, Error: errSupervisorStop.Error(), FinishedAt: time.Now().UTC(),
				})
				return
			}
			conversationMu := s.conversationLocks[claim.Task.ConversationID]
			if conversationMu == nil {
				conversationMu = &sync.Mutex{}
				s.conversationLocks[claim.Task.ConversationID] = conversationMu
			}
			owned.conversationMu = conversationMu
			s.running[claim.Task.ID] = owned
			if streamID, streamErr := identifier.New("substream_"); streamErr == nil {
				s.liveClock++
				s.live[claim.Task.ConversationID] = &liveEventJournal{streamID: streamID, next: 1, updated: s.liveClock}
			}
			s.cursor = owner
			s.workerWG.Add(1)
			s.signalChangedLocked()
			s.mu.Unlock()
			s.emitChanged(owner, claim.Task.ConversationID, claim.Task.ID)
			// Cancellation or dismissal can commit after the claim but before the
			// worker is published. Reconcile authoritative state now that later
			// operations can find the worker in the running map.
			if current, stateErr := s.repository.Task(context.Background(), claim.Task.ID); stateErr == nil {
				owned.generation.Store(current.CancellationGeneration)
				if TerminalTask(current.State) {
					owned.cancel(errConversationGone)
				} else if current.CancellationGeneration != claim.Task.CancellationGeneration {
					owned.cancel(errTaskCanceled)
				}
			}
			go s.execute(owned)
			claimed = true
			break
		}
		if !claimed {
			return
		}
	}
}

func rotateOwners(owners []string, cursor string) []string {
	if len(owners) < 2 || cursor == "" {
		return owners
	}
	for index, owner := range owners {
		if owner == cursor {
			start := (index + 1) % len(owners)
			rotated := append([]string(nil), owners[start:]...)
			return append(rotated, owners[:start]...)
		}
	}
	return owners
}

func (s *Supervisor) execute(owned *worker) {
	defer s.workerWG.Done()
	owned.conversationMu.Lock()
	defer owned.conversationMu.Unlock()
	claim := owned.claim
	if cause := context.Cause(owned.ctx); cause != nil {
		s.finishWorker(owned, ChildOutcome{State: stateForWorkerError(cause), Error: cause.Error()}, cause)
		return
	}
	runtime, openErr := s.factory.Open(owned.ctx, claim.Conversation)
	if openErr != nil {
		s.finishWorker(owned, ChildOutcome{State: stateForWorkerError(context.Cause(owned.ctx)), Error: openErr.Error()}, openErr)
		return
	}
	if claim.Conversation.DroidInitializedAt == nil {
		if err := s.repository.MarkConversationInitialized(context.Background(), claim.Conversation.ID, time.Now().UTC()); err != nil {
			_ = runtime.Close(context.Background())
			s.finishWorker(owned, ChildOutcome{State: TaskFailed, Error: err.Error()}, err)
			return
		}
		now := time.Now().UTC()
		claim.Conversation.DroidInitializedAt = &now
		owned.claim.Conversation.DroidInitializedAt = &now
	}
	owned.mu.Lock()
	owned.runtime = runtime
	cause := context.Cause(owned.ctx)
	owned.mu.Unlock()
	if cause != nil {
		_ = runtime.Abort(context.Background())
	}
	outcome, runErr := runtime.Run(owned.ctx, claim.Task, func(childTurnID string) error {
		generation := owned.generation.Load()
		_, err := s.repository.BindChildTurn(context.Background(), claim.Task.ID, generation, childTurnID)
		return err
	}, func(event LiveEvent) {
		s.publishChildEvent(claim.Task.OwnerSessionID, claim.Task.ConversationID, claim.Task.ID, event)
	})
	owned.mu.Lock()
	closeErr := runtime.Close(context.Background())
	owned.runtime = nil
	owned.mu.Unlock()
	if runErr == nil {
		runErr = closeErr
	} else if closeErr != nil {
		runErr = errors.Join(runErr, closeErr)
	}
	s.finishWorker(owned, outcome, runErr)
}

func stateForWorkerError(cause error) TaskState {
	switch {
	case errors.Is(cause, errTaskCanceled), errors.Is(cause, errConversationGone):
		return TaskAborted
	case errors.Is(cause, errSupervisorStop):
		return TaskInterrupted
	default:
		return TaskFailed
	}
}

func (s *Supervisor) finishWorker(owned *worker, outcome ChildOutcome, runErr error) {
	claim := owned.claim
	cause := context.Cause(owned.ctx)
	state := outcome.State
	if !TerminalTask(state) {
		state = stateForWorkerError(cause)
		if state == TaskFailed && runErr == nil {
			runErr = errors.New("child runtime returned a non-terminal state")
		}
	}
	if cause != nil {
		state = stateForWorkerError(cause)
	}
	errorText := outcome.Error
	if errorText == "" && runErr != nil {
		errorText = runErr.Error()
	}
	if errorText == "" && cause != nil {
		errorText = cause.Error()
	}
	completion := Completion{
		TaskID: claim.Task.ID, ConversationID: claim.Task.ConversationID,
		CancellationGeneration: owned.generation.Load(), State: state,
		ChildTurnID: outcome.TurnID, ResultSummary: outcome.Text, Error: errorText,
		FinishedAt: time.Now().UTC(),
	}
	completed, mailbox, durable := s.completeDurably(completion)
	if conversation, loadErr := s.repository.Conversation(context.Background(), claim.Task.ConversationID); loadErr == nil && conversation.DismissedAt != nil {
		_ = s.factory.Delete(context.Background(), conversation)
	}
	s.mu.Lock()
	delete(s.running, claim.Task.ID)
	s.signalChangedLocked()
	s.mu.Unlock()
	s.Wake()
	if !durable {
		return // Startup recovery will classify the still-running authoritative row.
	}
	// Parent notification I/O must not retain a child execution slot.
	s.emitChanged(completed.OwnerSessionID, completed.ConversationID, completed.ID)
	if mailbox != nil && s.sink != nil {
		s.sink.MailboxAdded(context.Background(), *mailbox)
	}
}

func (s *Supervisor) completeDurably(completion Completion) (Task, *MailboxItem, bool) {
	delay := 25 * time.Millisecond
	shutdownRetries := 0
	for {
		completed, mailbox, err := s.repository.Complete(context.Background(), completion)
		if err == nil {
			return completed, mailbox, true
		}
		if errors.Is(err, ErrConflict) {
			if current, loadErr := s.repository.Task(context.Background(), completion.TaskID); loadErr == nil {
				completion.CancellationGeneration = current.CancellationGeneration
				if TerminalTask(current.State) {
					completion.State = current.State
				} else {
					completion.State = TaskAborted
					if completion.Error == "" {
						completion.Error = errTaskCanceled.Error()
					}
				}
			}
		}
		if s.ctx.Err() != nil {
			shutdownRetries++
			if shutdownRetries >= 3 {
				return Task{}, nil, false
			}
		}
		time.Sleep(delay)
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}

func (s *Supervisor) publishChildEvent(owner string, conversationID ConversationID, taskID TaskID, event LiveEvent) {
	const (
		maxChildEventBytes   = 16 << 10
		maxChildJournalBytes = 1 << 20
		maxChildEvents       = 128
		maxGlobalLiveBytes   = 16 << 20
	)
	if liveEventBytes(event) > maxChildEventBytes {
		return
	}
	s.mu.Lock()
	journal := s.live[conversationID]
	if journal == nil {
		streamID, err := identifier.New("substream_")
		if err != nil {
			s.mu.Unlock()
			return
		}
		journal = &liveEventJournal{streamID: streamID, next: 1}
		s.live[conversationID] = journal
	}
	event.Sequence = journal.next
	journal.next++
	eventBytes := liveEventBytes(event)
	journal.events = append(journal.events, event)
	journal.bytes += eventBytes
	s.liveClock++
	journal.updated = s.liveClock
	for len(journal.events) > maxChildEvents || journal.bytes > maxChildJournalBytes {
		journal.bytes -= liveEventBytes(journal.events[0])
		journal.events = journal.events[1:]
	}
	for totalLiveEventBytes(s.live) > maxGlobalLiveBytes {
		var oldestID ConversationID
		oldest := ^uint64(0)
		for id, candidate := range s.live {
			if id != conversationID && candidate.updated < oldest {
				oldestID, oldest = id, candidate.updated
			}
		}
		if oldestID == "" {
			break
		}
		delete(s.live, oldestID)
	}
	s.signalChangedLocked()
	s.mu.Unlock()
	// Parent clients need lifecycle invalidations, not one full-roster reload per text delta.
	switch event.Kind {
	case "message.completed", "tool.started", "tool.completed", "turn.settled", "execution.settled":
		s.emitChanged(owner, conversationID, taskID)
	}
}

func liveEventBytes(event LiveEvent) int {
	return len(event.Kind) + len(event.TurnID) + len(event.MessageID) + len(event.Delta) + len(event.Text) + len(event.ToolCallID) + len(event.ToolName) + 64
}

func totalLiveEventBytes(journals map[ConversationID]*liveEventJournal) int {
	total := 0
	for _, journal := range journals {
		total += journal.bytes
	}
	return total
}

// LiveEvents returns a bounded runtime-local child event page.
func (s *Supervisor) LiveEvents(_ context.Context, conversationID ConversationID, streamID string, after int64) (LiveEventPage, error) {
	if after < 0 {
		return LiveEventPage{}, fmt.Errorf("%w: child event cursor is negative", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	journal := s.live[conversationID]
	if journal == nil {
		return LiveEventPage{}, ErrNotFound
	}
	page := LiveEventPage{StreamID: journal.streamID}
	if len(journal.events) == 0 {
		return page, nil
	}
	page.FirstSequence = journal.events[0].Sequence
	page.LastSequence = journal.events[len(journal.events)-1].Sequence
	if streamID != "" && streamID != journal.streamID || after > page.LastSequence || after > 0 && after < page.FirstSequence-1 {
		page.ResyncRequired = true
		return page, nil
	}
	for _, event := range journal.events {
		if event.Sequence > after {
			page.Events = append(page.Events, event)
		}
	}
	return page, nil
}

// Cancel generation-safely cancels only the named task.
func (s *Supervisor) Cancel(ctx context.Context, taskID TaskID, expectedGeneration uint64, reason string) (Task, error) {
	if err := s.available(); err != nil {
		return Task{}, err
	}
	task, err := s.repository.Cancel(ctx, taskID, expectedGeneration, reason, time.Now().UTC())
	if err != nil {
		return Task{}, err
	}
	if task.State == TaskRunning {
		s.mu.Lock()
		owned := s.running[task.ID]
		if owned != nil {
			owned.generation.Store(task.CancellationGeneration)
		}
		s.mu.Unlock()
		if owned != nil {
			s.cancelWorker(owned, errTaskCanceled)
		}
	}
	s.emitChanged(task.OwnerSessionID, task.ConversationID, task.ID)
	s.signalChanged()
	s.Wake()
	return task, nil
}

// Dismiss tombstones a conversation and cancels all runtime work it owns.
func (s *Supervisor) Dismiss(ctx context.Context, conversationID ConversationID, expectedGeneration uint64, reason string) ([]Task, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	tasks, err := s.repository.Dismiss(ctx, conversationID, expectedGeneration, reason, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	hasRunningWorker := false
	for _, task := range tasks {
		if task.StartedAt != nil {
			hasRunningWorker = true
		}
		s.mu.Lock()
		owned := s.running[task.ID]
		if owned != nil {
			hasRunningWorker = true
		}
		if owned != nil {
			owned.generation.Store(task.CancellationGeneration)
		}
		s.mu.Unlock()
		if owned != nil {
			s.cancelWorker(owned, errConversationGone)
		}
		s.emitChanged(task.OwnerSessionID, task.ConversationID, task.ID)
	}
	if !hasRunningWorker {
		s.cleanupConversation(conversationID)
	}
	s.mu.Lock()
	delete(s.live, conversationID)
	s.mu.Unlock()
	s.signalChanged()
	s.Wake()
	return tasks, nil
}

func (s *Supervisor) cancelWorker(owned *worker, cause error) {
	// ChildRuntime.Run owns translating context cancellation into its runtime's
	// bounded abort path. Do not synchronously block the scheduler or caller.
	owned.cancel(cause)
}

// CancelOwner stops every in-memory worker after the owner's archival transaction commits.
func (s *Supervisor) CancelOwner(ownerSessionID string) {
	s.mu.Lock()
	workers := make([]*worker, 0)
	activeConversations := make(map[ConversationID]bool)
	for _, owned := range s.running {
		if owned.claim.Task.OwnerSessionID == ownerSessionID {
			workers = append(workers, owned)
			activeConversations[owned.claim.Conversation.ID] = true
		}
	}
	s.mu.Unlock()
	for _, owned := range workers {
		s.cancelWorker(owned, errConversationGone)
	}
	if conversations, err := s.repository.ListDismissedConversations(context.Background(), ownerSessionID); err == nil {
		for _, conversation := range conversations {
			if !activeConversations[conversation.ID] {
				s.cleanupConversation(conversation.ID)
			}
		}
	}
	s.signalChanged()
	s.Wake()
}

func (s *Supervisor) cleanupConversation(conversationID ConversationID) {
	s.mu.Lock()
	conversationMu := s.conversationLocks[conversationID]
	if conversationMu == nil {
		conversationMu = &sync.Mutex{}
		s.conversationLocks[conversationID] = conversationMu
	}
	for _, owned := range s.running {
		if owned.claim.Conversation.ID == conversationID {
			s.mu.Unlock()
			return
		}
	}
	s.mu.Unlock()
	conversationMu.Lock()
	defer conversationMu.Unlock()
	s.mu.Lock()
	for _, owned := range s.running {
		if owned.claim.Conversation.ID == conversationID {
			s.mu.Unlock()
			return
		}
	}
	s.mu.Unlock()
	conversation, err := s.repository.Conversation(context.Background(), conversationID)
	if err == nil && conversation.DismissedAt != nil {
		_ = s.factory.Delete(context.Background(), conversation)
	}
}

// WaitTask waits for a named task to become terminal or for the caller's bounded context.
func (s *Supervisor) WaitTask(ctx context.Context, taskID TaskID) (Task, error) {
	for {
		task, err := s.repository.Task(ctx, taskID)
		if err != nil || TerminalTask(task.State) {
			return task, err
		}
		s.mu.Lock()
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Task{}, ctx.Err()
		case <-changed:
		}
	}
}

// Task returns authoritative task state without loading a child runtime.
func (s *Supervisor) Task(ctx context.Context, id TaskID) (Task, error) {
	return s.repository.Task(ctx, id)
}

// Conversation returns authoritative conversation state.
func (s *Supervisor) Conversation(ctx context.Context, id ConversationID) (Conversation, error) {
	return s.repository.Conversation(ctx, id)
}

// ConversationByAgent returns an owner's active conversation for one agent.
func (s *Supervisor) ConversationByAgent(ctx context.Context, owner, agent string) (Conversation, error) {
	return s.repository.ConversationByAgent(ctx, owner, agent)
}

// ListConversations returns one parent's authoritative roster.
func (s *Supervisor) ListConversations(ctx context.Context, owner string) ([]Conversation, error) {
	return s.repository.ListConversations(ctx, owner)
}

// ListTasks returns ordered task history for one conversation.
func (s *Supervisor) ListTasks(ctx context.Context, conversationID ConversationID) ([]Task, error) {
	return s.repository.ListTasks(ctx, conversationID)
}

// Transcript returns durable child history without exposing its storage identity.
func (s *Supervisor) Transcript(ctx context.Context, conversationID ConversationID) (Transcript, error) {
	conversation, err := s.repository.Conversation(ctx, conversationID)
	if err != nil {
		return Transcript{}, err
	}
	if conversation.DismissedAt != nil {
		return Transcript{}, ErrDismissed
	}
	s.mu.Lock()
	for _, owned := range s.running {
		if owned.claim.Conversation.ID != conversationID {
			continue
		}
		owned.mu.Lock()
		s.mu.Unlock()
		defer owned.mu.Unlock()
		if owned.runtime == nil {
			return Transcript{}, ErrConflict
		}
		transcript, err := owned.runtime.Transcript(ctx)
		if transcript.ConversationID == "" {
			transcript.ConversationID = conversationID
		}
		return transcript, err
	}
	conversationMu := s.conversationLocks[conversationID]
	if conversationMu == nil {
		conversationMu = &sync.Mutex{}
		s.conversationLocks[conversationID] = conversationMu
	}
	s.mu.Unlock()
	conversationMu.Lock()
	// A task may have been claimed while inspection waited for ownership.
	s.mu.Lock()
	for _, owned := range s.running {
		if owned.claim.Conversation.ID == conversationID {
			s.mu.Unlock()
			conversationMu.Unlock()
			return Transcript{}, ErrConflict
		}
	}
	s.mu.Unlock()
	conversation, err = s.repository.Conversation(ctx, conversationID)
	if err != nil || conversation.DismissedAt != nil {
		conversationMu.Unlock()
		if err == nil {
			err = ErrDismissed
		}
		return Transcript{}, err
	}
	runtime, err := s.factory.Open(ctx, conversation)
	if err != nil {
		conversationMu.Unlock()
		return Transcript{}, err
	}
	transcript, transcriptErr := runtime.Transcript(ctx)
	closeErr := runtime.Close(context.Background())
	conversationMu.Unlock()
	if transcript.ConversationID == "" {
		transcript.ConversationID = conversationID
	}
	return transcript, errors.Join(transcriptErr, closeErr)
}

// Shutdown stops new claims, interrupts workers, and waits for bounded cleanup.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	started := s.started
	if !s.closed {
		s.closed = true
		s.cancel(errSupervisorStop)
		workers := make([]*worker, 0, len(s.running))
		for _, owned := range s.running {
			workers = append(workers, owned)
		}
		s.signalChangedLocked()
		s.mu.Unlock()
		for _, owned := range workers {
			s.cancelWorker(owned, errSupervisorStop)
		}
	} else {
		s.mu.Unlock()
	}
	finished := make(chan struct{})
	go func() {
		if started {
			<-s.done
		}
		s.workerWG.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Supervisor) available() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if !s.started {
		return fmt.Errorf("%w: supervisor has not started", ErrClosed)
	}
	if s.startErr != nil {
		return s.startErr
	}
	return nil
}

func (s *Supervisor) signalChanged() {
	s.mu.Lock()
	s.signalChangedLocked()
	s.mu.Unlock()
}

func (s *Supervisor) signalChangedLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *Supervisor) emitChanged(owner string, conversationID ConversationID, taskID TaskID) {
	if s.sink != nil {
		s.sink.SubagentChanged(context.Background(), owner, conversationID, taskID)
	}
}
