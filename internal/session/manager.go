// Package session owns persisted Kit session runtimes above droids.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
)

// ErrBusy indicates that a session already has an active parent run.
var ErrBusy = errors.New("session already has an active parent run")

// ErrClosed indicates that the session manager is shutting down.
var ErrClosed = errors.New("session manager is closed")

// ErrInvalidInput identifies invalid session metadata or prompt input.
var ErrInvalidInput = errors.New("invalid session input")

// ErrRunNotAbortable indicates that a run has already begun settlement or ended.
var ErrRunNotAbortable = errors.New("run is not abortable")

// ErrBashBusy indicates that a session or daemon direct-bash lane is occupied.
var ErrBashBusy = errors.New("bash execution is busy")

// ErrBashNotAbortable indicates that a direct bash execution has already ended.
var ErrBashNotAbortable = errors.New("bash execution is not abortable")

// CreateInput contains metadata for a new persistent session.
type CreateInput struct {
	CWD           string
	Name          string
	Model         string
	ThinkingLevel string
}

// RunReservation is a durable, generation-bound prompt handle.
type RunReservation struct {
	SessionID string
	TurnID    string
	RunID     string
}

// ProviderErrorKind is Kit's runtime classification for a provider failure.
type ProviderErrorKind string

const (
	ProviderErrorAuthentication ProviderErrorKind = "authentication"
	ProviderErrorEntitlement    ProviderErrorKind = "entitlement"
	ProviderErrorUsageLimit     ProviderErrorKind = "usage_limit"
	ProviderErrorRateLimit      ProviderErrorKind = "rate_limit"
	ProviderErrorTransport      ProviderErrorKind = "transport"
	ProviderErrorProtocol       ProviderErrorKind = "protocol"
)

// PromptResult is the terminal projection of one parent run.
type PromptResult struct {
	SessionID    string
	TurnID       string
	RunID        string
	Text         string
	StopReason   string
	ErrorKind    ProviderErrorKind
	ErrorMessage string
	Status       RunStatus
}

// Manager owns at most one live droids runtime per loaded session.
type Manager struct {
	store        Repository
	providers    droids.Providers
	systemPrompt string
	bashContext  context.Context
	cancelBash   context.CancelCauseFunc

	mu         sync.Mutex
	runtimes   map[string]*runtime
	loading    map[string]*runtimeLoad
	closed     bool
	runs       sync.WaitGroup
	loads      sync.WaitGroup
	ops        sync.WaitGroup
	admissions sync.WaitGroup

	bashMu     sync.Mutex
	bashActive map[string]*activeBashExecution
	bashSlots  chan struct{}
	bashRuns   sync.WaitGroup
}

type runtimeLoad struct {
	done    chan struct{}
	runtime *runtime
	err     error
}

type runtime struct {
	droid               *droids.Droid
	storage             *droidStorage
	runMu               sync.Mutex
	stateMu             sync.Mutex
	cancelRun           context.CancelFunc
	activeRun           string
	phase               runPhase
	bashContextSequence int64
}

// NewManager creates a session runtime manager.
func NewManager(store Repository, providers droids.Providers, systemPrompt string) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if providers == nil {
		return nil, fmt.Errorf("droids providers are required")
	}
	bashContext, cancelBash := context.WithCancelCause(context.Background())
	return &Manager{
		store: store, providers: providers, systemPrompt: systemPrompt,
		bashContext: bashContext, cancelBash: cancelBash,
		runtimes:   make(map[string]*runtime),
		loading:    make(map[string]*runtimeLoad),
		bashActive: make(map[string]*activeBashExecution),
		bashSlots:  make(chan struct{}, maxConcurrentDirectBash),
	}, nil
}

// Create validates the workspace and model before persisting a session.
func (m *Manager) Create(ctx context.Context, input CreateInput) (SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return SessionRecord{}, err
	}
	defer m.ops.Done()
	if input.CWD == "" {
		return SessionRecord{}, fmt.Errorf("%w: session cwd is required", ErrInvalidInput)
	}
	if !filepath.IsAbs(input.CWD) {
		return SessionRecord{}, fmt.Errorf("%w: session cwd must be absolute", ErrInvalidInput)
	}
	cwd := filepath.Clean(input.CWD)
	info, err := os.Stat(cwd)
	if err != nil {
		return SessionRecord{}, fmt.Errorf("%w: inspect session cwd: %v", ErrInvalidInput, err)
	}
	if !info.IsDir() {
		return SessionRecord{}, fmt.Errorf("%w: session cwd %q is not a directory", ErrInvalidInput, cwd)
	}
	model, ok := m.providers.Model(input.Model)
	if !ok {
		return SessionRecord{}, fmt.Errorf("%w: unknown model %q", ErrInvalidInput, input.Model)
	}
	if err := validateThinkingLevel(model, input.ThinkingLevel); err != nil {
		return SessionRecord{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	id, err := identifier.New("session_")
	if err != nil {
		return SessionRecord{}, err
	}
	return m.store.CreateSession(ctx, NewSession{
		ID: id, CWD: cwd, Name: strings.TrimSpace(input.Name), Persistent: true,
		ModelProvider: model.Provider, ModelID: model.ID,
		ThinkingLevel: input.ThinkingLevel,
	})
}

// List returns persisted sessions, optionally filtered to a cwd.
func (m *Manager) List(ctx context.Context, cwd string) ([]SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return nil, err
	}
	defer m.ops.Done()
	if cwd != "" {
		if !filepath.IsAbs(cwd) {
			return nil, fmt.Errorf("%w: session list cwd must be absolute", ErrInvalidInput)
		}
		cwd = filepath.Clean(cwd)
	}
	return m.store.ListSessions(ctx, cwd)
}

// GetRun returns one exact durable parent-run generation.
func (m *Manager) GetRun(ctx context.Context, sessionID, runID string) (ParentRunRecord, error) {
	if err := m.beginOperation(); err != nil {
		return ParentRunRecord{}, err
	}
	defer m.ops.Done()
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(runID) == "" {
		return ParentRunRecord{}, fmt.Errorf("%w: session and run ids are required", ErrInvalidInput)
	}
	return m.store.GetParentRun(ctx, sessionID, runID)
}

// ReservePrompt durably allocates a queued turn before a client receives its
// generation-bound run handle.
func (m *Manager) ReservePrompt(
	ctx context.Context,
	sessionID, runID string,
) (RunReservation, error) {
	if err := m.beginOperation(); err != nil {
		return RunReservation{}, err
	}
	defer m.ops.Done()
	if !identifier.Valid(runID, "run_") {
		return RunReservation{}, fmt.Errorf("%w: run id is invalid", ErrInvalidInput)
	}
	turnID, err := identifier.New("turn_")
	if err != nil {
		return RunReservation{}, err
	}
	turn, run, err := m.store.ReserveParentRun(ctx, sessionID, turnID, runID)
	if err != nil {
		return RunReservation{}, err
	}
	return RunReservation{SessionID: sessionID, TurnID: turn.ID, RunID: run.ID}, nil
}

// StartPrompt reserves and starts one daemon-owned parent run before returning
// its generation-bound handle.
func (m *Manager) StartPrompt(ctx context.Context, sessionID, runID, prompt string) (RunReservation, error) {
	if err := m.beginAdmission(); err != nil {
		return RunReservation{}, err
	}
	defer m.admissions.Done()
	reservation, err := m.ReservePrompt(ctx, sessionID, runID)
	if err != nil {
		return RunReservation{}, err
	}
	started := make(chan error, 1)
	go func() {
		signaled := false
		_, runErr := m.runPrompt(context.Background(), sessionID, runID, prompt, true, func() {
			signaled = true
			started <- nil
		})
		if !signaled {
			started <- runErr
		}
	}()
	if err := <-started; err != nil {
		return RunReservation{}, err
	}
	return reservation, nil
}

// RunPrompt starts one already-reserved daemon-owned parent run and waits for
// its durable outcome. Cancelling ctx stops waiting but deliberately does not
// abort work; Abort is the only client cancellation path.
func (m *Manager) RunPrompt(ctx context.Context, sessionID, runID, prompt string) (PromptResult, error) {
	return m.runPrompt(ctx, sessionID, runID, prompt, false, nil)
}

func (m *Manager) runPrompt(
	ctx context.Context,
	sessionID, runID, prompt string,
	admitted bool,
	onStarted func(),
) (PromptResult, error) {
	if !admitted {
		if err := m.beginOperation(); err != nil {
			return PromptResult{}, err
		}
		defer m.ops.Done()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !identifier.Valid(runID, "run_") {
		return PromptResult{}, fmt.Errorf("%w: run id is invalid", ErrInvalidInput)
	}
	if strings.TrimSpace(prompt) == "" {
		cause := fmt.Errorf("%w: prompt is empty", ErrInvalidInput)
		return PromptResult{}, m.rejectReservation(sessionID, runID, cause)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return PromptResult{}, m.rejectReservation(sessionID, runID, err)
	}
	if !loaded.runMu.TryLock() {
		return PromptResult{}, m.rejectReservation(sessionID, runID, ErrBusy)
	}
	reservedRun, err := m.store.GetParentRun(ctx, sessionID, runID)
	if err != nil {
		loaded.runMu.Unlock()
		return PromptResult{}, m.rejectReservation(sessionID, runID, err)
	}
	latestBashContext := int64(-1)
	if reservedRun.Status == RunStatusQueued {
		latestBashContext, err = m.store.ClaimBashContext(ctx, sessionID, reservedRun.TurnID)
		if err != nil {
			inspectContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			claimed, unclaimed, inspectErr := m.store.InspectBashContextClaim(inspectContext, sessionID, reservedRun.TurnID)
			cancel()
			switch {
			case inspectErr != nil:
				loaded.runMu.Unlock()
				return PromptResult{}, m.rejectReservation(sessionID, runID, errors.Join(err, inspectErr))
			case claimed >= 0:
				latestBashContext = claimed
			case unclaimed < 0:
				latestBashContext = -1
			default:
				loaded.runMu.Unlock()
				return PromptResult{}, m.rejectReservation(sessionID, runID, err)
			}
		}
	}
	if latestBashContext > loaded.bashContextSequence {
		if err := m.reloadRuntime(ctx, sessionID, loaded); err != nil {
			loaded.runMu.Unlock()
			return PromptResult{}, m.rejectReservation(sessionID, runID, err)
		}
	}

	runContext, cancelRun := context.WithCancel(context.Background())
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancelRun()
		m.evict(sessionID, loaded)
		loaded.runMu.Unlock()
		return PromptResult{}, m.rejectReservation(sessionID, runID, ErrClosed)
	}
	m.runs.Add(1)
	loaded.setActive(cancelRun, runPhaseStarting, runID)
	m.mu.Unlock()
	turnID, startStatus, err := m.store.StartReservedParentRun(ctx, sessionID, runID)
	if err != nil {
		cancelRun()
		loaded.clearActive()
		m.runs.Done()
		m.evict(sessionID, loaded)
		loaded.runMu.Unlock()
		return PromptResult{}, m.rejectReservation(sessionID, runID, err)
	}
	if startStatus != RunStatusRunning {
		cancelRun()
		loaded.clearActive()
		m.runs.Done()
		m.evict(sessionID, loaded)
		loaded.runMu.Unlock()
		if startStatus == RunStatusAborted {
			return PromptResult{
				SessionID: sessionID, TurnID: turnID, RunID: runID,
				Status: RunStatusAborted, ErrorMessage: "aborted before execution",
			}, nil
		}
		return PromptResult{}, fmt.Errorf("run %q cannot start from status %q", runID, startStatus)
	}
	if err := loaded.storage.beginTurn(turnID); err != nil {
		m.finishAfterFailure(sessionID, turnID, runID, RunStatusInterrupted, err)
		cancelRun()
		loaded.clearActive()
		m.runs.Done()
		m.evict(sessionID, loaded)
		loaded.runMu.Unlock()
		return PromptResult{}, err
	}

	completion := make(chan promptCompletion, 1)
	loaded.setPhase(runPhaseRunning)
	go func() {
		result, err := m.executePrompt(runContext, loaded, sessionID, turnID, runID, prompt)
		cancelRun()
		loaded.clearActive()
		loaded.runMu.Unlock()
		m.runs.Done()
		completion <- promptCompletion{result: result, err: err}
	}()
	if onStarted != nil {
		onStarted()
	}

	select {
	case completed := <-completion:
		return completed.result, completed.err
	case <-ctx.Done():
		return PromptResult{}, ctx.Err()
	}
}

type promptCompletion struct {
	result PromptResult
	err    error
}

func (m *Manager) executePrompt(
	ctx context.Context,
	loaded *runtime,
	sessionID, turnID, runID, prompt string,
) (PromptResult, error) {
	// Parent execution belongs to the daemon, not an attached request. Closing
	// the runtime or calling Abort cancels droids explicitly.
	eventErr := m.appendLiveEvents([]NewEvent{{
		SessionID: sessionID, TurnID: turnID, RunID: runID,
		Kind: EventRunStarted, Status: RunStatusRunning,
	}})
	run, streamErr := loaded.droid.Stream(ctx, prompt)
	if streamErr == nil {
		drainErr := m.drainRunEvents(run, sessionID, turnID, runID, eventErr == nil)
		if eventErr == nil {
			eventErr = drainErr
		}
	}
	var assistant droids.AssistantMessage
	var runErr error
	if streamErr != nil {
		runErr = streamErr
	} else {
		assistant, runErr = run.Result()
	}
	loaded.setPhase(runPhaseSettling)
	persistenceErr := loaded.storage.endTurn()

	status := RunStatusCompleted
	executionErr := runErr
	switch {
	case persistenceErr != nil || eventErr != nil:
		status = RunStatusInterrupted
		executionErr = errors.Join(executionErr, eventErr)
		if persistenceErr != nil {
			executionErr = errors.Join(executionErr, fmt.Errorf("persist droids transcript: %w", persistenceErr))
		}
	case errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded), assistant.StopReason == droids.StopReasonAborted:
		status = RunStatusAborted
	case runErr != nil, assistant.StopReason == droids.StopReasonError:
		status = RunStatusFailed
	}
	result := PromptResult{
		SessionID:    sessionID,
		TurnID:       turnID,
		RunID:        runID,
		Text:         assistant.Text(),
		StopReason:   string(assistant.StopReason),
		ErrorKind:    projectDroidErrorKind(assistant.ErrorKind),
		ErrorMessage: assistant.ErrorMessage,
		Status:       status,
	}
	if executionErr != nil && result.ErrorMessage == "" {
		result.ErrorMessage = executionErr.Error()
	}

	finishContext, cancelFinish := context.WithTimeout(context.Background(), 5*time.Second)
	finishErr := m.store.FinishParentRun(
		finishContext,
		sessionID,
		turnID,
		runID,
		status,
		result.ErrorMessage,
	)
	cancelFinish()
	if status != RunStatusCompleted || finishErr != nil {
		m.evict(sessionID, loaded)
	}
	if finishErr != nil {
		reason := fmt.Sprintf("parent run settlement failed: %v", finishErr)
		recoveryContext, cancelRecovery := context.WithTimeout(context.Background(), 5*time.Second)
		recoveredStatus, recoveryErr := m.store.RecoverParentRun(
			recoveryContext, sessionID, turnID, runID, reason,
		)
		cancelRecovery()
		if recoveryErr != nil {
			return result, errors.Join(executionErr, finishErr, recoveryErr)
		}
		result.Status = recoveredStatus
		if recoveredStatus != RunStatusCompleted && result.ErrorMessage == "" {
			result.ErrorMessage = reason
		}
	}
	if result.Status != RunStatusCompleted {
	}
	if eventErr == nil {
		_ = m.appendLiveEvents([]NewEvent{{
			SessionID: sessionID, TurnID: turnID, RunID: runID,
			Kind: EventRunFinished, Status: result.Status, ErrorKind: result.ErrorKind,
			ErrorMessage: result.ErrorMessage,
		}})
	}
	// Provider failures, cancellation, and a transcript persistence failure are
	// durable terminal run outcomes represented by PromptResult. Go errors are
	// reserved for failures to establish or durably finish that outcome.
	return result, nil
}

func (m *Manager) drainRunEvents(run droids.Run, sessionID, turnID, runID string, persist bool) error {
	const maxBatch = 64
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	pending := make([]NewEvent, 0, maxBatch)
	var firstErr error
	flush := func() {
		if len(pending) == 0 {
			return
		}
		if persist {
			if err := m.appendLiveEvents(pending); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				persist = false
			}
		}
		pending = pending[:0]
	}
	events := run.Events()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				flush()
				return firstErr
			}
			for _, projected := range projectDroidEvent(sessionID, turnID, runID, event) {
				last := len(pending) - 1
				if last >= 0 && coalescibleDelta(pending[last], projected) {
					pending[last].Delta += projected.Delta
				} else {
					pending = append(pending, projected)
				}
			}
			if len(pending) >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func coalescibleDelta(previous, next NewEvent) bool {
	const maxCoalescedDeltaBytes = 16 << 10
	isDelta := next.Kind == EventAssistantTextDelta || next.Kind == EventThinkingDelta
	return isDelta && previous.Kind == next.Kind && previous.MessageID == next.MessageID &&
		previous.ContentIndex == next.ContentIndex && previous.RunID == next.RunID &&
		len(previous.Delta)+len(next.Delta) <= maxCoalescedDeltaBytes
}

func (m *Manager) appendLiveEvents(events []NewEvent) error {
	if len(events) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := m.store.AppendSessionEvents(ctx, events)
	return err
}

// Abort interrupts only the matching reserved or active run generation.
func (m *Manager) Abort(ctx context.Context, sessionID, runID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if !identifier.Valid(runID, "run_") {
		return fmt.Errorf("%w: run id is invalid", ErrInvalidInput)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	if loaded != nil && loaded.abort(runID) {
		return nil
	}
	status, err := m.store.AbortReservedParentRun(ctx, sessionID, runID, "aborted before execution")
	if err != nil {
		return err
	}
	if status == RunStatusAborted {
		return nil
	}
	if status == RunStatusRunning && loaded != nil && loaded.abort(runID) {
		return nil
	}
	return fmt.Errorf("%w: run %q has status %q", ErrRunNotAbortable, runID, status)
}

// Shutdown rejects new runs, aborts active droids, and waits for durable run
// finalization until ctx expires.
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if !m.closed {
		m.closed = true
	}
	runtimes := make([]*runtime, 0, len(m.runtimes))
	for _, loaded := range m.runtimes {
		runtimes = append(runtimes, loaded)
	}
	m.runtimes = nil
	m.mu.Unlock()
	m.cancelBash(errBashShutdown)
	for _, loaded := range runtimes {
		loaded.droid.Close()
	}
	m.bashMu.Lock()
	bashExecutions := make([]*activeBashExecution, 0, len(m.bashActive))
	for _, execution := range m.bashActive {
		bashExecutions = append(bashExecutions, execution)
	}
	m.bashMu.Unlock()
	for _, execution := range bashExecutions {
		execution.cancel(errBashShutdown)
	}

	done := make(chan struct{})
	go func() {
		m.admissions.Wait()
		m.loads.Wait()
		m.runs.Wait()
		m.bashRuns.Wait()
		m.ops.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for session runs to stop: %w", ctx.Err())
	}
}

// Close aborts and waits for every loaded runtime without a deadline.
func (m *Manager) Close() {
	_ = m.Shutdown(context.Background())
}

type runPhase uint8

const (
	runPhaseIdle runPhase = iota
	runPhaseStarting
	runPhaseRunning
	runPhaseSettling
)

func (r *runtime) setActive(cancel context.CancelFunc, phase runPhase, runID string) {
	r.stateMu.Lock()
	r.cancelRun = cancel
	r.activeRun = runID
	r.phase = phase
	r.stateMu.Unlock()
}

func (r *runtime) setPhase(phase runPhase) {
	r.stateMu.Lock()
	r.phase = phase
	r.stateMu.Unlock()
}

func (r *runtime) clearActive() {
	r.stateMu.Lock()
	r.cancelRun = nil
	r.activeRun = ""
	r.phase = runPhaseIdle
	r.stateMu.Unlock()
}

func (r *runtime) abort(expectedRunID string) bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	abortable := r.phase == runPhaseStarting || r.phase == runPhaseRunning
	if r.cancelRun == nil || r.activeRun != expectedRunID || !abortable {
		return false
	}
	r.cancelRun()
	r.droid.Abort()
	return true
}

func (m *Manager) runtime(ctx context.Context, sessionID string) (*runtime, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	if loaded, ok := m.runtimes[sessionID]; ok {
		m.mu.Unlock()
		return loaded, nil
	}
	if pending, ok := m.loading[sessionID]; ok {
		m.mu.Unlock()
		select {
		case <-pending.done:
			return pending.runtime, pending.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pending := &runtimeLoad{done: make(chan struct{})}
	m.loading[sessionID] = pending
	m.loads.Add(1)
	m.mu.Unlock()

	loaded, err := m.loadRuntime(ctx, sessionID)
	m.mu.Lock()
	delete(m.loading, sessionID)
	if m.closed {
		if loaded != nil {
			loaded.droid.Close()
		}
		loaded = nil
		err = ErrClosed
	} else if err == nil {
		m.runtimes[sessionID] = loaded
	}
	pending.runtime = loaded
	pending.err = err
	close(pending.done)
	m.mu.Unlock()
	m.loads.Done()
	return loaded, err
}

func (m *Manager) loadRuntime(ctx context.Context, sessionID string) (*runtime, error) {
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	droid, adapter, err := m.newDroid(record)
	if err != nil {
		return nil, err
	}
	return &runtime{droid: droid, storage: adapter, bashContextSequence: adapter.bashContextSequence()}, nil
}

func (m *Manager) reloadRuntime(ctx context.Context, sessionID string, loaded *runtime) error {
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	droid, adapter, err := m.newDroid(record)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.closed || m.runtimes[sessionID] != loaded {
		m.mu.Unlock()
		droid.Close()
		return ErrClosed
	}
	previous := loaded.droid
	loaded.droid = droid
	loaded.storage = adapter
	loaded.bashContextSequence = adapter.bashContextSequence()
	m.mu.Unlock()
	previous.Close()
	return nil
}

func (m *Manager) newDroid(record SessionRecord) (*droids.Droid, *droidStorage, error) {
	adapter := newDroidStorage(m.store, record.ID)
	droid, err := droids.New(droids.Options{
		Providers:        m.providers,
		Model:            record.ModelProvider + "/" + record.ModelID,
		Reasoning:        record.ThinkingLevel,
		SystemPrompt:     m.systemPrompt,
		Tools:            codingtools.New(record.CWD),
		Storage:          adapter,
		Session:          record.ID,
		MaxSteps:         16,
		MaxParallelTools: 4,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create runtime for session %q: %w", record.ID, err)
	}
	return droid, adapter, nil
}

func (m *Manager) evict(sessionID string, target *runtime) {
	m.mu.Lock()
	if current := m.runtimes[sessionID]; current == target {
		delete(m.runtimes, sessionID)
	}
	m.mu.Unlock()
	target.droid.Close()
}

func (m *Manager) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *Manager) beginAdmission() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	m.admissions.Add(1)
	return nil
}

func (m *Manager) beginOperation() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	m.ops.Add(1)
	return nil
}

func projectDroidErrorKind(kind droids.ErrorKind) ProviderErrorKind {
	switch kind {
	case droids.ErrorAuthentication:
		return ProviderErrorAuthentication
	case droids.ErrorEntitlement:
		return ProviderErrorEntitlement
	case droids.ErrorUsageLimit:
		return ProviderErrorUsageLimit
	case droids.ErrorRateLimit:
		return ProviderErrorRateLimit
	case droids.ErrorTransport:
		return ProviderErrorTransport
	case droids.ErrorProtocol:
		return ProviderErrorProtocol
	default:
		return ""
	}
}

func validateThinkingLevel(model droids.Model, level string) error {
	if level == "" {
		return nil
	}
	if level == "none" || level == "off" {
		if (model.API != droids.ModelAPIOpenAIResponses && model.API != droids.ModelAPIOpenAICodexResponses) || !model.Reasoning {
			return nil
		}
		for _, supported := range model.ReasoningLevels {
			if supported == "none" {
				return nil
			}
		}
		return fmt.Errorf("model %q does not support disabling reasoning", model.ID)
	}
	if !model.Reasoning {
		return fmt.Errorf("model %q does not support reasoning", model.ID)
	}
	for _, supported := range model.ReasoningLevels {
		if supported == level {
			return nil
		}
	}
	return fmt.Errorf("model %q does not support reasoning level %q", model.ID, level)
}

func (m *Manager) rejectReservation(sessionID, runID string, cause error) error {
	if !identifier.Valid(runID, "run_") {
		return cause
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, cleanupErr := m.store.AbortReservedParentRun(
		cleanupContext, sessionID, runID, "prompt request was rejected",
	)
	cancel()
	if cleanupErr != nil && !errors.Is(cleanupErr, ErrNotFound) {
		return errors.Join(cause, cleanupErr)
	}
	return cause
}

func (m *Manager) finishAfterFailure(
	sessionID, turnID, runID string,
	status RunStatus,
	cause error,
) {
	finishContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.store.FinishParentRun(finishContext, sessionID, turnID, runID, status, cause.Error())
}
