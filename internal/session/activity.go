package session

import (
	"context"
	"sort"
)

// Activity is what a session is doing that its user would want to see.
type Activity string

const (
	// ActivityRunning means a turn, bash command, or subagent is working.
	ActivityRunning Activity = "running"
	// ActivityAwaitingResponse means a request waits on the user. It takes
	// precedence over running.
	ActivityAwaitingResponse Activity = "awaitingResponse"
)

// SessionActivity reports one session that is not idle.
type SessionActivity struct {
	SessionID string
	Status    Activity
}

// Activity reports the saved sessions that are working or waiting on their
// user, ordered by session ID. A session it omits is idle. It reads loaded
// runtimes, active bash executions, and subagent tasks without loading any
// runtime, so sessions that are not loaded only report subagent work.
func (m *Manager) Activity(ctx context.Context) ([]SessionActivity, error) {
	if err := m.beginOperation(); err != nil {
		return nil, err
	}
	defer m.ops.Done()

	// Runtime locks are never acquired under Manager.mu, so copy the loaded
	// runtimes first.
	m.mu.Lock()
	loaded := make(map[string]*runtime, len(m.runtimes))
	for id, candidate := range m.runtimes {
		loaded[id] = candidate
	}
	excluded := make(map[string]struct{}, len(m.temporary)+len(m.deleting))
	for id := range m.temporary {
		excluded[id] = struct{}{}
	}
	for id, deleting := range m.deleting {
		if deleting {
			excluded[id] = struct{}{}
		}
	}
	m.mu.Unlock()

	activity := make(map[string]Activity)
	running := func(id string) {
		if activity[id] == "" {
			activity[id] = ActivityRunning
		}
	}
	for id, candidate := range loaded {
		candidate.mu.Lock()
		active := candidate.activeRun != ""
		candidate.mu.Unlock()
		if candidate.interactions != nil && len(candidate.interactions.snapshot()) > 0 {
			activity[id] = ActivityAwaitingResponse
		} else if active {
			running(id)
		}
	}

	m.bashMu.Lock()
	for id := range m.bashActive {
		running(id)
	}
	m.bashMu.Unlock()

	if m.mailbox != nil {
		owners, err := m.mailbox.WorkingSessions(ctx)
		if err != nil {
			return nil, err
		}
		for _, id := range owners {
			running(id)
		}
	}

	result := make([]SessionActivity, 0, len(activity))
	for id, value := range activity {
		if _, skip := excluded[id]; skip {
			continue
		}
		result = append(result, SessionActivity{SessionID: id, Status: value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result, nil
}
