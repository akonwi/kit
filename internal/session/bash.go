package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/codingtools"
	"github.com/akonwi/kit/internal/identifier"
)

const (
	maxDirectBashCommandBytes = 64 << 10
	maxConcurrentDirectBash   = 4
)

var (
	errBashUserAbort = errors.New("bash execution aborted by user")
	errBashShutdown  = errors.New("daemon stopped during bash execution")
)

type activeBashExecution struct {
	id     string
	cancel context.CancelCauseFunc
}

// StartBash durably admits and starts one direct composer shell execution.
// Repeating an execution id with the same immutable input is idempotent.
func (m *Manager) StartBash(
	ctx context.Context,
	sessionID, executionID, command string,
	excludeFromContext bool,
) (BashExecution, error) {
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
		return BashExecution{}, fmt.Errorf("%w: bash command must be non-empty valid UTF-8 without NUL and at most 64 KiB", ErrInvalidInput)
	}

	m.bashMu.Lock()
	defer m.bashMu.Unlock()

	existingRecord, err := m.store.GetBashExecution(ctx, sessionID, executionID)
	if err == nil {
		existing, decodeErr := decodeBashExecution(existingRecord)
		if decodeErr != nil {
			return BashExecution{}, decodeErr
		}
		if existing.Command != command || existing.ExcludeFromContext != excludeFromContext {
			return BashExecution{}, fmt.Errorf("%w: bash execution id was reused for different input", ErrInvalidInput)
		}
		if existing.Status == BashExecutionRunning {
			if active := m.bashActive[sessionID]; active == nil || active.id != executionID {
				settlement := BashExecutionResult{
					Status: BashExecutionInterrupted, ErrorMessage: "bash execution has no live supervisor",
					CompletedAt: time.Now().UTC(),
				}
				payload, encodeErr := encodeCompletedBashExecution(existing, settlement)
				if encodeErr != nil {
					return BashExecution{}, encodeErr
				}
				resolveContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				settled, settleErr := m.store.UpdateBashExecution(resolveContext, sessionID, executionID, payload)
				cancel()
				if settleErr != nil {
					return BashExecution{}, settleErr
				}
				return decodeBashExecution(settled)
			}
		}
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return BashExecution{}, err
	}
	if active := m.bashActive[sessionID]; active != nil {
		activeRecord, activeErr := m.store.GetBashExecution(ctx, sessionID, active.id)
		if activeErr != nil {
			return BashExecution{}, activeErr
		}
		activeExecution, decodeErr := decodeBashExecution(activeRecord)
		if decodeErr != nil {
			return BashExecution{}, decodeErr
		}
		if activeExecution.Status == BashExecutionRunning {
			return BashExecution{}, fmt.Errorf("%w: session already has an active bash execution", ErrBashBusy)
		}
		delete(m.bashActive, sessionID)
	}
	select {
	case m.bashSlots <- struct{}{}:
	case <-ctx.Done():
		return BashExecution{}, ctx.Err()
	default:
		return BashExecution{}, fmt.Errorf("%w: daemon bash capacity is exhausted", ErrBashBusy)
	}
	releaseSlot := true
	defer func() {
		if releaseSlot {
			<-m.bashSlots
		}
	}()
	if m.isClosed() {
		return BashExecution{}, ErrClosed
	}
	record, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return BashExecution{}, err
	}
	startedAt := time.Now().UTC()
	payload, err := encodeRunningBashExecution(command, record.CWD, excludeFromContext, startedAt)
	if err != nil {
		return BashExecution{}, err
	}
	message, err := m.store.CreateBashExecution(ctx, sessionID, NewMessageRecord{
		ID: executionID, Role: "bash", PayloadJSON: payload, CreatedAt: startedAt,
	})
	if err != nil {
		resolveContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		resolved, resolveErr := m.store.GetBashExecution(resolveContext, sessionID, executionID)
		cancel()
		if resolveErr != nil {
			return BashExecution{}, err
		}
		message = resolved
	}
	execution, err := decodeBashExecution(message)
	if err != nil {
		return BashExecution{}, err
	}
	runContext, cancel := context.WithCancelCause(m.bashContext)
	active := &activeBashExecution{id: executionID, cancel: cancel}
	m.bashActive[sessionID] = active
	m.bashRuns.Add(1)
	releaseSlot = false
	go m.executeBash(runContext, execution, active)
	return execution, nil
}

func (m *Manager) executeBash(ctx context.Context, execution BashExecution, active *activeBashExecution) {
	defer m.bashRuns.Done()
	result, runErr := codingtools.RunDirectBash(ctx, execution.Command, execution.CWD)
	codingtools.RemoveCommandOutput(result.OutputPath)
	<-m.bashSlots
	completedAt := time.Now().UTC()
	settlement := BashExecutionResult{
		Status: BashExecutionCompleted, Output: strings.TrimRight(result.Output, "\r\n"),
		ExitCode: cloneInt(result.ExitCode), Truncated: result.Truncated,
		TimedOut: result.TimedOut, CompletedAt: completedAt,
	}
	switch {
	case errors.Is(context.Cause(ctx), errBashShutdown):
		settlement.Status = BashExecutionInterrupted
		settlement.ErrorMessage = errBashShutdown.Error()
	case ctx.Err() != nil:
		settlement.Status = BashExecutionAborted
		settlement.ErrorMessage = errBashUserAbort.Error()
	case runErr != nil:
		settlement.Status = BashExecutionFailed
		settlement.ErrorMessage = runErr.Error()
	}
	payload, settlementErr := encodeCompletedBashExecution(execution, settlement)
	if settlementErr == nil {
		attempt := 0
		for {
			finishContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, settlementErr = m.store.UpdateBashExecution(finishContext, execution.SessionID, execution.ID, payload)
			cancel()
			if settlementErr == nil || m.isClosed() {
				break
			}
			delay := time.Duration(1<<min(attempt, 5)) * 50 * time.Millisecond
			time.Sleep(delay)
			attempt++
		}
	}
	if settlementErr == nil {
		m.bashMu.Lock()
		if m.bashActive[execution.SessionID] == active {
			delete(m.bashActive, execution.SessionID)
		}
		m.bashMu.Unlock()
	}
}

// GetBash returns one exact durable direct bash execution.
func (m *Manager) GetBash(ctx context.Context, sessionID, executionID string) (BashExecution, error) {
	if err := m.beginOperation(); err != nil {
		return BashExecution{}, err
	}
	defer m.ops.Done()
	if strings.TrimSpace(sessionID) == "" || !identifier.Valid(executionID, "bash_") {
		return BashExecution{}, fmt.Errorf("%w: session id and valid bash execution id are required", ErrInvalidInput)
	}
	record, err := m.store.GetBashExecution(ctx, sessionID, executionID)
	if err != nil {
		return BashExecution{}, err
	}
	return decodeBashExecution(record)
}

// AbortBash cancels only the matching active direct bash generation.
func (m *Manager) AbortBash(ctx context.Context, sessionID, executionID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sessionID) == "" || !identifier.Valid(executionID, "bash_") {
		return fmt.Errorf("%w: session id and valid bash execution id are required", ErrInvalidInput)
	}
	m.bashMu.Lock()
	active := m.bashActive[sessionID]
	if active == nil || active.id != executionID {
		m.bashMu.Unlock()
		record, err := m.store.GetBashExecution(ctx, sessionID, executionID)
		if err != nil {
			return err
		}
		execution, err := decodeBashExecution(record)
		if err != nil {
			return err
		}
		if execution.Status != BashExecutionRunning {
			return fmt.Errorf("%w: bash execution %q has status %q", ErrBashNotAbortable, executionID, execution.Status)
		}
		return fmt.Errorf("%w: bash execution %q is not owned by this daemon", ErrBashNotAbortable, executionID)
	}
	active.cancel(errBashUserAbort)
	m.bashMu.Unlock()
	return nil
}
