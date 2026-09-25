package server

import (
	"context"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/fileindex"
)

func TestSessionFileIndexCacheRefreshesForCWDAndAge(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	scans := 0
	cache := newSessionFileIndexCache()
	cache.now = func() time.Time { return now }
	cache.scan = func(_ context.Context, cwd string, _ fileindex.Options) (fileindex.Result, error) {
		scans++
		return fileindex.Result{Entries: []fileindex.Entry{{Path: cwd[1:] + ".go"}}, Truncated: cwd == "/two"}, nil
	}

	first, err := cache.load(context.Background(), "session_one", "/one", false)
	if err != nil || scans != 1 || first.Entries[0].Path != "one.go" || first.Truncated {
		t.Fatalf("first load = %+v, %v; scans = %d", first, err, scans)
	}
	if _, err := cache.load(context.Background(), "session_one", "/one", false); err != nil || scans != 1 {
		t.Fatalf("cached load error = %v; scans = %d", err, scans)
	}
	second, err := cache.load(context.Background(), "session_one", "/two", false)
	if err != nil || scans != 2 || !second.Truncated {
		t.Fatalf("cwd load = %+v, %v; scans = %d", second, err, scans)
	}
	if _, err := cache.load(context.Background(), "session_one", "/two", true); err != nil || scans != 3 {
		t.Fatalf("forced refresh error = %v; scans = %d", err, scans)
	}
	now = now.Add(sessionFileIndexRefreshInterval)
	if _, err := cache.load(context.Background(), "session_one", "/two", false); err != nil || scans != 4 {
		t.Fatalf("aged load error = %v; scans = %d", err, scans)
	}
}

func TestSessionFileIndexCacheOlderCompletionCannotReplaceNewerCWD(t *testing.T) {
	t.Parallel()
	cache := newSessionFileIndexCache()
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	cache.scan = func(_ context.Context, cwd string, _ fileindex.Options) (fileindex.Result, error) {
		if cwd == "/old" {
			close(oldStarted)
			<-releaseOld
		}
		return fileindex.Result{Entries: []fileindex.Entry{{Path: cwd[1:] + ".go"}}}, nil
	}
	if _, err := cache.load(context.Background(), "session_one", "/new", false); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan struct{})
	go func() {
		defer close(oldDone)
		_, _ = cache.load(context.Background(), "session_one", "/old", false)
	}()
	<-oldStarted
	if _, err := cache.load(context.Background(), "session_one", "/new", false); err != nil {
		t.Fatal(err)
	}
	close(releaseOld)
	<-oldDone
	cache.mu.Lock()
	cached := cache.entries["session_one"]
	cache.mu.Unlock()
	if cached.cwd != "/new" || cached.result.Entries[0].Path != "new.go" {
		t.Fatalf("cache regressed to older completion: %+v", cached)
	}
}
