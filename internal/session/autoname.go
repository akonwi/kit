package session

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/identifier"
)

const (
	autoNameMinUserTurns = 2
	autoNameMaxWords     = 8
	autoNameTimeout      = 45 * time.Second
	autoNameBusyDelay    = 200 * time.Millisecond
	autoNameBusyAttempts = 3
)

const autoNameSystemPrompt = "You generate concise conversation titles. Return title only. No quotes. No markdown. Maximum 8 words. Focus on the concrete task or topic."

func (m *Manager) scheduleAutoName(sessionID string) {
	if !m.autoName || sessionID == "" {
		return
	}
	m.mu.Lock()
	if m.closed || m.deleting[sessionID] {
		m.mu.Unlock()
		return
	}
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	if loaded == nil {
		return
	}
	loaded.mu.Lock()
	if loaded.autoNameSettled {
		loaded.mu.Unlock()
		return
	}
	if loaded.autoNameRunning {
		loaded.autoNameAgain = true
		loaded.mu.Unlock()
		return
	}
	loaded.autoNameRunning = true
	loaded.mu.Unlock()
	m.cleanups.Add(1)
	go m.autoNameLoop(sessionID)
}

func (m *Manager) autoNameLoop(sessionID string) {
	defer m.cleanups.Done()
	for {
		m.autoNameOnce(sessionID)
		m.mu.Lock()
		loaded := m.runtimes[sessionID]
		closed := m.closed || m.deleting[sessionID]
		m.mu.Unlock()
		if loaded == nil || closed {
			return
		}
		loaded.mu.Lock()
		if !loaded.autoNameAgain || loaded.autoNameSettled {
			loaded.autoNameRunning = false
			loaded.autoNameAgain = false
			loaded.mu.Unlock()
			return
		}
		loaded.autoNameAgain = false
		loaded.mu.Unlock()
	}
}

func (m *Manager) autoNameOnce(sessionID string) {
	name, ok := m.currentSessionName(sessionID)
	if !ok || strings.TrimSpace(name) != "" {
		m.settleAutoName(sessionID)
		return
	}
	m.mu.Lock()
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	if loaded == nil || loaded.droid == nil {
		return
	}
	turns, ready, err := autoNameEligibility(m.mailboxContext, loaded.droid)
	if err != nil || !ready || turns < autoNameMinUserTurns {
		return
	}
	var lastErr error
	for attempt := 0; attempt < autoNameBusyAttempts; attempt++ {
		lastErr = m.nameFromMemoryFork(sessionID, loaded.droid)
		if !errors.Is(lastErr, droids.ErrBusy) {
			break
		}
		timer := time.NewTimer(autoNameBusyDelay)
		select {
		case <-m.mailboxContext.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	if lastErr == nil {
		m.settleAutoName(sessionID)
	}
}

func (m *Manager) settleAutoName(sessionID string) {
	m.mu.Lock()
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	if loaded == nil {
		return
	}
	loaded.mu.Lock()
	loaded.autoNameSettled = true
	loaded.mu.Unlock()
}

func (m *Manager) currentSessionName(sessionID string) (string, bool) {
	m.mu.Lock()
	if record, ok := m.temporary[sessionID]; ok {
		m.mu.Unlock()
		return record.Name, true
	}
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return "", false
	}
	record, err := m.store.GetSession(context.Background(), sessionID)
	if err != nil {
		return "", false
	}
	return record.Name, true
}

func autoNameEligibility(ctx context.Context, droid *droids.Droid) (int, bool, error) {
	var after uint64
	users := 0
	sawAssistant := false
	assistantOK := false
	for {
		page, err := droid.History(ctx, droids.HistoryQuery{After: after, Limit: 100})
		if err != nil {
			return 0, false, err
		}
		for _, envelope := range page.Messages {
			switch message := envelope.Message.(type) {
			case droids.UserMessage:
				users++
			case droids.AssistantMessage:
				sawAssistant = true
				assistantOK = message.StopReason != droids.StopReasonError && message.StopReason != droids.StopReasonAborted
			}
		}
		if !page.HasMore {
			break
		}
		after = page.Next
	}
	return users, sawAssistant && assistantOK, nil
}

func (m *Manager) nameFromMemoryFork(sessionID string, parent *droids.Droid) error {
	ctx, cancel := context.WithTimeout(m.mailboxContext, autoNameTimeout)
	defer cancel()
	childID, err := identifier.New("naming_")
	if err != nil {
		return err
	}
	forked, err := parent.Fork(ctx, droids.ConversationID(childID), droids.ForkOptions{Store: droids.NewMemoryStore()})
	if err != nil {
		return err
	}
	child := forked.Droid
	defer child.Close()
	if err := child.Reconfigure(droids.RequestConfiguration{SystemPrompt: autoNameSystemPrompt}); err != nil {
		return err
	}
	if err := child.SetAdditionalTools(nil, ""); err != nil {
		return err
	}
	handle, err := child.Prompt(ctx, droids.Input{Content: []droids.InputContent{droids.TextInput{Text: "Name this conversation."}}}, droids.PromptOptions{})
	if err != nil {
		return err
	}
	outcome, err := handle.Wait(ctx)
	if err != nil {
		return err
	}
	if outcome.FinalMessage == nil {
		return nil
	}
	assistant, ok := outcome.FinalMessage.Message.(droids.AssistantMessage)
	if !ok {
		return nil
	}
	title := sessionTitle(assistant.Text())
	if title == "" {
		return nil
	}
	_, err = m.renameIfUnnamed(ctx, sessionID, title)
	return err
}

func sessionTitle(text string) string {
	line := ""
	for _, candidate := range strings.Split(text, "\n") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.Trim(candidate, "\"'`")
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			line = candidate
			break
		}
	}
	if line == "" || strings.EqualFold(line, "untitled") {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) > autoNameMaxWords {
		fields = fields[:autoNameMaxWords]
	}
	title := strings.Join(fields, " ")
	title = strings.TrimFunc(title, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
	if title == "" || !validSessionName(title) {
		return ""
	}
	return title
}

func (m *Manager) renameIfUnnamed(ctx context.Context, sessionID, name string) (SessionRecord, error) {
	if err := m.beginOperation(); err != nil {
		return SessionRecord{}, err
	}
	defer m.ops.Done()
	name = strings.TrimSpace(name)
	if name == "" || !validSessionName(name) {
		return SessionRecord{}, nil
	}
	unlockMetadata := m.lockSessionMetadata(sessionID)
	defer unlockMetadata()
	m.mu.Lock()
	if m.closed || m.deleting[sessionID] {
		m.mu.Unlock()
		return SessionRecord{}, ErrClosed
	}
	record, temporary := m.temporary[sessionID]
	loaded := m.runtimes[sessionID]
	m.mu.Unlock()
	if temporary {
		if strings.TrimSpace(record.Name) != "" {
			return record, nil
		}
		record.Name = name
		updatedAt := time.Now().UTC()
		if !updatedAt.After(record.UpdatedAt) {
			updatedAt = record.UpdatedAt.Add(time.Nanosecond)
		}
		record.UpdatedAt = updatedAt
		m.mu.Lock()
		if current, ok := m.temporary[sessionID]; ok && strings.TrimSpace(current.Name) == "" {
			m.temporary[sessionID] = record
			loaded = m.runtimes[sessionID]
			m.mu.Unlock()
			m.publishSessionRenamed(loaded, record)
			return record, nil
		}
		m.mu.Unlock()
		return record, nil
	}
	current, err := m.store.GetSession(ctx, sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	if strings.TrimSpace(current.Name) != "" {
		return current, nil
	}
	renamed, err := m.store.RenameSession(ctx, sessionID, name)
	if err != nil {
		return SessionRecord{}, err
	}
	m.publishSessionRenamed(loaded, renamed)
	return renamed, nil
}
