//go:build zoxidelive

package zoxide

// This file is gated behind the "zoxidelive" build tag so the default test run
// stays hermetic. Invoke with:
//
//   go test -tags=zoxidelive -run=TestE2E ./src/internal/ui/zoxide/... -v -count=1
//
// It queries the user's real zoxide database via go-zoxide.QueryAll("") and
// compares serial-uncached vs parallel-cached filter latency to verify the
// optimization in-situ.

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	zoxidelib "github.com/lazysegtree/go-zoxide"
)

func TestE2ERealZoxideDB(t *testing.T) {
	client, err := zoxidelib.New()
	if err != nil {
		t.Fatalf("zoxide client init: %v", err)
	}

	// QueryAll is what GetQueryCmd calls on an empty string.
	qStart := time.Now()
	results, err := client.QueryAll()
	qElapsed := time.Since(qStart)
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}

	n := len(results)
	t.Logf("QueryAll returned %d entries in %v", n, qElapsed)

	if n == 0 {
		t.Skip("empty zoxide db; nothing to measure")
	}

	// Report "interesting" paths (iCloud, network mounts) that can skew latency.
	for _, r := range results {
		if strings.Contains(r.Path, "com~apple~CloudDocs") ||
			strings.HasPrefix(r.Path, "/Volumes/") ||
			strings.Contains(r.Path, "/.Trash") {
			t.Logf("  potentially-slow path: %s", r.Path)
		}
	}

	// Clone to make each run independent.
	clone := func() []zoxidelib.Result {
		out := make([]zoxidelib.Result, n)
		copy(out, results)
		return out
	}

	// BEFORE: serial, no cache (W=1, fresh cache).
	c1 := newPathCache(time.Hour, defaultStatFn)
	b4Start := time.Now()
	before := filterExistingDirsWithWorkers(clone(), 1, c1)
	beforeElapsed := time.Since(b4Start)

	// AFTER (cold): parallel with default W, fresh cache.
	c2 := newPathCache(time.Hour, defaultStatFn)
	afColdStart := time.Now()
	afterCold := filterExistingDirsWithWorkers(clone(), defaultFilterWorkers, c2)
	afterColdElapsed := time.Since(afColdStart)

	// AFTER (warm): same cache, second invocation (simulates a subsequent keystroke).
	afWarmStart := time.Now()
	afterWarm := filterExistingDirsWithWorkers(clone(), defaultFilterWorkers, c2)
	afterWarmElapsed := time.Since(afWarmStart)

	stale := n - len(before)

	// Measure per-path outliers: find the slowest individual stat to characterize
	// where the perceived lag really comes from.
	slowest := time.Duration(0)
	var slowestPath string
	for _, r := range results {
		s := time.Now()
		_, _ = os.Stat(r.Path)
		d := time.Since(s)
		if d > slowest {
			slowest = d
			slowestPath = r.Path
		}
	}

	fmt.Printf(`
=== zoxide filter E2E ===
entries:                   %d
QueryAll time:             %v
BEFORE (serial, cold):     %v  (%d stale dropped)
AFTER  (W=%d, cold):       %v  (%d stale dropped)
AFTER  (W=%d, warm cache): %v  (%d stale dropped)
speedup (cold):            %.2fx
speedup (warm vs serial):  %.2fx
throughput (cold, after):  %.0f paths/ms
slowest single stat:       %v  (%s)
===========================
`,
		n, qElapsed,
		beforeElapsed, stale,
		defaultFilterWorkers, afterColdElapsed, n-len(afterCold),
		defaultFilterWorkers, afterWarmElapsed, n-len(afterWarm),
		float64(beforeElapsed)/float64(afterColdElapsed),
		float64(beforeElapsed)/float64(afterWarmElapsed),
		float64(n)/float64(afterColdElapsed.Milliseconds()+1),
		slowest, slowestPath,
	)
}
