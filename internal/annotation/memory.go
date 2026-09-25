package annotation

import (
	"context"
	"sync"
)

// MemoryRepository stores process-local annotation drafts for temporary sessions.
type MemoryRepository struct {
	mu      sync.Mutex
	next    map[string]uint64
	records map[string]map[uint64]Record
	pending map[string]map[string][]Record
}

// NewMemoryRepository constructs an empty process-local annotation repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{next: make(map[string]uint64), records: make(map[string]map[uint64]Record), pending: make(map[string]map[string][]Record)}
}

func (r *MemoryRepository) CreateAnnotation(_ context.Context, record Record, limit int) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.records[record.SessionID]
	if entries == nil {
		entries = make(map[uint64]Record)
		r.records[record.SessionID] = entries
	}
	pendingCount := 0
	for _, records := range r.pending[record.SessionID] {
		pendingCount += len(records)
	}
	if len(entries)+pendingCount >= limit {
		return Record{}, ErrCapacity
	}
	r.next[record.SessionID]++
	record.ID = r.next[record.SessionID]
	entries[record.ID] = record
	return record, nil
}

func (r *MemoryRepository) GetAnnotation(_ context.Context, sessionID string, id uint64) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID][id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return record, nil
}

func (r *MemoryRepository) ListAnnotations(_ context.Context, sessionID string, after uint64, limit int) ([]Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Record, 0, min(limit, len(r.records[sessionID])))
	for id := after + 1; id <= r.next[sessionID] && len(result) < limit; id++ {
		if record, ok := r.records[sessionID][id]; ok {
			result = append(result, record)
		}
	}
	return result, nil
}

func (r *MemoryRepository) UpdateAnnotationBody(_ context.Context, sessionID string, id uint64, body string) (Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID][id]
	if !ok {
		return Record{}, ErrNotFound
	}
	record.Body = body
	r.records[sessionID][id] = record
	return record, nil
}

func (r *MemoryRepository) DeleteAnnotation(_ context.Context, sessionID string, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[sessionID][id]; !ok {
		return ErrNotFound
	}
	delete(r.records[sessionID], id)
	return nil
}

func (r *MemoryRepository) ReserveAnnotations(_ context.Context, sessionID, submissionID string, ids []uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending[sessionID] == nil {
		r.pending[sessionID] = make(map[string][]Record)
	}
	if _, duplicate := r.pending[sessionID][submissionID]; duplicate {
		return nil
	}
	records := make([]Record, 0, len(ids))
	for _, id := range ids {
		record, ok := r.records[sessionID][id]
		if !ok {
			return ErrNotFound
		}
		records = append(records, record)
	}
	for _, id := range ids {
		delete(r.records[sessionID], id)
	}
	r.pending[sessionID][submissionID] = records
	return nil
}

func (r *MemoryRepository) FinalizeAnnotationSubmission(_ context.Context, sessionID, submissionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending[sessionID], submissionID)
	return nil
}

func (r *MemoryRepository) RollbackAnnotationSubmission(_ context.Context, sessionID, submissionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.pending[sessionID][submissionID] {
		r.records[sessionID][record.ID] = record
	}
	delete(r.pending[sessionID], submissionID)
	return nil
}

func (r *MemoryRepository) PendingAnnotationSubmissions(_ context.Context, sessionID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]string, 0, len(r.pending[sessionID]))
	for id := range r.pending[sessionID] {
		result = append(result, id)
	}
	return result, nil
}

// DeleteSession drops all process-local drafts and allocation state.
func (r *MemoryRepository) DeleteSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, sessionID)
	delete(r.next, sessionID)
	delete(r.pending, sessionID)
}
