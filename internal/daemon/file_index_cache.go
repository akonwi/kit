package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/fileindex"
)

const sessionFileIndexRefreshInterval = 5 * time.Minute

type cachedSessionFileIndex struct {
	cwd       string
	entries   []fileindex.Entry
	indexedAt time.Time
}

type sessionFileIndexCache struct {
	mu      sync.Mutex
	entries map[string]cachedSessionFileIndex
	now     func() time.Time
	scan    func(context.Context, string, fileindex.Options) ([]fileindex.Entry, error)
}

func newSessionFileIndexCache() *sessionFileIndexCache {
	return &sessionFileIndexCache{entries: make(map[string]cachedSessionFileIndex), now: time.Now, scan: fileindex.Scan}
}

func (cache *sessionFileIndexCache) load(ctx context.Context, sessionID, cwd string) ([]fileindex.Entry, error) {
	now := cache.now()
	cache.mu.Lock()
	cached, ok := cache.entries[sessionID]
	cache.mu.Unlock()
	if ok && cached.cwd == cwd && now.Sub(cached.indexedAt) < sessionFileIndexRefreshInterval {
		return append([]fileindex.Entry(nil), cached.entries...), nil
	}
	entries, err := cache.scan(ctx, cwd, fileindex.Options{})
	if err != nil {
		return nil, err
	}
	entries = append([]fileindex.Entry(nil), entries...)
	cache.mu.Lock()
	cache.entries[sessionID] = cachedSessionFileIndex{cwd: cwd, entries: entries, indexedAt: now}
	cache.mu.Unlock()
	return append([]fileindex.Entry(nil), entries...), nil
}
