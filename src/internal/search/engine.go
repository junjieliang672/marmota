package search

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

const (
	// MaxSearchDepth is the maximum depth for recursive searches
	MaxSearchDepth = 5

	// defaultCollectorWorkers is the default number of parallel ReadDir workers
	// used by RecursiveCollector.CollectStream when Workers is unset or non-positive.
	defaultCollectorWorkers = 8

	// defaultCollectorBatchSize is the default number of items emitted per batch
	// on the CollectStream output channel when BatchSize is unset or non-positive.
	defaultCollectorBatchSize = 256
)

// DefaultMaxResults is the default cap used by coordinator-driven recursive
// searches to bound memory. See spec-006 contracts.new_service_interfaces.FzfExecutor.
const DefaultMaxResults = 5000

// streamBatchBuffer is how many batches a streaming walk may run ahead of
// whoever reads them.
const streamBatchBuffer = 4

// FzfExecutor implements SearchExecutor using FZF
type FzfExecutor struct {
	timeout time.Duration
}

// NewFzfExecutor creates a new FZF-based search executor
func NewFzfExecutor(timeout time.Duration) *FzfExecutor {
	return &FzfExecutor{timeout: timeout}
}

// Execute performs FZF search on the provided items
func (e *FzfExecutor) Execute(ctx context.Context, query string, items []Searchable) ([]SearchResult, error) {
	return e.ExecuteWithCap(ctx, query, items, 0)
}

// ExecuteWithCap performs FZF search on the provided items and truncates the
// result to the top `maxResults` scored matches. A maxResults <= 0 is treated
// as "no cap" and behaves identically to Execute.
//
// Truncation happens AFTER scoring so the kept items are always the top-N by
// FZF score. Emits a single slog.Debug when truncation actually occurs.
func (e *FzfExecutor) ExecuteWithCap(
	ctx context.Context,
	query string,
	items []Searchable,
	maxResults int,
) ([]SearchResult, error) {
	start := time.Now()

	if len(items) == 0 {
		return nil, nil
	}

	// Extract filter prefix (f:, d:)
	filterType, cleanQuery := parseSearchTerm(query)

	// Build haystack from searchable names
	haystack := make([]string, 0, len(items))
	itemMap := make(map[int]Searchable)
	filteredIndex := 0

	for i, item := range items {
		// Apply file type filter
		if !matchesFileFilter(item, filterType) {
			continue
		}

		haystack = append(haystack, item.GetName())
		itemMap[filteredIndex] = items[i]
		filteredIndex++
	}

	if len(haystack) == 0 {
		return nil, nil
	}

	// Execute FZF search with timeout
	fzfResults, timedOut := utils.FzfSearchWithTimeout(ctx, cleanQuery, haystack, e.timeout)

	if timedOut {
		slog.Warn("FZF search timed out", "query", query, "timeout", e.timeout)
	}

	// Convert to SearchResult (fzf-lib already sorts by score desc).
	results := make([]SearchResult, 0, len(fzfResults))
	for i, match := range fzfResults {
		if item, ok := itemMap[int(match.HayIndex)]; ok {
			results = append(results, SearchResult{
				Item:  item,
				Score: match.Score,
				Index: i,
			})
		}
	}

	totalMatches := len(results)
	truncated := false
	if maxResults > 0 && totalMatches > maxResults {
		results = results[:maxResults]
		truncated = true
	}

	logArgs := []any{
		"query", query,
		"haystack_size", len(haystack),
		"match_count", len(results),
		"duration_ms", time.Since(start).Milliseconds(),
		"truncated", truncated,
	}
	if truncated {
		// Per spec observability.logging fzf_score_complete and the
		// truncation log requirement — emit total/kept exactly once when
		// truncation occurs.
		logArgs = append(logArgs, "total", totalMatches, "kept", len(results))
	}
	slog.Debug("fzf_score_complete", logArgs...)

	return results, nil
}

// matchesFileFilter checks if item matches the file type filter
func matchesFileFilter(item Searchable, filterType string) bool {
	switch filterType {
	case FilterTypeFiles:
		return !item.IsDirectory()
	case FilterTypeDirs:
		return item.IsDirectory()
	default:
		return true
	}
}

// RecursiveCollector walks directory trees to collect searchable items.
//
// In addition to the original serial Collect method, it exposes a parallel,
// cancellable CollectStream variant that emits results to a channel in
// batches (see spec-006).
type RecursiveCollector struct {
	MaxDepth   int
	ShowHidden bool

	// Workers controls the number of concurrent ReadDir workers used by
	// CollectStream. Values <= 0 fall back to defaultCollectorWorkers (8).
	Workers int

	// BatchSize controls how many items CollectStream groups together before
	// pushing onto the output channel. Values <= 0 fall back to
	// defaultCollectorBatchSize (256).
	BatchSize int
}

// NewRecursiveCollector creates a new recursive collector
func NewRecursiveCollector(maxDepth int, showHidden bool) *RecursiveCollector {
	if maxDepth <= 0 {
		maxDepth = MaxSearchDepth
	}
	return &RecursiveCollector{
		MaxDepth:   maxDepth,
		ShowHidden: showHidden,
	}
}

// Collect recursively collects all files/directories from rootPath
// Returns: items, depthLimited (true if depth limit was hit), error
func (r *RecursiveCollector) Collect(
	ctx context.Context,
	rootPath string,
	filterType string,
) ([]Searchable, bool, error) {
	var items []Searchable
	depthLimited := false
	baseDirDepth := strings.Count(rootPath, string(os.PathSeparator))

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil //nolint:nilerr // an entry it can't read is skipped, not the walk
		}

		// Skip the root directory itself
		if path == rootPath {
			return nil
		}

		// Check depth limit
		currentDepth := strings.Count(path, string(os.PathSeparator)) - baseDirDepth
		if currentDepth > r.MaxDepth {
			depthLimited = depthLimited || info.IsDir()
			return skipEntry(info)
		}

		// Skip dot files if needed
		if !r.ShowHidden && strings.HasPrefix(filepath.Base(path), ".") {
			return skipEntry(info)
		}

		// Check if it's a directory (including symlinks to dirs)
		isDir := info.IsDir() || isSymlinkToDir(rootPath, info, filepath.Base(path))

		// Apply file type filter: don't descend into filtered-out dirs
		if !shouldIncludeByFilter(isDir, filterType) {
			return skipEntry(info)
		}

		// Create searchable item
		item := &fileSearchable{
			name:      filepath.Base(path), // Filename only for FZF search
			location:  path,                // Full path for navigation
			directory: isDir,
			info:      info,
		}

		items = append(items, item)
		return nil
	})

	return items, depthLimited, err
}

// skipEntry is what a walk returns to leave an entry out: SkipDir for a
// folder, so that it isn't walked either.
func skipEntry(info os.FileInfo) error {
	if info.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// CollectStream walks the tree under rootPath using a bounded worker pool and
// emits collected items to `out` in batches of up to BatchSize. The output
// channel is always closed exactly once before this function returns, and the
// walk respects ctx cancellation within one directory's ReadDir granularity.
//
// Parity with Collect: the SET of GetLocation() values returned by CollectStream
// matches the serial Collect for the same (rootPath, filterType, ShowHidden,
// MaxDepth) tuple. Order is not guaranteed. See spec-006 AC-CORRECT-1.
//
// On ctx cancellation or deadline, any partial batches already produced remain
// valid; the function returns ctx.Err() (context.Canceled or
// context.DeadlineExceeded).
//
// Unreadable subdirectories are logged via slog.Debug and skipped; they do not
// abort the walk.
func (r *RecursiveCollector) CollectStream(
	ctx context.Context,
	rootPath string,
	filterType string,
	out chan<- []Searchable,
) (bool, error) {
	// Determine effective worker/batch counts.
	workers := r.Workers
	if workers <= 0 {
		workers = defaultCollectorWorkers
	}
	batchSize := r.BatchSize
	if batchSize <= 0 {
		batchSize = defaultCollectorBatchSize
	}

	// Stat the root first so non-existent paths surface as errors cleanly.
	rootInfo, statErr := os.Stat(rootPath)
	if statErr != nil {
		close(out)
		return false, statErr
	}
	if !rootInfo.IsDir() {
		close(out)
		return false, &os.PathError{Op: "readdir", Path: rootPath, Err: os.ErrInvalid}
	}

	w := &streamWalk{
		collector:  r,
		filterType: filterType,
		out:        out,
		batchSize:  batchSize,
		batch:      make([]Searchable, 0, batchSize),
		sem:        make(chan struct{}, workers),
	}

	// Kick off the root walk at depth 0.
	w.wg.Add(1)
	go w.walkDir(ctx, rootPath, 0)

	// Wait for all walkers to finish, then flush any trailing partial batch.
	w.wg.Wait()

	w.batchMu.Lock()
	w.flushLocked(ctx)
	w.batchMu.Unlock()

	close(out)

	return w.depthLimited.Load(), ctx.Err()
}

// streamWalk is one CollectStream walk: bounded workers read folders at
// once, and what they find goes to out in batches.
type streamWalk struct {
	collector  *RecursiveCollector
	filterType string
	out        chan<- []Searchable
	batchSize  int

	depthLimited atomic.Bool
	batchMu      sync.Mutex
	batch        []Searchable
	wg           sync.WaitGroup
	sem          chan struct{}
}

// flushLocked pushes the current batch to `out` and resets it. Caller must
// hold batchMu. Respects ctx: if the context is cancelled mid-send, the
// batch is dropped on the floor.
func (w *streamWalk) flushLocked(ctx context.Context) {
	if len(w.batch) == 0 {
		return
	}
	toSend := w.batch
	w.batch = make([]Searchable, 0, w.batchSize)
	// Unlock while sending to avoid holding the mutex across a channel
	// send that may block if the consumer is slow.
	w.batchMu.Unlock()
	select {
	case w.out <- toSend:
	case <-ctx.Done():
	}
	w.batchMu.Lock()
}

// emit adds one item to the pending batch, flushing on overflow.
func (w *streamWalk) emit(ctx context.Context, item Searchable) {
	w.batchMu.Lock()
	w.batch = append(w.batch, item)
	if len(w.batch) >= w.batchSize {
		w.flushLocked(ctx)
	}
	w.batchMu.Unlock()
}

// walkDir reads `path` (at the given depth) and processes its entries.
// It may spawn child goroutines for subdirectories; each child goroutine
// is tracked by wg.
func (w *streamWalk) walkDir(ctx context.Context, path string, depth int) {
	defer w.wg.Done()

	// Acquire a worker slot — bounded concurrency.
	select {
	case w.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-w.sem }()

	// Bail early on cancellation.
	if ctx.Err() != nil {
		return
	}

	entries, readErr := os.ReadDir(path)
	if readErr != nil {
		// Log and continue; unreadable dirs must not abort the walk.
		slog.Debug("recursive_walk_readdir_error",
			"path", path,
			"error", readErr,
		)
		return
	}

	for _, entry := range entries {
		// Check cancellation between entries so a cancelled walk stops
		// promptly inside a very large directory.
		if ctx.Err() != nil {
			return
		}
		w.visit(ctx, path, entry, depth+1)
	}
}

// visit emits one entry of the folder dir, at depth, when the walk takes
// it, and walks into it when it is a folder within MaxDepth.
func (w *streamWalk) visit(ctx context.Context, dir string, entry os.DirEntry, depth int) {
	entryName := entry.Name()
	fullPath := filepath.Join(dir, entryName)

	// Fetch FileInfo via entry.Info() — equivalent to Lstat for this
	// entry; matches filepath.Walk's behaviour for symlinks.
	info, infoErr := entry.Info()
	if infoErr != nil {
		// Transient or permission error on a single entry — mirror
		// the serial path which forwards err but also returns nil.
		return
	}

	// --- Order of checks MUST match serial Collect for parity ---
	isDir := info.IsDir() || isSymlinkToDir(dir, info, entryName)

	// 1) depth: dirs beyond MaxDepth are skipped entirely (and mark
	// the walk as depth-limited). Files beyond MaxDepth are still
	// emitted — they are leaves of a just-past-boundary directory
	// that we already chose to enter. This matches the test
	// expectation in TestCollectStream_MaxDepthLimited.
	if depth > w.collector.MaxDepth && isDir {
		w.depthLimited.Store(true)
		return
	}

	// 2) hidden
	if !w.collector.ShowHidden && strings.HasPrefix(entryName, ".") {
		return
	}

	// 3) filter: mirror serial, if the entry is a real directory, we skip
	// descending into it. Files just get skipped.
	if !shouldIncludeByFilter(isDir, w.filterType) {
		return
	}

	// Emit the item (basename for FZF search, full path for nav).
	w.emit(ctx, &fileSearchable{
		name:      entryName,
		location:  fullPath,
		directory: isDir,
		info:      info,
	})

	// Descend into REAL directories only that are within MaxDepth.
	// filepath.Walk does not follow symlinks by default; match that.
	if info.IsDir() && depth <= w.collector.MaxDepth {
		w.wg.Add(1)
		go w.walkDir(ctx, fullPath, depth)
	}
}

// shouldIncludeByFilter checks if an item should be included based on filter type
func shouldIncludeByFilter(isDir bool, filterType string) bool {
	switch filterType {
	case FilterTypeFiles:
		return !isDir
	case FilterTypeDirs:
		return isDir
	default:
		return true
	}
}

// isSymlinkToDir checks if a file is a symlink pointing to a directory
func isSymlinkToDir(basePath string, info os.FileInfo, name string) bool {
	if info.Mode()&os.ModeSymlink == 0 {
		return false
	}

	target, err := os.Readlink(filepath.Join(basePath, name))
	if err != nil {
		return false
	}

	// Make target path absolute
	if !filepath.IsAbs(target) {
		target = filepath.Join(basePath, target)
	}

	targetInfo, err := os.Stat(target)
	if err != nil {
		return false
	}

	return targetInfo.IsDir()
}

// fileSearchable is a basic implementation of Searchable for files/directories
type fileSearchable struct {
	name      string
	location  string
	directory bool
	info      os.FileInfo
}

func (f *fileSearchable) GetName() string {
	return f.name
}

func (f *fileSearchable) GetLocation() string {
	return f.location
}

func (f *fileSearchable) IsDirectory() bool {
	return f.directory
}

func (f *fileSearchable) GetMetadata() interface{} {
	return f.info
}
