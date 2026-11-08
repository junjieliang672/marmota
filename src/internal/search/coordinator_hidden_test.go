package search

// Tests for spec-006 follow-up: Coordinator must honour ShowHidden() from
// sources that implement HiddenAwareSource so post-Enter recursive searches
// match live-typing results. Sources that do NOT implement the interface must
// keep the legacy default of showHidden=true for backwards compatibility with
// non-file-panel callers (e.g. help-menu search).

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Test doubles specific to hidden-aware behaviour
// ---------------------------------------------------------------------------

// hiddenAwareSource implements BOTH SearchSource AND HiddenAwareSource. The
// showHidden value is returned from ShowHidden() to the Coordinator.
type hiddenAwareSource struct {
	baseDir    string
	showHidden bool
}

func (s *hiddenAwareSource) GetItems() []Searchable   { return nil }
func (s *hiddenAwareSource) GetBaseDirectory() string { return s.baseDir }
func (s *hiddenAwareSource) ShowHidden() bool         { return s.showHidden }

// legacySource implements ONLY SearchSource (no ShowHidden method). Used to
// assert that callers which don't opt in to HiddenAwareSource keep the legacy
// default of showHidden=true.
type legacySource struct {
	baseDir string
}

func (s *legacySource) GetItems() []Searchable   { return nil }
func (s *legacySource) GetBaseDirectory() string { return s.baseDir }

// recordingCollectorFactory returns a factory that records the (maxDepth,
// showHidden) tuple it was called with and delegates actual collection to the
// supplied mock. Safe for concurrent use.
type recordingCollectorFactory struct {
	lastMaxDepth   int32
	lastShowHidden int32 // 0/1
	calls          int32
	mock           *mockRecursiveCollector
}

func (f *recordingCollectorFactory) factory() func(int, bool) streamingCollector {
	return func(maxDepth int, showHidden bool) streamingCollector {
		atomic.StoreInt32(&f.lastMaxDepth, int32(maxDepth))
		var sh int32
		if showHidden {
			sh = 1
		}
		atomic.StoreInt32(&f.lastShowHidden, sh)
		atomic.AddInt32(&f.calls, 1)
		return f.mock
	}
}

func (f *recordingCollectorFactory) LastShowHidden() bool {
	return atomic.LoadInt32(&f.lastShowHidden) == 1
}

func (f *recordingCollectorFactory) Calls() int {
	return int(atomic.LoadInt32(&f.calls))
}

// ---------------------------------------------------------------------------
// Unit: HiddenAwareSource with ShowHidden()=false triggers a walk keyed by
// showHidden=false, which must NOT hit a cache entry built with true.
// ---------------------------------------------------------------------------

func TestCoordinator_HonorsShowHiddenFromSource(t *testing.T) {
	items := []Searchable{newFile("a.go"), newFile("b.go")}
	mock := &mockRecursiveCollector{items: items}
	rec := &recordingCollectorFactory{mock: mock}

	cfg := FilePanelConfig()
	cfg.Timeout = 2 * time.Second
	c := NewCoordinator(cfg)
	c.SetCollectorForTest(rec.factory())

	// First: showHidden=true. Miss → walks with showHidden=true.
	srcShow := &hiddenAwareSource{baseDir: "/same/dir", showHidden: true}
	_ = runRecursiveSearchSync(t, c, "a", srcShow)
	assert.Equal(t, 1, mock.CallCount(), "first search must walk")
	assert.True(t, rec.LastShowHidden(),
		"collector factory must be called with showHidden=true when source says true")

	// Second: same base dir, but showHidden=false. The cache key differs so
	// this MUST be a cache miss that walks again.
	srcHide := &hiddenAwareSource{baseDir: "/same/dir", showHidden: false}
	_ = runRecursiveSearchSync(t, c, "b", srcHide)
	assert.Equal(t, 2, mock.CallCount(),
		"different showHidden on same dir must be a distinct cache entry (miss → walk)")
	assert.False(t, rec.LastShowHidden(),
		"collector factory must be called with showHidden=false when source says false")

	// Third: repeat showHidden=false. Cache hit → no additional walk.
	_ = runRecursiveSearchSync(t, c, "a", srcHide)
	assert.Equal(t, 2, mock.CallCount(),
		"repeat search with same (dir,showHidden) must be a cache hit")
}

// ---------------------------------------------------------------------------
// Unit: Source that does NOT implement HiddenAwareSource must default to
// showHidden=true. This guards the help-menu search path.
// ---------------------------------------------------------------------------

func TestCoordinator_LegacySourceDefaultsShowHiddenTrue(t *testing.T) {
	items := []Searchable{newFile("x.go")}
	mock := &mockRecursiveCollector{items: items}
	rec := &recordingCollectorFactory{mock: mock}

	cfg := FilePanelConfig()
	cfg.Timeout = 2 * time.Second
	c := NewCoordinator(cfg)
	c.SetCollectorForTest(rec.factory())

	src := &legacySource{baseDir: "/legacy/dir"}
	_ = runRecursiveSearchSync(t, c, "x", src)

	assert.Equal(t, 1, mock.CallCount(), "legacy source must still trigger a walk")
	assert.True(t, rec.LastShowHidden(),
		"legacy source (no HiddenAwareSource) must default to showHidden=true")
}
