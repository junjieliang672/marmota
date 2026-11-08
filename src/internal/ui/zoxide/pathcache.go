package zoxide

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// pathCacheTTL bounds how long a cached directory existence lookup is trusted.
// 60s is a reasonable compromise: long enough to cover a rapid burst of
// keystrokes against the same zoxide db, short enough that rmdir/move
// operations are reflected within a minute without a manual invalidation.
const pathCacheTTL = 60 * time.Second

// statFunc returns (exists-as-dir, error). Kept as a package-level function
// variable so tests can inject a fake stat and count invocations.
type statFunc func(path string) (bool, error)

// defaultStatFn is the production stat implementation.
func defaultStatFn(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

type pathCacheEntry struct {
	existsAsDir bool
	checkedAt   time.Time
}

// pathCache is a concurrency-safe map of path -> (exists-as-dir, checkedAt).
// Entries are considered stale after pathCacheTTL and are re-stat'd on access.
type pathCache struct {
	mu       sync.RWMutex
	m        map[string]pathCacheEntry
	ttl      time.Duration
	stat     statFunc
	statHits atomic.Int64 // count of real stat calls (for tests/metrics)
}

func newPathCache(ttl time.Duration, stat statFunc) *pathCache {
	if stat == nil {
		stat = defaultStatFn
	}
	return &pathCache{
		m:    make(map[string]pathCacheEntry),
		ttl:  ttl,
		stat: stat,
	}
}

// ExistsAsDir returns true iff path exists and is a directory. On a cache hit
// that is within ttl, it returns the cached answer without calling stat. On a
// miss or expired entry, it stats and updates the cache.
func (c *pathCache) ExistsAsDir(path string) bool {
	c.mu.RLock()
	e, ok := c.m[path]
	c.mu.RUnlock()
	if ok && time.Since(e.checkedAt) < c.ttl {
		return e.existsAsDir
	}

	c.statHits.Add(1)
	exists, _ := c.stat(path)

	c.mu.Lock()
	c.m[path] = pathCacheEntry{existsAsDir: exists, checkedAt: time.Now()}
	c.mu.Unlock()
	return exists
}

// StatCallCount exposes the number of underlying stat calls performed. Primarily
// useful for tests to assert cache-hit behavior.
func (c *pathCache) StatCallCount() int64 {
	return c.statHits.Load()
}

// reset clears all cached entries and the stat counter. Intended for tests.
func (c *pathCache) reset() {
	c.mu.Lock()
	c.m = make(map[string]pathCacheEntry)
	c.mu.Unlock()
	c.statHits.Store(0)
}

// globalPathCache is shared across every filterExistingDirs invocation.
var globalPathCache = newPathCache(pathCacheTTL, defaultStatFn) //nolint:gochecknoglobals // shared, by design
