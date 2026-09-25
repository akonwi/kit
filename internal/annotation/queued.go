package annotation

import (
	"context"
	"fmt"
)

type queuedAnnotationKey struct {
	sessionID string
	id        uint64
}

// QueuedSubmission keeps validated annotation evidence immutable until a queued
// message is accepted or restored. Drafts stay durable; ownership is process-local,
// like the follow-up queue, so a restart leaves the original drafts recoverable.
type QueuedSubmission struct {
	service   *Service
	sessionID string
	ids       []uint64
	records   []Record
}

func (s *Service) isQueued(sessionID string, id uint64) bool {
	_, ok := s.queued.Load(queuedAnnotationKey{sessionID, id})
	return ok
}

// Queue captures an immutable snapshot, pins its drafts, and releases the mutation lock.
// The caller must eventually accept the snapshot or Release it on restore/shutdown.
func (p *PreparedSubmission) Queue() *QueuedSubmission {
	if p == nil || p.done || p.reserved || p.queued != nil {
		panic("invalid queued annotation preparation")
	}
	queued := &QueuedSubmission{service: p.service, sessionID: p.sessionID, ids: append([]uint64(nil), p.ids...), records: p.Records}
	for _, id := range queued.ids {
		p.service.queued.Store(queuedAnnotationKey{p.sessionID, id}, queued)
	}
	p.Release()
	return queued
}

// Prepare locks the retained snapshot for acceptance without re-reading workspace
// evidence that the active run may have changed since queue admission.
func (q *QueuedSubmission) Prepare(ctx context.Context) (*PreparedSubmission, error) {
	lock := q.service.sessionLock(q.sessionID)
	lock.Lock()
	if err := ctx.Err(); err != nil {
		lock.Unlock()
		return nil, err
	}
	for _, id := range q.ids {
		owner, ok := q.service.queued.Load(queuedAnnotationKey{q.sessionID, id})
		if !ok || owner != q {
			lock.Unlock()
			return nil, fmt.Errorf("queued annotation %d is no longer owned", id)
		}
	}
	return &PreparedSubmission{service: q.service, sessionID: q.sessionID, ids: q.ids, Records: q.records, lock: lock, queued: q}, nil
}

// Release returns the original drafts to normal editing and submission.
func (q *QueuedSubmission) Release() {
	lock := q.service.sessionLock(q.sessionID)
	lock.Lock()
	defer lock.Unlock()
	q.releaseLocked()
}

func (q *QueuedSubmission) releaseLocked() {
	for _, id := range q.ids {
		q.service.queued.CompareAndDelete(queuedAnnotationKey{q.sessionID, id}, q)
	}
}
