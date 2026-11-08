package zoxide

import (
	"log/slog"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	zoxidelib "github.com/lazysegtree/go-zoxide"
)

// defaultFilterWorkers is the worker-pool size for the parallel path-existence
// filter. Chosen empirically via BenchmarkFilterExistingDirs (see bench_test.go):
//
//   - For fast local stats (~1.8us/call), serial is actually optimal because
//     goroutine dispatch dominates work. Regressions scale sub-linearly with W:
//     W=2 costs ~4%, W=8 costs ~40% (total: ~350us extra — imperceptible).
//
//   - For slow stats (simulated 1ms/call, representative of NFS/FUSE/iCloud),
//     throughput scales near-linearly with W up to at least 32. W=8 gives ~8x
//     improvement (74ms vs 571ms serial) — well inside the "no perceived lag"
//     envelope for a 500-entry zoxide db.
//
// W=8 is the smallest value that (a) fully masks typical slow-FS latency spikes
// without (b) adding meaningful overhead on the all-local-disk case, and is
// comfortably below GOMAXPROCS on every machine this ships on.
const defaultFilterWorkers = 8

// filterExistingDirs returns a slice containing only the zoxide results whose
// Path still exists on disk AND is a directory. This prunes stale entries that
// the zoxide database remembers even after the underlying directory has been
// deleted (or replaced by a file).
//
// Order is preserved — zoxide returns results by frecency rank and callers
// rely on that ordering.
//
// Internally it uses a worker pool plus a package-level path-existence cache
// with TTL (see pathcache.go) to avoid redundant stat syscalls across
// back-to-back keystrokes.
func filterExistingDirs(results []zoxidelib.Result) []zoxidelib.Result {
	return filterExistingDirsWithWorkers(results, defaultFilterWorkers, globalPathCache)
}

// filterExistingDirsWithWorkers is the workhorse. Exposed to tests/benchmarks
// so the worker count and cache can be varied independently. If workers <= 1
// it degrades to a simple serial loop, which both avoids goroutine overhead
// for trivially small inputs and preserves an apples-to-apples baseline for
// benchmarking.
func filterExistingDirsWithWorkers(
	results []zoxidelib.Result,
	workers int,
	cache *pathCache,
) []zoxidelib.Result {
	if cache == nil {
		cache = globalPathCache
	}
	n := len(results)
	if n == 0 {
		return results[:0]
	}

	start := time.Now()

	// Boolean keep-flags, indexed by input position. Preserves order trivially:
	// we just scan results once at the end and keep those where keep[i] == true.
	keep := make([]bool, n)

	if workers <= 1 {
		// Serial fallback (and fast path for tiny inputs).
		for i := range results {
			keep[i] = cache.ExistsAsDir(results[i].Path)
		}
	} else {
		// Fan-out: pre-populate a buffered channel of indices so workers can
		// pull in whatever order they finish without blocking the producer.
		jobs := make(chan int, n)
		for i := range n {
			jobs <- i
		}
		close(jobs)

		// Fan-in via WaitGroup; each worker writes into its own keep[i] slot,
		// so no mutex needed (distinct cache lines would be nicer but for 500
		// entries the false sharing cost is negligible).
		var wg sync.WaitGroup
		w := workers
		if w > n {
			w = n
		}
		wg.Add(w)
		for range w {
			go func() {
				defer wg.Done()
				for i := range jobs {
					keep[i] = cache.ExistsAsDir(results[i].Path)
				}
			}()
		}
		wg.Wait()
	}

	// Collect in original order. We can't safely reuse results[:0] in the
	// parallel case because workers may have been reading later indices while
	// we overwrote earlier ones; allocate a fresh slice. For the common case
	// where most paths survive, this costs one extra allocation per keystroke,
	// which is trivial next to the goroutine + stat work.
	filtered := make([]zoxidelib.Result, 0, n)
	for i := range results {
		if keep[i] {
			filtered = append(filtered, results[i])
		}
	}

	dropped := n - len(filtered)
	if dropped > 0 {
		slog.Debug("Filtered stale zoxide entries",
			"operation", "zoxide_filter_stale",
			"dropped", dropped,
			"kept", len(filtered),
			"workers", workers,
			"took", time.Since(start))
	}
	return filtered
}

func (m *Model) Open() tea.Cmd {
	m.open = true
	m.justOpened = true
	m.textInput.SetValue("")
	_ = m.textInput.Focus()

	// Return async command for initial query instead of blocking
	return m.GetQueryCmd("")
}

func (m *Model) Close() {
	m.open = false
	m.textInput.Blur()
	m.textInput.SetValue("")
	m.results = []zoxidelib.Result{}
	m.cursor = 0
	m.picked = false
	m.renderIndex = 0
}

func (m *Model) IsOpen() bool {
	return m.open
}

func (m *Model) GetWidth() int {
	return m.width
}

func (m *Model) GetMaxHeight() int {
	return m.maxHeight
}

func (m *Model) SetWidth(width int) {
	if width < ZoxideMinWidth {
		slog.Warn("Zoxide initialized with too less width", "width", width)
		width = ZoxideMinWidth
	}
	m.width = width
	// Excluding borders(2), SpacePadding(1), Prompt(2), and one extra character that is appended
	// by textInput.View()
	m.textInput.Width = width - modalInputPadding
}

func (m *Model) SetMaxHeight(maxHeight int) {
	if maxHeight < ZoxideMinHeight {
		slog.Warn("Zoxide initialized with too less maxHeight", "maxHeight", maxHeight)
		maxHeight = ZoxideMinHeight
	}
	m.maxHeight = maxHeight
}

func (m *Model) GetResults() []zoxidelib.Result {
	out := make([]zoxidelib.Result, len(m.results))
	copy(out, m.results)
	return out
}

func (m *Model) GetTextInputValue() string {
	return m.textInput.Value()
}

func isKeyAlphaNum(msg tea.KeyMsg) bool {
	r := []rune(msg.String())
	if len(r) != 1 {
		return false
	}
	return unicode.IsLetter(r[0]) || unicode.IsNumber(r[0])
}
