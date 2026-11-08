package search

// Tests for spec-006 Coordinator tree-index cache (treeIndexCache).
//
// RED PHASE: these tests reference
//   - Coordinator.ClearIndexCache
//   - Coordinator internal cache behaviour (hit / TTL / LRU)
// which do NOT exist yet. The cache is expected to be keyed by
// (absPath, showHidden, filterType), hold up to 4 entries, TTL 30s.
//
// NOTE: Because the spec calls for a private indexCache plus a private
// treeIndexCache type, the *unit* test has to observe cache behaviour
// indirectly, through the coordinator's public surface. These tests use a
// mockRecursiveCollector plugged into the coordinator via a small
// injection seam that implementers must expose (see T3). If they choose a
// different seam (e.g. constructor option `WithCollectorFactory`) the
// tests should be adapted — the invariant under test is CACHE BEHAVIOUR,
// not the specific injection mechanism.
//
// Expected injection surface (pick one; these tests assume option A):
//
//   Option A (preferred, additive):
//     func (c *Coordinator) SetCollectorForTest(f func(maxDepth int, showHidden bool) StreamingCollector)
//
//   Option B (constructor arg):
//     NewCoordinatorWithCollector(cfg SearchConfig, factory func(...) StreamingCollector) *Coordinator
//
// Where StreamingCollector is the interface the coordinator uses internally,
// satisfied both by *RecursiveCollector and by the mocks below.
//
// The spec permits implementers to pick the seam. If they prefer B, the
// two tests below should be updated to use the new constructor.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// mockRecursiveCollector counts CollectStream calls and returns a fixed
// set of Searchable items. Safe for concurrent use.
type mockRecursiveCollector struct {
	callCount int32
	items     []Searchable
	delay     time.Duration
}

func (m *mockRecursiveCollector) CollectStream(
	ctx context.Context,
	root, filter string,
	out chan<- []Searchable,
) (bool, error) {
	atomic.AddInt32(&m.callCount, 1)
	defer close(out)

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	// Emit in one batch for simplicity.
	select {
	case out <- m.items:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return false, nil
}

func (m *mockRecursiveCollector) Collect(ctx context.Context, root, filter string) ([]Searchable, bool, error) {
	atomic.AddInt32(&m.callCount, 1)
	return m.items, false, nil
}

func (m *mockRecursiveCollector) CallCount() int { return int(atomic.LoadInt32(&m.callCount)) }

// newCachedCoordinator builds a coordinator wired to use `collector` for
// recursive walks. It depends on an injection helper on Coordinator that
// implementers must add (see note at top of file).
func newCachedCoordinator(t *testing.T, collector *mockRecursiveCollector) *Coordinator {
	t.Helper()
	cfg := FilePanelConfig()
	cfg.Timeout = 2 * time.Second

	c := NewCoordinator(cfg)

	// Expected helper. Compile failure here means implementer needs to add
	// it per T3. Suggested signature:
	//   func (c *Coordinator) SetCollectorForTest(factory func(maxDepth int, showHidden bool) streamingCollector)
	c.SetCollectorForTest(func(maxDepth int, showHidden bool) streamingCollector {
		return collector
	})
	return c
}

// mockRecursiveSource is a SearchSource whose GetBaseDirectory returns the
// configured dir and whose GetItems returns nothing (recursive path ignores
// GetItems).
type mockRecursiveSource struct {
	baseDir string
}

func (s *mockRecursiveSource) GetItems() []Searchable   { return nil }
func (s *mockRecursiveSource) GetBaseDirectory() string { return s.baseDir }

// runRecursiveSearchSync drives StartSearch + the returned tea.Cmd to
// completion and returns whichever final message the coordinator emitted.
//
// The spec says StartSearch for recursive may emit a Partial msg first,
// then a Complete msg; we drain until we see the Complete msg.
func runRecursiveSearchSync(t *testing.T, c *Coordinator, query string, src SearchSource) AsyncSearchCompleteMsg {
	t.Helper()
	cmd := c.StartSearch(query, src, true)
	require.NotNil(t, cmd, "recursive StartSearch must return a tea.Cmd")

	deadline := time.After(5 * time.Second)
	for {
		resultCh := make(chan interface{}, 1)
		go func() { resultCh <- cmd() }()

		select {
		case raw := <-resultCh:
			switch v := raw.(type) {
			case AsyncSearchCompleteMsg:
				c.HandleAsyncResult(v)
				return v
			case AsyncSearchPartialMsg:
				// Partial is informational — keep draining. But a tea.Cmd
				// returns ONE message and becomes a no-op afterwards, so in
				// practice the partial message may come via a separate cmd.
				// If only one message is ever returned and it's Partial, the
				// implementation should also wire a follow-up cmd; absent
				// that, we break out here.
				_ = v
				// Can't call cmd() again safely on a stateful Cmd, so bail.
				t.Fatalf(
					"StartSearch returned only an AsyncSearchPartialMsg; expected the cmd to deliver a final Complete. Implementers should return a tea.Batch of (partial-emitting cmd, final cmd) or similar.",
				)
			default:
				t.Fatalf("unexpected message from StartSearch cmd: %T", raw)
			}
		case <-deadline:
			t.Fatalf("StartSearch cmd did not complete within 5s")
		}
	}
}

// ---------------------------------------------------------------------------
// Unit: Coordinator treeIndexCache hit skips the walk
// ---------------------------------------------------------------------------

func TestCoordinator_CacheHitSkipsWalk(t *testing.T) {
	items := []Searchable{
		newFile("report.pdf"),
		newFile("summary.pdf"),
		newFile("notes.txt"),
	}
	collector := &mockRecursiveCollector{items: items}
	c := newCachedCoordinator(t, collector)
	src := &mockRecursiveSource{baseDir: "/cached/dir"}

	// First search — cache miss, walk happens.
	_ = runRecursiveSearchSync(t, c, "report", src)
	assert.Equal(t, 1, collector.CallCount(), "first StartSearch must walk")

	// Second search, same dir + same filter → cache HIT, no walk.
	start := time.Now()
	_ = runRecursiveSearchSync(t, c, "summary", src)
	elapsed := time.Since(start)

	assert.Equal(t, 1, collector.CallCount(),
		"second StartSearch on same dir must NOT re-walk (cache hit)")
	assert.Less(t, elapsed, 100*time.Millisecond,
		"cache hit path must be fast (<100ms); got %s", elapsed)
}

// ---------------------------------------------------------------------------
// Unit: Coordinator treeIndexCache evicts after TTL
//
// The spec says TTL=30s. We do NOT want a 30s sleep in tests, so this test
// requires that implementers expose a clock injection seam (e.g.
// c.SetNowForTest(func() time.Time)). If they picked a different seam,
// adapt accordingly.
// ---------------------------------------------------------------------------

func TestCoordinator_CacheEvictsAfterTTL(t *testing.T) {
	items := []Searchable{newFile("a.go"), newFile("b.go")}
	collector := &mockRecursiveCollector{items: items}
	c := newCachedCoordinator(t, collector)
	src := &mockRecursiveSource{baseDir: "/ttl/dir"}

	// Drive time manually via a test clock.
	fakeNow := time.Unix(1_700_000_000, 0)
	c.SetNowForTest(func() time.Time { return fakeNow })

	// t=0: first search — cache miss, walk happens.
	_ = runRecursiveSearchSync(t, c, "a", src)
	assert.Equal(t, 1, collector.CallCount())

	// t=15s: second search — within TTL → cache hit, no walk.
	fakeNow = fakeNow.Add(15 * time.Second)
	_ = runRecursiveSearchSync(t, c, "b", src)
	assert.Equal(t, 1, collector.CallCount(), "within-TTL search must be cache hit")

	// t=31s: third search — beyond TTL (30s) → cache miss, walk again.
	fakeNow = fakeNow.Add(16 * time.Second)
	_ = runRecursiveSearchSync(t, c, "a", src)
	assert.Equal(t, 2, collector.CallCount(), "beyond-TTL search must re-walk")
}

// ---------------------------------------------------------------------------
// Unit: Coordinator treeIndexCache evicts via LRU when 5th entry added
// ---------------------------------------------------------------------------

func TestCoordinator_CacheLRUEviction(t *testing.T) {
	items := []Searchable{newFile("only.txt")}
	collector := &mockRecursiveCollector{items: items}
	c := newCachedCoordinator(t, collector)

	// Freeze time so TTL can't interfere.
	fakeNow := time.Unix(1_700_000_000, 0)
	c.SetNowForTest(func() time.Time { return fakeNow })

	dirs := []string{"/d/1", "/d/2", "/d/3", "/d/4", "/d/5"}

	// Fill cache with 4 distinct dirs. Each is a miss.
	for i := range 4 {
		_ = runRecursiveSearchSync(t, c, "q", &mockRecursiveSource{baseDir: dirs[i]})
	}
	assert.Equal(t, 4, collector.CallCount(), "each of 4 distinct dirs must trigger a walk")

	// Add 5th — this evicts dirs[0] (oldest).
	_ = runRecursiveSearchSync(t, c, "q", &mockRecursiveSource{baseDir: dirs[4]})
	assert.Equal(t, 5, collector.CallCount(), "5th distinct dir must trigger a walk")

	// Re-search dirs[1..4] — must all be cache hits (still within the 4-entry cap).
	for i := 1; i < 5; i++ {
		_ = runRecursiveSearchSync(t, c, "q", &mockRecursiveSource{baseDir: dirs[i]})
	}
	assert.Equal(t, 5, collector.CallCount(),
		"dirs[1..4] must be cache hits after LRU eviction of dirs[0]")

	// Re-search dirs[0] — must be a MISS (evicted).
	_ = runRecursiveSearchSync(t, c, "q", &mockRecursiveSource{baseDir: dirs[0]})
	assert.Equal(t, 6, collector.CallCount(), "evicted dirs[0] must re-walk")
}

// ---------------------------------------------------------------------------
// Unit: ClearIndexCache drops the cache
// ---------------------------------------------------------------------------

func TestCoordinator_ClearIndexCache(t *testing.T) {
	items := []Searchable{newFile("x.txt")}
	collector := &mockRecursiveCollector{items: items}
	c := newCachedCoordinator(t, collector)
	src := &mockRecursiveSource{baseDir: "/clear/dir"}

	// Freeze time so TTL doesn't accidentally evict.
	fakeNow := time.Unix(1_700_000_000, 0)
	c.SetNowForTest(func() time.Time { return fakeNow })

	_ = runRecursiveSearchSync(t, c, "x", src)
	assert.Equal(t, 1, collector.CallCount())

	// Cache hit.
	_ = runRecursiveSearchSync(t, c, "x", src)
	assert.Equal(t, 1, collector.CallCount())

	// Clear invalidates the cache.
	c.ClearIndexCache()

	// Next search MUST walk again.
	_ = runRecursiveSearchSync(t, c, "x", src)
	assert.Equal(t, 2, collector.CallCount(),
		"after ClearIndexCache the next search must re-walk")
}
