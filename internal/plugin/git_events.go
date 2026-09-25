package plugin

import (
	"context"
	"encoding/json"
	"time"
)

const (
	gitEventPollInterval = 10 * time.Second
	gitEventProbeTimeout = 2 * time.Second
)

func cloneGitContext(value *GitContext) *GitContext {
	if value == nil {
		return nil
	}
	result := *value
	if value.PullRequest != nil {
		pr := *value.PullRequest
		result.PullRequest = &pr
	}
	if value.Branch != nil {
		branch := *value.Branch
		result.Branch = &branch
	}
	return &result
}
func sameGitContext(a, b *GitContext) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Root != b.Root || a.Dirty != b.Dirty || !samePullRequest(a.PullRequest, b.PullRequest) {
		return false
	}
	if a.Branch == nil || b.Branch == nil {
		return a.Branch == nil && b.Branch == nil
	}
	return *a.Branch == *b.Branch
}

// gitLoop owns volatile Git observation independently of client attachments and
// sequential plugin initialization. Samples are coalesced, never replayed.
func (h *Host) gitLoop() {
	defer close(h.gitDone)
	ticker := time.NewTicker(gitEventPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.refreshGit()
		case <-h.config.ProjectChanged:
			h.refreshGit()
		}
	}
}

func (h *Host) refreshGit() {
	if h.config.Project == nil {
		return
	}
	h.mu.Lock()
	view := h.view
	var eligible []InstanceID
	for _, entry := range h.entries {
		if !h.closed && h.currentReadyEntryLocked(entry) && entry.context.Project.Cwd == view.cwd {
			eligible = append(eligible, entry.owner)
		}
	}
	h.mu.Unlock()
	if len(eligible) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(h.ctx, gitEventProbeTimeout)
	next, err := h.config.Project(ctx, view.cwd)
	expired := ctx.Err() != nil
	cancel()
	if err != nil || expired {
		return
	}
	git := cloneGitContext(next.Git)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil || h.view.epoch != view.epoch || h.view.projectEpoch != view.projectEpoch || h.view.cwd != view.cwd {
		return
	}
	for _, owner := range eligible {
		entry := h.activeEntryLocked(owner)
		if entry == nil || entry.instance == nil || !h.currentReadyEntryLocked(entry) || entry.context.Project.Cwd != view.cwd {
			continue
		}
		h.publishGitLocked(entry, git)
	}
}

// publishGitLocked queues under host/instance admission, then advances that
// generation's baseline. It never waits for pipe IO or plugin acknowledgment.
func (h *Host) publishGitLocked(entry *hostEntry, next *GitContext) {
	if sameGitContext(entry.context.Project.Git, next) {
		return
	}
	params, err := json.Marshal(struct {
		Git *GitContext `json:"git"`
	}{next})
	if err == nil {
		err = entry.instance.tryNotify("kit/events/git.changed", params)
	}
	if err != nil {
		entry.instance.beginStop("runtime", err)
		return
	}
	entry.context.Project.Git = cloneGitContext(next)
}

func samePullRequest(a, b *PullRequestContext) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
