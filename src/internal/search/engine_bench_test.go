package search

// Benchmarks for spec-006 (fuzzy-search-perf).
//
// This file holds the full benchmark surface used to measure the spec-006
// performance improvements:
//   - Serial RecursiveCollector (baseline)
//   - Parallel CollectStream with Workers=1/4/8 (regression guard + main win)
//   - FzfExecutor.Execute / ExecuteWithCap no-regression benches
//
// The Coordinator-level benchmarks live in coordinator_bench_test.go.
//
// Running the whole post-implementation suite:
//   go test ./src/internal/search/... \
//     -bench='BenchmarkCollector|BenchmarkFzf|BenchmarkCoordinator' \
//     -benchmem -benchtime=5x -count=3 -timeout 300s

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Shared benchmark/test helpers
// ---------------------------------------------------------------------------

// buildSyntheticTree builds a synthetic directory tree with approximately n
// files spread across a depth-5 structure rooted at b.TempDir(). It is
// intentionally simple (balanced fan-out, 20 files per leaf, deterministic
// filenames) so benchmark variance comes from the collector, not the tree
// shape. Returns the absolute root path.
//
// Layout (depth 5, fan-out tuned to hit ~n files):
//
//	root/
//	  dir_0/
//	    dir_0_0/
//	      dir_0_0_0/
//	        dir_0_0_0_0/
//	          file_0000.txt ... file_0019.txt (20 files per leaf)
//	  ...
//
// The helper picks a fan-out F such that F^4 * 20 >= n, so total files are
// close to n but never less than n.
func buildSyntheticTree(tb testing.TB, n int) string {
	tb.Helper()

	root := tb.TempDir()
	leavesNeeded := (n + syntheticFilesPerLeaf - 1) / syntheticFilesPerLeaf
	tree := syntheticTree{tb: tb, fanout: syntheticFanout(leavesNeeded), filesLeft: n}
	tree.build(root, 0)
	return root
}

// The synthetic tree's shape: 20 files per leaf at depth 4.
const (
	syntheticFilesPerLeaf = 20
	syntheticDepth        = 4 // 4 nested dirs under root = depth 5 from root perspective
)

// syntheticFanout is the smallest fan-out F with F^syntheticDepth >= leaves.
func syntheticFanout(leaves int) int {
	fanout := 1
	for pow := 1; pow < leaves; fanout++ {
		pow = 1
		for range syntheticDepth {
			pow *= fanout
		}
		if pow >= leaves {
			break
		}
	}
	return max(fanout, 1)
}

// syntheticTree writes the files of a synthetic tree until filesLeft runs
// out.
type syntheticTree struct {
	tb        testing.TB
	fanout    int
	filesLeft int
}

func (s *syntheticTree) build(path string, lvl int) {
	if s.filesLeft <= 0 {
		return
	}
	if lvl == syntheticDepth {
		s.writeLeaf(path)
		return
	}
	for i := 0; i < s.fanout && s.filesLeft > 0; i++ {
		sub := filepath.Join(path, fmt.Sprintf("dir_%d_%d", lvl, i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			s.tb.Fatalf("mkdir: %v", err)
		}
		s.build(sub, lvl+1)
	}
}

// writeLeaf creates a leaf folder's files.
func (s *syntheticTree) writeLeaf(path string) {
	for i := 0; i < syntheticFilesPerLeaf && s.filesLeft > 0; i++ {
		fname := fmt.Sprintf("file_%06d.txt", s.filesLeft)
		// Mix in a token ~1% of the time to make fzf queries meaningful.
		if i%100 == 0 {
			fname = fmt.Sprintf("abc_%06d.txt", s.filesLeft)
		}
		if err := os.WriteFile(filepath.Join(path, fname), nil, 0o644); err != nil {
			s.tb.Fatalf("write file: %v", err)
		}
		s.filesLeft--
	}
}

// randomFilenames produces n pseudo-random lowercase filenames for FZF
// haystack benchmarks. Deterministic by seed.
func randomFilenames(n int, seed int64) []string {
	r := rand.New(rand.NewSource(seed))
	out := make([]string, n)
	letters := "abcdefghijklmnopqrstuvwxyz"
	for i := range n {
		length := 5 + r.Intn(12)
		b := make([]byte, length)
		for j := range length {
			b[j] = letters[r.Intn(len(letters))]
		}
		// Occasionally inject an 'abc' token so queries match.
		if i%200 == 0 {
			copy(b, "abc")
		}
		out[i] = string(b) + ".txt"
	}
	return out
}

// buildSyntheticItems produces n in-memory Searchable items for executor
// benchmarks (no disk involved).
func buildSyntheticItems(n int) []Searchable {
	names := randomFilenames(n, 42)
	out := make([]Searchable, n)
	for i, name := range names {
		out[i] = &mockSearchable{
			name:      name,
			location:  "/synthetic/" + name,
			directory: i%10 == 0,
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Baseline benchmarks — compile and run on main@HEAD
// ---------------------------------------------------------------------------

// BenchmarkFzfExecutor_50k measures the scoring path on a 50k haystack with a
// short fuzzy query. Used as a no-regression target (AC-PERF-2).
func BenchmarkFzfExecutor_50k(b *testing.B) {
	items := buildSyntheticItems(50_000)
	executor := NewFzfExecutor(10 * time.Second)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := executor.Execute(ctx, "abc", items)
		if err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

// BenchmarkCollector_Serial_50k measures the current single-threaded
// filepath.Walk based Collect() on a 50k-file synthetic tree. This is the
// AC-PERF-1 baseline (target <400ms parallel vs ~1200ms baseline).
func BenchmarkCollector_Serial_50k(b *testing.B) {
	root := buildSyntheticTree(b, 50_000)
	rc := NewRecursiveCollector(MaxSearchDepth, true) // showHidden=true to match iCloud defaults
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		items, _, err := rc.Collect(ctx, root, "")
		if err != nil {
			b.Fatalf("collect: %v", err)
		}
		if len(items) == 0 {
			b.Fatalf("expected non-empty items, got 0")
		}
	}
}

// BenchmarkCollector_Serial_10k — smaller fixture for faster CI iteration.
func BenchmarkCollector_Serial_10k(b *testing.B) {
	root := buildSyntheticTree(b, 10_000)
	rc := NewRecursiveCollector(MaxSearchDepth, true)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		items, _, err := rc.Collect(ctx, root, "")
		if err != nil {
			b.Fatalf("collect: %v", err)
		}
		_ = items
	}
}

// ---------------------------------------------------------------------------
// Spec-006 parallel benchmarks — CollectStream
// ---------------------------------------------------------------------------

// benchmarkCollectStream drives CollectStream with the given worker count on a
// 50k-file synthetic tree. It drains the output channel fully so the measured
// wall-clock covers the ENTIRE walk (not just time-to-first-batch).
//
// Fixture build happens once before b.ResetTimer so only the walk cost is
// measured.
func benchmarkCollectStream(b *testing.B, workers int) {
	b.Helper()
	root := buildSyntheticTree(b, 50_000)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		rc := NewRecursiveCollector(MaxSearchDepth, true)
		rc.Workers = workers
		rc.BatchSize = 256
		// Buffered enough that the walker rarely blocks on the drain.
		out := make(chan []Searchable, 32)

		doneCh := make(chan struct{})
		go func() {
			_, _ = rc.CollectStream(ctx, root, "", out)
			close(doneCh)
		}()

		total := 0
		for batch := range out {
			total += len(batch)
		}
		<-doneCh

		if total == 0 {
			b.Fatalf("expected non-empty items from CollectStream")
		}
	}
}

// BenchmarkCollector_Parallel_50k_W8 is the headline performance benchmark
// for spec-006 AC-PERF-1 (target < 400ms parallel vs ~1200ms baseline).
func BenchmarkCollector_Parallel_50k_W8(b *testing.B) {
	benchmarkCollectStream(b, 8)
}

// BenchmarkCollector_Parallel_50k_W4 — secondary worker count for tuning.
func BenchmarkCollector_Parallel_50k_W4(b *testing.B) {
	benchmarkCollectStream(b, 4)
}

// BenchmarkCollector_Parallel_50k_W1 — regression guard. Single-worker
// CollectStream must NOT be materially slower than the serial Collect path.
func BenchmarkCollector_Parallel_50k_W1(b *testing.B) {
	benchmarkCollectStream(b, 1)
}

// BenchmarkCollector_Parallel_50k_W16 — comparison point for the
// Workers=16 vs Workers=8 tuning question. Used to decide whether the
// default worker count should bump up.
func BenchmarkCollector_Parallel_50k_W16(b *testing.B) {
	benchmarkCollectStream(b, 16)
}

// ---------------------------------------------------------------------------
// Spec-006 FzfExecutor.ExecuteWithCap bench — cap=DefaultMaxResults (5000)
// ---------------------------------------------------------------------------

// BenchmarkFzfExecutor_ExecuteWithCap_50k measures the scoring path with the
// production cap enabled. Must stay within a few percent of the uncapped
// Execute — the cap only truncates AFTER scoring, so the wins come from
// smaller downstream slices and fewer allocations on the final copy.
//
// Name-prefixed with BenchmarkFzfExecutor_ so it is picked up by the
// canonical run filter 'BenchmarkCollector|BenchmarkFzf|BenchmarkCoordinator'.
func BenchmarkFzfExecutor_ExecuteWithCap_50k(b *testing.B) {
	items := buildSyntheticItems(50_000)
	executor := NewFzfExecutor(10 * time.Second)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_, err := executor.ExecuteWithCap(ctx, "abc", items, DefaultMaxResults)
		if err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}
