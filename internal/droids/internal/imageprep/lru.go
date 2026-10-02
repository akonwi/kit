package imageprep

import (
	"container/list"
	"sync"
)

// lru is a byte-bounded least-recently-used cache of prepared results.
type lru struct {
	mu       sync.Mutex
	capacity int
	size     int
	order    *list.List // front is most recently used
	entries  map[string]*list.Element
}

type lruEntry struct {
	key    string
	result Result
}

func newLRU(capacity int) *lru {
	return &lru{capacity: capacity, order: list.New(), entries: map[string]*list.Element{}}
}

func (c *lru) get(key string) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return Result{}, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*lruEntry).result, true
}

func (c *lru) put(key string, result Result) {
	size := len(result.Data)
	if c.capacity <= 0 || size > c.capacity {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		c.size -= len(element.Value.(*lruEntry).result.Data)
		element.Value = &lruEntry{key: key, result: result}
		c.order.MoveToFront(element)
	} else {
		c.entries[key] = c.order.PushFront(&lruEntry{key: key, result: result})
	}
	c.size += size
	for c.size > c.capacity {
		oldest := c.order.Back()
		entry := oldest.Value.(*lruEntry)
		c.order.Remove(oldest)
		delete(c.entries, entry.key)
		c.size -= len(entry.result.Data)
	}
}

func (c *lru) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// keySet is a count-bounded set of keys that evicts the least recently added
// or confirmed key.
type keySet struct {
	mu       sync.Mutex
	capacity int
	order    *list.List
	entries  map[string]*list.Element
}

func newKeySet(capacity int) *keySet {
	return &keySet{capacity: capacity, order: list.New(), entries: map[string]*list.Element{}}
}

func (s *keySet) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	element, ok := s.entries[key]
	if ok {
		s.order.MoveToFront(element)
	}
	return ok
}

func (s *keySet) add(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if element, ok := s.entries[key]; ok {
		s.order.MoveToFront(element)
		return
	}
	s.entries[key] = s.order.PushFront(key)
	for s.order.Len() > s.capacity {
		oldest := s.order.Back()
		s.order.Remove(oldest)
		delete(s.entries, oldest.Value.(string))
	}
}
