package storage

import kitsession "github.com/akonwi/kit/internal/session"

type SessionRecord = kitsession.SessionRecord
type NewSession = kitsession.NewSession

var ErrNotFound = kitsession.ErrNotFound

var _ kitsession.Repository = (*Store)(nil)
