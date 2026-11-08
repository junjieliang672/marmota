package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultCoordinatorDebounce is the minimum gap between a StartSearch call and
// the launch of a new recursive walk. See spec-006 contracts.new_fields for
// Coordinator.debounceDelay.
const defaultCoordinatorDebounce = 80 * time.Millisecond

// streamingCollector is the narrow interface the Coordinator uses to drive
// recursive walks via a channel. It is satisfied by *RecursiveCollector and by
// test doubles in coordinator_cache_test.go / coordinator_cancel_test.go.
type streamingCollector interface {
	CollectStream(ctx context.Context, rootPath string, filterType string, out chan<- []Searchable) (bool, error)
}

// collectorFactory builds a streamingCollector for a given (maxDepth, showHidden)
// tuple. Tests inject their own factory via SetCollectorForTest; production
// uses the default factory which constructs a *RecursiveCollector.
type collectorFactory func(maxDepth int, showHidden bool) streamingCollector

// defaultCollectorFactory is the production collectorFactory — returns a
// freshly constructed *RecursiveCollector.
func defaultCollectorFactory(maxDepth int, showHidden bool) streamingCollector {
	return NewRecursiveCollector(maxDepth, showHidden)
}

// Coordinator manages search lifecycle for any panel
type Coordinator struct {
	config   SearchConfig
	stack    *Stack
	executor SearchExecutor

	// State
	isTyping     bool
	currentQuery string
	isSearching  bool

	// Spec-006 additions: index cache + single-flight cancellation.
	indexCache       *treeIndexCache
	activeMu         sync.Mutex
	activeCancel     context.CancelFunc
	activeID         uint64 // monotonic id of the current active search; guarded by activeMu
	debounceDelay    time.Duration
	collectorFactory collectorFactory

	// partialMsgCh carries at-most-one queued AsyncSearchPartialMsg.
	// The walk goroutine pushes non-blockingly (drops on full — a stale
	// partial is harmless because a newer search will overwrite it or the
	// Complete msg will replace the layer shortly). The model pumps
	// messages off this channel via ListenForPartial (see spec-006 T5).
	partialMsgCh chan AsyncSearchPartialMsg
}

// NewCoordinator creates a new search coordinator
func NewCoordinator(config SearchConfig) *Coordinator {
	return &Coordinator{
		config:           config,
		stack:            NewStack(""),
		executor:         NewFzfExecutor(config.Timeout),
		indexCache:       newTreeIndexCache(),
		debounceDelay:    defaultCoordinatorDebounce,
		collectorFactory: defaultCollectorFactory,
		partialMsgCh:     make(chan AsyncSearchPartialMsg, 1),
	}
}

// StartSearch initiates a new search layer
// Returns tea.Cmd for async searches, nil for sync searches
func (c *Coordinator) StartSearch(query string, source SearchSource, recursive bool) tea.Cmd {
	if query == "" {
		return nil
	}

	c.currentQuery = query

	// Determine search mode
	mode := c.determineMode(query, recursive)

	if mode == ModeRecursive {
		// Launch async recursive search
		return c.startAsyncRecursiveSearch(query, source)
	}

	// Sync search (regular or filter)
	results, err := c.executeSearch(query, source, mode)

	if err != nil {
		slog.Error("Search failed", "query", query, "mode", mode, "error", err)
		return nil
	}

	// Push layer
	layer := Layer{
		Mode:    mode,
		Query:   query,
		Results: results,
		Error:   err,
	}

	c.stack.Push(layer)
	return nil
}

// determineMode determines the search mode based on query and flags
func (c *Coordinator) determineMode(query string, recursive bool) SearchMode {
	// Check for file type prefix
	filterType, _ := parseSearchTerm(query)
	hasFileTypeFilter := filterType != ""

	if recursive || hasFileTypeFilter {
		return ModeRecursive
	}

	if !c.stack.IsEmpty() && c.config.EnableFiltering {
		return ModeFilter
	}

	return ModeRegular
}

// executeSearch performs sync search based on mode
func (c *Coordinator) executeSearch(query string, source SearchSource, mode SearchMode) ([]Searchable, error) {
	ctx := context.Background()

	var items []Searchable

	switch mode {
	case ModeRegular:
		// Search current source items
		items = source.GetItems()

	case ModeFilter:
		// Filter parent layer results
		parent := c.stack.Peek()
		if parent != nil {
			items = parent.Results
		} else {
			items = source.GetItems()
		}

	case ModeRecursive:
		// This should not happen in sync path
		slog.Warn("Recursive search attempted in sync path")
		items = source.GetItems()
	}

	// Execute FZF search
	results, err := c.executor.Execute(ctx, query, items)
	if err != nil {
		return nil, err
	}

	// Extract searchable items
	searchableResults := make([]Searchable, len(results))
	for i, result := range results {
		searchableResults[i] = result.Item
	}

	return searchableResults, nil
}

// startAsyncRecursiveSearch launches async recursive search. Per spec-006:
//   - any prior in-flight walk is cancelled first (<50ms target);
//   - a cache hit on (dir, showHidden, filterType) skips the walk entirely;
//   - on cache miss, the walk writes its result to the cache on completion.
func (c *Coordinator) startAsyncRecursiveSearch(query string, source SearchSource) tea.Cmd {
	c.isSearching = true

	// Create placeholder layer immediately
	layer := Layer{
		Mode:      ModeRecursive,
		Query:     query,
		Searching: true,
		Results:   []Searchable{},
	}
	c.stack.Push(layer)

	ctx, cancel := context.WithCancel(context.Background())
	myID := c.swapActiveWalk(cancel)

	filterType, cleanQuery := parseSearchTerm(query)
	showHidden := c.resolveShowHidden(source)
	baseDir := source.GetBaseDirectory()
	req := recursiveSearch{
		query:      query,
		cleanQuery: cleanQuery,
		filterType: filterType,
		baseDir:    baseDir,
		showHidden: showHidden,
		cacheKey: treeIndexCacheKey{
			dir:        baseDir,
			showHidden: showHidden,
			filterType: filterType,
		},
	}

	cached, hit := c.indexCache.Get(req.cacheKey)

	return func() tea.Msg {
		// Recover from any panic in the walk goroutine. Walk runs in a
		// tea.Cmd goroutine; an unrecovered panic would corrupt the
		// Bubble Tea runtime (spec-006 observability.error_boundaries).
		// Registered FIRST so it runs LAST (Go defers are LIFO) — after
		// clearActiveCancel has released the cancel slot.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("recursive_walk_panic",
					"error", r,
					"stack", string(debug.Stack()),
					"query", query,
					"dir", baseDir,
				)
			}
		}()
		defer c.clearActiveCancel(myID, cancel)

		slog.Debug("recursive_walk_start",
			"query", query,
			"dir", baseDir,
			"filter", filterType,
			"workers", c.resolveWorkers(),
			"cache_hit", hit,
		)

		if hit {
			return c.searchCached(ctx, req, cached)
		}
		return c.walkAndSearch(ctx, req)
	}
}

// recursiveSearch is what one recursive search looks for, and where.
type recursiveSearch struct {
	query, cleanQuery, filterType, baseDir string
	showHidden                             bool
	cacheKey                               treeIndexCacheKey
}

// swapActiveWalk makes the walk that cancel stops the in-flight one, and
// returns its generation id:
//  1. cancel any prior walk,
//  2. install this walk's cancel + bump the generation id.
//
// Doing both under the same critical section prevents a race where a
// third StartSearch arrives between (1) and (2) and loses the cancel.
func (c *Coordinator) swapActiveWalk(cancel context.CancelFunc) uint64 {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeCancel != nil {
		c.activeCancel()
		slog.Debug("recursive_walk_cancelled",
			"reason", "new_search",
		)
	}
	c.activeCancel = cancel
	c.activeID++
	return c.activeID
}

// searchCached scores a cache hit's items: the walk is skipped entirely,
// straight to FZF.
func (c *Coordinator) searchCached(ctx context.Context, req recursiveSearch, cached []Searchable) tea.Msg {
	slog.Debug("index_cache_hit",
		"dir", req.baseDir,
		"filter", req.filterType,
		"age_ms", 0,
		"file_count", len(cached),
	)
	// Apply the same cap as the walk path to keep UI result size
	// consistent.
	results, truncated, searchErr := c.scoreCapAndConvert(ctx, req.cleanQuery, cached, DefaultMaxResults)
	return AsyncSearchCompleteMsg{
		Query:     req.query,
		Results:   results,
		Truncated: truncated,
		Error:     searchErr,
	}
}

// walkAndSearch handles a cache miss: it launches the streaming walk and
// emits AT MOST ONE AsyncSearchPartialMsg after the first batch is scored
// (per decision 006-004 "streaming-first-batch-only"), then the final
// AsyncSearchCompleteMsg when the walk finishes. A cancelled walk emits
// nothing.
func (c *Coordinator) walkAndSearch(ctx context.Context, req recursiveSearch) tea.Msg {
	collector := c.collectorFactory(c.config.MaxDepth, req.showHidden)
	walkStart := time.Now()
	items, depthLimited, walkErr := c.runStreamingWalk(ctx, collector, req.baseDir, req.filterType,
		req.cleanQuery, req.query)

	// Cancellation surfaces as ctx.Err on the drain side — AC-CORRECT-3
	// says we must emit NEITHER partial nor complete for the cancelled
	// query. We already suppressed the partial inside runStreamingWalk;
	// here we swallow the final by returning nil.
	if ctx.Err() != nil {
		slog.Debug("recursive_walk_cancelled",
			"query", req.query,
			"dir", req.baseDir,
			"files_walked_so_far", len(items),
			"duration_ms", time.Since(walkStart).Milliseconds(),
		)
		return nil
	}

	// Non-cancelled completion — update cache with walked items.
	if walkErr == nil {
		c.indexCache.Put(req.cacheKey, items)
	}

	timedOut := errors.Is(walkErr, context.DeadlineExceeded)

	if walkErr != nil && !errors.Is(walkErr, context.DeadlineExceeded) && !errors.Is(walkErr, context.Canceled) {
		slog.Error("Recursive collection failed", "error", walkErr, "path", req.baseDir)
		return AsyncSearchCompleteMsg{
			Query: req.query,
			Error: walkErr,
		}
	}

	// Final FZF pass on the full accumulated set, with the DefaultMaxResults
	// cap applied for memory safety (AC-MEM-1).
	results, truncated, searchErr := c.scoreCapAndConvert(
		context.Background(),
		req.cleanQuery,
		items,
		DefaultMaxResults,
	)

	slog.Debug("recursive_walk_complete",
		"query", req.query,
		"dir", req.baseDir,
		"file_count", len(items),
		"duration_ms", time.Since(walkStart).Milliseconds(),
		"depth_limited", depthLimited,
		"cancelled", false,
		"truncated", truncated,
	)

	return AsyncSearchCompleteMsg{
		Query:        req.query,
		Results:      results,
		DepthLimited: depthLimited,
		TimedOut:     timedOut,
		Truncated:    truncated,
		Error:        searchErr,
	}
}

// runStreamingWalk drives collector.CollectStream, accumulating every batch
// into a single slice. The FIRST batch triggers a one-shot FZF score pass
// whose result is pushed as an AsyncSearchPartialMsg via c.partialMsgCh
// (non-blocking; dropped on full). All subsequent batches are only
// accumulated — per decision 006-004 we do not spam the UI with per-batch
// updates. On ctx cancellation the partial is suppressed (AC-CORRECT-3).
func (c *Coordinator) runStreamingWalk(
	ctx context.Context,
	collector streamingCollector,
	baseDir, filterType, cleanQuery, originalQuery string,
) ([]Searchable, bool, error) {
	out := make(chan []Searchable, streamBatchBuffer)

	var (
		depthLimited bool
		walkErr      error
	)

	done := make(chan struct{})
	go func() {
		// Recover from panics in the collector goroutine. An unrecovered
		// panic here would crash the whole process and therefore the
		// Bubble Tea runtime (spec-006 observability.error_boundaries).
		// Registered FIRST so it runs LAST — this guarantees we also
		// close(out) on panic, which is required to unblock the drain
		// loop below (CollectStream is normally responsible for closing
		// out, but a panic can skip that).
		defer func() {
			if r := recover(); r != nil {
				walkErr = fmt.Errorf("panic in recursive walk: %v", r)
				slog.Error("recursive_walk_panic",
					"error", r,
					"stack", string(debug.Stack()),
					"query", originalQuery,
					"dir", baseDir,
				)
				// Best-effort close to unblock the drain loop. If the
				// collector already closed out, this would panic — the
				// recover at the top of THIS deferred func will not catch
				// it because we're already inside it; so guard with a
				// nested recover.
				func() {
					defer func() { _ = recover() }()
					close(out)
				}()
			}
		}()
		defer close(done)
		depthLimited, walkErr = collector.CollectStream(ctx, baseDir, filterType, out)
	}()

	var (
		items          []Searchable
		partialEmitted bool
	)

	for batch := range out {
		items = append(items, batch...)

		if partialEmitted {
			continue
		}
		// Emit first-batch partial exactly once. We deliberately do NOT
		// return early from the loop — we keep draining while the score
		// happens on a snapshot of what's been collected so far. Scoring
		// the snapshot inline (same goroutine) is fine because it is a
		// pure CPU operation on a few-hundred-item slice; we are not
		// blocking the walker goroutine, which is filling `out` in its
		// own goroutine.
		snapshot := append([]Searchable(nil), items...)
		partialResults, partialTruncated, partialErr := c.scoreCapAndConvert(
			ctx,
			cleanQuery,
			snapshot,
			DefaultMaxResults,
		)
		if partialErr == nil && ctx.Err() == nil {
			c.pushPartial(AsyncSearchPartialMsg{
				Query:     originalQuery,
				Results:   partialResults,
				Truncated: partialTruncated,
			})
		}
		partialEmitted = true
	}
	<-done

	return items, depthLimited, walkErr
}

// pushPartial enqueues a partial msg onto the coordinator's single-slot
// channel, dropping on full. Safe to call concurrently.
func (c *Coordinator) pushPartial(msg AsyncSearchPartialMsg) {
	if c.partialMsgCh == nil {
		return
	}
	select {
	case c.partialMsgCh <- msg:
	default:
		// Channel is full — a prior partial is still queued and will be
		// drained by the model's ListenForPartial. Since results are
		// cumulative and ordered, dropping the older-but-still-useful
		// partial is preferable, so swap it out.
		select {
		case <-c.partialMsgCh:
		default:
		}
		select {
		case c.partialMsgCh <- msg:
		default:
		}
	}
}

// ListenForPartial returns a tea.Cmd that blocks until a partial search
// message is available and then delivers it to the Bubble Tea runtime.
// The model (spec-006 T5) must re-register this cmd after handling each
// partial to keep the listener active across searches.
func (c *Coordinator) ListenForPartial() tea.Cmd {
	return func() tea.Msg {
		if c.partialMsgCh == nil {
			return nil
		}
		msg, ok := <-c.partialMsgCh
		if !ok {
			return nil
		}
		return msg
	}
}

// scoreCapAndConvert runs the FZF executor with the given result cap and
// extracts the Searchable items from the returned SearchResult slice. The
// second return value is true when scoring found more matches than the cap
// and the result was truncated to the top `maxResults` scores.
func (c *Coordinator) scoreCapAndConvert(
	ctx context.Context,
	cleanQuery string,
	items []Searchable,
	maxResults int,
) ([]Searchable, bool, error) {
	results, err := c.executor.ExecuteWithCap(ctx, cleanQuery, items, maxResults)
	if err != nil {
		slog.Error("FZF search failed", "error", err)
		return nil, false, err
	}
	truncated := maxResults > 0 && len(results) == maxResults
	searchable := make([]Searchable, len(results))
	for i, r := range results {
		searchable[i] = r.Item
	}
	return searchable, truncated, nil
}

// clearActiveCancel is called in the walk goroutine's defer to release the
// per-search context and, IF this walk is still the active one (myID matches
// the current generation), clear the stored CancelFunc so later calls to
// CancelActiveSearch see an empty slot. If a newer StartSearch has already
// replaced us, the slot belongs to that newer search — leave it alone.
func (c *Coordinator) clearActiveCancel(myID uint64, mine context.CancelFunc) {
	// Always release our own context to free resources.
	mine()

	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeID == myID {
		c.activeCancel = nil
	}
}

// CancelActiveSearch cancels the in-flight recursive walk context if any. Safe
// to call when no search is active. See spec-006 AC-PERF-6 (50ms target) and
// AC-CORRECT-3 (cancelled walk must not emit AsyncSearchCompleteMsg).
func (c *Coordinator) CancelActiveSearch() {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeCancel != nil {
		c.activeCancel()
		c.activeCancel = nil
		// Bump the generation so the cancelled walk's deferred
		// clearActiveCancel becomes a no-op on the slot (it would otherwise
		// race with a brand-new StartSearch installed right after this).
		c.activeID++
		slog.Debug("recursive_walk_cancelled",
			"reason", "explicit",
		)
	}
}

// ClearIndexCache drops every entry in the tree-index cache. Called on known
// filesystem mutations.
func (c *Coordinator) ClearIndexCache() {
	c.indexCache.Clear()
	slog.Debug("index_cache_evict",
		"reason", "clear",
	)
}

// CollectCached performs a synchronous recursive walk and returns the result
// set, reusing the tree-index cache when a fresh entry exists for
// (baseDir, showHidden, filterType). This is the sync counterpart to
// startAsyncRecursiveSearch's cache-hit fast path — callers like the
// live-typing file-panel search need sync semantics (one tea.Update tick
// must return the items).
//
// The cache key is shared with startAsyncRecursiveSearch, so the live path
// and post-Enter path mutually populate the same cache:
//   - first keystroke: cache miss, walk runs (~70ms on 13k iCloud files),
//     result populates cache on success;
//   - subsequent keystrokes: cache hit, walk is skipped (<5ms total for
//     the live path);
//   - pressing Enter on the same (dir, showHidden, filterType): cache hit
//     so the post-Enter path is near-instant.
//
// On ctx cancellation the partial walk result is discarded (no cache write)
// and ctx.Err() is returned. The caller may choose to surface the items
// regardless (the recursiveWalk honours ctx between directories).
func (c *Coordinator) CollectCached(
	ctx context.Context,
	source SearchSource,
	filterType string,
) ([]Searchable, bool, error) {
	baseDir := source.GetBaseDirectory()
	showHidden := c.resolveShowHidden(source)
	cacheKey := treeIndexCacheKey{
		dir:        baseDir,
		showHidden: showHidden,
		filterType: filterType,
	}

	if cached, hit := c.indexCache.Get(cacheKey); hit {
		slog.Debug("index_cache_hit",
			"dir", baseDir,
			"filter", filterType,
			"age_ms", 0,
			"file_count", len(cached),
			"path", "sync",
		)
		// We don't know the depth-limited bit from a cached entry (the cache
		// currently only stores items). Returning false here is safe because
		// depth-limited is informational — the walk that populated the cache
		// would have logged it, and the user-visible banner is emitted only
		// on the async post-Enter path.
		return cached, false, nil
	}

	slog.Debug("recursive_walk_start",
		"dir", baseDir,
		"filter", filterType,
		"workers", c.resolveWorkers(),
		"cache_hit", false,
		"path", "sync",
	)
	walkStart := time.Now()

	collector := c.collectorFactory(c.config.MaxDepth, showHidden)
	out := make(chan []Searchable, streamBatchBuffer)

	type walkResult struct {
		depthLimited bool
		err          error
	}
	resultCh := make(chan walkResult, 1)

	go func() {
		// Recover from panics in the collector goroutine (mirror of the
		// async path's error boundary — spec-006 observability.error_boundaries).
		defer func() {
			if r := recover(); r != nil {
				slog.Error("recursive_walk_panic",
					"error", r,
					"stack", string(debug.Stack()),
					"dir", baseDir,
					"path", "sync",
				)
				// Best-effort: surface as an error. The close(out) inside
				// CollectStream may or may not have run; drain loop below
				// handles either case because resultCh is buffered.
				resultCh <- walkResult{depthLimited: false, err: fmt.Errorf("panic in recursive walk: %v", r)}
			}
		}()
		dl, err := collector.CollectStream(ctx, baseDir, filterType, out)
		resultCh <- walkResult{depthLimited: dl, err: err}
	}()

	var items []Searchable
	for batch := range out {
		items = append(items, batch...)
	}
	res := <-resultCh

	if ctx.Err() != nil {
		// Cancelled — don't cache.
		return items, res.depthLimited, ctx.Err()
	}

	if res.err == nil {
		c.indexCache.Put(cacheKey, items)
	}

	slog.Debug("recursive_walk_complete",
		"dir", baseDir,
		"file_count", len(items),
		"duration_ms", time.Since(walkStart).Milliseconds(),
		"depth_limited", res.depthLimited,
		"path", "sync",
	)
	return items, res.depthLimited, res.err
}

// SetCollectorForTest replaces the internal collector factory with one that
// returns the supplied streamingCollector (typically a test mock). Intended
// for unit tests only.
func (c *Coordinator) SetCollectorForTest(factory func(maxDepth int, showHidden bool) streamingCollector) {
	if factory == nil {
		c.collectorFactory = defaultCollectorFactory
		return
	}
	c.collectorFactory = factory
}

// SetNowForTest injects a fake clock into the index cache. Intended for unit
// tests that assert TTL behaviour without real sleeps.
func (c *Coordinator) SetNowForTest(now func() time.Time) {
	c.indexCache.SetNow(now)
}

// resolveShowHidden returns the showHidden flag the collector should use. If
// the SearchSource implements HiddenAwareSource (e.g. the file-panel source),
// we honour its ShowHidden() value so the recursive walk matches what the
// live-typing (non-async) path produces. Sources that do NOT implement the
// interface (e.g. the help-menu search) keep the legacy default of true,
// preserving pre-fix behaviour.
func (c *Coordinator) resolveShowHidden(source SearchSource) bool {
	if h, ok := source.(HiddenAwareSource); ok {
		return h.ShowHidden()
	}
	return true
}

// resolveWorkers returns the worker count for debug logging. The production
// collector uses its own Workers field (default 8); test doubles ignore this.
func (c *Coordinator) resolveWorkers() int {
	return defaultCollectorWorkers
}

// HandleAsyncResult processes async search results
func (c *Coordinator) HandleAsyncResult(msg AsyncSearchCompleteMsg) {
	c.isSearching = false

	// Update the searching layer with results
	top := c.stack.Peek()
	if top != nil && top.Query == msg.Query {
		top.Results = msg.Results
		top.Searching = false
		top.DepthLimited = msg.DepthLimited
		top.TimedOut = msg.TimedOut
		top.Error = msg.Error
	}
}

// HandleESC handles two-stage ESC behavior
// Returns true if should close panel, false otherwise
func (c *Coordinator) HandleESC() bool {
	if !c.config.TwoStageESC {
		// Single stage - just clear everything
		c.stack.Clear()
		c.isTyping = false
		return true
	}

	// Two-stage ESC
	if c.isTyping {
		// Stage 1: Blur search bar
		c.isTyping = false
		return false // Don't close panel
	}

	// Stage 2: Pop one layer
	if !c.stack.IsEmpty() {
		c.stack.Pop()
		return false // Don't close panel yet
	}

	// No layers left, OK to close panel
	return true
}

// GetCurrentResults returns active search results
func (c *Coordinator) GetCurrentResults() []Searchable {
	if c.stack.IsEmpty() {
		return nil
	}
	top := c.stack.Peek()
	if top == nil {
		return nil
	}
	return top.Results
}

// IsSearching returns true if async search is in progress
func (c *Coordinator) IsSearching() bool {
	return c.isSearching
}

// SetTyping updates the typing state
func (c *Coordinator) SetTyping(typing bool) {
	c.isTyping = typing
}

// IsTyping returns true if user is currently typing
func (c *Coordinator) IsTyping() bool {
	return c.isTyping
}

// GetStack returns the search stack
func (c *Coordinator) GetStack() *Stack {
	return c.stack
}

// Clear clears all search state
func (c *Coordinator) Clear() {
	c.stack.Clear()
	c.isTyping = false
	c.isSearching = false
	c.currentQuery = ""
}

// GetCurrentQuery returns the current search query
func (c *Coordinator) GetCurrentQuery() string {
	return c.currentQuery
}

// HasActiveSearch returns true if there are any search layers
func (c *Coordinator) HasActiveSearch() bool {
	return !c.stack.IsEmpty()
}
