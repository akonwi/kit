package tui

import (
	"context"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
	"go.rockorager.dev/vaxis/ui"
)

const (
	vcsRefreshInterval = 5 * time.Second
	vcsRequestTimeout  = 3 * time.Second
)

func vcsRefreshNeeded(events []protocol.SessionEvent) bool {
	for _, event := range events {
		if event.Kind == protocol.SessionEventToolCompleted || event.Kind == protocol.SessionEventRunFinished {
			return true
		}
	}
	return false
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
	return location + " (" + head + ")"
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
	monitorContext, cancel := context.WithCancel(parent)
	s.vcsContext = monitorContext
	s.vcsCancel = cancel
	bound := s.bound
	runtime := s.Context().Runtime()
	request := func() {
		go requestVCSStatus(monitorContext, bound, runtime, s)
	}
	request()
	go func() {
		ticker := time.NewTicker(vcsRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-monitorContext.Done():
				return
			case <-ticker.C:
				request()
			}
		}
	}()
}

func (s *appState) refreshVCSStatus() {
	if s.vcsContext == nil || s.bound == nil {
		return
	}
	go requestVCSStatus(s.vcsContext, s.bound, s.Context().Runtime(), s)
}

func requestVCSStatus(ctx context.Context, bound sessionclient.Session, runtime ui.Runtime, state *appState) {
	result, ok := fetchVCSStatus(ctx, bound)
	if !ok {
		return
	}
	runtime.Dispatch(func() {
		state.SetState(func() { state.applyVCSStatus(result) })
	})
}

func fetchVCSStatus(ctx context.Context, bound sessionclient.Session) (protocol.SessionVCSStatus, bool) {
	requestContext, cancel := context.WithTimeout(ctx, vcsRequestTimeout)
	defer cancel()
	result, err := bound.VCSStatus(requestContext)
	return result, err == nil
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
	if s.vcsCancel != nil {
		s.vcsCancel()
	}
	s.vcsContext = nil
	s.vcsCancel = nil
}
