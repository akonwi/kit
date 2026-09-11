package daemon

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
	cache.scan = func(_ context.Context, cwd string, _ fileindex.Options) ([]fileindex.Entry, error) {
		scans++
		return []fileindex.Entry{{Path: cwd[1:] + ".go"}}, nil
	}

	first, err := cache.load(context.Background(), "session_one", "/one")
	if err != nil || scans != 1 || first[0].Path != "one.go" {
		t.Fatalf("first load = %+v, %v; scans = %d", first, err, scans)
	}
	if _, err := cache.load(context.Background(), "session_one", "/one"); err != nil || scans != 1 {
		t.Fatalf("cached load error = %v; scans = %d", err, scans)
	}
	if _, err := cache.load(context.Background(), "session_one", "/two"); err != nil || scans != 2 {
		t.Fatalf("cwd load error = %v; scans = %d", err, scans)
	}
	now = now.Add(sessionFileIndexRefreshInterval)
	if _, err := cache.load(context.Background(), "session_one", "/two"); err != nil || scans != 3 {
		t.Fatalf("aged load error = %v; scans = %d", err, scans)
	}
}
