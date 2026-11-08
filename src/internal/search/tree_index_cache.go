package search

import (
	"container/list"
	"log/slog"
	"sync"
	"time"
)

// treeIndexCacheCapacity is the maximum number of entries the cache can hold.
// See spec-006 decision 006-003 (LRU size 4).
const treeIndexCacheCapacity = 4

// treeIndexCacheTTL is the maximum age an entry can reach before it is
// considered stale and evicted. See spec-006 decision 006-003 (TTL 30s).
const treeIndexCacheTTL = 30 * time.Second

// treeIndexCacheKey identifies a cached walk result. Entries for the same
// directory but different showHidden / filterType combinations coexist.
type treeIndexCacheKey struct {
	dir        string
	showHidden bool
	filterType string
}

// treeIndexCacheEntry holds the materialised result of a recursive walk along
// with the timestamp at which it was recorded (used for TTL expiry).
type treeIndexCacheEntry struct {
	items      []Searchable
	recordedAt time.Time
}

// treeIndexCache is a bounded LRU + TTL cache mapping (dir, showHidden,
// filterType) to the full []Searchable produced by a recursive walk. It is
// safe for concurrent use.
type treeIndexCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	nowFn    func() time.Time

	// LRU implemented as a doubly-linked list of *lruElement, with a map from
	// key to *list.Element for O(1) lookup. Front of list = most recently used.
	lru   *list.List
	index map[treeIndexCacheKey]*list.Element
}

// lruElement is the payload stored inside the LRU list.
type lruElement struct {
	key   treeIndexCacheKey
	entry treeIndexCacheEntry
}

// newTreeIndexCache constructs an empty cache with spec-mandated capacity and
// TTL. A nil nowFn falls back to time.Now.
func newTreeIndexCache() *treeIndexCache {
	return &treeIndexCache{
		capacity: treeIndexCacheCapacity,
		ttl:      treeIndexCacheTTL,
		nowFn:    time.Now,
		lru:      list.New(),
		index:    make(map[treeIndexCacheKey]*list.Element),
	}
}

// SetNow injects a clock for tests. Must not be called concurrently with Get/Put.
func (c *treeIndexCache) SetNow(fn func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fn == nil {
		c.nowFn = time.Now
		return
	}
	c.nowFn = fn
}

// Get returns the cached items for key if present and still fresh.
// Accessing an entry moves it to the front of the LRU list. Stale entries are
// evicted on access.
func (c *treeIndexCache) Get(key treeIndexCacheKey) ([]Searchable, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.index[key]
	if !ok {
		return nil, false
	}
	payload := entryOf(elem)
	if c.nowFn().Sub(payload.entry.recordedAt) > c.ttl {
		// TTL expired — evict and report miss.
		c.removeElementLocked(elem)
		slog.Debug("index_cache_evict",
			"dir", key.dir,
			"reason", "ttl",
		)
		return nil, false
	}
	c.lru.MoveToFront(elem)
	return payload.entry.items, true
}

// Put stores items for key. Updates recordedAt to now and evicts the least
// recently used entry if capacity would be exceeded.
func (c *treeIndexCache) Put(key treeIndexCacheKey, items []Searchable) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFn()

	if elem, ok := c.index[key]; ok {
		payload := entryOf(elem)
		payload.entry.items = items
		payload.entry.recordedAt = now
		c.lru.MoveToFront(elem)
		return
	}

	payload := &lruElement{
		key: key,
		entry: treeIndexCacheEntry{
			items:      items,
			recordedAt: now,
		},
	}
	elem := c.lru.PushFront(payload)
	c.index[key] = elem

	// Evict LRU if over capacity.
	for c.lru.Len() > c.capacity {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		oldestKey := entryOf(oldest).key
		c.removeElementLocked(oldest)
		slog.Debug("index_cache_evict",
			"dir", oldestKey.dir,
			"reason", "lru",
		)
	}
}

// Clear removes every entry in the cache.
func (c *treeIndexCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Init()
	c.index = make(map[treeIndexCacheKey]*list.Element)
}

// ClearKey evicts any entry whose directory matches `dir` regardless of
// showHidden / filterType. Used by callers that observe a filesystem mutation
// and want to invalidate the affected directory.
func (c *treeIndexCache) ClearKey(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, elem := range c.index {
		if k.dir != dir {
			continue
		}
		c.removeElementLocked(elem)
		slog.Debug("index_cache_evict",
			"dir", k.dir,
			"reason", "invalidate",
		)
	}
}

// removeElementLocked removes a list element from both the list and the index.
// Caller must hold c.mu.
func (c *treeIndexCache) removeElementLocked(elem *list.Element) {
	payload := entryOf(elem)
	delete(c.index, payload.key)
	c.lru.Remove(elem)
}

// entryOf is the cache entry a list element holds: the list holds nothing
// else, so a nil one is a bug, which the caller's use of it shows at once.
func entryOf(elem *list.Element) *lruElement {
	payload, _ := elem.Value.(*lruElement)
	return payload
}
