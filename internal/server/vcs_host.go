package server

import (
	"context"
	"errors"

	"github.com/akonwi/kit/internal/plugin"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/vcs"
)

func (h *sessionPluginHost) Start() {
	h.vcs.Start()
	h.Host.Start()
}
func (h *sessionPluginHost) ChangeCWD(cwd string) {
	h.vcs.ChangeCWD(cwd)
	h.Host.ChangeCWD(cwd)
}
func (h *sessionPluginHost) Close(ctx context.Context) error {
	return errors.Join(h.vcs.Close(ctx), h.Host.Close(ctx))
}
func (h *sessionPluginHost) projectContext(ctx context.Context, cwd string) (plugin.ProjectContext, error) {
	value, err := h.vcs.Read(ctx)
	if err != nil {
		return plugin.ProjectContext{}, err
	}
	result := plugin.ProjectContext{Cwd: cwd}
	if value.CWD != cwd {
		return result, errors.New("workspace VCS observation changed")
	}
	if value.Git == nil {
		return result, nil
	}
	status := value.Git
	var branch *string
	if status.Head.Kind != vcs.HeadDetached && status.Head.Name != "" {
		name := status.Head.Name
		branch = &name
	}
	result.Git = &plugin.GitContext{Root: status.Root, Branch: branch, Dirty: status.Dirty}
	if value.PullRequest != nil {
		result.Git.PullRequest = &plugin.PullRequestContext{Number: value.PullRequest.Number, URL: value.PullRequest.URL}
	}
	return result, nil
}
func (h *sessionPluginHost) VCS(ctx context.Context) (session.VCSUpdate, error) {
	value, err := h.vcs.Read(ctx)
	return projectVCSUpdate(value), err
}
func (h *sessionPluginHost) CurrentPullRequestStatus() session.PullRequestStatusUpdate {
	return projectPullRequestStatusUpdate(h.vcs.Current())
}
func (h *sessionPluginHost) SubscribePullRequestStatus() (session.PullRequestStatusSubscription, error) {
	source, err := h.vcs.SubscribePullRequestStatus()
	if err != nil {
		return nil, err
	}
	return sessionPullRequestStatusSubscription{source}, nil
}

func (h *sessionPluginHost) SubscribeVCS() (session.VCSSubscription, error) {
	source, err := h.vcs.Subscribe()
	if err != nil {
		return nil, err
	}
	return sessionVCSSubscription{source}, nil
}

type sessionPullRequestStatusSubscription struct {
	source *vcs.PullRequestStatusSubscription
}

func (s sessionPullRequestStatusSubscription) Close() { s.source.Close() }
func (s sessionPullRequestStatusSubscription) Next(ctx context.Context) (session.PullRequestStatusUpdate, error) {
	value, err := s.source.Next(ctx)
	return session.PullRequestStatusUpdate{CWD: value.CWD, Root: value.Root, RepositoryID: value.RepositoryID, Branch: value.Branch, Status: value.Status}, err
}
func projectPullRequestStatusUpdate(value vcs.Snapshot) session.PullRequestStatusUpdate {
	result := session.PullRequestStatusUpdate{CWD: value.CWD, Status: value.PullRequest}
	if value.Git != nil && value.Git.Head.Kind == vcs.HeadBranch {
		result.Root = value.Git.Root
		result.RepositoryID = value.Git.RepositoryID
		result.Branch = value.Git.Head.Name
	}
	return result
}

type sessionVCSSubscription struct{ source *vcs.Subscription }

func (s sessionVCSSubscription) Close() { s.source.Close() }
func (s sessionVCSSubscription) Next(ctx context.Context) (session.VCSUpdate, error) {
	value, err := s.source.Next(ctx)
	return projectVCSUpdate(value), err
}
func projectVCSUpdate(value vcs.Snapshot) session.VCSUpdate {
	result := session.VCSUpdate{CWD: value.CWD}
	if value.Git != nil {
		git := value.Git
		result.Status = &session.VCSStatus{Root: git.Root, HeadKind: string(git.Head.Kind), HeadName: git.Head.Name, HeadOID: git.Head.OID, Dirty: git.Dirty}
		if value.PullRequest != nil {
			result.Status.PullRequest = &session.VCSPullRequest{Number: value.PullRequest.Number, URL: value.PullRequest.URL}
		}
	}
	return result
}
