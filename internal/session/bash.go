package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
)

const (
	maxDirectBashCommandBytes = 64 << 10
	maxConcurrentDirectBash   = 4
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

	m.bashMu.Lock()
	defer m.bashMu.Unlock()
	if m.sessionDeleting(sessionID) {
		return BashExecution{}, ErrDeleteBusy
	}
	if history := m.bashHistory[sessionID]; history != nil {
		if existing, ok := history[executionID]; ok {
			if existing.Command != command || existing.ExcludeFromContext != exclude {
				return BashExecution{}, fmt.Errorf("%w: bash execution id was reused", ErrInvalidInput)
			}
			return existing, nil
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
	sequence := m.bashNextSequence[sessionID]
	m.bashNextSequence[sessionID] = sequence + 1
	execution := BashExecution{
		ID: executionID, SessionID: sessionID, Sequence: sequence, Command: command, CWD: loaded.cwd,
		Status: BashExecutionRunning, ExcludeFromContext: exclude, StartedAt: time.Now().UTC(),
	}
	if m.bashHistory[sessionID] == nil {
		m.bashHistory[sessionID] = make(map[string]BashExecution)
	}
	pruneBashHistory(m.bashHistory[sessionID], 64)
	m.bashHistory[sessionID][executionID] = execution
	runContext, cancel := context.WithCancelCause(m.bashContext)
	active := &activeBashExecution{id: executionID, cancel: cancel, runtime: loaded, done: make(chan struct{})}
	m.bashActive[sessionID] = active
	m.bashRuns.Add(1)
	go m.executeBash(runContext, execution, active)
	return execution, nil
}

func (m *Manager) executeBash(ctx context.Context, execution BashExecution, active *activeBashExecution) {
	defer m.bashRuns.Done()
	defer close(active.done)
	result, runErr := codingtools.RunDirectBash(ctx, execution.Command, execution.CWD)
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

	informErr := error(nil)
	if !settled.ExcludeFromContext && settled.Status != BashExecutionInterrupted {
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
				informErr = active.runtime.droid.Inform(context.Background(), boundary)
				if informErr == nil || m.isClosed() {
					break
				}
				time.Sleep(time.Duration(1<<min(attempt, 5)) * 50 * time.Millisecond)
			}
		}
	}
	if informErr != nil {
		settled.Status = BashExecutionInterrupted
		settled.ErrorMessage = "persist bash boundary: " + informErr.Error()
	}
	m.bashMu.Lock()
	m.bashHistory[execution.SessionID][execution.ID] = settled
	if m.bashActive[execution.SessionID] == active {
		delete(m.bashActive, execution.SessionID)
	}
	m.bashMu.Unlock()
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
	return BashExecution{}, fmt.Errorf("bash execution %q: %w", executionID, ErrNotFound)
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
