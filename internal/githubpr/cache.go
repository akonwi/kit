package githubpr

import (
	"context"
	"sync"
	"time"
)

const (
	cacheTTL             = time.Minute
	maxCacheEntries      = 128
	maxConcurrentLookups = 4
)

type cacheKey struct{ cwd, root, branch string }
type cacheEntry struct {
	value             *PullRequest
	updated, accessed time.Time
	loading           bool
}

// Cache owns bounded asynchronous PR lookups. Reads never wait for gh. Positive
// and negative entries expire after a minute; in-flight work is shared per key.
type Cache struct {
	changed chan struct{}
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	entries map[cacheKey]*cacheEntry
	running int
	closed  bool
	workers sync.WaitGroup
	lookup  func(context.Context, string, string) *PullRequest
	now     func() time.Time
}

// NewCache creates a daemon-owned cache. Close must be called to join its workers.
func NewCache(parent context.Context) *Cache {
	ctx, cancel := context.WithCancel(parent)
	return &Cache{changed: make(chan struct{}), ctx: ctx, cancel: cancel, entries: make(map[cacheKey]*cacheEntry), lookup: Lookup, now: time.Now}
}

// Get returns the cached metadata and schedules a refresh when needed. The caller
// must supply a currently observed named branch, never a detached or unborn head.
func (c *Cache) Get(cwd, root, branch string) *PullRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil || branch == "" {
		return nil
	}
	key := cacheKey{cwd, root, branch}
	now := c.now()
	entry := c.entries[key]
	if entry == nil {
		if len(c.entries) >= maxCacheEntries {
			var oldest cacheKey
			var victim *cacheEntry
			for k, v := range c.entries {
				if !v.loading && (victim == nil || v.accessed.Before(victim.accessed)) {
					oldest = k
					victim = v
				}
			}
			if victim == nil {
				return nil
			}
			delete(c.entries, oldest)
		}
		entry = &cacheEntry{}
		c.entries[key] = entry
	}
	entry.accessed = now
	if !entry.loading && (entry.updated.IsZero() || now.Sub(entry.updated) >= cacheTTL) && c.running < maxConcurrentLookups {
		entry.loading = true
		c.running++
		c.workers.Add(1)
		go c.refresh(key, entry)
	}
	return clone(entry.value)
}
func clone(value *PullRequest) *PullRequest {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
func (c *Cache) refresh(key cacheKey, entry *cacheEntry) {
	defer c.workers.Done()
	// Bind execution to the probed repository, not a cwd that may acquire a
	// nested repository before this asynchronous worker starts.
	value := c.lookup(c.ctx, key.root, key.branch)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running--
	entry.loading = false
	if c.closed || c.ctx.Err() != nil {
		return
	}
	entry.value = clone(value)
	entry.updated = c.now()
	close(c.changed)
	c.changed = make(chan struct{})
}

// Close revokes new reads, cancels subprocesses, and joins all owned workers.
func (c *Cache) Close() {
	c.mu.Lock()
	c.closed = true
	c.cancel()
	c.mu.Unlock()
	c.workers.Wait()
}

// Changes returns an invalidation generation. Sample it before Get to avoid
// missing a completion; the channel closes when any lookup completes.
func (c *Cache) Changes() <-chan struct{} { c.mu.Lock(); defer c.mu.Unlock(); return c.changed }
