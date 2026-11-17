package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/identifier"
)

const (
	maxDirectBashCommandBytes = 64 << 10
	maxConcurrentDirectBash   = 4
	// maxLiveBashOutputBytes bounds the output a bash event carries.
	maxLiveBashOutputBytes = 64 << 10
	// maxLiveBashErrorBytes bounds the error message a bash event carries.
	maxLiveBashErrorBytes = 4 << 10
	// maxSnapshotBashExecutions bounds the commands a snapshot lists.
	maxSnapshotBashExecutions = 32
)

var (
	errBashUserAbort         = errors.New("bash execution aborted by user")
	errBashShutdown          = errors.New("daemon stopped during bash execution")
	errBashTemporaryDisposed = errors.New("temporary session disposed during bash execution")
)

type BashExecutionStatus string

const (
	BashExecutionRunning     BashExecutionStatus = "running"
	BashExecutionCompleted   BashExecutionStatus = "completed"
	BashExecutionFailed      BashExecutionStatus = "failed"
	BashExecutionAborted     BashExecutionStatus = "aborted"
	BashExecutionInterrupted BashExecutionStatus = "interrupted"
)

type BashExecution struct {
	ID                 string
	SessionID          string
	Sequence           int64
	Command            string
	CWD                string
	Status             BashExecutionStatus
	Output             string
	ExitCode           *int
	ExcludeFromContext bool
	Truncated          bool
	TimedOut           bool
	ErrorMessage       string
	StartedAt          time.Time
	CompletedAt        *time.Time
}

// BashHistoryEntry is the durable session-owned representation of a settled
// direct shell execution. It is independent of whether the execution was
// informed to the droid as model context.
type BashHistoryEntry struct {
	Execution BashExecution
}

type activeBashExecution struct {
	id      string
	cancel  context.CancelCauseFunc
	runtime *runtime
	done    chan struct{}
}

func (m *Manager) StartBash(ctx context.Context, sessionID, executionID, command string, exclude bool) (BashExecution, error) {
	if err := m.beginAdmission(); err != nil {
		return BashExecution{}, err
	}
	defer m.admissions.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	command = strings.TrimSpace(command)
	if strings.TrimSpace(sessionID) == "" || !identifier.Valid(executionID, "bash_") {
		return BashExecution{}, fmt.Errorf("%w: session id and valid bash execution id are required", ErrInvalidInput)
	}
	if command == "" || len(command) > maxDirectBashCommandBytes || !utf8.ValidString(command) || strings.IndexByte(command, 0) >= 0 {
		return BashExecution{}, fmt.Errorf("%w: bash command is invalid", ErrInvalidInput)
	}
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return BashExecution{}, err
	}

	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	if m.sessionDeleting(sessionID) {
		return BashExecution{}, ErrDeleteBusy
	}
	cwd := loaded.workspace.CWD()
	m.bashMu.Lock()
	defer m.bashMu.Unlock()
	if m.bashHistoryRepository != nil && m.bashNextSequence[sessionID] == 0 {
		persisted, err := m.bashHistoryRepository.ListBashHistory(ctx, sessionID)
		if err != nil {
			return BashExecution{}, err
		}
		for _, execution := range persisted {
			if execution.Sequence >= m.bashNextSequence[sessionID] {
				m.bashNextSequence[sessionID] = execution.Sequence + 1
			}
		}
	}
	if history := m.bashHistory[sessionID]; history != nil {
		if existing, ok := history[executionID]; ok {
			if existing.Command != command || existing.ExcludeFromContext != exclude {
				return BashExecution{}, fmt.Errorf("%w: bash execution id was reused", ErrInvalidInput)
			}
			return existing, nil
		}
	}
	if m.bashHistoryRepository != nil {
		persisted, err := m.bashHistoryRepository.ListBashHistory(ctx, sessionID)
		if err != nil {
			return BashExecution{}, err
		}
		for _, execution := range persisted {
			if execution.ID != executionID {
				continue
			}
			if execution.Command != command || execution.ExcludeFromContext != exclude {
				return BashExecution{}, fmt.Errorf("%w: bash execution id was reused", ErrInvalidInput)
			}
			if m.bashHistory[sessionID] == nil {
				m.bashHistory[sessionID] = make(map[string]BashExecution)
			}
			m.bashHistory[sessionID][executionID] = execution
			return execution, nil
		}
	}
	received, err := loaded.droid.BoundaryReceived(ctx, executionID)
	if err != nil {
		return BashExecution{}, err
	}
	if received {
		return BashExecution{}, fmt.Errorf("%w: bash execution %q is already durable in droid history", ErrBashNotAbortable, executionID)
	}
	if m.bashActive[sessionID] != nil {
		return BashExecution{}, fmt.Errorf("%w: session already has an active bash execution", ErrBashBusy)
	}
	select {
	case m.bashSlots <- struct{}{}:
	case <-ctx.Done():
		return BashExecution{}, ctx.Err()
	default:
		return BashExecution{}, fmt.Errorf("%w: daemon bash capacity is exhausted", ErrBashBusy)
	}
	if m.isClosed() {
		<-m.bashSlots
		return BashExecution{}, ErrClosed
	}
	startedAt := time.Now().UTC()
	if err := m.touchSessionActivity(ctx, sessionID, startedAt); err != nil {
		<-m.bashSlots
		return BashExecution{}, err
	}
	sequence := m.bashNextSequence[sessionID]
	m.bashNextSequence[sessionID] = sequence + 1
	execution := BashExecution{
		ID: executionID, SessionID: sessionID, Sequence: sequence, Command: command, CWD: cwd,
		Status: BashExecutionRunning, ExcludeFromContext: exclude, StartedAt: startedAt,
	}
	if m.bashHistory[sessionID] == nil {
		m.bashHistory[sessionID] = make(map[string]BashExecution)
	}
	pruneBashHistory(m.bashHistory[sessionID], 64)
	m.bashHistory[sessionID][executionID] = execution
	runContext, cancel := context.WithCancelCause(m.bashContext)
	active := &activeBashExecution{id: executionID, cancel: cancel, runtime: loaded, done: make(chan struct{})}
	m.bashActive[sessionID] = active
	publishBash(loaded.events, EventBashStarted, execution)
	m.bashRuns.Add(1)
	go m.executeBash(runContext, execution, active)
	return execution, nil
}

// publishBash appends a bash event for execution to the session's live
// stream. An execution that cannot be announced invalidates the stream so
// clients resynchronize from a snapshot, which lists it.
func publishBash(events *eventLog, kind EventKind, execution BashExecution) {
	if events == nil {
		return
	}
	live := liveBashExecution(execution)
	if err := events.append([]NewEvent{{SessionID: execution.SessionID, Kind: kind, Bash: &live}}); err != nil {
		events.invalidate()
	}
}

// liveBashExecution bounds an execution for the live stream: its output and
// error message are shortened at a character boundary when they would exceed
// the event payload bound, and shortened output is marked truncated.
func liveBashExecution(execution BashExecution) BashExecution {
	execution.ExitCode = cloneInt(execution.ExitCode)
	execution.ErrorMessage = truncateUTF8(execution.ErrorMessage, maxLiveBashErrorBytes)
	budget := min(maxLiveBashOutputBytes, maxEventPayloadBytes-(execution.payloadBytes()-len(execution.Output)))
	if len(execution.Output) > budget {
		execution.Output = truncateUTF8(execution.Output, max(budget, 0))
		execution.Truncated = true
	}
	return execution
}

// payloadBytes is the execution's size against the event payload bound.
func (execution BashExecution) payloadBytes() int {
	return len(execution.ID) + len(execution.SessionID) + len(execution.Command) + len(execution.Status) +
		len(execution.Output) + len(execution.ErrorMessage) + 64
}

func (m *Manager) executeBash(ctx context.Context, execution BashExecution, active *activeBashExecution) {
	defer m.bashRuns.Done()
	defer close(active.done)
	result, runErr := codingtools.RunDirectBash(ctx, execution.Command, execution.CWD, execution.SessionID)
	codingtools.RemoveCommandOutput(result.OutputPath)
	<-m.bashSlots
	completedAt := time.Now().UTC()
	settled := execution
	settled.Status = BashExecutionCompleted
	settled.Output = strings.TrimRight(result.Output, "\r\n")
	settled.ExitCode = cloneInt(result.ExitCode)
	settled.Truncated = result.Truncated
	settled.TimedOut = result.TimedOut
	settled.CompletedAt = &completedAt
	switch {
	case errors.Is(context.Cause(ctx), errBashShutdown):
		settled.Status, settled.ErrorMessage = BashExecutionInterrupted, errBashShutdown.Error()
	case errors.Is(context.Cause(ctx), errBashTemporaryDisposed):
		settled.Status, settled.ErrorMessage = BashExecutionInterrupted, errBashTemporaryDisposed.Error()
	case ctx.Err() != nil:
		settled.Status, settled.ErrorMessage = BashExecutionAborted, errBashUserAbort.Error()
	case runErr != nil:
		settled.Status, settled.ErrorMessage = BashExecutionFailed, runErr.Error()
	}

	m.mu.Lock()
	_, temporary := m.temporary[execution.SessionID]
	m.mu.Unlock()
	historyPersisted := temporary || m.bashHistoryRepository == nil
	if !historyPersisted {
		if err := m.bashHistoryRepository.AppendBashHistory(context.Background(), BashHistoryEntry{Execution: settled}); err != nil {
			settled.Status = BashExecutionInterrupted
			settled.ErrorMessage = "persist bash history: " + err.Error()
		} else {
			historyPersisted = true
		}
	}
	informErr := error(nil)
	if historyPersisted && !settled.ExcludeFromContext && settled.Status != BashExecutionInterrupted {
		details, err := encodeBashDetails(settled)
		if err != nil {
			informErr = err
		} else {
			message := bashContextMessage(settled)
			boundary := droids.BoundaryMessage{
				ID: settled.ID, Kind: "bash", Source: "composer",
				Content: message.Content, Details: details,
			}
			for attempt := 0; ; attempt++ {
				active.runtime.mu.Lock()
				informErr = informBoundary(context.Background(), active.runtime.droid, active.runtime.events, execution.SessionID, boundary)
				active.runtime.mu.Unlock()
				if informErr == nil || m.isClosed() {
					break
				}
				time.Sleep(time.Duration(1<<min(attempt, 5)) * 50 * time.Millisecond)
			}
		}
	}
	if informErr != nil && historyPersisted {
		// The session record remains authoritative even if model-context delivery
		// is interrupted; do not rewrite the durable terminal payload here. The
		// command never reached the model, so it reads as interrupted here.
		settled.Status = BashExecutionInterrupted
		settled.ErrorMessage = "persist bash boundary: " + informErr.Error()
	}
	// The session stops being busy with the command in the same step that
	// announces it, so its bash.completed precedes the next command's
	// bash.started.
	m.bashMu.Lock()
	m.bashHistory[execution.SessionID][execution.ID] = settled
	if m.bashActive[execution.SessionID] == active {
		delete(m.bashActive, execution.SessionID)
	}
	publishBash(active.runtime.events, EventBashCompleted, settled)
	m.bashMu.Unlock()
}

// unrecordedBash reports whether the transcript doesn't record execution:
// it is running, excluded from context, or settled without joining the
// conversation.
func unrecordedBash(execution BashExecution) bool {
	return execution.Status == BashExecutionRunning || execution.ExcludeFromContext || execution.Status == BashExecutionInterrupted
}

// snapshotBashExecutionsLocked lists, in sequence order, the commands the
// transcript doesn't record that started no earlier than since (any time when
// since is zero), keeping the most recent. The running command is always
// listed, as its in-memory record, even after its settled record is stored.
// Callers hold m.bashMu.
func (m *Manager) snapshotBashExecutionsLocked(ctx context.Context, sessionID string, since time.Time) ([]BashExecution, error) {
	byID := make(map[string]BashExecution)
	if m.bashHistoryRepository != nil {
		persisted, err := m.bashHistoryRepository.RecentUnrecordedBashHistory(ctx, sessionID, since, maxSnapshotBashExecutions)
		if err != nil {
			return nil, err
		}
		for _, execution := range persisted {
			byID[execution.ID] = execution
		}
	}
	for id, execution := range m.bashHistory[sessionID] {
		if execution.Status == BashExecutionRunning || unrecordedBash(execution) && !execution.StartedAt.Before(since) {
			byID[id] = execution
		} else if _, listed := byID[id]; listed && !unrecordedBash(execution) {
			delete(byID, id)
		}
	}
	executions := make([]BashExecution, 0, len(byID))
	for _, execution := range byID {
		executions = append(executions, execution)
	}
	sort.Slice(executions, func(i, j int) bool { return executions[i].Sequence < executions[j].Sequence })
	if len(executions) > maxSnapshotBashExecutions {
		executions = executions[len(executions)-maxSnapshotBashExecutions:]
	}
	for index := range executions {
		executions[index] = liveBashExecution(executions[index])
	}
	return executions, nil
}

func (m *Manager) GetBash(ctx context.Context, sessionID, executionID string) (BashExecution, error) {
	if err := m.beginOperation(); err != nil {
		return BashExecution{}, err
	}
	defer m.ops.Done()
	m.bashMu.Lock()
	defer m.bashMu.Unlock()
	if execution, ok := m.bashHistory[sessionID][executionID]; ok {
		return execution, nil
	}
	if m.bashHistoryRepository != nil {
		persisted, err := m.bashHistoryRepository.ListBashHistory(ctx, sessionID)
		if err != nil {
			return BashExecution{}, err
		}
		for _, execution := range persisted {
			if execution.ID == executionID {
				return execution, nil
			}
		}
	}
	return BashExecution{}, fmt.Errorf("bash execution %q: %w", executionID, ErrNotFound)
}

// BashHistory returns one newest-first page of durable direct shell history. It
// does not require the session runtime to be loaded and does not fall back to
// the in-memory projection, so recall reflects persisted state across restarts.
func (m *Manager) BashHistory(ctx context.Context, sessionID string, before uint64, limit int) (BashHistoryPage, error) {
	if err := m.beginOperation(); err != nil {
		return BashHistoryPage{}, err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sessionID) == "" {
		return BashHistoryPage{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if m.bashHistoryRepository == nil {
		return BashHistoryPage{}, nil
	}
	executions, hasMore, err := m.bashHistoryRepository.BashHistoryPage(ctx, sessionID, before, limit)
	if err != nil {
		return BashHistoryPage{}, err
	}
	page := BashHistoryPage{Entries: executions, HasMore: hasMore}
	if hasMore && len(executions) > 0 {
		page.Cursor = uint64(executions[len(executions)-1].Sequence)
	}
	return page, nil
}

func (m *Manager) AbortBash(ctx context.Context, sessionID, executionID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	m.bashMu.Lock()
	defer m.bashMu.Unlock()
	active := m.bashActive[sessionID]
	if active == nil || active.id != executionID {
		return fmt.Errorf("%w: bash execution %q is not active", ErrBashNotAbortable, executionID)
	}
	active.cancel(errBashUserAbort)
	return nil
}

func pruneBashHistory(history map[string]BashExecution, limit int) {
	for len(history) >= limit {
		oldestID := ""
		oldestSequence := int64(^uint64(0) >> 1)
		for id, execution := range history {
			if execution.Status != BashExecutionRunning && execution.Sequence < oldestSequence {
				oldestID, oldestSequence = id, execution.Sequence
			}
		}
		if oldestID == "" {
			return
		}
		delete(history, oldestID)
	}
}

func (m *Manager) sessionDeleting(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleting[sessionID]
}

func (m *Manager) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}
