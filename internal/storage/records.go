package storage

import kitsession "github.com/akonwi/kit/internal/session"

type SessionRecord = kitsession.SessionRecord
type NewSession = kitsession.NewSession
type CWDMutation = kitsession.CWDMutation

var ErrNotFound = kitsession.ErrNotFound

var _ kitsession.Repository = (*Store)(nil)
