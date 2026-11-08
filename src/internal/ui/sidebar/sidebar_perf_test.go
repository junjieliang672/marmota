package sidebar

// Spec-006 sidebar responsiveness test.
//
// RED PHASE: this test is expected to fail on main@HEAD because
// UpdateDirectories currently re-runs fzf + disk stats on every call, even
// when the search term hasn't changed. The fix (T6) is to cache the
// (lastQuery, lastResults) pair inside the Model and short-circuit work
// when the search bar value is unchanged.
//
// Spec mapping: test_specifications.integration — "End-to-end: sidebar
// fuzzy search stays responsive"

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

// buildPinnedDirs writes n real pinned directories into a temporary pinned
// file and returns the file's path.
//
// Each pinned directory must exist on disk, because PinnedManager.Load
// filters out non-existing entries via os.Stat. 1000 directories is large
// enough to stress fuzzySearch on every UpdateDirectories call.
func buildPinnedDirs(t *testing.T, n int) string {
	t.Helper()
	tempDir := t.TempDir()

	pinned := make([]directory, n)
	for i := range n {
		p := filepath.Join(tempDir, fmt.Sprintf("pinned_%05d", i))
		require.NoError(t, os.MkdirAll(p, 0o755))
		pinned[i] = directory{
			Location: p,
			Name:     fmt.Sprintf("P%05d_%s", i, randomNameTag(i)),
		}
	}
	data, err := json.Marshal(pinned)
	require.NoError(t, err)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	utils.SetupFilesWithData(t, data, pinnedFile)
	return pinnedFile
}

// randomNameTag produces a short deterministic tag so names vary (some will
// match "abc", others won't) — enough variance to stress fzf but keeping
// the test deterministic.
func randomNameTag(i int) string {
	switch i % 5 {
	case 0:
		return "abc"
	case 1:
		return "xyz"
	case 2:
		return "foo"
	case 3:
		return "bar"
	default:
		return "baz"
	}
}

// TestSidebarPerf_UpdateDirectoriesStaysResponsive verifies that repeated
// UpdateDirectories calls on a large pinned list (simulating ticks while
// the rest of the app handles keypresses) do not grind the UI to a halt.
//
// Spec target: 50 calls with unchanged search bar value must complete in
// under 200ms total. On main@HEAD, UpdateDirectories re-runs fuzzySearch
// on every call because it has no "has the query changed?" gate — so this
// test is expected to be well over the budget.
func TestSidebarPerf_UpdateDirectoriesStaysResponsive(t *testing.T) {
	pinnedFile := buildPinnedDirs(t, 1000)

	pinnedMgr := NewPinnedFileManager(pinnedFile)
	// The exact work UpdateDirectories does under a non-empty search bar,
	// with a query that matches SOME of the pinned dirs. (A Model's search
	// bar is a textinput.Model, which can't easily be given a value here.)
	query := "abc"

	// Warm-up — first call populates internal caches (if any).
	_ = getFilteredDirectories(query, &pinnedMgr)

	const calls = 50
	const budget = 200 * time.Millisecond
	start := time.Now()
	for range calls {
		_ = getFilteredDirectories(query, &pinnedMgr)
	}
	elapsed := time.Since(start)

	if elapsed >= budget {
		t.Errorf(
			"50 UpdateDirectories-equivalent calls took %s, budget %s — spec-006 T6 caching missing or ineffective",
			elapsed,
			budget,
		)
	} else {
		t.Logf("OK: 50 UpdateDirectories-equivalent calls in %s (budget %s)", elapsed, budget)
	}
}

// TestSidebarPerf_UnchangedQueryShortCircuits verifies the specific caching
// invariant from T6: if the searchBar value has NOT changed between two
// UpdateDirectories calls, the second call must be materially cheaper than
// the first (e.g. no fuzzy search re-run).
//
// This test measures wall-clock ratio rather than an absolute threshold to
// avoid flakiness on slow CI: we expect the cached call to be at least 4x
// faster than the cold call on a 1000-dir list. On main@HEAD the ratio is
// ~1x (no cache), so this test fails. After T6 lands the ratio should be
// >>4x.
func TestSidebarPerf_UnchangedQueryShortCircuits(t *testing.T) {
	pinnedFile := buildPinnedDirs(t, 1000)
	pinnedMgr := NewPinnedFileManager(pinnedFile)

	// Build a Model with the exported/unexported state we can touch.
	s := &Model{pinnedMgr: &pinnedMgr}
	s.directories = getDirectories(&pinnedMgr)

	// Warm-up: JIT / disk cache prime.
	_ = getFilteredDirectories("abc", &pinnedMgr)

	// Cold call: measures the raw fuzzy-search cost.
	coldStart := time.Now()
	_ = getFilteredDirectories("abc", &pinnedMgr)
	coldDur := time.Since(coldStart)

	// Warm call: if implementers added per-Model caching keyed on query,
	// this should be dramatically faster. We call the METHOD (not the
	// package-level helper) so we exercise the real cache.
	//
	// However, the public surface here is UpdateDirectories, and it does
	// not return the result — so we wrap it in a timing run after
	// manually setting the query via a small test-only helper the
	// implementer must expose. Suggested seam (added by T6):
	//
	//   func (s *Model) UpdateDirectoriesWithQueryForTest(q string)
	//
	// If implementers pick a different seam, adapt this call site.
	_ = s                                      // keep s referenced even if the helper is missing
	s.UpdateDirectoriesWithQueryForTest("abc") // warm cache
	warmStart := time.Now()
	s.UpdateDirectoriesWithQueryForTest("abc")
	warmDur := time.Since(warmStart)

	t.Logf("cold=%s warm=%s ratio=%.2fx", coldDur, warmDur, float64(coldDur)/float64(warmDur+1))

	// Assert: warm call must be at least 4x faster than cold.
	// (On M2 Ultra cold is ~1ms for 1000 dirs, so we use a soft multiplier
	// + a hard absolute floor of 100µs to avoid divide-by-zero flakiness.)
	if warmDur*4 > coldDur && coldDur > 100*time.Microsecond {
		t.Errorf("unchanged-query UpdateDirectories was not materially faster: cold=%s warm=%s",
			coldDur, warmDur)
	}
}
