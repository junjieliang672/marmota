package search

// Coordinator benchmarks for spec-006.
//
// These benchmarks exercise the end-to-end recursive search path via the
// Coordinator, including the treeIndexCache.

import (
	"testing"
	"time"
)

// BenchmarkCoordinator_CacheHit measures the second (cache-hit) recursive
// search on a 50k-file tree. Target: p50 < 100ms end-to-end (AC-PERF-5).
//
// The first StartSearch call warms the cache (cache miss, full walk); b.N
// iterations each issue the SAME query on the SAME directory so they all hit
// the cache and only the FZF scoring + result-copy runs.
func BenchmarkCoordinator_CacheHit(b *testing.B) {
	root := buildSyntheticTree(b, 50_000)

	cfg := FilePanelConfig()
	cfg.Timeout = 10 * time.Second
	c := NewCoordinator(cfg)

	src := &mockRecursiveSource{baseDir: root}

	// Warm the cache — first search does the full walk.
	warmCmd := c.StartSearch("abc", src, true)
	if warmCmd == nil {
		b.Fatalf("expected warm-up StartSearch to return a tea.Cmd")
	}
	warmMsg := warmCmd()
	warmComplete, ok := warmMsg.(AsyncSearchCompleteMsg)
	if !ok {
		b.Fatalf("warm-up: expected AsyncSearchCompleteMsg, got %T", warmMsg)
	}
	c.HandleAsyncResult(warmComplete)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		cmd := c.StartSearch("abc", src, true)
		if cmd == nil {
			b.Fatalf("expected Cmd from cache-hit StartSearch")
		}
		msg := cmd()
		m, ok := msg.(AsyncSearchCompleteMsg)
		if !ok {
			b.Fatalf("expected AsyncSearchCompleteMsg, got %T", msg)
		}
		// Feed the result back so the stack stays consistent for the next
		// iteration (avoids unbounded layer growth).
		b.StopTimer()
		c.HandleAsyncResult(m)
		b.StartTimer()
	}
}
