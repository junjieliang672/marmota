//go:build manual_icloud

package internal

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/junjieliang672/marmota/src/internal/search"
)

// TestICloudParityAfterShowHiddenFix walks the iCloud directory via BOTH the
// live-typing recursive path (returnDirElementBySearchStringRecursive) AND the
// post-Enter Coordinator path, and asserts both return the SAME set of result
// locations for query "bytedance offer".
//
// Post spec-006 live-perf changes:
//   - The live-typing path now routes through Coordinator.CollectCached, so
//     it shares the same tree-index cache as the post-Enter path. First
//     keystroke should show a cold walk (~70ms on 13k iCloud files),
//     subsequent keystrokes should be <5ms (FZF only).
//   - Search results use FZF-score order by default (no post-sort via
//     sortElements unless the user confirms a sort via the `o` menu).
//     The parity check now expects IDENTICAL order between live and coord —
//     both are in FZF score order.
//
// Run with:
//
//	SPF_ICLOUD_DIR="$HOME/Library/Mobile Documents/com~apple~CloudDocs" \
//	SPF_ICLOUD_QUERY="bytedance offer" \
//	go test -tags=manual_icloud -run TestICloudParityAfterShowHiddenFix -v -count=1 -timeout 300s ./src/internal/
func TestICloudParityAfterShowHiddenFix(t *testing.T) {
	dir := os.Getenv("SPF_ICLOUD_DIR")
	if dir == "" {
		t.Skip("SPF_ICLOUD_DIR not set")
	}
	query := os.Getenv("SPF_ICLOUD_QUERY")
	if query == "" {
		query = "bytedance offer"
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("iCloud dir not accessible: %v", err)
	}

	t.Logf("iCloud dir: %s", dir)
	t.Logf("query:     %q", query)

	for _, toggleDotFile := range []bool{false, true} {
		t.Run(fmt.Sprintf("toggleDotFile=%v", toggleDotFile), func(t *testing.T) {
			// -------- LIVE-TYPING PATH --------
			// Use a dedicated coordinator so we can observe the cache-warm
			// benefit specifically on the live path (mirrors what a model
			// instance does via m.getSearchCoordinator()).
			liveCoord := search.NewCoordinator(search.FilePanelConfig())

			// Cold keystroke: first time typing the query, no cache.
			liveStart := time.Now()
			liveElems := returnDirElementBySearchStringRecursive(
				liveCoord,
				dir,
				toggleDotFile,
				query,
			)
			liveDur := time.Since(liveStart)
			liveLocs := make([]string, 0, len(liveElems))
			for _, e := range liveElems {
				liveLocs = append(liveLocs, e.location)
			}
			t.Logf("[LIVE cold] %d results in %v", len(liveLocs), liveDur)
			for i, loc := range liveLocs {
				if i < 10 {
					t.Logf("  live #%d: %s", i+1, loc)
				}
			}

			// Warm keystroke: simulate the user typing another letter —
			// coordinator should cache-hit. Measure 3 warm calls to get a
			// stable average.
			warmRuns := 3
			var warmLiveTotal time.Duration
			var liveWarmElems []element
			for i := 0; i < warmRuns; i++ {
				wStart := time.Now()
				liveWarmElems = returnDirElementBySearchStringRecursive(
					liveCoord,
					dir,
					toggleDotFile,
					query+"x", // different query but same (dir, showHidden, filterType) → cache hit
				)
				warmLiveTotal += time.Since(wStart)
			}
			warmLiveAvg := warmLiveTotal / time.Duration(warmRuns)
			_ = liveWarmElems
			t.Logf("[LIVE warm avg over %d runs] %v (per keystroke)", warmRuns, warmLiveAvg)

			// -------- POST-ENTER COORDINATOR PATH --------
			// Use a *separate* coordinator instance so its cold-walk
			// measurement isn't polluted by the live coordinator's cache.
			cfg := search.DefaultConfig()
			cfg.EnableRecursive = true
			cfg.Timeout = 30 * time.Second
			coord := search.NewCoordinator(cfg)
			src := panelSearchSource{baseDir: dir, showHidden: toggleDotFile}

			coldStart := time.Now()
			cmd := coord.StartSearch(query, src, true)
			if cmd == nil {
				t.Fatal("coordinator StartSearch returned nil cmd")
			}
			rawMsg := cmd()
			coldDur := time.Since(coldStart)
			complete, ok := rawMsg.(search.AsyncSearchCompleteMsg)
			if !ok {
				t.Fatalf("expected AsyncSearchCompleteMsg, got %T", rawMsg)
			}
			coordLocs := make([]string, 0, len(complete.Results))
			for _, r := range complete.Results {
				coordLocs = append(coordLocs, r.GetLocation())
			}
			t.Logf(
				"[COORDINATOR] %d results in %v (truncated=%v, timedOut=%v)",
				len(coordLocs),
				coldDur,
				complete.Truncated,
				complete.TimedOut,
			)
			for i, loc := range coordLocs {
				if i < 10 {
					t.Logf("  coord #%d: %s", i+1, loc)
				}
			}

			// -------- COORDINATOR CACHE HIT --------
			coord.HandleAsyncResult(complete)
			warmStart := time.Now()
			cmd2 := coord.StartSearch(query, src, true)
			rawMsg2 := cmd2()
			warmDur := time.Since(warmStart)
			complete2, ok := rawMsg2.(search.AsyncSearchCompleteMsg)
			if !ok {
				t.Fatalf("expected AsyncSearchCompleteMsg on warm run, got %T", rawMsg2)
			}
			warmLocs := make([]string, 0, len(complete2.Results))
			for _, r := range complete2.Results {
				warmLocs = append(warmLocs, r.GetLocation())
			}
			t.Logf("[COORDINATOR warm] %d results in %v", len(warmLocs), warmDur)

			// -------- PARITY CHECK --------
			// Both paths now return FZF-score order (live path no longer
			// post-sorts via sortElements). Order AND set MUST match.
			if len(liveLocs) != len(coordLocs) {
				t.Errorf("LIVE vs COORDINATOR result count differs: live=%d coord=%d", len(liveLocs), len(coordLocs))
			}
			mismatch := 0
			maxReport := 10
			for i := 0; i < len(liveLocs) && i < len(coordLocs); i++ {
				if liveLocs[i] != coordLocs[i] {
					if mismatch < maxReport {
						t.Errorf("order mismatch at index %d: live=%q coord=%q", i, liveLocs[i], coordLocs[i])
					}
					mismatch++
				}
			}
			if mismatch > maxReport {
				t.Errorf("... %d additional mismatches suppressed", mismatch-maxReport)
			}

			// Summary
			fmt.Printf("\n=== toggleDotFile=%v ===\n", toggleDotFile)
			fmt.Printf("Live cold (1st keystroke):   %d results, %v\n", len(liveLocs), liveDur)
			fmt.Printf("Live warm (per keystroke):   %v  avg of %d\n", warmLiveAvg, warmRuns)
			fmt.Printf("Coordinator cold:            %d results, %v\n", len(coordLocs), coldDur)
			fmt.Printf("Coordinator warm:            %d results, %v\n", len(warmLocs), warmDur)
			if len(liveLocs) == len(coordLocs) && mismatch == 0 {
				fmt.Println("PARITY (fzf-order): OK (live == coordinator)")
			} else {
				fmt.Printf(
					"PARITY: FAIL (count live=%d coord=%d, mismatches=%d)\n",
					len(liveLocs),
					len(coordLocs),
					mismatch,
				)
			}
			fmt.Println()
		})
	}
}
