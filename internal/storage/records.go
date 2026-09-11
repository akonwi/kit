package storage

import (
	kitsession "github.com/akonwi/kit/internal/session"
	kitsubagent "github.com/akonwi/kit/internal/subagent"
)

type SessionRecord = kitsession.SessionRecord
type NewSession = kitsession.NewSession
type CWDMutation = kitsession.CWDMutation
type ConfigurationUpdate = kitsession.ConfigurationUpdate
type ConfigurationConflictError = kitsession.ConfigurationConflictError

var ErrNotFound = kitsession.ErrNotFound

var _ kitsession.Repository = (*Store)(nil)
var _ kitsubagent.Repository = (*Store)(nil)
