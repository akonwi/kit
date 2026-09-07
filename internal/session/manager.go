// Package session owns Kit's session registry and live droid harnesses.
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
	"github.com/akonwi/kit/internal/droids/sqlitestore"
	"github.com/akonwi/kit/internal/identifier"
)

var (
	ErrBusy              = errors.New("session already has an active parent run")
	ErrClosed            = errors.New("session manager is closed")
	ErrInvalidInput      = errors.New("invalid session input")
	ErrRunNotAbortable   = errors.New("run is not abortable")
	ErrBashBusy          = errors.New("bash execution is busy")
	ErrBashNotAbortable  = errors.New("bash execution is not abortable")
	ErrDroidStoreMissing = errors.New("initialized session droid store is missing")
)

// CreateInput contains metadata for a new persistent session.
type CreateInput struct {
	ID            string
	CWD           string
	Name          string
	Model         string
	ThinkingLevel string
}

// RunReservation identifies a durably admitted droid turn.
type RunReservation struct {
	SessionID string
	TurnID    string
	RunID     string
}

// ProviderErrorKind is Kit's protocol-facing provider failure class.
type ProviderErrorKind string

const (
	ProviderErrorAuthentication ProviderErrorKind = "authentication"
	ProviderErrorEntitlement    ProviderErrorKind = "entitlement"
	ProviderErrorUsageLimit     ProviderErrorKind = "usage_limit"
	ProviderErrorRateLimit      ProviderErrorKind = "rate_limit"
	ProviderErrorTransport      ProviderErrorKind = "transport"
	ProviderErrorProtocol       ProviderErrorKind = "protocol"
)

// PromptResult is the terminal projection of one droid turn.
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

type Manager struct {
	store           Repository
	providers       droids.Providers
	systemPrompt    string
	droidDirectory  string
	temporaryDroids bool
	bashContext     context.Context
	cancelBash      context.CancelCauseFunc

	mu         sync.Mutex
	runtimes   map[string]*runtime
	loading    map[string]*runtimeLoad
	closed     bool
	runs       sync.WaitGroup
	loads      sync.WaitGroup
	ops        sync.WaitGroup
	admissions sync.WaitGroup

	bashMu           sync.Mutex
	bashActive       map[string]*activeBashExecution
	bashHistory      map[string]map[string]BashExecution
	bashNextSequence map[string]int64
	bashSlots        chan struct{}
	bashRuns         sync.WaitGroup
}

type runtimeLoad struct {
	done    chan struct{}
	runtime *runtime
	err     error
}

type runtime struct {
	droid       *droids.Droid
	droidStore  *sqlitestore.Store
	cwd         string
	events      *eventLog
	eventCursor droids.EventSequence

	// admissionMu remains held for a complete parent turn. controlMu protects
	// the prompt-admission/active-binding and generation-checked abort boundary.
	admissionMu sync.Mutex
	controlMu   sync.Mutex
	stateMu     sync.Mutex
	activeRun   string
	runs        map[string]*liveRun
	recovery    *droids.ExecutionSnapshot
}

type liveRun struct {
	record         RunProjection
	result         PromptResult
	done           chan struct{}
	completeStream bool
}

type ManagerOption func(*managerOptions) error
type managerOptions struct{ droidDirectory string }

func WithDroidStoreDirectory(directory string) ManagerOption {
	return func(options *managerOptions) error {
		if strings.TrimSpace(directory) == "" {
			return fmt.Errorf("droid store directory is required")
		}
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return fmt.Errorf("resolve droid store directory: %w", err)
		}
		options.droidDirectory = absolute
		return nil
	}
}

func NewManager(store Repository, providers droids.Providers, systemPrompt string, opts ...ManagerOption) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if providers == nil {
		return nil, fmt.Errorf("droids providers are required")
	}
	options := managerOptions{}
	for _, apply := range opts {
		if apply != nil {
			if err := apply(&options); err != nil {
				return nil, err
			}
		}
	}
	temporary := false
	if options.droidDirectory == "" {
		if locator, ok := store.(interface{ DroidStoreDirectory() string }); ok {
			options.droidDirectory = locator.DroidStoreDirectory()
		}
	}
	if options.droidDirectory == "" {
		var err error
		options.droidDirectory, err = os.MkdirTemp("", "kit-droids-")
		if err != nil {
			return nil, fmt.Errorf("create temporary droid store directory: %w", err)
		}
		temporary = true
	}
	if err := os.MkdirAll(options.droidDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create droid store directory: %w", err)
	}
	bashContext, cancelBash := context.WithCancelCause(context.Background())
	return &Manager{
		store: store, providers: providers, systemPrompt: systemPrompt,
		droidDirectory: options.droidDirectory, temporaryDroids: temporary,
		bashContext: bashContext, cancelBash: cancelBash,
		runtimes: make(map[string]*runtime), loading: make(map[string]*runtimeLoad),
		bashActive: make(map[string]*activeBashExecution), bashHistory: make(map[string]map[string]BashExecution),
		bashNextSequence: make(map[string]int64),
		bashSlots:        make(chan struct{}, maxConcurrentDirectBash),
	}, nil
}

func (m *Manager) Create(ctx context.Context, input CreateInput) (SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return SessionRecord{}, err
	}
	defer m.ops.Done()
	if input.CWD == "" || !filepath.IsAbs(input.CWD) {
		return SessionRecord{}, fmt.Errorf("%w: session cwd must be absolute", ErrInvalidInput)
	}
	cwd := filepath.Clean(input.CWD)
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return SessionRecord{}, fmt.Errorf("%w: session cwd must be an existing directory", ErrInvalidInput)
	}
	model, ok := m.providers.Model(input.Model)
	if !ok {
		return SessionRecord{}, fmt.Errorf("%w: unknown model %q", ErrInvalidInput, input.Model)
	}
	if err := validateThinkingLevel(model, input.ThinkingLevel); err != nil {
		return SessionRecord{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	id := input.ID
	clientSelectedID := id != ""
	if clientSelectedID {
		if !identifier.Valid(id, "session_") {
			return SessionRecord{}, fmt.Errorf("%w: invalid session id", ErrInvalidInput)
		}
	} else {
		var err error
		id, err = identifier.New("session_")
		if err != nil {
			return SessionRecord{}, err
		}
	}
	requested := NewSession{
		ID: id, CWD: cwd, Name: strings.TrimSpace(input.Name), Persistent: true,
		ModelProvider: model.Provider, ModelID: model.ID, ThinkingLevel: input.ThinkingLevel,
	}
	record, err := m.store.CreateSession(ctx, requested)
	if err == nil || !clientSelectedID {
		return record, err
	}
	existing, loadErr := m.store.GetSession(ctx, id)
	if loadErr == nil {
		if sessionMatchesCreate(existing, requested) {
			return existing, nil
		}
		return SessionRecord{}, fmt.Errorf("%w: session id is already assigned to a different request", ErrInvalidInput)
	}
	return SessionRecord{}, err
}

func sessionMatchesCreate(record SessionRecord, input NewSession) bool {
	return record.ID == input.ID && record.CWD == input.CWD && record.Name == input.Name &&
		record.Persistent == input.Persistent && record.ParentSessionID == input.ParentSessionID &&
		record.ModelProvider == input.ModelProvider && record.ModelID == input.ModelID &&
		record.ThinkingLevel == input.ThinkingLevel && record.ArchivedAt == nil
}

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

// StartPrompt admits one droid turn and returns its canonical identity.
func (m *Manager) StartPrompt(ctx context.Context, sessionID, prompt string) (RunReservation, error) {
	if err := m.beginAdmission(); err != nil {
		return RunReservation{}, err
	}
	defer m.admissions.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(prompt) == "" {
		return RunReservation{}, fmt.Errorf("%w: prompt is empty", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return RunReservation{}, err
	}
	if !loaded.admissionMu.TryLock() {
		return RunReservation{}, ErrBusy
	}
	loaded.controlMu.Lock()
	release := func() {
		loaded.controlMu.Unlock()
		loaded.admissionMu.Unlock()
	}
	snapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		release()
		return RunReservation{}, err
	}
	if snapshot.Active != nil {
		release()
		return RunReservation{}, ErrBusy
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	if err != nil {
		release()
		return RunReservation{}, err
	}
	handle, err := loaded.droid.Prompt(ctx, droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: prompt}},
	}, droids.PromptOptions{})
	if err != nil {
		subscription.Close()
		release()
		if errors.Is(err, droids.ErrBusy) {
			return RunReservation{}, ErrBusy
		}
		return RunReservation{}, err
	}
	turnID := string(handle.TurnID())
	if err := loaded.events.reset(); err != nil {
		_ = loaded.droid.Abort(context.Background())
		subscription.Close()
		release()
		return RunReservation{}, err
	}
	run := &liveRun{record: RunProjection{
		ID: turnID, SessionID: sessionID, TurnID: turnID, Status: RunStatusRunning,
	}, done: make(chan struct{}), completeStream: true}
	loaded.stateMu.Lock()
	pruneRuns(loaded.runs, 128)
	loaded.activeRun = turnID
	loaded.runs[turnID] = run
	loaded.stateMu.Unlock()
	if err := loaded.events.append([]NewEvent{
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning},
		{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventUserMessage, Text: boundedLiveText(prompt)},
	}); err != nil {
		_ = loaded.droid.Abort(context.Background())
		subscription.Close()
		release()
		return RunReservation{}, err
	}
	loaded.controlMu.Unlock()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = loaded.droid.Abort(context.Background())
		subscription.Close()
		loaded.admissionMu.Unlock()
		return RunReservation{}, ErrClosed
	}
	m.runs.Add(1)
	m.mu.Unlock()
	go m.executePrompt(loaded, run, handle, subscription, sessionID, prompt)
	return RunReservation{SessionID: sessionID, TurnID: turnID, RunID: turnID}, nil
}

func (m *Manager) RunPrompt(ctx context.Context, sessionID, prompt string) (PromptResult, error) {
	reservation, err := m.StartPrompt(ctx, sessionID, prompt)
	if err != nil {
		return PromptResult{}, err
	}
	return m.waitRun(ctx, sessionID, reservation.RunID)
}

func (m *Manager) GetRun(ctx context.Context, sessionID, runID string) (RunProjection, error) {
	if err := m.beginOperation(); err != nil {
		return RunProjection{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return RunProjection{}, err
	}
	loaded.stateMu.Lock()
	run := loaded.runs[runID]
	if run != nil {
		record := run.record
		loaded.stateMu.Unlock()
		return record, nil
	}
	loaded.stateMu.Unlock()
	turn, err := loaded.droid.Turn(ctx, droids.TurnID(runID))
	if errors.Is(err, droids.ErrTurnNotFound) {
		return RunProjection{}, fmt.Errorf("run %q: %w", runID, ErrNotFound)
	}
	if err != nil {
		return RunProjection{}, err
	}
	record := RunProjection{
		ID: runID, SessionID: sessionID, TurnID: runID,
		Status: projectExecutionStatus(turn.Status),
	}
	if turn.Error != nil {
		record.Error = turn.Error.Message
	}
	if record.Status != RunStatusCompleted && record.Error == "" {
		record.Error = "droid execution " + string(turn.Status)
	}
	return record, nil
}

func (m *Manager) waitRun(ctx context.Context, sessionID, runID string) (PromptResult, error) {
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return PromptResult{}, err
	}
	loaded.stateMu.Lock()
	run := loaded.runs[runID]
	loaded.stateMu.Unlock()
	if run == nil {
		return PromptResult{}, fmt.Errorf("run %q: %w", runID, ErrNotFound)
	}
	select {
	case <-run.done:
		loaded.stateMu.Lock()
		result := run.result
		loaded.stateMu.Unlock()
		return result, nil
	case <-ctx.Done():
		return PromptResult{}, ctx.Err()
	}
}

func (m *Manager) executePrompt(loaded *runtime, run *liveRun, handle droids.ExecutionHandle, subscription droids.Subscription, sessionID, prompt string) {
	defer m.runs.Done()
	turnID := run.record.TurnID
	drainDone := make(chan error, 1)
	go func() { drainDone <- m.drainRunEvents(subscription, loaded, sessionID, turnID, turnID) }()
	outcome, waitErr := handle.Wait(context.Background())
	terminalState, continuationErr := continueDroidToTerminal(loaded.droid, outcome.Status)
	waitErr = errors.Join(waitErr, continuationErr)
	if terminalState.Execution != nil {
		outcome.Status = terminalState.Execution.Status
		if outcome.Error == nil {
			outcome.Error = terminalState.Execution.Error
		}
	}
	subscription.Close()
	drainErr := <-drainDone
	if drainErr != nil {
		loaded.events.invalidate()
	}
	result := promptResultFromOutcome(sessionID, turnID, outcome, errors.Join(waitErr, drainErr))

	loaded.controlMu.Lock()
	if err := loaded.events.append([]NewEvent{{
		SessionID: sessionID, TurnID: turnID, RunID: turnID,
		Kind: EventRunFinished, Status: result.Status, ErrorKind: result.ErrorKind,
		ErrorMessage: result.ErrorMessage,
	}}); err != nil {
		loaded.events.invalidate()
	}
	loaded.stateMu.Lock()
	run.record.Status = result.Status
	run.record.Error = result.ErrorMessage
	run.result = result
	if loaded.activeRun == turnID {
		loaded.activeRun = ""
	}
	close(run.done)
	loaded.stateMu.Unlock()
	loaded.controlMu.Unlock()
	loaded.admissionMu.Unlock()
}

func promptResultFromOutcome(sessionID, turnID string, outcome droids.Outcome, waitErr error) PromptResult {
	result := PromptResult{SessionID: sessionID, TurnID: turnID, RunID: turnID, Status: projectExecutionStatus(outcome.Status)}
	if outcome.FinalMessage != nil {
		if assistant, ok := outcome.FinalMessage.Message.(droids.AssistantMessage); ok {
			result.Text = assistant.Text()
			result.StopReason = string(assistant.StopReason)
			result.ErrorKind = projectDroidErrorKind(assistant.ErrorKind)
			result.ErrorMessage = assistant.ErrorMessage
		}
	}
	if outcome.Error != nil && result.ErrorMessage == "" {
		result.ErrorMessage = outcome.Error.Message
	}
	if result.Status != RunStatusCompleted && result.ErrorMessage == "" {
		if waitErr != nil {
			result.ErrorMessage = waitErr.Error()
		} else {
			result.ErrorMessage = "droid execution " + string(outcome.Status)
		}
	}
	return result
}

func continueDroidToTerminal(droid *droids.Droid, status droids.ExecutionStatus) (droids.QuiescentState, error) {
	state := droids.QuiescentState{Execution: &droids.ExecutionSnapshot{Status: status}}
	var continuationErr error
	for status == droids.ExecutionPaused || status == droids.ExecutionInterrupted {
		if err := droid.Resume(context.Background()); err != nil {
			continuationErr = errors.Join(continuationErr, err)
			if errors.Is(err, droids.ErrUnsafeContinuation) {
				if abortErr := droid.Abort(context.Background()); abortErr != nil {
					return state, errors.Join(continuationErr, abortErr)
				}
			} else if errors.Is(err, droids.ErrClosed) {
				return state, continuationErr
			} else {
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		var err error
		state, err = droid.WaitQuiescent(context.Background())
		if err != nil {
			return state, errors.Join(continuationErr, err)
		}
		if state.Execution == nil {
			return state, continuationErr
		}
		status = state.Execution.Status
	}
	return state, continuationErr
}

func projectExecutionStatus(status droids.ExecutionStatus) RunStatus {
	switch status {
	case droids.ExecutionCompleted:
		return RunStatusCompleted
	case droids.ExecutionFailed:
		return RunStatusFailed
	case droids.ExecutionAborted:
		return RunStatusAborted
	default:
		return RunStatusInterrupted
	}
}

func (m *Manager) drainRunEvents(subscription droids.Subscription, loaded *runtime, sessionID, turnID, runID string) error {
	for envelope := range subscription.Events() {
		if envelope.Durable {
			loaded.stateMu.Lock()
			if envelope.Sequence > loaded.eventCursor {
				loaded.eventCursor = envelope.Sequence
			}
			loaded.stateMu.Unlock()
		}
		if err := loaded.events.append(projectDroidEvent(sessionID, turnID, runID, envelope.Event)); err != nil {
			return err
		}
	}
	if err := subscription.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (m *Manager) Abort(ctx context.Context, sessionID, runID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	loaded.controlMu.Lock()
	defer loaded.controlMu.Unlock()
	loaded.stateMu.Lock()
	matches := loaded.activeRun == runID
	loaded.stateMu.Unlock()
	if !matches {
		return fmt.Errorf("%w: run %q is not current", ErrRunNotAbortable, runID)
	}
	if err := loaded.droid.Abort(ctx); err != nil && !errors.Is(err, droids.ErrNoActiveExecution) {
		return err
	}
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	m.closed = true
	runtimes := make([]*runtime, 0, len(m.runtimes))
	for _, loaded := range m.runtimes {
		runtimes = append(runtimes, loaded)
	}
	m.runtimes = nil
	m.mu.Unlock()
	m.cancelBash(errBashShutdown)
	var shutdownErr error
	for _, loaded := range runtimes {
		shutdownErr = errors.Join(shutdownErr, loaded.droid.Shutdown(ctx))
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
		for _, loaded := range runtimes {
			shutdownErr = errors.Join(shutdownErr, loaded.droidStore.Close())
		}
		if m.temporaryDroids {
			shutdownErr = errors.Join(shutdownErr, os.RemoveAll(m.droidDirectory))
		}
		return shutdownErr
	case <-ctx.Done():
		return errors.Join(shutdownErr, ctx.Err())
	}
}

func (m *Manager) Close() { _ = m.Shutdown(context.Background()) }

func (m *Manager) runtime(ctx context.Context, sessionID string) (*runtime, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	if loaded := m.runtimes[sessionID]; loaded != nil {
		m.mu.Unlock()
		return loaded, nil
	}
	if pending := m.loading[sessionID]; pending != nil {
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
	startRecovery := false
	if m.closed {
		if loaded != nil {
			_ = loaded.close(context.Background())
		}
		loaded, err = nil, ErrClosed
	} else if err == nil {
		m.runtimes[sessionID] = loaded
		startRecovery = loaded.recovery != nil
		if startRecovery {
			loaded.admissionMu.Lock()
			m.runs.Add(1)
		}
	}
	pending.runtime, pending.err = loaded, err
	close(pending.done)
	m.mu.Unlock()
	if startRecovery {
		go m.resumeRuntime(loaded, sessionID)
	}
	m.loads.Done()
	return loaded, err
}

func (m *Manager) loadRuntime(ctx context.Context, sessionID string) (*runtime, error) {
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return m.newDroid(ctx, record)
}

func (m *Manager) newDroid(ctx context.Context, record SessionRecord) (*runtime, error) {
	if !identifier.Valid(record.ID, "session_") {
		return nil, fmt.Errorf("session %q has an invalid droid identity", record.ID)
	}
	path := filepath.Join(m.droidDirectory, record.ID+".db")
	if record.DroidInitializedAt != nil {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%w: %s", ErrDroidStoreMissing, record.ID)
			}
			return nil, err
		}
	}
	store, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: path})
	if err != nil {
		return nil, fmt.Errorf("open droid store for session %q: %w", record.ID, err)
	}
	droid, err := droids.Open(ctx, droids.ConversationID(record.ID), droids.Config{
		Store: store, Providers: m.providers,
		Model:     record.ModelProvider + "/" + record.ModelID,
		Reasoning: record.ThinkingLevel, SystemPrompt: m.systemPrompt,
		Tools: codingtools.New(record.CWD),
	})
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("open runtime for session %q: %w", record.ID, err)
	}
	if record.DroidInitializedAt == nil {
		if err := m.store.MarkDroidInitialized(ctx, record.ID, time.Now().UTC()); err != nil {
			_ = droid.Close()
			_ = store.Close()
			return nil, err
		}
	}
	events, err := newEventLog()
	if err != nil {
		_ = droid.Close()
		_ = store.Close()
		return nil, err
	}
	snapshot, err := droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		_ = droid.Close()
		_ = store.Close()
		return nil, err
	}
	loaded := &runtime{
		droid: droid, droidStore: store, cwd: record.CWD, events: events, eventCursor: snapshot.LastEvent,
		runs: make(map[string]*liveRun),
	}
	quiescent, err := droid.WaitQuiescent(ctx)
	if err != nil {
		_ = loaded.close(context.Background())
		return nil, err
	}
	if quiescent.Execution != nil && (quiescent.Execution.Status == droids.ExecutionPaused || quiescent.Execution.Status == droids.ExecutionInterrupted) {
		turnID := string(quiescent.TurnID)
		run := &liveRun{record: RunProjection{ID: turnID, SessionID: record.ID, TurnID: turnID, Status: RunStatusRunning}, done: make(chan struct{})}
		loaded.activeRun = turnID
		loaded.runs[turnID] = run
		copy := *quiescent.Execution
		loaded.recovery = &copy
		_ = loaded.events.append([]NewEvent{{SessionID: record.ID, TurnID: turnID, RunID: turnID, Kind: EventRunStarted, Status: RunStatusRunning}})
		loaded.events.invalidate()
	}
	return loaded, nil
}

func (m *Manager) resumeRuntime(loaded *runtime, sessionID string) {
	defer m.runs.Done()
	loaded.stateMu.Lock()
	turnID := loaded.activeRun
	run := loaded.runs[turnID]
	loaded.stateMu.Unlock()
	if run == nil {
		loaded.admissionMu.Unlock()
		return
	}
	subscription, err := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{After: loaded.eventCursor, IncludeTransient: true, Buffer: 256})
	if err != nil {
		abortErr := loaded.droid.Abort(context.Background())
		state, waitErr := loaded.droid.WaitQuiescent(context.Background())
		status := RunStatusInterrupted
		if abortErr == nil && waitErr == nil && state.Execution != nil {
			status = projectExecutionStatus(state.Execution.Status)
		}
		result := PromptResult{
			SessionID: sessionID, TurnID: turnID, RunID: turnID, Status: status,
			ErrorMessage: "resume event subscription: " + errors.Join(err, abortErr, waitErr).Error(),
		}
		loaded.controlMu.Lock()
		loaded.events.invalidate()
		loaded.stateMu.Lock()
		run.record.Status, run.record.Error, run.result = result.Status, result.ErrorMessage, result
		loaded.activeRun = ""
		close(run.done)
		loaded.stateMu.Unlock()
		loaded.controlMu.Unlock()
		loaded.admissionMu.Unlock()
		return
	}
	drainDone := make(chan error, 1)
	go func() { drainDone <- m.drainRunEvents(subscription, loaded, sessionID, turnID, turnID) }()
	state, waitErr := continueDroidToTerminal(loaded.droid, droids.ExecutionInterrupted)
	subscription.Close()
	drainErr := <-drainDone
	if drainErr != nil {
		loaded.events.invalidate()
	}
	outcome := droids.Outcome{Status: droids.ExecutionInterrupted}
	if state.Execution != nil {
		outcome.Status, outcome.Error = state.Execution.Status, state.Execution.Error
	}
	result := promptResultFromOutcome(sessionID, turnID, outcome, errors.Join(waitErr, drainErr))
	loaded.controlMu.Lock()
	if err := loaded.events.append([]NewEvent{{SessionID: sessionID, TurnID: turnID, RunID: turnID, Kind: EventRunFinished, Status: result.Status, ErrorKind: result.ErrorKind, ErrorMessage: result.ErrorMessage}}); err != nil {
		loaded.events.invalidate()
	}
	loaded.stateMu.Lock()
	run.record.Status, run.record.Error, run.result = result.Status, result.ErrorMessage, result
	loaded.activeRun = ""
	close(run.done)
	loaded.stateMu.Unlock()
	loaded.controlMu.Unlock()
	loaded.admissionMu.Unlock()
}

func (r *runtime) close(ctx context.Context) error {
	return errors.Join(r.droid.Shutdown(ctx), r.droidStore.Close())
}

func pruneRuns(runs map[string]*liveRun, limit int) {
	for len(runs) >= limit {
		removed := false
		for id, run := range runs {
			if run.record.Status != RunStatusRunning {
				delete(runs, id)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
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
