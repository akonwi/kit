package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/akonwi/kit/internal/droids"
)

type droidStorage struct {
	store     Repository
	sessionID string

	appendMu        sync.Mutex
	mu              sync.Mutex
	turnID          string
	runErr          error
	contextSequence int64
}

var _ droids.Storage = (*droidStorage)(nil)

func newDroidStorage(store Repository, sessionID string) *droidStorage {
	return &droidStorage{store: store, sessionID: sessionID, contextSequence: -1}
}

func (s *droidStorage) beginTurn(turnID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnID != "" {
		return fmt.Errorf("droids storage already has active turn %q", s.turnID)
	}
	s.turnID = turnID
	s.runErr = nil
	return nil
}

func (s *droidStorage) endTurn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.runErr
	s.turnID = ""
	s.runErr = nil
	return err
}

func (s *droidStorage) Load(ctx context.Context, sessionID string) ([]droids.Message, error) {
	if sessionID != s.sessionID {
		return nil, fmt.Errorf("droids requested session %q from storage bound to %q", sessionID, s.sessionID)
	}
	records, err := s.store.ListReplayMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	records = orderReplayMessages(records)
	messages := make([]droids.Message, 0, len(records))
	for _, record := range records {
		if record.Role == "bash" {
			execution, err := decodeBashExecution(record)
			if err != nil {
				return nil, fmt.Errorf("decode message %q: %w", record.ID, err)
			}
			if execution.Status == BashExecutionRunning || execution.Status == BashExecutionInterrupted || execution.ExcludeFromContext {
				continue
			}
			messages = append(messages, bashContextMessage(execution))
			s.contextSequence = max(s.contextSequence, record.Sequence)
			continue
		}
		message, err := decodeDroidMessage(record.Role, record.PayloadJSON)
		if err != nil {
			return nil, fmt.Errorf("decode message %q: %w", record.ID, err)
		}
		if assistant, ok := message.(droids.AssistantMessage); ok {
			assistant.ID = record.ID
			message = assistant
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (s *droidStorage) bashContextSequence() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contextSequence
}

// orderReplayMessages keeps standalone bash observations from splitting the
// provider-required assistant/tool-result shape of a completed parent turn.
func orderReplayMessages(records []MessageRecord) []MessageRecord {
	if len(records) < 2 {
		return records
	}
	nextTurn := make([]string, len(records))
	nearest := ""
	for index := len(records) - 1; index >= 0; index-- {
		nextTurn[index] = nearest
		if records[index].Role != "bash" {
			nearest = records[index].TurnID
		}
	}
	firstByTurn := make(map[string]int)
	lastByTurn := make(map[string]int)
	for index, record := range records {
		if record.Role != "bash" {
			if _, found := firstByTurn[record.TurnID]; !found {
				firstByTurn[record.TurnID] = index
			}
			lastByTurn[record.TurnID] = index
		}
	}
	ordered := make([]MessageRecord, 0, len(records))
	deferredBefore := make(map[string][]MessageRecord)
	deferredAfter := make(map[string][]MessageRecord)
	var deferredUntilNextRun []MessageRecord
	previousTurn := ""
	for index, record := range records {
		if record.Role == "bash" {
			execution, err := decodeBashExecution(record)
			if err == nil && execution.ContextBeforeTurnID != "" {
				if _, found := firstByTurn[execution.ContextBeforeTurnID]; found {
					deferredBefore[execution.ContextBeforeTurnID] = append(deferredBefore[execution.ContextBeforeTurnID], record)
				} else {
					deferredUntilNextRun = append(deferredUntilNextRun, record)
				}
				continue
			}
			if previousTurn != "" && nextTurn[index] == previousTurn {
				deferredAfter[previousTurn] = append(deferredAfter[previousTurn], record)
				continue
			}
			ordered = append(ordered, record)
			continue
		}
		if firstByTurn[record.TurnID] == index {
			ordered = append(ordered, deferredBefore[record.TurnID]...)
			delete(deferredBefore, record.TurnID)
		}
		ordered = append(ordered, record)
		previousTurn = record.TurnID
		if lastByTurn[record.TurnID] == index {
			ordered = append(ordered, deferredAfter[record.TurnID]...)
			delete(deferredAfter, record.TurnID)
		}
	}
	for _, record := range records {
		if pending := deferredAfter[record.TurnID]; len(pending) > 0 {
			ordered = append(ordered, pending...)
			delete(deferredAfter, record.TurnID)
		}
	}
	ordered = append(ordered, deferredUntilNextRun...)
	return ordered
}

func (s *droidStorage) Append(ctx context.Context, sessionID string, messages ...droids.Message) error {
	s.appendMu.Lock()
	defer s.appendMu.Unlock()

	if sessionID != s.sessionID {
		err := fmt.Errorf("droids appended session %q to storage bound to %q", sessionID, s.sessionID)
		s.recordError(err)
		return err
	}
	s.mu.Lock()
	turnID := s.turnID
	previousErr := s.runErr
	s.mu.Unlock()
	if previousErr != nil {
		return fmt.Errorf("droids storage turn is poisoned: %w", previousErr)
	}
	if turnID == "" {
		err := errors.New("droids appended a message without an active Kit turn")
		s.recordError(err)
		return err
	}

	records := make([]NewMessageRecord, 0, len(messages))
	for _, message := range messages {
		role, payload, createdAt, err := encodeDroidMessage(message)
		if err != nil {
			s.recordError(err)
			return err
		}
		messageID := ""
		if assistant, ok := message.(droids.AssistantMessage); ok {
			messageID = assistant.ID
			if messageID == "" {
				err := errors.New("droids appended an assistant message without an id")
				s.recordError(err)
				return err
			}
		}
		records = append(records, NewMessageRecord{
			ID: messageID, Role: role, PayloadJSON: payload, CreatedAt: createdAt,
		})
	}
	if _, err := s.store.AppendMessages(ctx, sessionID, turnID, records); err != nil {
		s.recordError(err)
		return err
	}
	return nil
}

func (s *droidStorage) recordError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runErr == nil {
		s.runErr = err
	}
}
