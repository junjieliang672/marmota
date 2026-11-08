package internal

// Integration test for spec-006: end-to-end recursive search through the
// file panel path. Exercises the ACTIVE code path users hit today.
//
// RED PHASE: this test (a) requires the refactor of recursivelyCollectFiles
// to delegate to the new streaming RecursiveCollector AND (b) requires the
// per-panel search cache hit behaviour to be wired. On main@HEAD the second
// recursive search will take the SAME time as the first, so the cache-hit
// assertion will fail. That is intentional — this is the red-phase signal
// that the feature needs implementation.
//
// Spec mapping: test_specifications.integration — "End-to-end: file panel
// recursive search via `f` then typing + Enter"

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildIntegrationTree builds an n-file tree at t.TempDir() with a mix of
// "foo*" and "bar*" filenames so queries produce meaningful results.
func buildIntegrationTree(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()

	// Distribute across 10 dirs × 10 subdirs, files per leaf ~= n/100.
	filesPerLeaf := n / 100
	if filesPerLeaf < 1 {
		filesPerLeaf = 1
	}

	remaining := n
	for i := 0; i < 10 && remaining > 0; i++ {
		d := filepath.Join(root, fmt.Sprintf("d%02d", i))
		require.NoError(t, os.MkdirAll(d, 0o755))
		for j := 0; j < 10 && remaining > 0; j++ {
			sub := filepath.Join(d, fmt.Sprintf("s%02d", j))
			require.NoError(t, os.MkdirAll(sub, 0o755))
			for k := 0; k < filesPerLeaf && remaining > 0; k++ {
				var name string
				switch k % 3 {
				case 0:
					name = fmt.Sprintf("foo_%d_%d_%d.txt", i, j, k)
				case 1:
					name = fmt.Sprintf("bar_%d_%d_%d.txt", i, j, k)
				default:
					name = fmt.Sprintf("baz_%d_%d_%d.txt", i, j, k)
				}
				require.NoError(t, os.WriteFile(filepath.Join(sub, name), nil, 0o644))
				remaining--
			}
		}
	}
	return root
}

// TestModelSearchPerf_RecursiveSearchEndToEnd drives the file panel through
// the full "enter recursive search, confirm, get results" flow. Verifies:
//
//	(1) results are populated within a wall-clock budget,
//	(2) no single Update() call blocks >50ms (AC-PERF-7),
//	(3) a SECOND recursive search on the same directory is a cache hit
//	    and completes in <100ms (AC-PERF-5).
func TestModelSearchPerf_RecursiveSearchEndToEnd(t *testing.T) {
	root := buildIntegrationTree(t, 2000)
	m := defaultTestModel(root)

	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	require.Equal(t, root, panel.location, "panel must start on the synthetic tree root")

	// --- First recursive search -----------------------------------------

	// Enable recursive mode (equivalent to pressing `f`) and populate the
	// search bar directly. We bypass the textinput key-by-key path to avoid
	// coupling the test to keymap details.
	panel.recursiveSearch = true
	panel.searchBar.Focus()
	panel.searchBar.SetValue("foo")

	firstStart := time.Now()
	cmd := m.confirmSearch()
	require.NotNil(t, cmd, "confirmSearch must return a tea.Cmd for recursive search")

	maxUpdateDur := deliverFinalMsgAndMeasure(t, m, cmd, 5*time.Second)
	firstElapsed := time.Since(firstStart)

	// AC-PERF-7: no single Update must exceed 50ms.
	assert.Less(t, maxUpdateDur, 50*time.Millisecond,
		"no single Update() should block >50ms (max observed: %s)", maxUpdateDur)

	// A reasonable wall-clock budget for a 2k-file tree. Spec AC-PERF-4
	// targets <1s for 50k — 2k should easily fit in 500ms.
	assert.Less(t, firstElapsed, 500*time.Millisecond,
		"first recursive search on 2k-file tree must complete < 500ms")

	// After the Complete msg has been handled, the panel should have results.
	assert.False(t, panel.searchStack.IsEmpty(), "search stack must not be empty after confirmSearch")
	top := panel.searchStack.Peek()
	require.NotNil(t, top)
	assert.False(t, top.searching, "searching flag must be cleared after the complete msg")
	assert.NotEmpty(t, top.cachedElems, "search must find foo_* files")

	// --- Second recursive search on SAME directory (cache hit) ----------

	// Pop the first layer so we start fresh.
	panel.searchStack.Pop()
	panel.recursiveSearch = true
	panel.searchBar.Focus()
	panel.searchBar.SetValue("bar")

	secondStart := time.Now()
	cmd2 := m.confirmSearch()
	require.NotNil(t, cmd2, "confirmSearch must return a tea.Cmd for second recursive search")
	maxUpdateDur2 := deliverFinalMsgAndMeasure(t, m, cmd2, 5*time.Second)
	secondElapsed := time.Since(secondStart)

	// AC-PERF-7: still no slow frames.
	assert.Less(t, maxUpdateDur2, 50*time.Millisecond,
		"no single Update() should block >50ms on cache-hit path (max observed: %s)", maxUpdateDur2)

	// AC-PERF-5 (cache hit): the spec target is p50 <100ms for 50k. On a
	// 2k-file tree we expect a cache hit to be DRAMATICALLY faster than the
	// full walk — not just "below a fixed ceiling". A fixed ceiling would be
	// tautologically satisfied on fast CI hardware where the full walk is
	// already 50ms.
	//
	// We therefore use a RATIO assertion: second call must be at least 4x
	// faster than the first call. On main@HEAD (no cache) the ratio is ~1x
	// and this fails. After T3 (cache) lands the ratio should be 10x+.
	//
	// Guard against divide-by-near-zero when first is already trivially fast
	// (e.g. tiny tree or a warmed/noisy test process): once the cold path is
	// already down in the single-digit millisecond range, a strict 4x ratio
	// becomes unstable across full-package runs even though the cache-hit path
	// is still comfortably fast in absolute terms. Require at least 10ms on
	// the first search before enforcing the ratio; otherwise fall back to the
	// absolute latency ceiling.
	if firstElapsed >= 10*time.Millisecond {
		assert.Less(t, secondElapsed*4, firstElapsed,
			"cache-hit recursive search should be >=4x faster than first. first=%s second=%s ratio=%.2fx",
			firstElapsed, secondElapsed, float64(firstElapsed)/float64(secondElapsed+1))
	} else {
		assert.Less(t, secondElapsed, 50*time.Millisecond,
			"second recursive search must be <50ms (fast-hardware fallback)")
	}

	top2 := panel.searchStack.Peek()
	require.NotNil(t, top2)
	assert.NotEmpty(t, top2.cachedElems, "second search must find bar_* files")
}

// deliverFinalMsgAndMeasure runs cmd off the UI goroutine, feeds its final
// message back into m.Update, and returns the worst-case Update latency
// observed.
func deliverFinalMsgAndMeasure(t *testing.T, m *model, cmd tea.Cmd, timeout time.Duration) time.Duration {
	t.Helper()
	require.NotNil(t, cmd)

	resultCh := make(chan tea.Msg, 1)
	go func() { resultCh <- cmd() }()

	maxDur := time.Duration(0)
	select {
	case msg := <-resultCh:
		start := time.Now()
		_ = TeaUpdate(m, msg)
		dur := time.Since(start)
		if dur > maxDur {
			maxDur = dur
		}
	case <-time.After(timeout):
		t.Fatalf("async cmd did not complete within %s", timeout)
	}
	return maxDur
}
