package droids

import (
	"context"
	"sync"
)

// storage.go — the durable layer seam. A droid loads its transcript on start
// and appends as messages complete. This interface doubles as the persistence
// and observability hook described in the Go Agent Runtime design: back it
// with Postgres for durable runs, or leave it nil to default to in-memory.

// Storage persists a droid's conversation, keyed by session id.
type Storage interface {
	// Load returns the stored transcript for a session, or nil if none exists.
	Load(ctx context.Context, session string) ([]Message, error)
	// Append durably records newly completed messages for a session, in order.
	Append(ctx context.Context, session string, messages ...Message) error
}

// MemoryStorage is an in-process Storage useful for tests and ephemeral use.
type MemoryStorage struct {
	mu       sync.Mutex
	sessions map[string][]Message
}

// NewMemoryStorage returns an empty in-memory store.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{sessions: map[string][]Message{}}
}

func (s *MemoryStorage) Load(_ context.Context, session string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := s.sessions[session]
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

func (s *MemoryStorage) Append(_ context.Context, session string, messages ...Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string][]Message{}
	}
	s.sessions[session] = append(s.sessions[session], messages...)
	return nil
}
