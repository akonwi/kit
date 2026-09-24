package tui

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/sessionclient"
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
		{name: "branch with pull request", status: &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 123, URL: "https://github.com/akonwi/kit/pull/123"},
		}, want: "~/repo (main · PR #123)"},
		{name: "dirty branch with pull request", status: &protocol.VCSStatus{
			Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "feature"}, Dirty: true,
			PullRequest: &protocol.GitHubPullRequest{Number: 7, URL: "https://github.com/akonwi/kit/pull/7"},
		}, want: "~/repo (feature* · PR #7)"},
		{name: "unsafe pull request URL renders no PR", status: &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 5, URL: "javascript:alert(1)"},
		}, want: "~/repo (main)"},
		{name: "non-positive pull request number renders no PR", status: &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 0, URL: "https://github.com/akonwi/kit/pull/5"},
		}, want: "~/repo (main)"},
		{name: "detached head suppresses pull request", status: &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadDetached, OID: oid},
			PullRequest: &protocol.GitHubPullRequest{Number: 9, URL: "https://github.com/akonwi/kit/pull/9"},
		}, want: "~/repo (detached@0123456)"},
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

func TestFooterPullRequestURLValidatesTarget(t *testing.T) {
	t.Parallel()
	branchPR := func(url string) *protocol.VCSStatus {
		return &protocol.VCSStatus{
			Head:        protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 42, URL: url},
		}
	}
	if got := footerPullRequestURL(branchPR("https://github.com/akonwi/kit/pull/42")); got != "https://github.com/akonwi/kit/pull/42" {
		t.Fatalf("safe https URL = %q", got)
	}
	if got := footerPullRequestURL(branchPR("http://github.internal/pull/42")); got != "http://github.internal/pull/42" {
		t.Fatalf("safe http URL = %q", got)
	}
	for _, unsafe := range []string{
		"",
		"/pull/42",
		"javascript:alert(1)",
		"mailto:pr@example.com",
		"https://user:secret@github.com/pull/42",
		"https://github.com/pull/42\n",
		"https://github.com/pull/\x1b42",
	} {
		if got := footerPullRequestURL(branchPR(unsafe)); got != "" {
			t.Errorf("footerPullRequestURL(%q) = %q, want empty", unsafe, got)
		}
	}
	if got := footerPullRequestURL(nil); got != "" {
		t.Fatalf("nil status URL = %q", got)
	}
}

func TestStaleVCSResultCannotRestorePullRequestAfterCWDChange(t *testing.T) {
	t.Parallel()
	state := appState{
		session:      protocol.SessionInfo{ID: "session_current", CWD: "/repo"},
		locationBase: "~/repo", location: "~/repo",
	}
	withPR := protocol.SessionVCSStatus{
		SessionID: "session_current", CWD: "/repo",
		Status: &protocol.VCSStatus{
			Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: 12, URL: "https://github.com/akonwi/kit/pull/12"},
		},
	}
	if !state.applyVCSStatus(withPR) || state.location != "~/repo (main · PR #12)" {
		t.Fatalf("pull request did not reach the footer: %q", state.location)
	}
	if footerPullRequestURL(state.vcsStatus) != "https://github.com/akonwi/kit/pull/12" {
		t.Fatalf("click target missing: %q", footerPullRequestURL(state.vcsStatus))
	}
	// The session changes directory: presentation resets and in-flight results
	// for the old cwd must not restore the old pull request.
	state.session.CWD = "/other"
	state.vcsStatus = nil
	state.locationBase, state.location = "~/other", "~/other"
	if state.applyVCSStatus(withPR) {
		t.Fatal("stale result for the previous cwd was accepted")
	}
	if state.location != "~/other" || footerPullRequestURL(state.vcsStatus) != "" {
		t.Fatalf("stale result restored PR presentation: %q %q", state.location, footerPullRequestURL(state.vcsStatus))
	}
	// A fresh result for the new cwd without a PR keeps the footer clean.
	if !state.applyVCSStatus(protocol.SessionVCSStatus{SessionID: "session_current", CWD: "/other"}) {
		t.Fatal("fresh result for the new cwd was rejected")
	}
	if state.location != "~/other" || footerPullRequestURL(state.vcsStatus) != "" {
		t.Fatalf("fresh empty status left PR presentation: %q", state.location)
	}
}

func prStatusResult(session, cwd string, number int) protocol.SessionVCSStatus {
	return protocol.SessionVCSStatus{
		SessionID: session, CWD: cwd,
		Status: &protocol.VCSStatus{
			Root: "/repo", Head: protocol.VCSHead{Kind: protocol.VCSHeadBranch, Name: "main"},
			PullRequest: &protocol.GitHubPullRequest{Number: number, URL: "https://github.com/akonwi/kit/pull/" + strconv.Itoa(number)},
		},
	}
}

func TestCanceledAttachmentCallbackCannotMutateReattachedState(t *testing.T) {
	t.Parallel()
	state := appState{
		session:      protocol.SessionInfo{ID: "session_current", CWD: "/repo"},
		locationBase: "~/repo", location: "~/repo",
	}
	// Attachment A is canceled, the user detaches and re-attaches (A→B→A):
	// same session and cwd, but a fresh monitor.
	oldContext, cancelOld := context.WithCancel(context.Background())
	old := &vcsMonitor{ctx: oldContext, cancel: cancelOld}
	state.vcs = old
	cancelOld()
	stale := prStatusResult("session_current", "/repo", 11)
	if state.applyVCSResult(old, 1, stale) {
		t.Fatal("canceled attachment callback mutated state")
	}
	current := &vcsMonitor{ctx: context.Background()}
	current.issued.Store(1)
	state.vcs = current
	if state.applyVCSResult(old, 2, stale) {
		t.Fatal("previous attachment's monitor mutated the new attachment")
	}
	if state.location != "~/repo" || state.vcsStatus != nil {
		t.Fatalf("stale callbacks changed presentation: %q", state.location)
	}
	// The new attachment's own response still applies.
	if !state.applyVCSResult(current, 1, prStatusResult("session_current", "/repo", 12)) {
		t.Fatal("current attachment response was rejected")
	}
	if state.location != "~/repo (main · PR #12)" {
		t.Fatalf("current attachment result not presented: %q", state.location)
	}
}

func TestOutOfOrderVCSResponsesCannotRollPresentationBackward(t *testing.T) {
	t.Parallel()
	state := appState{
		session:      protocol.SessionInfo{ID: "session_current", CWD: "/repo"},
		locationBase: "~/repo", location: "~/repo",
	}
	monitor := &vcsMonitor{ctx: context.Background()}
	state.vcs = monitor
	monitor.issued.Store(2)
	// The later-issued request completes first with a newer pull request.
	if !state.applyVCSResult(monitor, 2, prStatusResult("session_current", "/repo", 20)) {
		t.Fatal("later-issued response was rejected")
	}
	if state.location != "~/repo (main · PR #20)" {
		t.Fatalf("newer response not presented: %q", state.location)
	}
	// The earlier-issued request completes afterward with older metadata.
	if state.applyVCSResult(monitor, 1, prStatusResult("session_current", "/repo", 19)) {
		t.Fatal("earlier-issued response rolled presentation backward")
	}
	if state.location != "~/repo (main · PR #20)" {
		t.Fatalf("presentation rolled backward: %q", state.location)
	}
	// Replaying the same sequence is also rejected.
	if state.applyVCSResult(monitor, 2, prStatusResult("session_current", "/repo", 19)) {
		t.Fatal("replayed sequence was accepted")
	}
	monitor.issued.Store(3)
	// The next issued request may clear the pull request going forward.
	if !state.applyVCSResult(monitor, 3, protocol.SessionVCSStatus{SessionID: "session_current", CWD: "/repo"}) {
		t.Fatal("newer clearing response was rejected")
	}
	if state.location != "~/repo" {
		t.Fatalf("clearing response not presented: %q", state.location)
	}
}

func TestEarlierVCSResponseCannotRestorePRWhileNewerRefreshIsPending(t *testing.T) {
	state := appState{session: protocol.SessionInfo{ID: "session_current", CWD: "/repo"}, locationBase: "~/repo", location: "~/repo"}
	monitor := &vcsMonitor{ctx: context.Background()}
	monitor.issued.Store(2)
	state.vcs = monitor
	if state.applyVCSResult(monitor, 1, prStatusResult("session_current", "/repo", 19)) {
		t.Fatal("superseded request restored old PR")
	}
	if state.location != "~/repo" || state.vcsStatus != nil {
		t.Fatalf("pending refresh presentation=%q", state.location)
	}
	if !state.applyVCSResult(monitor, 2, prStatusResult("session_current", "/repo", 20)) {
		t.Fatal("latest result rejected")
	}
	if state.location != "~/repo (main · PR #20)" {
		t.Fatalf("latest presentation=%q", state.location)
	}
}

func TestWatchVCSStreamDeliversServerPushedUpdatesInOrder(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor := &vcsMonitor{ctx: ctx, cancel: cancel}
	connections := 0
	bound := fakeSession{id: "session_current", watchVCS: func(watchCtx context.Context, receive func(protocol.SessionVCSStatus)) error {
		connections++
		switch connections {
		case 1:
			// Initial latest snapshot, then a deduplicated update.
			receive(protocol.SessionVCSStatus{SessionID: "session_current", CWD: "/repo"})
			receive(prStatusResult("session_current", "/repo", 21))
			return errors.New("stream reset") // transient: reconnect fresh
		default:
			receive(prStatusResult("session_current", "/repo", 22))
			<-watchCtx.Done()
			return watchCtx.Err()
		}
	}}
	var delivered []protocol.SessionVCSStatus
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchVCSStream(monitor, bound, time.Millisecond, func(result protocol.SessionVCSStatus) {
			delivered = append(delivered, result)
			if len(delivered) == 3 {
				cancel()
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream watcher did not finish")
	}
	if len(delivered) != 3 || connections != 2 {
		t.Fatalf("delivered %d over %d connections", len(delivered), connections)
	}
	if delivered[0].Status != nil || delivered[1].Status.PullRequest.Number != 21 || delivered[2].Status.PullRequest.Number != 22 {
		t.Fatalf("unexpected delivery order: %+v", delivered)
	}
}

func TestWatchVCSStreamStopsOnTerminalErrorsWithoutFallback(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor := &vcsMonitor{ctx: ctx, cancel: cancel}
	connections, snapshots := 0, 0
	bound := fakeSession{
		id: "session_current",
		watchVCS: func(context.Context, func(protocol.SessionVCSStatus)) error {
			connections++
			return &sessionclient.VCSWatchTerminalError{Err: errors.New("session not found")}
		},
		vcsStatus: func(context.Context) (protocol.SessionVCSStatus, error) {
			snapshots++
			return protocol.SessionVCSStatus{}, nil
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchVCSStream(monitor, bound, time.Millisecond, func(protocol.SessionVCSStatus) {
			t.Error("terminal failure delivered a status")
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal error did not stop the watcher")
	}
	if connections != 1 || snapshots != 0 {
		t.Fatalf("connections=%d snapshots=%d, want 1 and 0", connections, snapshots)
	}
}

func TestWatchVCSStreamReconnectsAfterTransientOpenFailure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor := &vcsMonitor{ctx: ctx, cancel: cancel}
	connections := 0
	bound := fakeSession{
		id: "session_current",
		watchVCS: func(watchCtx context.Context, receive func(protocol.SessionVCSStatus)) error {
			connections++
			if connections == 1 {
				return errors.New("connection refused")
			}
			receive(prStatusResult("session_current", "/repo", 30))
			<-watchCtx.Done()
			return watchCtx.Err()
		},
		vcsStatus: func(context.Context) (protocol.SessionVCSStatus, error) {
			t.Fatal("stream watcher polled snapshot endpoint")
			return protocol.SessionVCSStatus{}, nil
		},
	}
	var delivered []protocol.SessionVCSStatus
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchVCSStream(monitor, bound, time.Millisecond, func(result protocol.SessionVCSStatus) {
			delivered = append(delivered, result)
			cancel()
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transient failure did not reconnect")
	}
	if connections != 2 || len(delivered) != 1 || delivered[0].Status.PullRequest.Number != 30 {
		t.Fatalf("connections=%d deliveries=%+v", connections, delivered)
	}
}
