package zoxide

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	zoxidelib "github.com/lazysegtree/go-zoxide"
	"github.com/stretchr/testify/assert"
)

// fakeStat is a stat implementation that records every call it receives and
// returns a predetermined answer for each path. Any path not in the answers
// map returns (false, os.ErrNotExist).
type fakeStat struct {
	mu      sync.Mutex
	calls   map[string]int
	answers map[string]bool
	total   atomic.Int64
}

func newFakeStat(answers map[string]bool) *fakeStat {
	return &fakeStat{
		calls:   make(map[string]int),
		answers: answers,
	}
}

func (f *fakeStat) fn(path string) (bool, error) {
	f.total.Add(1)
	f.mu.Lock()
	f.calls[path]++
	f.mu.Unlock()
	if ans, ok := f.answers[path]; ok {
		return ans, nil
	}
	return false, errors.New("does not exist")
}

func (f *fakeStat) callCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func (f *fakeStat) totalCalls() int64 {
	return f.total.Load()
}

func TestPathCacheHitSkipsStat(t *testing.T) {
	fs := newFakeStat(map[string]bool{"/a": true})
	c := newPathCache(60*time.Second, fs.fn)

	// First call: miss -> stat.
	assert.True(t, c.ExistsAsDir("/a"))
	assert.Equal(t, 1, fs.callCount("/a"), "first call should stat")

	// Subsequent calls within TTL: hit -> no stat.
	for range 5 {
		assert.True(t, c.ExistsAsDir("/a"))
	}
	assert.Equal(t, 1, fs.callCount("/a"), "cache hits should not call stat")
}

func TestPathCacheMissStoresResult(t *testing.T) {
	fs := newFakeStat(map[string]bool{"/real": true, "/file": false})
	c := newPathCache(60*time.Second, fs.fn)

	assert.True(t, c.ExistsAsDir("/real"))
	assert.False(t, c.ExistsAsDir("/file"))
	assert.False(t, c.ExistsAsDir("/missing")) // not in answers

	// Each distinct path stats once.
	assert.Equal(t, 1, fs.callCount("/real"))
	assert.Equal(t, 1, fs.callCount("/file"))
	assert.Equal(t, 1, fs.callCount("/missing"))

	// Repeat: should all hit cache.
	c.ExistsAsDir("/real")
	c.ExistsAsDir("/file")
	c.ExistsAsDir("/missing")
	assert.Equal(t, int64(3), fs.totalCalls(), "repeat calls should all be cache hits")
}

func TestPathCacheExpiredEntryRestats(t *testing.T) {
	fs := newFakeStat(map[string]bool{"/a": true})
	// Use a tiny TTL so we can trigger expiry without sleeping too long.
	c := newPathCache(5*time.Millisecond, fs.fn)

	assert.True(t, c.ExistsAsDir("/a"))
	assert.Equal(t, 1, fs.callCount("/a"))

	// Wait out the TTL.
	time.Sleep(15 * time.Millisecond)

	assert.True(t, c.ExistsAsDir("/a"))
	assert.Equal(t, 2, fs.callCount("/a"), "expired entry should re-stat")
}

func TestPathCacheConcurrentAccess(t *testing.T) {
	// Run under -race to catch data races in the cache.
	fs := newFakeStat(map[string]bool{
		"/a": true, "/b": true, "/c": false, "/d": true,
	})
	c := newPathCache(60*time.Second, fs.fn)

	var wg sync.WaitGroup
	paths := []string{"/a", "/b", "/c", "/d", "/missing"}
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				_ = c.ExistsAsDir(paths[i%len(paths)])
			}
		}()
	}
	wg.Wait()

	// Each distinct path should have been stat'd at least once. Since multiple
	// goroutines may race on first-miss it could be stat'd more than once per
	// path, but total stat calls should still be vastly lower than the 6400
	// ExistsAsDir invocations we made — otherwise the cache isn't helping.
	const totalLookups = 32 * 200
	assert.Less(t, fs.totalCalls(), int64(totalLookups/10),
		"with caching, total stat calls should be at least 10x fewer than total lookups")
}

func TestFilterExistingDirsWithWorkersPreservesOrder(t *testing.T) {
	// Build input with known existence outcomes.
	fs := newFakeStat(map[string]bool{
		"/keep/1": true,
		"/keep/2": true,
		"/keep/3": true,
		"/keep/4": true,
		"/drop/1": false, // exists but is a file
	})
	c := newPathCache(60*time.Second, fs.fn)

	input := []zoxidelib.Result{
		{Path: "/keep/1", Score: 100},
		{Path: "/drop/1", Score: 90},
		{Path: "/missing/x", Score: 80}, // stat err
		{Path: "/keep/2", Score: 70},
		{Path: "/keep/3", Score: 60},
		{Path: "/drop/1", Score: 55}, // duplicate to exercise cache
		{Path: "/keep/4", Score: 50},
	}

	for _, workers := range []int{1, 2, 4, 8, 16} {
		c.reset()
		got := filterExistingDirsWithWorkers(append([]zoxidelib.Result(nil), input...), workers, c)

		// Order must match input order of surviving entries.
		wantPaths := []string{"/keep/1", "/keep/2", "/keep/3", "/keep/4"}
		gotPaths := make([]string, len(got))
		for i, r := range got {
			gotPaths[i] = r.Path
		}
		assert.Equal(t, wantPaths, gotPaths, "workers=%d: order must match input frecency ordering", workers)
	}
}

func TestFilterExistingDirsWithWorkersCacheIsShared(t *testing.T) {
	fs := newFakeStat(map[string]bool{"/x": true, "/y": true})
	c := newPathCache(60*time.Second, fs.fn)

	input := []zoxidelib.Result{{Path: "/x"}, {Path: "/y"}}

	// First pass stats everything.
	got := filterExistingDirsWithWorkers(input, 4, c)
	assert.Len(t, got, 2)
	firstCalls := fs.totalCalls()
	assert.GreaterOrEqual(t, firstCalls, int64(2), "cold cache should have stat'd both")

	// Second pass: cache should absorb everything.
	got2 := filterExistingDirsWithWorkers(input, 4, c)
	assert.Len(t, got2, 2)
	assert.Equal(t, firstCalls, fs.totalCalls(), "warm cache should not stat again")
}

func TestFilterExistingDirsWithWorkersEmpty(t *testing.T) {
	fs := newFakeStat(nil)
	c := newPathCache(60*time.Second, fs.fn)
	got := filterExistingDirsWithWorkers([]zoxidelib.Result{}, 8, c)
	assert.Empty(t, got)
	assert.Equal(t, int64(0), fs.totalCalls())
}

func TestFilterExistingDirsWithWorkersSerialFallback(t *testing.T) {
	// workers <= 1 should produce identical output to the parallel path.
	fs := newFakeStat(map[string]bool{"/a": true, "/b": true, "/c": false})
	c := newPathCache(60*time.Second, fs.fn)
	input := []zoxidelib.Result{{Path: "/a"}, {Path: "/b"}, {Path: "/c"}, {Path: "/missing"}}

	serial := filterExistingDirsWithWorkers(append([]zoxidelib.Result(nil), input...), 1, c)
	c.reset()
	parallel := filterExistingDirsWithWorkers(append([]zoxidelib.Result(nil), input...), 8, c)

	assert.Equal(t, serial, parallel, "serial and parallel outputs must match")
}
