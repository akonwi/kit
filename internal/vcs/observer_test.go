package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/githubpr"
	"github.com/akonwi/kit/internal/pullrequest"
)

func testObserver(t *testing.T, cache *githubpr.Cache) *Observer {
	t.Helper()
	o := NewObserver(t.Context(), "/repo", cache)
	o.interval = time.Hour
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := o.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return o
}
func nextObserved(t *testing.T, s *Subscription) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	value, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestObserverSharesProbeAndCoalescesFreshSnapshots(t *testing.T) {
	o := testObserver(t, nil)
	var count atomic.Int32
	var state atomic.Pointer[Status]
	state.Store(&Status{Root: "/repo", Head: Head{Kind: HeadBranch, Name: "main"}})
	o.probe = func(context.Context, string) (*Status, error) { count.Add(1); copy := *state.Load(); return &copy, nil }
	first, _ := o.Subscribe()
	defer first.Close()
	second, _ := o.Subscribe()
	defer second.Close()
	if initial := nextObserved(t, first); initial.CWD != "/repo" || initial.Git != nil {
		t.Fatalf("initial=%+v", initial)
	}
	o.Start()
	value, err := o.Read(t.Context())
	if err != nil || value.Git == nil || value.Git.Head.Name != "main" {
		t.Fatalf("read=%+v %v", value, err)
	}
	for range 3 {
		_, _ = o.Read(t.Context())
	}
	if count.Load() != 1 {
		t.Fatalf("readers reprobed Git %d times", count.Load())
	}
	for _, branch := range []string{"one", "two", "three"} {
		state.Store(&Status{Root: "/repo", Head: Head{Kind: HeadBranch, Name: branch}})
		o.refreshGit()
	}
	for _, source := range []*Subscription{first, second} {
		if value := nextObserved(t, source); value.Git.Head.Name != "three" {
			t.Fatalf("slow client received old state: %+v", value)
		}
	}
	o.refreshGit()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := first.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("duplicate snapshot: %v", err)
	}
	reconnect, _ := o.Subscribe()
	defer reconnect.Close()
	fresh := nextObserved(t, reconnect)
	if fresh.Git.Head.Name != "three" {
		t.Fatal(fresh)
	}
	fresh.Git.Head.Name = "mutated"
	if o.Current().Git.Head.Name != "three" {
		t.Fatal("subscriber mutated shared state")
	}
}

// Regression for #235: a transient probe failure published Git: nil, which
// plugins and clients read as "not a repository".
func TestObserverKeepsLastGitSnapshotWhenProbeFails(t *testing.T) {
	o := testObserver(t, nil)
	var failure atomic.Pointer[error]
	o.probe = func(context.Context, string) (*Status, error) {
		if err := failure.Load(); err != nil {
			return nil, *err
		}
		return &Status{Root: "/repo", Head: Head{Kind: HeadBranch, Name: "main"}, Dirty: true}, nil
	}
	o.Start()
	value, err := o.Read(t.Context())
	if err != nil || value.Git == nil || value.Git.Head.Name != "main" {
		t.Fatalf("read=%+v %v", value, err)
	}
	source, err := o.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	_ = nextObserved(t, source)
	for _, transient := range []error{ErrUnavailable, context.DeadlineExceeded} {
		failure.Store(&transient)
		o.refreshGit()
		if current := o.Current(); current.Git == nil || current.Git.Head.Name != "main" || !current.Git.Dirty {
			t.Fatalf("after %v current=%+v", transient, current)
		}
	}
	failure.Store(nil)
	o.refreshGit()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if value, err := source.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("transient failure published %+v, %v", value, err)
	}
}

// After a probe fails, an identity mismatch must not wake an immediate
// reprobe, which would fail again in a tight loop.
func TestObserverDoesNotReprobeIdentityMismatchAfterProbeFailure(t *testing.T) {
	repository := t.TempDir()
	runGitTest(t, repository, "init", "-b", "main")
	cache := githubpr.NewCache(t.Context())
	defer cache.Close()
	o := testObserver(t, cache)
	var failing atomic.Bool
	o.probe = func(context.Context, string) (*Status, error) {
		if failing.Load() {
			return nil, ErrUnavailable
		}
		return &Status{Root: repository, RepositoryID: "stale", Head: Head{Kind: HeadBranch, Name: "main"}}, nil
	}
	drainWake := func() bool {
		select {
		case <-o.wake:
			return true
		default:
			return false
		}
	}
	o.refreshGit()
	o.refreshPR()
	if !drainWake() {
		t.Fatal("identity mismatch after a successful probe did not request a reprobe")
	}
	failing.Store(true)
	o.refreshGit()
	o.refreshPR()
	if drainWake() {
		t.Fatal("identity mismatch after a failed probe requested an immediate reprobe")
	}
}

func TestObserverCompletesInitialReadWhenFirstProbeFails(t *testing.T) {
	o := testObserver(t, nil)
	o.probe = func(context.Context, string) (*Status, error) { return nil, ErrUnavailable }
	o.Start()
	value, err := o.Read(t.Context())
	if err != nil || value.CWD != "/repo" || value.Git != nil {
		t.Fatalf("read=%+v %v", value, err)
	}
}

func TestObserverPRCompletionPushesWithoutGitPollOrClientRead(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release")
	if err := syscall.Mkfifo(release, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIT_PR_RELEASE", release)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	// The lookup blocks on the FIFO only for "pr view", so the test's open for
	// writing doubles as proof that the fake gh has started. "pr checks" has
	// nothing to report.
	script := `#!/bin/sh
if [ "$2" = view ]; then
  read -r _ < "$KIT_PR_RELEASE"
  printf '%s' '{"number":47,"url":"https://github.com/a/b/pull/47","headRefName":"main"}'
else
  printf '[]'
fi
`
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cache := githubpr.NewCache(t.Context())
	defer cache.Close()
	o := testObserver(t, cache)
	var probes atomic.Int32
	o.probe = func(_ context.Context, cwd string) (*Status, error) {
		probes.Add(1)
		if cwd != "/repo" {
			return nil, nil
		}
		return &Status{Root: root, Head: Head{Kind: HeadBranch, Name: "main"}}, nil
	}
	source, _ := o.Subscribe()
	defer source.Close()
	_ = nextObserved(t, source)
	o.Start()
	local := nextObserved(t, source)
	if local.Git == nil || local.PullRequest != nil {
		t.Fatalf("local=%+v", local)
	}
	releaseBlockedLookup(t, release)
	// Delivery still spans the fake gh exiting and a second gh exec for
	// "pr checks". Both run within githubpr's five-second lookup timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	remote, err := source.Next(ctx)
	cancel()
	if err != nil {
		t.Fatalf("PR-only update: %v", err)
	}
	if remote.PullRequest == nil || remote.PullRequest.Number != 47 || remote.Git.Head.Name != "main" {
		t.Fatalf("PR-only update=%+v", remote)
	}
	if probes.Load() != 1 {
		t.Fatalf("PR delivery reprobed Git: %d", probes.Load())
	}
	o.ChangeCWD("/outside")
	cleared := nextObserved(t, source)
	if cleared.CWD != "/outside" || cleared.PullRequest != nil {
		t.Fatalf("cwd retained PR=%+v", cleared)
	}
}

// releaseBlockedLookup waits until the fake gh has opened the FIFO for
// reading, then lets it continue. Process start-up, which can be slow on a
// loaded machine or on the first exec of a new file, happens before this
// returns instead of inside the caller's delivery deadline.
func releaseBlockedLookup(t *testing.T, fifo string) {
	t.Helper()
	// githubpr kills a lookup after five seconds, including gh start-up.
	deadline := time.Now().Add(5 * time.Second)
	for {
		writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_, err = writer.WriteString("go\n")
			closeErr := writer.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("release fake gh: %v %v", err, closeErr)
			}
			return
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("fake gh did not start within the lookup timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestObserverSeparatesRichPullRequestTransitionsFromFooterUpdates(t *testing.T) {
	o := testObserver(t, nil)
	footer, err := o.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer footer.Close()
	rich, err := o.SubscribePullRequestStatus()
	if err != nil {
		t.Fatal(err)
	}
	defer rich.Close()
	_ = nextObserved(t, footer)
	if _, err := rich.Next(t.Context()); err != nil {
		t.Fatal(err)
	}
	git := &Status{Root: "/repo", Head: Head{Kind: HeadBranch, Name: "main"}}
	initial := &pullrequest.Status{Number: 42, URL: "https://github.com/a/b/pull/42", HeadRefName: "main", Checks: []pullrequest.Check{{Name: "test", State: pullrequest.CheckPending}}}
	o.mu.Lock()
	o.publishLocked(Snapshot{CWD: "/repo", Git: git, PullRequest: initial})
	o.mu.Unlock()
	if value := nextObserved(t, footer); value.PullRequest == nil || value.PullRequest.Number != 42 {
		t.Fatalf("footer=%+v", value)
	}
	if value, err := rich.Next(t.Context()); err != nil || value.Status == nil || value.Status.Checks[0].State != pullrequest.CheckPending {
		t.Fatalf("rich=%+v err=%v", value, err)
	}
	passed := pullrequest.Clone(initial)
	passed.Checks[0].State = pullrequest.CheckPassed
	o.mu.Lock()
	o.publishLocked(Snapshot{CWD: "/repo", Git: git, PullRequest: passed})
	o.mu.Unlock()
	if value, err := rich.Next(t.Context()); err != nil || value.Status.Checks[0].State != pullrequest.CheckPassed {
		t.Fatalf("rich transition=%+v err=%v", value, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := footer.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rich transition leaked into footer: %v", err)
	}
}

func TestPullRequestStatusSubscriptionFailsExplicitlyOnOverflow(t *testing.T) {
	o := testObserver(t, nil)
	source, err := o.SubscribePullRequestStatus()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	git := &Status{Root: "/repo", Head: Head{Kind: HeadBranch, Name: "main"}}
	for number := 1; number <= maxPullRequestTransitions; number++ {
		o.mu.Lock()
		o.publishLocked(Snapshot{CWD: "/repo", Git: git, PullRequest: &pullrequest.Status{Number: number, URL: "https://github.com/a/b/pull/1"}})
		o.mu.Unlock()
	}
	for range maxPullRequestTransitions {
		if _, err := source.Next(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Next(t.Context()); !errors.Is(err, ErrPullRequestTransitionOverflow) {
		t.Fatalf("overflow error=%v", err)
	}
}

func TestObserverCWDRevokesStaleProbeAndCloseJoins(t *testing.T) {
	o := testObserver(t, nil)
	started, ended := make(chan struct{}), make(chan struct{})
	o.probe = func(ctx context.Context, cwd string) (*Status, error) {
		if cwd == "/repo" {
			close(started)
			<-ctx.Done()
			close(ended)
			return &Status{Root: "/repo", Head: Head{Kind: HeadBranch, Name: "stale"}}, nil
		}
		return &Status{Root: cwd, Head: Head{Kind: HeadBranch, Name: "fresh"}}, nil
	}
	source, _ := o.Subscribe()
	defer source.Close()
	o.Start()
	<-started
	o.ChangeCWD("/new")
	value, err := o.Read(t.Context())
	if err != nil || value.CWD != "/new" || value.Git == nil || value.Git.Head.Name != "fresh" {
		t.Fatalf("stale cwd sample=%+v %v", value, err)
	}
	select {
	case <-ended:
	default:
		t.Fatal("old probe not canceled")
	}
	if err := o.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	// A queued last value may drain, but the closed source then terminates.
	_, _ = source.Next(t.Context())
	if _, err := source.Next(t.Context()); err == nil {
		t.Fatal("closed observer retained subscription")
	}
}
func TestObserverBoundsSubscriptionsAndCloseBeforeStart(t *testing.T) {
	o := testObserver(t, nil)
	for range maxSubscribers {
		if _, err := o.Subscribe(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := o.Subscribe(); !errors.Is(err, ErrSubscriberLimit) {
		t.Fatalf("capacity=%v", err)
	}
	if err := o.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	o.Start()
	if _, err := o.Subscribe(); err == nil {
		t.Fatal("closed observer accepted subscriber")
	}
}
