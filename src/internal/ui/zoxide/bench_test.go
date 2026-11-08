package zoxide

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	zoxidelib "github.com/lazysegtree/go-zoxide"
)

// buildSyntheticResults creates N temp directories (half real, half fake) and
// returns a zoxide.Result slice preserving the zoxide-like random-ish ordering
// (alternating real/fake so no worker can short-circuit a whole half).
func buildSyntheticResults(tb testing.TB, n int) []zoxidelib.Result {
	tb.Helper()
	root := tb.TempDir()
	results := make([]zoxidelib.Result, 0, n)
	for i := range n {
		var p string
		if i%2 == 0 {
			p = filepath.Join(root, fmt.Sprintf("real-%04d", i))
			if err := os.MkdirAll(p, 0o755); err != nil {
				tb.Fatalf("mkdir: %v", err)
			}
		} else {
			p = filepath.Join(root, fmt.Sprintf("fake-%04d", i))
			// deliberately don't create it
		}
		results = append(results, zoxidelib.Result{Path: p, Score: float64(n - i)})
	}
	return results
}

// BenchmarkFilterExistingDirs exercises the filter across a sweep of worker
// counts, against both a fast (real os.Stat on local temp dirs) and a slow
// (sleep-injected) stat function. Goal: find the minimum W where throughput
// is within 10% of the best observed value.
//
// Run with:
//
//	go test -bench=BenchmarkFilterExistingDirs ./src/internal/ui/zoxide/... \
//	    -benchtime=200ms -benchmem -run=^$
func BenchmarkFilterExistingDirs(b *testing.B) {
	const n = 500
	results := buildSyntheticResults(b, n)

	workers := []int{1, 2, 4, 8, 16, 32}

	b.Run("fast-local-stat", func(b *testing.B) {
		for _, w := range workers {
			b.Run(fmt.Sprintf("W=%02d", w), func(b *testing.B) {
				// Each iteration uses a fresh cache so we measure the COLD
				// (worst-case) path, which is what dominates real perceived
				// keystroke lag on the very first query of a session.
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					c := newPathCache(time.Hour, defaultStatFn)
					_ = filterExistingDirsWithWorkers(results, w, c)
				}
			})
		}
	})

	b.Run("slow-sim-stat-1ms", func(b *testing.B) {
		// Simulate a network/FUSE filesystem: 1ms per stat. This makes
		// goroutine-overhead-vs-work-per-stat comparable to real slow paths.
		slowStat := func(path string) (bool, error) {
			time.Sleep(1 * time.Millisecond)
			_, err := os.Stat(path)
			return err == nil, err
		}
		for _, w := range workers {
			b.Run(fmt.Sprintf("W=%02d", w), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					c := newPathCache(time.Hour, slowStat)
					_ = filterExistingDirsWithWorkers(results, w, c)
				}
			})
		}
	})

	b.Run("warm-cache", func(b *testing.B) {
		// Pre-populate the cache, then benchmark. This represents the
		// second/third keystroke scenario where most paths have already been
		// confirmed. Worker count should barely matter here.
		c := newPathCache(time.Hour, defaultStatFn)
		_ = filterExistingDirsWithWorkers(results, 8, c)
		for _, w := range workers {
			b.Run(fmt.Sprintf("W=%02d", w), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					_ = filterExistingDirsWithWorkers(results, w, c)
				}
			})
		}
	})
}
