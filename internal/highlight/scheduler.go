package highlight

import (
	"container/list"
	"context"
	"crypto/sha256"
	"sync"
	"time"
	"unsafe"
)

const (
	DefaultWorkers          = 2
	DefaultQueueDepth       = 8
	DefaultCacheBytes       = 8 << 20
	DefaultHighlightTimeout = 750 * time.Millisecond
)

type job struct {
	ctx      context.Context
	request  Request
	response chan Result
}

// Scheduler bounds concurrent native parser work, queues, and cached semantic results.
type Scheduler struct {
	jobs   chan job
	cache  *resultCache
	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
}

// NewScheduler starts a fixed worker pool.
func NewScheduler(workers, queueDepth, cacheBytes int) *Scheduler {
	workers = max(1, workers)
	queueDepth = max(1, queueDepth)
	s := &Scheduler{jobs: make(chan job, queueDepth), cache: newResultCache(cacheBytes)}
	s.wg.Add(workers)
	for range workers {
		go s.worker()
	}
	return s
}

// Highlight waits for a bounded worker. Queue saturation and cancellation
// return readable plain text rather than blocking the UI indefinitely.
func (s *Scheduler) Highlight(ctx context.Context, request Request) Result {
	ctx, cancel := context.WithTimeout(ctx, DefaultHighlightTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Plain(request, err)
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return Plain(request, errSchedulerClosed)
	}
	if cached, ok := s.cache.get(cacheKeyFor(request)); ok {
		s.mu.RUnlock()
		return cached
	}
	s.mu.RUnlock()
	response := make(chan Result, 1)
	work := job{ctx: ctx, request: request, response: response}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return Plain(request, errSchedulerClosed)
	}
	select {
	case s.jobs <- work:
		s.mu.RUnlock()
	case <-ctx.Done():
		s.mu.RUnlock()
		return Plain(request, ctx.Err())
	default:
		s.mu.RUnlock()
		return Plain(request, errQueueFull)
	}
	select {
	case result := <-response:
		return result
	case <-ctx.Done():
		return Plain(request, ctx.Err())
	}
}

// Close drains accepted jobs and releases worker-owned native parser resources.
func (s *Scheduler) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.jobs)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Scheduler) worker() {
	defer s.wg.Done()
	var engine treeSitterEngine
	defer engine.close()
	for work := range s.jobs {
		if work.ctx.Err() != nil {
			work.response <- Plain(work.request, work.ctx.Err())
			continue
		}
		result := Validate(engine.highlight(work.ctx, work.request))
		if result.Err == nil {
			s.cache.put(cacheKeyFor(work.request), result)
		}
		work.response <- result
	}
}

type resultCache struct {
	mu      sync.Mutex
	limit   int
	used    int
	entries map[[32]byte]*list.Element
	recency *list.List
}

type cacheEntry struct {
	key  [32]byte
	size int
	data Result
}

func newResultCache(limit int) *resultCache {
	return &resultCache{limit: max(0, limit), entries: make(map[[32]byte]*list.Element), recency: list.New()}
}

func cacheKeyFor(request Request) [32]byte {
	return sha256.Sum256([]byte(DetectLanguage(request.Language, request.Path) + "\x00" + request.Path + "\x00" + request.Revision + "\x00" + request.Source))
}

func (c *resultCache) get(key [32]byte) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element := c.entries[key]
	if element == nil {
		return Result{}, false
	}
	c.recency.MoveToFront(element)
	return cloneResult(element.Value.(cacheEntry).data), true
}

func (c *resultCache) put(key [32]byte, result Result) {
	// Include retained payload plus a conservative per-entry allowance for the
	// result, key, list element, and map bucket overhead.
	size := len(result.Source) + len(result.Spans)*int(unsafe.Sizeof(Span{})) + 256
	if c.limit == 0 || size > c.limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.entries[key]; existing != nil {
		c.used -= existing.Value.(cacheEntry).size
		c.recency.Remove(existing)
	}
	entry := cacheEntry{key: key, size: size, data: cloneResult(result)}
	c.entries[key] = c.recency.PushFront(entry)
	c.used += size
	for c.used > c.limit {
		oldest := c.recency.Back()
		entry := oldest.Value.(cacheEntry)
		delete(c.entries, entry.key)
		c.used -= entry.size
		c.recency.Remove(oldest)
	}
}

func cloneResult(result Result) Result {
	result.Spans = append([]Span(nil), result.Spans...)
	return result
}

var Default = NewScheduler(DefaultWorkers, DefaultQueueDepth, DefaultCacheBytes)
