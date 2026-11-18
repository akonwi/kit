package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/akonwi/kit/droids"
	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/subagent"
)

// ToolConfirmation is a yes-or-no question a model tool asks the user of the
// session running it before the tool acts.
type ToolConfirmation struct {
	Title        string
	Detail       string
	ConfirmLabel string
	CancelLabel  string
}

// ToolConfirmationResult is the user's answer to a ToolConfirmation.
// Resolution is the interaction's resolution, such as answered,
// negative_confirmation, user_cancelled, run_abort, or unavailable.
type ToolConfirmationResult struct {
	Confirmed  bool
	Resolution string
}

// ConfirmToolCall asks the user of sessionID to approve the model tool call
// identified by call, through the session's confirm interaction. It waits
// until the user answers, the call's context ends, or the session closes.
// Only a loaded session, whose model is running the call, can ask.
//
// It is the interim approval path for delete_session (ADR 0071). Droids'
// tool confirmation (ADR 0073) replaces it: the tool then declares its
// confirmation, and droids asks before the call runs.
func (m *Manager) ConfirmToolCall(ctx context.Context, sessionID string, call droids.ToolContext, confirmation ToolConfirmation) (ToolConfirmationResult, error) {
	m.mu.Lock()
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	if loaded == nil {
		return ToolConfirmationResult{}, fmt.Errorf("%w: session %s is not running", ErrClosed, sessionID)
	}
	settlement, err := executeInteraction(ctx, sessionID, loaded.interactions, call, InteractionRequest{
		Kind: InteractionConfirm, Title: confirmation.Title, Detail: confirmation.Detail,
		ConfirmLabel: confirmation.ConfirmLabel, CancelLabel: confirmation.CancelLabel,
	})
	if err != nil {
		return ToolConfirmationResult{}, err
	}
	response := settlement.response
	confirmed := !response.Cancelled && response.Confirmed != nil && *response.Confirmed
	return ToolConfirmationResult{Confirmed: confirmed, Resolution: settlement.reason}, nil
}

// DeletionBlocker reports why Delete would refuse sessionID as busy now, or
// nil if nothing is in its way. The answer can change before a later Delete,
// which checks again.
func (m *Manager) DeletionBlocker(ctx context.Context, sessionID string) error {
	if err := m.beginOperation(); err != nil {
		return err
	}
	defer m.ops.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	if !identifier.Valid(sessionID, "session_") {
		return fmt.Errorf("%w: invalid session id", ErrInvalidInput)
	}
	busy := func(reason string) error { return fmt.Errorf("%w: %s", ErrDeleteBusy, reason) }
	m.mu.Lock()
	if _, temporary := m.temporary[sessionID]; temporary {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrTemporary, sessionID)
	}
	reason := ""
	switch {
	case m.deleting[sessionID]:
		reason = "it is already being deleted"
	case m.creating[sessionID] != nil || m.loading[sessionID] != nil || m.sessionOperations[sessionID] > 0:
		reason = "another operation on it is in progress"
	case m.peerAdmissionCounts[sessionID] > 0:
		reason = "a peer query with it is in progress"
	}
	if reason == "" {
		for _, request := range m.peerActive {
			if request.SenderSessionID == sessionID || request.RecipientSessionID == sessionID {
				reason = "a peer query with it is in progress"
				break
			}
		}
	}
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	if reason != "" {
		return busy(reason)
	}
	if loaded != nil {
		// Long runtime operations, such as compaction, hold the runtime's
		// lock across droid I/O; a session in one is busy, not worth waiting
		// for.
		if !tryLockWithin(ctx, &loaded.mu, deletionBlockerLockWait) {
			if err := ctx.Err(); err != nil {
				return err
			}
			return busy("another operation on it is in progress")
		}
		running, queued := loaded.activeRun != "", loaded.followUps.Count > 0
		loaded.mu.Unlock()
		if running {
			return busy("it is running a turn")
		}
		if queued {
			return busy("it has queued prompts")
		}
	}
	m.bashMu.Lock()
	shell := m.bashActive[sessionID] != nil
	m.bashMu.Unlock()
	if shell {
		return busy("a shell command is running in it")
	}
	if _, unfinished, err := m.ownedSubagentConversations(ctx, sessionID); err != nil {
		return err
	} else if unfinished {
		return busy("one of its subagents has unfinished work")
	}
	return nil
}

// deletionBlockerLockWait bounds how long DeletionBlocker waits for a
// session's runtime lock.
const deletionBlockerLockWait = 2 * time.Second

// tryLockWithin acquires mu without blocking other lockers' attempts, giving
// up when ctx ends or wait passes.
func tryLockWithin(ctx context.Context, mu *sync.Mutex, wait time.Duration) bool {
	if mu.TryLock() {
		return true
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			if mu.TryLock() {
				return true
			}
		}
	}
}

// ownedSubagentConversations lists the subagent conversations sessionID owns,
// dismissed ones included, and reports whether any has an unfinished task.
// It stops listing at the first unfinished task.
func (m *Manager) ownedSubagentConversations(ctx context.Context, sessionID string) ([]subagent.Conversation, bool, error) {
	if m.mailbox == nil {
		return nil, false, nil
	}
	conversations, err := m.mailbox.ListConversations(ctx, sessionID)
	if err != nil {
		return nil, false, fmt.Errorf("list subagent conversations for deletion: %w", err)
	}
	dismissed, err := m.mailbox.ListDismissedConversations(ctx, sessionID)
	if err != nil {
		return nil, false, fmt.Errorf("list dismissed subagent conversations for deletion: %w", err)
	}
	conversations = append(conversations, dismissed...)
	for _, conversation := range conversations {
		tasks, err := m.mailbox.ListTasks(ctx, conversation.ID)
		if err != nil {
			return nil, false, fmt.Errorf("list subagent tasks for deletion: %w", err)
		}
		for _, task := range tasks {
			if !subagent.TerminalTask(task.State) {
				return conversations, true, nil
			}
		}
	}
	return conversations, false, nil
}

func deleteLeafSession(ctx context.Context, store Repository, sessionID string) error {
	leaf, ok := store.(leafSessionDeleter)
	if !ok {
		return fmt.Errorf("session store cannot delete only childless sessions")
	}
	return leaf.DeleteLeafSession(ctx, sessionID)
}
