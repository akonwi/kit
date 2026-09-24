package githubpr

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func waitCache(t *testing.T, c *Cache) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		running := c.running
		c.mu.Unlock()
		if running == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("cache lookup did not finish")
}
func TestCachePositiveNegativeExpiryAndDetachedCopies(t *testing.T) {
	c := NewCache(t.Context())
	defer c.Close()
	now := time.Now()
	c.now = func() time.Time { return now }
	var calls atomic.Int32
	c.lookup = func(_ context.Context, _, branch string) *PullRequest {
		calls.Add(1)
		if branch == "missing" {
			return nil
		}
		return &PullRequest{Number: 42, URL: "https://github.com/a/b/pull/42"}
	}
	for _, branch := range []string{"main", "missing"} {
		if got := c.Get("/repo", "/repo", branch); got != nil {
			t.Fatalf("cold cache=%v", got)
		}
		waitCache(t, c)
		for range 3 {
			got := c.Get("/repo", "/repo", branch)
			if branch == "main" {
				if got == nil || got.Number != 42 {
					t.Fatalf("cached PR=%v", got)
				}
				got.Number = 99
			} else if got != nil {
				t.Fatalf("negative result=%v", got)
			}
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("cache misses=%d", calls.Load())
	}
	c.mu.Lock()
	now = now.Add(cacheTTL)
	c.mu.Unlock()
	_ = c.Get("/repo", "/repo", "main")
	waitCache(t, c)
	if calls.Load() != 3 {
		t.Fatalf("expired cache misses=%d", calls.Load())
	}
}
func TestCacheCoalescesBoundsAndJoinsCancellation(t *testing.T) {
	c := NewCache(t.Context())
	started := make(chan struct{}, maxConcurrentLookups)
	var ended atomic.Int32
	c.lookup = func(ctx context.Context, _, _ string) *PullRequest {
		started <- struct{}{}
		<-ctx.Done()
		ended.Add(1)
		return nil
	}
	for i := range 200 {
		_ = c.Get("/repo", "/repo", fmt.Sprint(i))
	}
	for range maxConcurrentLookups {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("worker not started")
		}
	}
	for range 10 {
		_ = c.Get("/repo", "/repo", "0")
	}
	c.mu.Lock()
	entries, running := len(c.entries), c.running
	c.mu.Unlock()
	if entries != maxCacheEntries || running != maxConcurrentLookups {
		t.Fatalf("bounds: entries=%d running=%d", entries, running)
	}
	c.Close()
	if ended.Load() != maxConcurrentLookups {
		t.Fatalf("workers not joined: %d", ended.Load())
	}
	if got := c.Get("/repo", "/repo", "new"); got != nil {
		t.Fatal("closed cache returned metadata")
	}
}
func TestCacheSeparatesCWDRepositoryAndBranch(t *testing.T) {
	c := NewCache(t.Context())
	defer c.Close()
	release := make(chan struct{})
	started := make(chan struct{})
	c.lookup = func(ctx context.Context, cwd, branch string) *PullRequest {
		if branch == "old" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil
			}
			return &PullRequest{Number: 1}
		}
		return &PullRequest{Number: 2}
	}
	_ = c.Get("/a", "/a", "old")
	<-started
	_ = c.Get("/a", "/a", "new")
	_ = c.Get("/b", "/b", "new")
	_ = c.Get("/a", "/nested", "new")
	close(release)
	waitCache(t, c)
	for _, key := range []cacheKey{{"/a", "/a", "new"}, {"/b", "/b", "new"}, {"/a", "/nested", "new"}} {
		if got := c.Get(key.cwd, key.root, key.branch); got == nil || got.Number != 2 {
			t.Fatalf("stale lookup crossed key: %v", got)
		}
	}
	c.mu.Lock()
	entries := len(c.entries)
	c.mu.Unlock()
	if entries != 4 {
		t.Fatalf("distinct keys=%d", entries)
	}
}

func TestCacheLookupUsesCapturedRepositoryRoot(t *testing.T) {
	c := NewCache(t.Context())
	defer c.Close()
	observed := make(chan string, 1)
	c.lookup = func(_ context.Context, root, branch string) *PullRequest { observed <- root; return nil }
	_ = c.Get("/repo/subdir", "/repo", "main")
	select {
	case root := <-observed:
		if root != "/repo" {
			t.Fatalf("lookup used %q", root)
		}
	case <-time.After(time.Second):
		t.Fatal("lookup not scheduled")
	}
}

func TestCacheInvalidationIncludesNegativeRefreshCompletion(t *testing.T) {
	c := NewCache(t.Context())
	defer c.Close()
	now := time.Now()
	c.now = func() time.Time { return now }
	var missing atomic.Bool
	c.lookup = func(context.Context, string, string) *PullRequest {
		if missing.Load() {
			return nil
		}
		return &PullRequest{Number: 42, URL: "https://github.com/a/b/pull/42"}
	}
	changed := c.Changes()
	_ = c.Get("/repo", "/repo", "main")
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("positive completion was not published")
	}
	if value := c.Get("/repo", "/repo", "main"); value == nil || value.Number != 42 {
		t.Fatal(value)
	}
	next := c.Changes()
	missing.Store(true)
	c.mu.Lock()
	now = now.Add(cacheTTL)
	c.mu.Unlock()
	_ = c.Get("/repo", "/repo", "main")
	select {
	case <-next:
	case <-time.After(time.Second):
		t.Fatal("negative completion was not published")
	}
	if value := c.Get("/repo", "/repo", "main"); value != nil {
		t.Fatalf("failed refresh retained PR: %+v", value)
	}
}
