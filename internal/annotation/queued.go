package annotation

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// PinQueued marks drafts as belonging to a queued message that already
// carries their snapshot, as a durable queue restored after a restart does.
// Pinned drafts cannot be edited, deleted, or submitted again until they are
// unpinned or consumed.
func (s *Service) PinQueued(sessionID string, ids []uint64) error {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	for _, id := range ids {
		if s.isQueued(sessionID, id) {
			return fmt.Errorf("annotation %d already belongs to a queued message", id)
		}
	}
	queued := &QueuedSubmission{service: s, sessionID: sessionID, ids: append([]uint64(nil), ids...)}
	for _, id := range ids {
		s.queued.Store(queuedAnnotationKey{sessionID, id}, queued)
	}
	return nil
}

// UnpinQueued returns drafts of a withdrawn queued message to normal editing
// and submission.
func (s *Service) UnpinQueued(sessionID string, ids []uint64) {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	for _, id := range ids {
		s.queued.Delete(queuedAnnotationKey{sessionID, id})
	}
}

// ReserveQueued durably hides the drafts of a queued message under
// submissionID before the message is accepted, so a crash leaves them
// recoverable by RecoverSubmissions. FinalizeQueued or RollbackQueued ends it.
func (s *Service) ReserveQueued(ctx context.Context, sessionID, submissionID string, ids []uint64) error {
	if !strings.HasPrefix(submissionID, "annotation_submission_") {
		return fmt.Errorf("annotation submission identity is invalid")
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	return s.repositoryFor(sessionID).ReserveAnnotations(ctx, sessionID, submissionID, ids)
}

// FinalizeQueued consumes reserved drafts once their message was accepted,
// and unpins them.
func (s *Service) FinalizeQueued(ctx context.Context, sessionID, submissionID string, ids []uint64) error {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	if err := s.repositoryFor(sessionID).FinalizeAnnotationSubmission(ctx, sessionID, submissionID); err != nil {
		return err
	}
	for _, id := range ids {
		s.queued.Delete(queuedAnnotationKey{sessionID, id})
	}
	return nil
}

// RollbackQueued restores reserved drafts whose message was not accepted;
// they stay pinned to their queued message.
func (s *Service) RollbackQueued(ctx context.Context, sessionID, submissionID string) error {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	return s.repositoryFor(sessionID).RollbackAnnotationSubmission(ctx, sessionID, submissionID)
}

// ConsumeQueued removes the drafts a queued message submitted once that
// message was accepted, and unpins them. It is idempotent by submissionID,
// and drafts that are already gone count as consumed.
func (s *Service) ConsumeQueued(ctx context.Context, sessionID, submissionID string, ids []uint64) error {
	if !strings.HasPrefix(submissionID, "annotation_submission_") {
		return fmt.Errorf("annotation submission identity is invalid")
	}
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()
	repository := s.repositoryFor(sessionID)
	err := repository.ReserveAnnotations(ctx, sessionID, submissionID, ids)
	if err == nil {
		err = repository.FinalizeAnnotationSubmission(ctx, sessionID, submissionID)
	} else if errors.Is(err, ErrNotFound) {
		err = nil
	}
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.queued.Delete(queuedAnnotationKey{sessionID, id})
	}
	return nil
}
