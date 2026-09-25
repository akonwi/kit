package server

import (
	"context"
	"sync"
	"time"

	"github.com/akonwi/kit/internal/fileindex"
)

const sessionFileIndexRefreshInterval = 5 * time.Minute

type cachedSessionFileIndex struct {
	cwd       string
	result    fileindex.Result
	indexedAt time.Time
}

type sessionFileIndexCache struct {
	mu          sync.Mutex
	entries     map[string]cachedSessionFileIndex
	generations map[string]uint64
	pendingCWD  map[string]string
	now         func() time.Time
	scan        func(context.Context, string, fileindex.Options) (fileindex.Result, error)
}

func newSessionFileIndexCache() *sessionFileIndexCache {
	return &sessionFileIndexCache{entries: make(map[string]cachedSessionFileIndex), generations: make(map[string]uint64), pendingCWD: make(map[string]string), now: time.Now, scan: fileindex.ScanResult}
}

func (cache *sessionFileIndexCache) load(ctx context.Context, sessionID, cwd string, refresh bool) (fileindex.Result, error) {
	now := cache.now()
	cache.mu.Lock()
	cached, ok := cache.entries[sessionID]
	if !refresh && ok && cached.cwd == cwd && now.Sub(cached.indexedAt) < sessionFileIndexRefreshInterval {
		if pending := cache.pendingCWD[sessionID]; pending != "" && pending != cwd {
			cache.generations[sessionID]++
			delete(cache.pendingCWD, sessionID)
		}
		cache.mu.Unlock()
		return cloneFileIndexResult(cached.result), nil
	}
	cache.generations[sessionID]++
	generation := cache.generations[sessionID]
	cache.pendingCWD[sessionID] = cwd
	cache.mu.Unlock()
	result, err := cache.scan(ctx, cwd, fileindex.Options{})
	if err != nil {
		return fileindex.Result{}, err
	}
	result = cloneFileIndexResult(result)
	cache.mu.Lock()
	if cache.generations[sessionID] == generation {
		cache.entries[sessionID] = cachedSessionFileIndex{cwd: cwd, result: result, indexedAt: now}
		delete(cache.pendingCWD, sessionID)
	}
	cache.mu.Unlock()
	return cloneFileIndexResult(result), nil
}

func cloneFileIndexResult(result fileindex.Result) fileindex.Result {
	result.Entries = append([]fileindex.Entry(nil), result.Entries...)
	return result
}
