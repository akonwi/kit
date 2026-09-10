package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/akonwi/kit/internal/protocol"
)

func TestFormatVCSLocation(t *testing.T) {
	t.Parallel()
	const oid = "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name   string
		status *protocol.VCSStatus
		want   string
	}{
		{name: "unavailable", want: "~/repo"},
		{name: "clean branch", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"}}, want: "~/repo (main)"},
		{name: "dirty branch", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "feature"}, Dirty: true}, want: "~/repo (feature*)"},
		{name: "unborn", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadUnborn, Name: "main"}, Dirty: true}, want: "~/repo (main*)"},
		{name: "detached", status: &protocol.VCSStatus{Head: protocol.VCSHead{Kind: protocol.VCSHeadDetached, OID: oid}}, want: "~/repo (detached@0123456)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := formatVCSLocation("~/repo", test.status); got != test.want {
				t.Fatalf("formatVCSLocation() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestApplyVCSStatusCorrelatesOnlySessionAndCWD(t *testing.T) {
	t.Parallel()
	state := appState{
		session:      protocol.SessionInfo{ID: "session_current", CWD: "/repo"},
		locationBase: "~/repo", location: "~/repo",
	}
	clean := protocol.SessionVCSStatus{
		SessionID: "session_current", CWD: "/repo",
		Status: &protocol.VCSStatus{Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"}},
	}
	dirty := clean
	dirty.Status = &protocol.VCSStatus{Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"}, Dirty: true}
	if !state.applyVCSStatus(dirty) || !state.applyVCSStatus(clean) {
		t.Fatal("same-session concurrent results were not accepted")
	}
	if state.location != "~/repo (main)" {
		t.Fatalf("last completed result did not win: %q", state.location)
	}
	if state.applyVCSStatus(protocol.SessionVCSStatus{SessionID: "session_other", CWD: "/repo"}) {
		t.Fatal("status for another session was accepted")
	}
	if state.applyVCSStatus(protocol.SessionVCSStatus{SessionID: "session_current", CWD: "/other"}) {
		t.Fatal("status for another cwd was accepted")
	}
	if state.location != "~/repo (main)" {
		t.Fatalf("mismatched result changed location: %q", state.location)
	}
	if !state.applyVCSStatus(protocol.SessionVCSStatus{SessionID: "session_current", CWD: "/repo"}) || state.location != "~/repo" {
		t.Fatalf("successful unavailable status did not clear VCS presentation: %q", state.location)
	}
}

func TestFetchVCSStatusDoesNotApplyTransportFailures(t *testing.T) {
	t.Parallel()
	bound := fakeSession{id: "session_current", vcsStatus: func(context.Context) (protocol.SessionVCSStatus, error) {
		return protocol.SessionVCSStatus{}, errors.New("transport failed")
	}}
	if _, ok := fetchVCSStatus(context.Background(), bound); ok {
		t.Fatal("transport failure produced an applicable status")
	}
}

func TestVCSRefreshNeededAfterWorkspaceMutationOpportunity(t *testing.T) {
	t.Parallel()
	if vcsRefreshNeeded([]protocol.SessionEvent{{Kind: protocol.SessionEventAssistantCompleted}}) {
		t.Fatal("assistant completion requested a VCS refresh")
	}
	if !vcsRefreshNeeded([]protocol.SessionEvent{{Kind: protocol.SessionEventToolCompleted}}) {
		t.Fatal("tool completion did not request a VCS refresh")
	}
	if !vcsRefreshNeeded([]protocol.SessionEvent{{Kind: protocol.SessionEventRunFinished}}) {
		t.Fatal("run settlement did not request a VCS refresh")
	}
}
