package search

// Tests for Coordinator.CollectCached (sync cache-aware walk) — spec-006 live-typing
// tree-index cache fix.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoordinator_CollectCached_CacheHitSkipsWalk mirrors
// TestCoordinator_CacheHitSkipsWalk but exercises the new sync API used by
// the live-typing file panel search path.
func TestCoordinator_CollectCached_CacheHitSkipsWalk(t *testing.T) {
	items := []Searchable{
		newFile("report.pdf"),
		newFile("summary.pdf"),
		newFile("notes.txt"),
	}
	collector := &mockRecursiveCollector{items: items}
	c := newCachedCoordinator(t, collector)
	src := &mockRecursiveSource{baseDir: "/cached/dir"}

	// First call — cache miss, walk happens.
	got, _, err := c.CollectCached(context.Background(), src, "")
	require.NoError(t, err)
	assert.Len(t, got, 3)
	assert.Equal(t, 1, collector.CallCount(), "first CollectCached must walk")

	// Second call — same (dir, showHidden, filterType) → cache HIT, no walk.
	start := time.Now()
	got2, _, err := c.CollectCached(context.Background(), src, "")
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Len(t, got2, 3)
	assert.Equal(t, 1, collector.CallCount(),
		"second CollectCached on same dir must NOT re-walk (cache hit)")
	assert.Less(t, elapsed, 50*time.Millisecond,
		"cache hit path must be fast (<50ms); got %s", elapsed)
}

// TestCoordinator_CollectCached_SharesCacheWithAsync asserts that a sync
// CollectCached populates the same cache the async post-Enter path reads,
// and vice versa — the whole point of this change.
func TestCoordinator_CollectCached_SharesCacheWithAsync(t *testing.T) {
	items := []Searchable{newFile("a.txt"), newFile("b.txt")}
	collector := &mockRecursiveCollector{items: items}
	c := newCachedCoordinator(t, collector)
	src := &mockRecursiveSource{baseDir: "/shared/dir"}

	// Step 1: live path populates cache.
	_, _, err := c.CollectCached(context.Background(), src, "")
	require.NoError(t, err)
	assert.Equal(t, 1, collector.CallCount())

	// Step 2: async post-Enter path reads same cache → no walk.
	_ = runRecursiveSearchSync(t, c, "a", src)
	assert.Equal(t, 1, collector.CallCount(),
		"async path must read cache populated by sync CollectCached")

	// Step 3: reverse direction — clear cache, populate via async, read via sync.
	c.ClearIndexCache()
	_ = runRecursiveSearchSync(t, c, "a", src)
	assert.Equal(t, 2, collector.CallCount(), "after clear, async path re-walks")

	_, _, err = c.CollectCached(context.Background(), src, "")
	require.NoError(t, err)
	assert.Equal(t, 2, collector.CallCount(),
		"sync path must read cache populated by async search")
}

// TestCoordinator_CollectCached_HonoursShowHidden verifies that the sync
// cache key includes showHidden (from HiddenAwareSource), so toggling the
// dotfile flag forces a cache miss.
func TestCoordinator_CollectCached_HonoursShowHidden(t *testing.T) {
	items := []Searchable{newFile("x.txt")}
	collector := &mockRecursiveCollector{items: items}
	c := newCachedCoordinator(t, collector)

	srcHidden := &hiddenAwareSource{baseDir: "/h/dir", showHidden: true}
	srcVisible := &hiddenAwareSource{baseDir: "/h/dir", showHidden: false}

	_, _, err := c.CollectCached(context.Background(), srcHidden, "")
	require.NoError(t, err)
	assert.Equal(t, 1, collector.CallCount())

	// Different showHidden → miss, walks again.
	_, _, err = c.CollectCached(context.Background(), srcVisible, "")
	require.NoError(t, err)
	assert.Equal(t, 2, collector.CallCount(),
		"showHidden toggle must force a cache miss")

	// Repeating hidden=true hits the original cache entry.
	_, _, err = c.CollectCached(context.Background(), srcHidden, "")
	require.NoError(t, err)
	assert.Equal(t, 2, collector.CallCount())
}
