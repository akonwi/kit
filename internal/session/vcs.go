package session

import (
	"context"
	"errors"
)

// VCSPullRequest is optional remote metadata for one named branch.
type VCSPullRequest struct {
	Number int
	URL    string
}

// VCSStatus is the session-owned projection of combined repository metadata.
type VCSStatus struct {
	Root, HeadKind, HeadName, HeadOID string
	Dirty                             bool
	PullRequest                       *VCSPullRequest
}

// VCSUpdate binds volatile repository state to its authoritative workspace.
type VCSUpdate struct {
	CWD    string
	Status *VCSStatus
}

// VCSSubscription supplies a fresh snapshot and latest-only live changes.
type VCSSubscription interface {
	Next(context.Context) (VCSUpdate, error)
	Close()
}

// VCSHost is the optional built-in observation port of a loaded session host.
type VCSHost interface {
	VCS(context.Context) (VCSUpdate, error)
	SubscribeVCS() (VCSSubscription, error)
}

// ErrVCSUnavailable means this runtime has no built-in observation adapter.
var ErrVCSUnavailable = errors.New("session VCS observation unavailable")

// VCS reads the shared current status, waiting only for initial local observation.
func (m *Manager) VCS(ctx context.Context, id string) (VCSUpdate, error) {
	if err := m.beginOperation(); err != nil {
		return VCSUpdate{}, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, id)
	if err != nil {
		return VCSUpdate{}, err
	}
	host, ok := loaded.plugins.(VCSHost)
	if !ok {
		return VCSUpdate{}, ErrVCSUnavailable
	}
	return host.VCS(ctx)
}

// SubscribeVCS observes the same state used by the owning session's plugins.
// The caller must close the subscription; runtime shutdown closes it as well.
func (m *Manager) SubscribeVCS(ctx context.Context, id string) (VCSSubscription, error) {
	if err := m.beginOperation(); err != nil {
		return nil, err
	}
	defer m.ops.Done()
	loaded, err := m.runtime(ctx, id)
	if err != nil {
		return nil, err
	}
	host, ok := loaded.plugins.(VCSHost)
	if !ok {
		return nil, ErrVCSUnavailable
	}
	return host.SubscribeVCS()
}
