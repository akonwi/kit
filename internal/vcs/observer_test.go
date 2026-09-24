package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/githubpr"
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
func TestObserverPRCompletionPushesWithoutGitPollOrClientRead(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release")
	t.Setenv("KIT_PR_RELEASE", release)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
while [ ! -f "$KIT_PR_RELEASE" ]; do sleep 0.01; done
printf '%s' '{"number":47,"url":"https://github.com/a/b/pull/47","headRefName":"main"}'
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
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	remote := nextObserved(t, source)
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
