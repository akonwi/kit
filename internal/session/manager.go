// Package session owns persisted Kit session runtimes above droids.
package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	droid         *droids.Droid
	droidStore    *sqlitestore.Store
	boundaries    *boundaryTracker
	eventCursor   droids.EventSequence
	historyCursor uint64
	admissionMu   sync.Mutex
	stateMu       sync.Mutex
	activeRun     string
	phase         runPhase
	recovery      *ParentRunRecord
}

// ManagerOption configures session runtime ownership.
type ManagerOption func(*managerOptions) error

type managerOptions struct {
	droidDirectory string
}

// WithDroidStoreDirectory selects the private directory containing one SQLite
// database per droid conversation.
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

// NewManager creates a session runtime manager.
func NewManager(store Repository, providers droids.Providers, systemPrompt string, opts ...ManagerOption) (*Manager, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if providers == nil {
		return nil, fmt.Errorf("droids providers are required")
	}
	options := managerOptions{}
	for _, apply := range opts {
		if apply == nil {
			continue
		}
		if err := apply(&options); err != nil {
			return nil, err
		}
	}
	temporaryDroids := false
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
		temporaryDroids = true
	}
	if err := os.MkdirAll(options.droidDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create droid store directory: %w", err)
	}
	bashContext, cancelBash := context.WithCancelCause(context.Background())
	return &Manager{
		store: store, providers: providers, systemPrompt: systemPrompt,
		droidDirectory: options.droidDirectory, temporaryDroids: temporaryDroids,
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
	if _, err := m.runtime(ctx, sessionID); err != nil {
		return ParentRunRecord{}, err
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
	if _, err := m.runtime(ctx, sessionID); err != nil {
		return RunReservation{}, err
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
	if !loaded.admissionMu.TryLock() {
		return PromptResult{}, m.rejectReservation(sessionID, runID, ErrBusy)
	}
	droidSnapshot, err := loaded.droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		loaded.admissionMu.Unlock()
		return PromptResult{}, m.rejectReservation(sessionID, runID, err)
	}
	if droidSnapshot.Active != nil {
		loaded.admissionMu.Unlock()
		return PromptResult{}, m.rejectReservation(sessionID, runID, ErrBusy)
	}
	reservedRun, err := m.store.GetParentRun(ctx, sessionID, runID)
	if err != nil {
		loaded.admissionMu.Unlock()
		return PromptResult{}, m.rejectReservation(sessionID, runID, err)
	}
	if reservedRun.Status == RunStatusAborted {
		loaded.admissionMu.Unlock()
		return PromptResult{
			SessionID: sessionID, TurnID: reservedRun.TurnID, RunID: runID,
			Status: RunStatusAborted, ErrorMessage: "aborted before execution",
		}, nil
	}
	if reservedRun.Status != RunStatusQueued {
		loaded.admissionMu.Unlock()
		return PromptResult{}, fmt.Errorf("run %q cannot start from status %q", runID, reservedRun.Status)
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
				loaded.admissionMu.Unlock()
				return PromptResult{}, m.rejectReservation(sessionID, runID, errors.Join(err, inspectErr))
			case claimed >= 0:
				latestBashContext = claimed
			case unclaimed < 0:
				latestBashContext = -1
			default:
				loaded.admissionMu.Unlock()
				return PromptResult{}, m.rejectReservation(sessionID, runID, err)
			}
		}
	}
	if latestBashContext > loaded.boundaries.bashContextSequence() {
		if err := m.syncBashContext(ctx, loaded, latestBashContext); err != nil {
			loaded.admissionMu.Unlock()
			return PromptResult{}, m.rejectReservation(sessionID, runID, err)
		}
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		loaded.admissionMu.Unlock()
		return PromptResult{}, m.rejectReservation(sessionID, runID, ErrClosed)
	}
	m.runs.Add(1)
	m.mu.Unlock()

	// Subscribe before droid admission so immediate provider output is buffered
	// until the Kit projection is ready to consume it.
	subscription, subscriptionErr := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	handle, promptErr := loaded.droid.Prompt(ctx, droids.Input{
		Content: []droids.InputContent{droids.TextInput{Text: prompt}},
	}, droids.PromptOptions{})
	if promptErr != nil {
		loaded.admissionMu.Unlock()
		if subscription != nil {
			subscription.Close()
		}
		m.runs.Done()
		if errors.Is(promptErr, droids.ErrBusy) {
			promptErr = ErrBusy
		}
		return PromptResult{}, m.rejectReservation(sessionID, runID, promptErr)
	}

	projectionContext, cancelProjection := context.WithTimeout(context.Background(), 5*time.Second)
	turnID, startStatus, err := m.store.StartReservedParentRun(projectionContext, sessionID, runID, string(handle.TurnID()))
	cancelProjection()
	if err != nil {
		inspectContext, cancelInspect := context.WithTimeout(context.Background(), 5*time.Second)
		projected, inspectErr := m.store.GetParentRun(inspectContext, sessionID, runID)
		cancelInspect()
		if inspectErr == nil && projected.Status == RunStatusRunning && projected.DroidTurnID == string(handle.TurnID()) {
			turnID, startStatus, err = projected.TurnID, projected.Status, nil
		} else if inspectErr != nil {
			err = errors.Join(err, inspectErr)
		}
	}
	if err != nil || startStatus != RunStatusRunning {
		abortContext, cancelAbort := context.WithTimeout(context.Background(), 5*time.Second)
		abortErr := loaded.droid.Abort(abortContext)
		_, waitErr := handle.Wait(abortContext)
		cancelAbort()
		if subscription != nil {
			subscription.Close()
		}
		m.evict(sessionID, loaded)
		m.runs.Done()
		loaded.admissionMu.Unlock()
		if err != nil {
			return PromptResult{}, errors.Join(err, abortErr, waitErr)
		}
		if startStatus == RunStatusAborted {
			return PromptResult{
				SessionID: sessionID, TurnID: turnID, RunID: runID,
				Status: RunStatusAborted, ErrorMessage: "aborted before projection started",
			}, nil
		}
		return PromptResult{}, errors.Join(fmt.Errorf("run %q cannot start from status %q", runID, startStatus), abortErr, waitErr)
	}
	loaded.setActive(runPhaseStarting, runID)

	completion := make(chan promptCompletion, 1)
	loaded.setPhase(runPhaseRunning)
	go func() {
		result, err := m.executePrompt(loaded, handle, subscription, subscriptionErr, sessionID, turnID, runID, prompt)
		loaded.clearActive()
		loaded.admissionMu.Unlock()
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
	loaded *runtime,
	handle droids.ExecutionHandle,
	subscription droids.Subscription,
	subscriptionErr error,
	sessionID, turnID, runID, prompt string,
) (PromptResult, error) {
	initialEvents := []NewEvent{
		{SessionID: sessionID, TurnID: turnID, RunID: runID, Kind: EventRunStarted, Status: RunStatusRunning},
		{SessionID: sessionID, TurnID: turnID, RunID: runID, Kind: EventUserMessage, Text: boundedLiveText(prompt)},
	}
	eventErr := subscriptionErr
	if err := m.appendLiveEvents(initialEvents); eventErr == nil {
		eventErr = err
	}

	drainDone := make(chan error, 1)
	if subscription != nil {
		go func() {
			drainDone <- m.drainRunEvents(subscription, loaded, sessionID, turnID, runID, eventErr == nil)
		}()
	} else {
		drainDone <- nil
	}

	outcome, waitErr := handle.Wait(context.Background())
	terminalState, continuationErr := continueDroidToTerminal(loaded.droid, outcome.Status)
	waitErr = errors.Join(waitErr, continuationErr)
	if terminalState.Execution != nil {
		outcome.Status = terminalState.Execution.Status
		if outcome.Error == nil {
			outcome.Error = terminalState.Execution.Error
		}
	}
	if subscription != nil {
		subscription.Close()
	}
	if drainErr := <-drainDone; eventErr == nil {
		eventErr = drainErr
	}
	loaded.setPhase(runPhaseSettling)
	latestAssistant, projectionErr := m.projectDroidHistory(context.Background(), loaded, sessionID, turnID, handle.TurnID())

	status := projectExecutionStatus(outcome.Status)
	result := PromptResult{
		SessionID: sessionID, TurnID: turnID, RunID: runID,
		Status: status,
	}
	var finalAssistant *droids.AssistantMessage
	if outcome.FinalMessage != nil {
		if assistant, ok := outcome.FinalMessage.Message.(droids.AssistantMessage); ok {
			finalAssistant = &assistant
		}
	}
	if finalAssistant == nil {
		finalAssistant = latestAssistant
	}
	if finalAssistant != nil {
		result.Text = finalAssistant.Text()
		result.StopReason = string(finalAssistant.StopReason)
		result.ErrorKind = projectDroidErrorKind(finalAssistant.ErrorKind)
		result.ErrorMessage = finalAssistant.ErrorMessage
	}
	if outcome.Error != nil && result.ErrorMessage == "" {
		result.ErrorMessage = outcome.Error.Message
	}
	executionErr := waitErr
	if executionErr != nil && result.Status != RunStatusCompleted && result.ErrorMessage == "" {
		result.ErrorMessage = executionErr.Error()
	}
	if result.Status != RunStatusCompleted && result.ErrorMessage == "" {
		result.ErrorMessage = "droid execution " + string(outcome.Status)
	}
	if outcome.Status == droids.ExecutionInterrupted && errors.Is(waitErr, droids.ErrClosed) {
		return result, waitErr
	}
	result, finishErr := m.finishDroidProjection(sessionID, turnID, runID, result, executionErr)
	if finishErr != nil {
		m.evict(sessionID, loaded)
	}
	if eventErr == nil && projectionErr == nil && finishErr == nil {
		_ = m.appendLiveEvents([]NewEvent{{
			SessionID: sessionID, TurnID: turnID, RunID: runID,
			Kind: EventRunFinished, Status: result.Status, ErrorKind: result.ErrorKind,
			ErrorMessage: result.ErrorMessage,
		}})
	}
	return result, errors.Join(projectionErr, finishErr)
}

func continueDroidToTerminal(droid *droids.Droid, status droids.ExecutionStatus) (droids.QuiescentState, error) {
	state := droids.QuiescentState{
		TurnID:    droids.TurnID(""),
		Execution: &droids.ExecutionSnapshot{Status: status},
	}
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

func terminalRunStatus(status RunStatus) bool {
	switch status {
	case RunStatusCompleted, RunStatusFailed, RunStatusAborted, RunStatusInterrupted:
		return true
	default:
		return false
	}
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

func (m *Manager) finishDroidProjection(
	sessionID, turnID, runID string,
	result PromptResult,
	executionErr error,
) (PromptResult, error) {
	finish := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return m.store.FinishParentRun(ctx, sessionID, turnID, runID, result.Status, result.ErrorMessage)
	}
	finishErr := finish()
	if finishErr == nil {
		return result, nil
	}
	inspect := func() (ParentRunRecord, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return m.store.GetParentRun(ctx, sessionID, runID)
	}
	record, inspectErr := inspect()
	if inspectErr == nil && terminalRunStatus(record.Status) && record.Status == result.Status {
		result.Status = record.Status
		result.ErrorMessage = record.Error
		return result, nil
	}
	retryErr := finish()
	if retryErr == nil {
		return result, nil
	}
	record, finalInspectErr := inspect()
	if finalInspectErr == nil && terminalRunStatus(record.Status) && record.Status == result.Status {
		result.Status = record.Status
		result.ErrorMessage = record.Error
		return result, nil
	}
	return result, errors.Join(executionErr, finishErr, inspectErr, retryErr, finalInspectErr)
}

func (m *Manager) projectDroidHistory(
	ctx context.Context,
	loaded *runtime,
	sessionID, _ string,
	targetTurnID droids.TurnID,
) (*droids.AssistantMessage, error) {
	runs, err := m.store.ListParentRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	kitTurns := make(map[droids.TurnID]string, len(runs))
	for _, run := range runs {
		if run.DroidTurnID != "" {
			kitTurns[droids.TurnID(run.DroidTurnID)] = run.TurnID
		}
	}
	cursor := loaded.historyCursor
	var latestAssistant *droids.AssistantMessage
	for {
		page, err := loaded.droid.History(ctx, droids.HistoryQuery{After: cursor, Limit: 1000})
		if err != nil {
			return nil, err
		}
		byTurn := make(map[droids.TurnID][]NewMessageRecord)
		var turnOrder []droids.TurnID
		for _, envelope := range page.Messages {
			kitTurnID := kitTurns[envelope.TurnID]
			if kitTurnID == "" {
				return nil, fmt.Errorf("droid turn %q has no Kit projection mapping", envelope.TurnID)
			}
			if _, boundary := envelope.Message.(droids.ContextMessage); boundary {
				continue
			}
			if _, found := byTurn[envelope.TurnID]; !found {
				turnOrder = append(turnOrder, envelope.TurnID)
			}
			if envelope.TurnID == targetTurnID {
				if assistant, ok := envelope.Message.(droids.AssistantMessage); ok {
					copy := assistant
					latestAssistant = &copy
				}
			}
			role, payload, createdAt, err := encodeDroidMessage(envelope.Message)
			if err != nil {
				return nil, fmt.Errorf("encode droid message %q: %w", envelope.ID, err)
			}
			byTurn[envelope.TurnID] = append(byTurn[envelope.TurnID], NewMessageRecord{
				ID: string(envelope.ID), Role: role, PayloadJSON: payload, CreatedAt: createdAt,
			})
		}
		for _, droidTurnID := range turnOrder {
			if _, err := m.store.ProjectDroidMessages(ctx, sessionID, kitTurns[droidTurnID], byTurn[droidTurnID]); err != nil {
				return nil, err
			}
		}
		cursor = page.Next
		loaded.historyCursor = cursor
		if !page.HasMore {
			return latestAssistant, nil
		}
	}
}

func (m *Manager) drainRunEvents(subscription droids.Subscription, loaded *runtime, sessionID, turnID, runID string, persist bool) error {
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
	events := subscription.Events()
	for {
		select {
		case envelope, ok := <-events:
			if !ok {
				flush()
				if err := subscription.Err(); firstErr == nil && err != nil && !errors.Is(err, context.Canceled) {
					firstErr = err
				}
				return firstErr
			}
			if envelope.Durable && envelope.Sequence > loaded.eventCursor {
				loaded.eventCursor = envelope.Sequence
			}
			for _, projected := range projectDroidEvent(sessionID, turnID, runID, envelope.Event) {
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
	var shutdownErr error
	for _, loaded := range runtimes {
		shutdownErr = errors.Join(shutdownErr, loaded.droid.Shutdown(ctx))
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
		for _, loaded := range runtimes {
			shutdownErr = errors.Join(shutdownErr, loaded.droidStore.Close())
		}
		if m.temporaryDroids {
			shutdownErr = errors.Join(shutdownErr, os.RemoveAll(m.droidDirectory))
		}
		return shutdownErr
	case <-ctx.Done():
		return errors.Join(shutdownErr, fmt.Errorf("wait for session runs to stop: %w", ctx.Err()))
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

func (r *runtime) close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	shutdownErr := r.droid.Shutdown(ctx)
	storeErr := r.droidStore.Close()
	return errors.Join(shutdownErr, storeErr)
}

func (r *runtime) setActive(phase runPhase, runID string) {
	r.stateMu.Lock()
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
	r.activeRun = ""
	r.phase = runPhaseIdle
	r.stateMu.Unlock()
}

func (r *runtime) abort(expectedRunID string) bool {
	r.stateMu.Lock()
	abortable := r.phase == runPhaseStarting || r.phase == runPhaseRunning
	matches := r.activeRun == expectedRunID && abortable
	r.stateMu.Unlock()
	if !matches {
		return false
	}
	return r.droid.Abort(context.Background()) == nil
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
	startRecovery := false
	delete(m.loading, sessionID)
	if m.closed {
		if loaded != nil {
			_ = loaded.close(context.Background())
		}
		loaded = nil
		err = ErrClosed
	} else if err == nil {
		m.runtimes[sessionID] = loaded
		if loaded.recovery != nil {
			loaded.admissionMu.Lock()
			m.runs.Add(1)
			loaded.setActive(runPhaseRunning, loaded.recovery.ID)
			startRecovery = true
		}
	}
	pending.runtime = loaded
	pending.err = err
	close(pending.done)
	m.mu.Unlock()
	if startRecovery {
		go m.resumeRuntime(loaded, *loaded.recovery)
	}
	m.loads.Done()
	return loaded, err
}

func (m *Manager) resumeRuntime(loaded *runtime, run ParentRunRecord) {
	defer m.runs.Done()
	defer loaded.admissionMu.Unlock()
	defer loaded.clearActive()
	loaded.recovery = nil

	subscription, subscriptionErr := loaded.droid.Subscribe(context.Background(), droids.SubscribeOptions{
		After: loaded.eventCursor, IncludeTransient: true, Buffer: 256,
	})
	drainDone := make(chan error, 1)
	if subscription != nil {
		go func() {
			drainDone <- m.drainRunEvents(subscription, loaded, run.SessionID, run.TurnID, run.ID, subscriptionErr == nil)
		}()
	} else {
		drainDone <- nil
	}

	state, waitErr := continueDroidToTerminal(loaded.droid, droids.ExecutionInterrupted)
	if subscription != nil {
		subscription.Close()
	}
	drainErr := <-drainDone
	latestAssistant, projectionErr := m.projectDroidHistory(
		context.Background(), loaded, run.SessionID, run.TurnID, droids.TurnID(run.DroidTurnID),
	)

	status := RunStatusInterrupted
	if state.Execution != nil {
		status = projectExecutionStatus(state.Execution.Status)
	}
	result := PromptResult{SessionID: run.SessionID, TurnID: run.TurnID, RunID: run.ID, Status: status}
	if latestAssistant != nil {
		result.Text = latestAssistant.Text()
		result.StopReason = string(latestAssistant.StopReason)
		result.ErrorKind = projectDroidErrorKind(latestAssistant.ErrorKind)
		result.ErrorMessage = latestAssistant.ErrorMessage
	}
	failure := errors.Join(waitErr, drainErr, projectionErr)
	if state.Execution != nil && state.Execution.Status == droids.ExecutionInterrupted && errors.Is(waitErr, droids.ErrClosed) {
		return
	}
	if result.Status != RunStatusCompleted && result.ErrorMessage == "" {
		if failure != nil {
			result.ErrorMessage = failure.Error()
		} else if state.Execution != nil {
			result.ErrorMessage = "droid execution " + string(state.Execution.Status)
		} else {
			result.ErrorMessage = "droid execution interrupted"
		}
	}
	result, finishErr := m.finishDroidProjection(run.SessionID, run.TurnID, run.ID, result, failure)
	if subscriptionErr == nil && drainErr == nil {
		_ = m.appendLiveEvents([]NewEvent{{
			SessionID: run.SessionID, TurnID: run.TurnID, RunID: run.ID,
			Kind: EventRunFinished, Status: result.Status, ErrorKind: result.ErrorKind,
			ErrorMessage: result.ErrorMessage,
		}})
	}
	if finishErr != nil {
		m.evict(run.SessionID, loaded)
	}
}

func (m *Manager) loadRuntime(ctx context.Context, sessionID string) (*runtime, error) {
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return m.newDroid(ctx, record)
}

func (m *Manager) syncBashContext(ctx context.Context, loaded *runtime, through int64) error {
	boundaries, err := loaded.boundaries.bashBoundaries(ctx, loaded.boundaries.bashContextSequence(), through)
	if err != nil {
		return err
	}
	fresh := make([]sequencedBoundary, 0, len(boundaries))
	for _, boundary := range boundaries {
		received, err := loaded.droid.BoundaryReceived(ctx, boundary.message.ID)
		if err != nil {
			return err
		}
		if received {
			loaded.boundaries.markBashContextSequence(boundary.sequence)
			continue
		}
		fresh = append(fresh, boundary)
	}
	if len(fresh) == 0 {
		return nil
	}
	hash := sha256.New()
	receipts := make([]string, 0, len(fresh))
	content := make([]droids.InputContent, 0, len(fresh))
	for _, boundary := range fresh {
		receipts = append(receipts, boundary.message.ID)
		_, _ = hash.Write([]byte(boundary.message.ID))
		_, _ = hash.Write([]byte{0})
		content = append(content, boundary.message.Content...)
	}
	batch := droids.BoundaryMessage{
		ID: "bash_batch_" + hex.EncodeToString(hash.Sum(nil)), ReceiptIDs: receipts,
		Kind: "bash", Source: "composer", Content: content,
	}
	if err := loaded.droid.Inform(ctx, batch); err != nil {
		return fmt.Errorf("inform droid of %d bash executions: %w", len(fresh), err)
	}
	loaded.boundaries.markBashContextSequence(fresh[len(fresh)-1].sequence)
	return nil
}

func (m *Manager) newDroid(ctx context.Context, record SessionRecord) (*runtime, error) {
	if !identifier.Valid(record.ID, "session_") {
		return nil, fmt.Errorf("session %q has an invalid droid identity", record.ID)
	}
	droidPath := filepath.Join(m.droidDirectory, record.ID+".db")
	droidStore, err := sqlitestore.Open(ctx, sqlitestore.Options{Path: droidPath})
	if err != nil {
		return nil, fmt.Errorf("open droid store for session %q: %w", record.ID, err)
	}
	droid, err := droids.Open(ctx, droids.ConversationID(record.ID), droids.Config{
		Store: droidStore, Providers: m.providers,
		Model:     record.ModelProvider + "/" + record.ModelID,
		Reasoning: record.ThinkingLevel, SystemPrompt: m.systemPrompt,
		Tools: codingtools.New(record.CWD),
	})
	if err != nil {
		_ = droidStore.Close()
		return nil, fmt.Errorf("open runtime for session %q: %w", record.ID, err)
	}
	quiescent, err := droid.WaitQuiescent(ctx)
	if err != nil {
		_ = droid.Close()
		_ = droidStore.Close()
		return nil, fmt.Errorf("inspect droid state for session %q: %w", record.ID, err)
	}
	if err := m.bindDroidProjection(ctx, record.ID, quiescent); err != nil {
		_ = droid.Close()
		_ = droidStore.Close()
		return nil, err
	}
	historyCursor, err := m.reconcileDroidHistory(ctx, droid, record.ID)
	if err != nil {
		_ = droid.Close()
		_ = droidStore.Close()
		return nil, fmt.Errorf("inspect runtime history for session %q: %w", record.ID, err)
	}
	loaded := &runtime{
		droid: droid, droidStore: droidStore,
		boundaries: newBoundaryTracker(m.store, record.ID), historyCursor: historyCursor,
	}
	if err := m.syncBashContext(ctx, loaded, int64(^uint64(0)>>1)); err != nil {
		_ = droid.Close()
		_ = droidStore.Close()
		return nil, err
	}
	snapshot, err := droid.Snapshot(ctx, droids.SnapshotOptions{RecentMessageLimit: 1})
	if err != nil {
		_ = droid.Close()
		_ = droidStore.Close()
		return nil, fmt.Errorf("snapshot runtime for session %q: %w", record.ID, err)
	}
	loaded.eventCursor = snapshot.LastEvent
	if quiescent.Execution != nil {
		runs, err := m.store.ListParentRuns(ctx, record.ID)
		if err != nil {
			_ = loaded.close(context.Background())
			return nil, err
		}
		for index := range runs {
			run := runs[index]
			if run.DroidTurnID != string(quiescent.TurnID) || run.Status != RunStatusRunning {
				continue
			}
			switch quiescent.Execution.Status {
			case droids.ExecutionPaused, droids.ExecutionInterrupted:
				loaded.recovery = &run
			case droids.ExecutionCompleted, droids.ExecutionFailed, droids.ExecutionAborted:
				result := PromptResult{
					SessionID: record.ID, TurnID: run.TurnID, RunID: run.ID,
					Status: projectExecutionStatus(quiescent.Execution.Status),
				}
				if quiescent.Execution.Error != nil {
					result.ErrorMessage = quiescent.Execution.Error.Message
				}
				if result.Status != RunStatusCompleted && result.ErrorMessage == "" {
					result.ErrorMessage = "droid execution " + string(quiescent.Execution.Status)
				}
				if _, err := m.finishDroidProjection(record.ID, run.TurnID, run.ID, result, nil); err != nil {
					_ = loaded.close(context.Background())
					return nil, err
				}
			}
			break
		}
	}
	return loaded, nil
}

func (m *Manager) bindDroidProjection(ctx context.Context, sessionID string, state droids.QuiescentState) error {
	if state.Execution == nil || state.TurnID == "" {
		return nil
	}
	runs, err := m.store.ListParentRuns(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.DroidTurnID == string(state.TurnID) {
			return nil
		}
	}
	for index := len(runs) - 1; index >= 0; index-- {
		run := runs[index]
		if run.DroidTurnID != "" || (run.Status != RunStatusQueued && run.Status != RunStatusInterrupted) {
			continue
		}
		if _, status, err := m.store.StartReservedParentRun(ctx, sessionID, run.ID, string(state.TurnID)); err != nil {
			return fmt.Errorf("repair droid turn projection for run %q: %w", run.ID, err)
		} else if status != RunStatusRunning {
			return fmt.Errorf("repair droid turn projection for run %q returned %q", run.ID, status)
		}
		return nil
	}
	return nil
}

func (m *Manager) reconcileDroidHistory(ctx context.Context, droid *droids.Droid, sessionID string) (uint64, error) {
	runs, err := m.store.ListParentRuns(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	kitTurns := make(map[droids.TurnID]string, len(runs))
	for _, run := range runs {
		if run.DroidTurnID != "" {
			kitTurns[droids.TurnID(run.DroidTurnID)] = run.TurnID
		}
	}
	var cursor uint64
	for {
		page, err := droid.History(ctx, droids.HistoryQuery{After: cursor, Limit: 1000})
		if err != nil {
			return 0, err
		}
		byTurn := make(map[droids.TurnID][]NewMessageRecord)
		var turnOrder []droids.TurnID
		for _, envelope := range page.Messages {
			kitTurnID := kitTurns[envelope.TurnID]
			if kitTurnID == "" {
				continue
			}
			if _, boundary := envelope.Message.(droids.ContextMessage); boundary {
				continue
			}
			if _, found := byTurn[envelope.TurnID]; !found {
				turnOrder = append(turnOrder, envelope.TurnID)
			}
			role, payload, createdAt, err := encodeDroidMessage(envelope.Message)
			if err != nil {
				return 0, fmt.Errorf("encode droid message %q: %w", envelope.ID, err)
			}
			byTurn[envelope.TurnID] = append(byTurn[envelope.TurnID], NewMessageRecord{
				ID: string(envelope.ID), Role: role, PayloadJSON: payload, CreatedAt: createdAt,
			})
		}
		for _, droidTurnID := range turnOrder {
			if _, err := m.store.ProjectDroidMessages(ctx, sessionID, kitTurns[droidTurnID], byTurn[droidTurnID]); err != nil {
				return 0, err
			}
		}
		cursor = page.Next
		if !page.HasMore {
			return cursor, nil
		}
	}
}

func (m *Manager) evict(sessionID string, target *runtime) {
	// Keep the closed/closing runtime as a tombstone so another caller cannot
	// open the same per-session SQLite database while workers are quiescing.
	_ = target.close(context.Background())
	m.mu.Lock()
	if current := m.runtimes[sessionID]; current == target {
		delete(m.runtimes, sessionID)
	}
	m.mu.Unlock()
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
