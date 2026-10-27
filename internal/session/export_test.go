package session

import "context"

// HoldAdmissionForTest holds the session's admission authority without a
// turn, as a subagent mailbox or peer query does while it decides whether to
// start one, until the returned function is called.
func (m *Manager) HoldAdmissionForTest(ctx context.Context, sessionID string) (func(), error) {
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	loaded.admissionMu.Lock()
	return loaded.admissionMu.Unlock, nil
}

// QueueFollowUpForTest queues prompt on an idle session, as a turn that ended
// with prompts queued behind it leaves the queue.
func (m *Manager) QueueFollowUpForTest(ctx context.Context, sessionID string, prompt PromptInput) error {
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	loaded.mu.Lock()
	defer loaded.mu.Unlock()
	loaded.nextFollowUpID++
	prompt.queueID = loaded.nextFollowUpID
	loaded.followUps = append(loaded.followUps, prompt)
	return nil
}

// StartQueuedFollowUpsForTest starts the queue as the end of a turn does.
func (m *Manager) StartQueuedFollowUpsForTest(ctx context.Context, sessionID string) error {
	loaded, err := m.runtime(ctx, sessionID)
	if err != nil {
		return err
	}
	loaded.transitionMu.Lock()
	defer loaded.transitionMu.Unlock()
	m.startQueuedFollowUps(loaded, sessionID)
	return nil
}
