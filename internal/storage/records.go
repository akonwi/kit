package storage

import kitsession "github.com/akonwi/kit/internal/session"

// Persistence record aliases remain here for storage callers while the owning
// vocabulary and repository port live in the session domain.
type SessionRecord = kitsession.SessionRecord
type NewSession = kitsession.NewSession
type TurnRecord = kitsession.TurnRecord
type ParentRunRecord = kitsession.ParentRunRecord
type RunStatus = kitsession.RunStatus
type NewMessageRecord = kitsession.NewMessageRecord
type MessageRecord = kitsession.MessageRecord

const (
	RunStatusPending     = kitsession.RunStatusPending
	RunStatusQueued      = kitsession.RunStatusQueued
	RunStatusRunning     = kitsession.RunStatusRunning
	RunStatusCompleted   = kitsession.RunStatusCompleted
	RunStatusFailed      = kitsession.RunStatusFailed
	RunStatusAborted     = kitsession.RunStatusAborted
	RunStatusInterrupted = kitsession.RunStatusInterrupted
)

var ErrNotFound = kitsession.ErrNotFound

var _ kitsession.Repository = (*Store)(nil)
