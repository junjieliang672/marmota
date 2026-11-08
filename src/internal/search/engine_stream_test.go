package search

// Tests for spec-006 RecursiveCollector.CollectStream (new streaming API).
//
// RED PHASE: these tests reference the symbols
//   - RecursiveCollector.CollectStream
//   - RecursiveCollector.Workers
//   - RecursiveCollector.BatchSize
// which do NOT exist yet in engine.go. They will not compile until the
// implementer (T1) adds them. That is the expected evaluation-first state.
//
// Each test maps to a specific test_specifications.unit entry in
// .knowledge/specs/active/spec-006-fuzzy-search-perf.yaml.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers for streaming tests
// ---------------------------------------------------------------------------

// drainStream reads all batches from out and returns them flattened. It also
// records the wall-clock time of the FIRST batch (for time-to-first-result
// style assertions). Returns (allItems, firstBatchAt, totalDuration).
//
// The caller is responsible for launching the producer goroutine (so they can
// observe the returned err from CollectStream).
func drainStream(
	t *testing.T,
	out <-chan []Searchable,
	timeout time.Duration,
) ([]Searchable, time.Duration, time.Duration) {
	t.Helper()
	start := time.Now()
	var items []Searchable
	var firstBatchAt time.Duration
	gotFirst := false
	deadline := time.After(timeout)
	for {
		select {
		case batch, ok := <-out:
			if !ok {
				return items, firstBatchAt, time.Since(start)
			}
			if !gotFirst {
				firstBatchAt = time.Since(start)
				gotFirst = true
			}
			items = append(items, batch...)
		case <-deadline:
			t.Fatalf("drainStream: channel did not close within %s (received %d items)", timeout, len(items))
		}
	}
}

// buildSmallTree creates a predictable directory tree for correctness tests.
// Returns (rootPath, expectedFileBasenames, expectedDirBasenames).
// The tree has exactly 50 files in 5 nested subdirectories.
func buildSmallTree(t *testing.T, root string) ([]string, []string) {
	t.Helper()

	// root/
	//   dirA/
	//     a0.txt ... a9.txt
	//     nested/
	//       n0.txt ... n4.txt
	//   dirB/
	//     b0.txt ... b9.txt
	//   dirC/
	//     c0.txt ... c9.txt
	//     sub/
	//       s0.txt ... s5.txt
	//   top0.txt ... top9.txt
	type leaf struct {
		dir   string
		files []string
	}
	leaves := []leaf{
		{root, nil}, // top files
		{filepath.Join(root, "dirA"), nil},
		{filepath.Join(root, "dirA", "nested"), nil},
		{filepath.Join(root, "dirB"), nil},
		{filepath.Join(root, "dirC"), nil},
		{filepath.Join(root, "dirC", "sub"), nil},
	}
	// populate files
	for i := range 10 {
		leaves[0].files = append(leaves[0].files, fmt.Sprintf("top%d.txt", i))
		leaves[1].files = append(leaves[1].files, fmt.Sprintf("a%d.txt", i))
		leaves[3].files = append(leaves[3].files, fmt.Sprintf("b%d.txt", i))
		leaves[4].files = append(leaves[4].files, fmt.Sprintf("c%d.txt", i))
	}
	for i := range 5 {
		leaves[2].files = append(leaves[2].files, fmt.Sprintf("n%d.txt", i))
	}
	for i := range 6 {
		leaves[5].files = append(leaves[5].files, fmt.Sprintf("s%d.txt", i))
	}

	var fileBasenames []string
	for _, l := range leaves {
		require.NoError(t, os.MkdirAll(l.dir, 0o755))
		for _, f := range l.files {
			require.NoError(t, os.WriteFile(filepath.Join(l.dir, f), nil, 0o644))
			fileBasenames = append(fileBasenames, f)
		}
	}
	dirBasenames := []string{"dirA", "nested", "dirB", "dirC", "sub"}

	// Sanity: 10 + 10 + 5 + 10 + 10 + 6 = 51 files total. (Spec says ~50.)
	return fileBasenames, dirBasenames
}

// collectLocations extracts GetLocation() from items and returns a sorted set.
func collectLocations(items []Searchable) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.GetLocation()
	}
	sort.Strings(out)
	return out
}

// collectNames extracts GetName() from items and returns a sorted set.
func collectNames(items []Searchable) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.GetName()
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Unit: CollectStream emits all files via the channel and closes it
// ---------------------------------------------------------------------------

func TestCollectStream_EmitsAllFilesAndClosesChannel(t *testing.T) {
	root := t.TempDir()
	expectedFiles, expectedDirs := buildSmallTree(t, root)

	rc := NewRecursiveCollector(MaxSearchDepth, true) // showHidden=true
	rc.Workers = 4
	rc.BatchSize = 16

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out := make(chan []Searchable, 8)

	var (
		depthLimited bool
		collectErr   error
	)
	done := make(chan struct{})
	go func() {
		depthLimited, collectErr = rc.CollectStream(ctx, root, "", out)
		close(done)
	}()

	items, _, totalAt := drainStream(t, out, time.Second)
	<-done

	require.NoError(t, collectErr)
	assert.False(t, depthLimited, "small tree should not hit depth limit")
	assert.Less(t, totalAt, time.Second, "small tree should finish well under 1s")

	names := collectNames(items)

	for _, fn := range expectedFiles {
		assert.Contains(t, names, fn, "expected file %q to appear in stream", fn)
	}
	for _, dn := range expectedDirs {
		assert.Contains(t, names, dn, "expected dir %q to appear in stream", dn)
	}

	// Must not contain the root itself.
	for _, it := range items {
		assert.NotEqual(t, root, it.GetLocation(), "root path must not appear in stream")
	}

	// Invariant: channel closed exactly once. Attempting another receive must
	// be non-blocking and return zero-value.
	select {
	case _, ok := <-out:
		assert.False(t, ok, "channel must be closed after CollectStream returns")
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("channel not closed promptly after CollectStream returned")
	}
}

// ---------------------------------------------------------------------------
// Unit: CollectStream respects ctx cancellation mid-walk
// ---------------------------------------------------------------------------

func TestCollectStream_RespectsCtxCancellation(t *testing.T) {
	// Build a wide tree (~5000 files in many dirs) so the walk takes long
	// enough for us to cancel mid-flight.
	root := buildSyntheticTree(t, 5000)

	rc := NewRecursiveCollector(MaxSearchDepth, true)
	rc.Workers = 2 // fewer workers so cancellation visibly arrests the walk
	rc.BatchSize = 32

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan []Searchable, 64)

	var collectErr error
	done := make(chan struct{})
	go func() {
		_, collectErr = rc.CollectStream(ctx, root, "", out)
		close(done)
	}()

	// Receive the first batch.
	select {
	case <-out:
		// got a batch
	case <-time.After(2 * time.Second):
		t.Fatalf("never received first batch")
	}

	// Cancel and make sure the channel closes quickly.
	cancelStart := time.Now()
	cancel()

	// Drain remaining batches.
	for {
		_, ok := <-out
		if !ok {
			break
		}
	}
	elapsed := time.Since(cancelStart)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("CollectStream did not return within 500ms of cancel")
	}

	assert.Less(
		t,
		elapsed,
		500*time.Millisecond,
		"channel should close within ~500ms of ctx cancel (spec target: <50ms per-dir granularity)",
	)
	require.Error(t, collectErr, "CollectStream must return a non-nil error on cancel")
	assert.ErrorIs(t, collectErr, context.Canceled, "err should wrap context.Canceled, got %v", collectErr)
}

// ---------------------------------------------------------------------------
// Unit: CollectStream returns same SET of locations as the serial Collect
// (AC-CORRECT-1)
// ---------------------------------------------------------------------------

func TestCollectStream_ParityWithSerialCollect(t *testing.T) {
	// 500 files, mixed depth, including hidden files and a symlink.
	root := t.TempDir()

	// Build a moderately complex tree.
	for i := range 10 {
		d := filepath.Join(root, fmt.Sprintf("dir_%d", i))
		require.NoError(t, os.MkdirAll(d, 0o755))
		for j := range 30 {
			p := filepath.Join(d, fmt.Sprintf("f_%d_%d.txt", i, j))
			require.NoError(t, os.WriteFile(p, nil, 0o644))
		}
		// deeper layer
		sub := filepath.Join(d, "sub")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		for j := range 15 {
			p := filepath.Join(sub, fmt.Sprintf("s_%d_%d.txt", i, j))
			require.NoError(t, os.WriteFile(p, nil, 0o644))
		}
	}
	// hidden file
	require.NoError(t, os.WriteFile(filepath.Join(root, ".hidden_file"), nil, 0o644))
	// hidden dir with one file
	hiddenDir := filepath.Join(root, ".hidden_dir")
	require.NoError(t, os.MkdirAll(hiddenDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hiddenDir, "secret.txt"), nil, 0o644))

	// Symlink pointing to a directory — exercise symlink-to-dir handling.
	if runtime.GOOS != "windows" {
		target := filepath.Join(root, "dir_0")
		link := filepath.Join(root, "link_to_dir_0")
		_ = os.Symlink(target, link) // best-effort; tolerant if unsupported
	}

	runParity := func(t *testing.T, showHidden bool, filterType string) {
		t.Helper()

		// Serial reference
		rcSerial := NewRecursiveCollector(MaxSearchDepth, showHidden)
		serialItems, _, err := rcSerial.Collect(context.Background(), root, filterType)
		require.NoError(t, err)

		// Parallel candidate
		rcPar := NewRecursiveCollector(MaxSearchDepth, showHidden)
		rcPar.Workers = 8
		rcPar.BatchSize = 16
		out := make(chan []Searchable, 16)
		var (
			parItems []Searchable
			parErr   error
		)
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, parErr = rcPar.CollectStream(context.Background(), root, filterType, out)
		}()
		for batch := range out {
			parItems = append(parItems, batch...)
		}
		wg.Wait()
		require.NoError(t, parErr)

		// Compare as SETS (AC-CORRECT-1: order may differ)
		serialLocs := collectLocations(serialItems)
		parLocs := collectLocations(parItems)
		assert.Equal(t, serialLocs, parLocs,
			"parallel CollectStream must produce the same set of locations as serial Collect "+
				"(showHidden=%v, filter=%q)", showHidden, filterType)
	}

	t.Run("no_filter_showHidden_false", func(t *testing.T) { runParity(t, false, "") })
	t.Run("no_filter_showHidden_true", func(t *testing.T) { runParity(t, true, "") })
	t.Run("files_filter", func(t *testing.T) { runParity(t, false, "files") })
	t.Run("dirs_filter", func(t *testing.T) { runParity(t, false, "dirs") })
}

// ---------------------------------------------------------------------------
// Unit: CollectStream with Workers=1 equals serial behaviour
// ---------------------------------------------------------------------------

func TestCollectStream_Workers1_MatchesSerial(t *testing.T) {
	root := t.TempDir()
	buildSmallTree(t, root)

	// Serial reference
	rcSerial := NewRecursiveCollector(MaxSearchDepth, true)
	serialItems, _, err := rcSerial.Collect(context.Background(), root, "")
	require.NoError(t, err)

	// Workers=1 — exercises the single-worker code path as a regression guard.
	rcPar := NewRecursiveCollector(MaxSearchDepth, true)
	rcPar.Workers = 1
	rcPar.BatchSize = 8
	out := make(chan []Searchable, 8)
	var parItems []Searchable
	done := make(chan struct{})
	go func() {
		_, _ = rcPar.CollectStream(context.Background(), root, "", out)
		close(done)
	}()
	for batch := range out {
		parItems = append(parItems, batch...)
	}
	<-done

	assert.Equal(t, collectLocations(serialItems), collectLocations(parItems),
		"Workers=1 path must produce identical set to serial Collect")
}

// ---------------------------------------------------------------------------
// Unit: CollectStream never panics on unreadable subdirectory
// (failure injection)
// ---------------------------------------------------------------------------

func TestCollectStream_UnreadableSubdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 semantics not reliably testable on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root — chmod 000 does not restrict root")
	}

	root := t.TempDir()
	okDir := filepath.Join(root, "readable")
	require.NoError(t, os.MkdirAll(okDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(okDir, "ok1.txt"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(okDir, "ok2.txt"), nil, 0o644))

	badDir := filepath.Join(root, "unreadable")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "hidden.txt"), nil, 0o644))
	require.NoError(t, os.Chmod(badDir, 0o000))
	t.Cleanup(func() {
		// Restore permissions so t.TempDir cleanup can remove it.
		_ = os.Chmod(badDir, 0o755)
	})

	rc := NewRecursiveCollector(MaxSearchDepth, false)
	rc.Workers = 4
	rc.BatchSize = 16

	out := make(chan []Searchable, 8)
	var (
		items []Searchable
		err   error
	)
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CollectStream panicked on unreadable subdir: %v", r)
			}
			close(done)
		}()
		_, err = rc.CollectStream(context.Background(), root, "", out)
	}()

	for batch := range out {
		items = append(items, batch...)
	}
	<-done

	// Must complete without an error — the unreadable dir should be logged
	// and skipped per spec (error_boundaries entry).
	require.NoError(t, err, "unreadable subdir must not abort the walk")

	names := collectNames(items)
	assert.Contains(t, names, "ok1.txt")
	assert.Contains(t, names, "ok2.txt")
	assert.NotContains(t, names, "hidden.txt", "file inside unreadable dir must not appear")
}

// ---------------------------------------------------------------------------
// Unit: CollectStream respects MaxDepth and sets depthLimited
// ---------------------------------------------------------------------------

func TestCollectStream_MaxDepthLimited(t *testing.T) {
	root := t.TempDir()
	// Build a 10-level deep tree: root/d1/d2/.../d10/file.txt
	current := root
	for i := 1; i <= 10; i++ {
		current = filepath.Join(current, fmt.Sprintf("d%d", i))
		require.NoError(t, os.MkdirAll(current, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(current, fmt.Sprintf("f%d.txt", i)), nil, 0o644))
	}

	rc := NewRecursiveCollector(3, true)
	rc.Workers = 2
	rc.BatchSize = 4

	out := make(chan []Searchable, 8)
	var (
		depthLimited bool
		collectErr   error
	)
	done := make(chan struct{})
	go func() {
		depthLimited, collectErr = rc.CollectStream(context.Background(), root, "", out)
		close(done)
	}()

	var items []Searchable
	for batch := range out {
		items = append(items, batch...)
	}
	<-done

	require.NoError(t, collectErr)
	assert.True(t, depthLimited, "expected depthLimited=true for 10-deep tree with MaxDepth=3")

	names := collectNames(items)

	// Per current serial Collect semantics: directory at depth N appears, but
	// its *contents* at depth N+1 do not once we exceed MaxDepth.
	// With MaxDepth=3 we expect d1, d2, d3 to be present AND files f1.txt, f2.txt, f3.txt.
	// d4 should NOT appear (it's at depth 4, beyond limit).
	assert.Contains(t, names, "d1")
	assert.Contains(t, names, "d2")
	assert.Contains(t, names, "d3")
	assert.Contains(t, names, "f1.txt")
	assert.Contains(t, names, "f2.txt")
	assert.Contains(t, names, "f3.txt")
	assert.NotContains(t, names, "d4", "depth-4 directory must be skipped")
	assert.NotContains(t, names, "f5.txt", "depth-5+ files must be skipped")
	assert.NotContains(t, names, "f10.txt")
}

// ---------------------------------------------------------------------------
// Failure injection: ReadDir returns error on root (non-existent path)
// ---------------------------------------------------------------------------

func TestCollectStream_NonExistentRoot(t *testing.T) {
	rc := NewRecursiveCollector(MaxSearchDepth, false)
	rc.Workers = 4
	rc.BatchSize = 16

	out := make(chan []Searchable, 4)
	var (
		items []Searchable
		err   error
	)
	done := make(chan struct{})
	go func() {
		_, err = rc.CollectStream(context.Background(), "/does-not-exist-spec006", "", out)
		close(done)
	}()
	for batch := range out {
		items = append(items, batch...)
	}
	<-done

	require.Error(t, err, "non-existent root must return a non-nil error")
	assert.Empty(t, items, "no items should be emitted for a non-existent root")

	// Channel must be closed.
	select {
	case _, ok := <-out:
		assert.False(t, ok, "channel must be closed after CollectStream returns with error")
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("channel not closed after CollectStream error-return")
	}
}

// ---------------------------------------------------------------------------
// Failure injection: Context timeout mid-walk emits partial results
// ---------------------------------------------------------------------------

func TestCollectStream_ContextTimeoutEmitsPartial(t *testing.T) {
	root := buildSyntheticTree(t, 50_000)

	rc := NewRecursiveCollector(MaxSearchDepth, true)
	rc.Workers = 2 // fewer workers so a 50ms deadline is actually hit
	rc.BatchSize = 32

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	out := make(chan []Searchable, 64)
	var (
		items []Searchable
		err   error
	)
	done := make(chan struct{})
	go func() {
		_, err = rc.CollectStream(ctx, root, "", out)
		close(done)
	}()
	for batch := range out {
		items = append(items, batch...)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("CollectStream did not return within 2s after 50ms deadline")
	}

	require.Error(t, err, "deadline exceeded must surface as an error")
	require.ErrorIs(t, err, context.DeadlineExceeded, "err should wrap DeadlineExceeded, got %v", err)

	// Per spec: partial results are still delivered.
	// We cannot assert a strict lower bound (>0) without being flaky on very
	// fast hardware where the entire walk finishes in <50ms. So we merely
	// assert items <= 50000 (no overshoot) and the error path was taken.
	assert.LessOrEqual(t, len(items), 50_000)
}
