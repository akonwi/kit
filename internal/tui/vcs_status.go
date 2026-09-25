package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

const (
	vcsReconnectMinDelay = time.Second
	vcsReconnectMaxDelay = 15 * time.Second
)

// vcsMonitor owns one attachment's server-pushed repository stream. A new
// monitor is created per attachment (and per cwd restart) so deliveries from a
// previous attachment to the same session and cwd (A→B→A) can never mutate
// the current one, and issued sequences establish per-attachment freshness:
// only the latest issued delivery may apply.
type vcsMonitor struct {
	ctx    context.Context
	cancel context.CancelFunc
	// issued is incremented when a request starts; goroutine-safe.
	issued atomic.Uint64
	// applied is the highest sequence presented so far; UI thread only.
	applied uint64
}

func formatVCSLocation(location string, status *protocol.VCSStatus) string {
	if status == nil {
		return location
	}
	var head string
	switch status.Head.Kind {
	case protocol.VCSHeadBranch, protocol.VCSHeadUnborn:
		head = status.Head.Name
	case protocol.VCSHeadDetached:
		head = "detached@" + status.Head.OID[:min(7, len(status.Head.OID))]
	}
	if head == "" {
		return location
	}
	if status.Dirty {
		head += "*"
	}
	if number, _, ok := footerPullRequest(status); ok {
		head += " " + glyphMiddleDot + " PR #" + strconv.Itoa(number)
	}
	return location + " (" + head + ")"
}

// footerPullRequest returns pull-request metadata for footer presentation only
// when it is complete and its target is safe to open: a positive number and an
// absolute, credential-free http(s) URL on a named branch head. Partial or
// unsafe metadata renders nothing rather than an inert or dangerous affordance.
func footerPullRequest(status *protocol.VCSStatus) (int, string, bool) {
	if status == nil || status.PullRequest == nil || status.Head.Kind != protocol.VCSHeadBranch {
		return 0, "", false
	}
	pr := status.PullRequest
	if pr.Number <= 0 {
		return 0, "", false
	}
	safe := safeExternalHyperlink(pr.URL)
	if safe == "" || (!strings.HasPrefix(safe, "https://") && !strings.HasPrefix(safe, "http://")) {
		return 0, "", false
	}
	return pr.Number, safe, true
}

func footerPullRequestText(status *protocol.VCSStatus) string {
	number, _, ok := footerPullRequest(status)
	if !ok {
		return ""
	}
	return "PR #" + strconv.Itoa(number)
}

// footerPullRequestURL is the validated primary-click target for the footer
// location, or empty when no pull request should be presented.
func footerPullRequestURL(status *protocol.VCSStatus) string {
	_, target, _ := footerPullRequest(status)
	return target
}

func (s *appState) startVCSMonitoring() {
	s.stopVCSMonitoring()
	if s.bound == nil || s.session.ID == "" || s.session.CWD == "" {
		return
	}
	parent := s.attachmentCtx
	if parent == nil {
		parent = s.ctx
	}
	monitor := &vcsMonitor{}
	monitor.ctx, monitor.cancel = context.WithCancel(parent)
	s.vcs = monitor
	bound := s.bound
	runtime := s.Context().Runtime()
	go watchVCSStream(monitor, bound, vcsReconnectMinDelay, func(result protocol.SessionVCSStatus) {
		dispatchVCSResult(monitor, runtime, s, result)
	})
}

// watchVCSStream owns one attachment's server-pushed repository stream. It
// reconnects with bounded backoff on transient failures and stops on terminal
// authentication, missing-session, and protocol failures.
func watchVCSStream(monitor *vcsMonitor, bound sessionclient.Session, minDelay time.Duration, deliver func(protocol.SessionVCSStatus)) {
	delay := minDelay
	for monitor.ctx.Err() == nil {
		delivered := false
		err := bound.WatchVCS(monitor.ctx, func(result protocol.SessionVCSStatus) {
			delivered = true
			deliver(result)
		})
		if monitor.ctx.Err() != nil {
			return
		}
		var terminal *sessionclient.VCSWatchTerminalError
		if errors.As(err, &terminal) {
			return
		}
		if delivered {
			delay = minDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-monitor.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, vcsReconnectMaxDelay)
	}
}

func dispatchVCSResult(monitor *vcsMonitor, runtime ui.Runtime, state *appState, result protocol.SessionVCSStatus) {
	sequence := monitor.issued.Add(1)
	runtime.Dispatch(func() {
		state.SetState(func() { state.applyVCSResult(monitor, sequence, result) })
	})
}

// applyVCSResult presents one completed response on the UI thread. It drops
// callbacks whose monitor is not the current attachment's or whose attachment
// was canceled, then drops superseded responses so an earlier read cannot
// restore a stale clickable PR while a newer refresh is pending.
func (s *appState) applyVCSResult(monitor *vcsMonitor, sequence uint64, result protocol.SessionVCSStatus) bool {
	if s.vcs != monitor || monitor.ctx == nil || monitor.ctx.Err() != nil {
		return false
	}
	if sequence != monitor.issued.Load() || sequence <= monitor.applied {
		return false
	}
	if !s.applyVCSStatus(result) {
		return false
	}
	monitor.applied = sequence
	return true
}

func (s *appState) applyVCSStatus(result protocol.SessionVCSStatus) bool {
	if result.SessionID != s.session.ID || result.CWD != s.session.CWD {
		return false
	}
	s.vcsStatus = result.Status
	s.location = formatVCSLocation(s.locationBase, result.Status)
	return true
}

func (s *appState) stopVCSMonitoring() {
	if s.vcs != nil && s.vcs.cancel != nil {
		s.vcs.cancel()
	}
	s.vcs = nil
}
