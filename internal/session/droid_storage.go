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

	appendMu sync.Mutex
	mu       sync.Mutex
	turnID   string
	runErr   error
}

var _ droids.Storage = (*droidStorage)(nil)

func newDroidStorage(store Repository, sessionID string) *droidStorage {
	return &droidStorage{store: store, sessionID: sessionID}
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
	messages := make([]droids.Message, 0, len(records))
	for _, record := range records {
		message, err := decodeDroidMessage(record.Role, record.PayloadJSON)
		if err != nil {
			return nil, fmt.Errorf("decode message %q: %w", record.ID, err)
		}
		messages = append(messages, message)
	}
	return messages, nil
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
		records = append(records, NewMessageRecord{
			Role: role, PayloadJSON: payload, CreatedAt: createdAt,
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
