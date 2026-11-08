package search

// Tests for spec-006 T4 — AsyncSearchPartialMsg streaming wiring.
//
// These tests exercise the streaming path added in startAsyncRecursiveSearch:
// the walk pushes a first-batch AsyncSearchPartialMsg via the coordinator's
// internal partial-msg channel before the final AsyncSearchCompleteMsg is
// produced by the tea.Cmd returned from StartSearch.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoStageCollector emits the first batch after `firstBatchDelay`, then
// finishes after another `completionDelay`. It is cancellable via ctx.
type twoStageCollector struct {
	firstBatch      []Searchable
	remainingBatch  []Searchable
	firstBatchDelay time.Duration
	completionDelay time.Duration
	callCount       int32
}

func (t *twoStageCollector) CollectStream(
	ctx context.Context,
	root, filter string,
	out chan<- []Searchable,
) (bool, error) {
	atomic.AddInt32(&t.callCount, 1)
	defer close(out)

	// Wait before emitting the first batch.
	select {
	case <-time.After(t.firstBatchDelay):
	case <-ctx.Done():
		return false, ctx.Err()
	}

	select {
	case out <- t.firstBatch:
	case <-ctx.Done():
		return false, ctx.Err()
	}

	// Wait before emitting the final batch.
	select {
	case <-time.After(t.completionDelay):
	case <-ctx.Done():
		return false, ctx.Err()
	}

	if len(t.remainingBatch) > 0 {
		select {
		case out <- t.remainingBatch:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return false, nil
}

// TestCoordinator_EmitsPartialBeforeComplete verifies that a recursive search
// emits exactly one AsyncSearchPartialMsg after the first batch and that this
// partial arrives BEFORE the AsyncSearchCompleteMsg. This is AC-PERF-3's
// observable check.
func TestCoordinator_EmitsPartialBeforeComplete(t *testing.T) {
	collector := &twoStageCollector{
		firstBatch: []Searchable{
			newFile("alpha.go"),
			newFile("alphabet.go"),
			newFile("beta.go"),
		},
		remainingBatch: []Searchable{
			newFile("gamma.go"),
			newFile("delta.go"),
		},
		firstBatchDelay: 100 * time.Millisecond,
		completionDelay: 400 * time.Millisecond,
	}

	cfg := FilePanelConfig()
	cfg.Timeout = 5 * time.Second
	c := NewCoordinator(cfg)
	c.SetCollectorForTest(func(maxDepth int, showHidden bool) streamingCollector {
		return collector
	})

	src := &mockRecursiveSource{baseDir: "/streaming/dir"}

	// Listener goroutine that records when (and whether) the partial msg
	// arrives via the coordinator's listener cmd.
	type partialStamp struct {
		msg AsyncSearchPartialMsg
		at  time.Time
	}
	partialCh := make(chan partialStamp, 1)
	listen := c.ListenForPartial()
	require.NotNil(t, listen, "ListenForPartial must return a non-nil cmd")
	go func() {
		msg := listen()
		if pm, ok := msg.(AsyncSearchPartialMsg); ok {
			partialCh <- partialStamp{msg: pm, at: time.Now()}
		}
	}()

	// Kick off the search.
	startAt := time.Now()
	cmd := c.StartSearch("alpha", src, true)
	require.NotNil(t, cmd, "recursive StartSearch must return a tea.Cmd")

	// Drive the final cmd in the background so we can observe both messages.
	type finalStamp struct {
		msg AsyncSearchCompleteMsg
		at  time.Time
	}
	finalCh := make(chan finalStamp, 1)
	go func() {
		raw := cmd()
		if cm, ok := raw.(AsyncSearchCompleteMsg); ok {
			finalCh <- finalStamp{msg: cm, at: time.Now()}
		}
	}()

	var partial partialStamp
	select {
	case partial = <-partialCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("AsyncSearchPartialMsg never arrived")
	}

	// Partial must arrive well before the walk completes.
	partialLatency := partial.at.Sub(startAt)
	assert.Less(t, partialLatency, 400*time.Millisecond,
		"partial should arrive shortly after first batch (got %s)", partialLatency)

	// Partial should carry the query + first-batch-scored results.
	assert.Equal(t, "alpha", partial.msg.Query)
	assert.NotEmpty(t, partial.msg.Results, "partial results must not be empty")

	var final finalStamp
	select {
	case final = <-finalCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("AsyncSearchCompleteMsg never arrived")
	}

	assert.True(t, final.at.After(partial.at),
		"partial (%s) must arrive before complete (%s)", partial.at, final.at)
	assert.Equal(t, "alpha", final.msg.Query)
	assert.NotEmpty(t, final.msg.Results)
	assert.NoError(t, final.msg.Error)
}

// TestCoordinator_CachedSearchDoesNotEmitPartial verifies that cache-hit
// searches skip partial emission entirely — there is no walk, so there is
// nothing to stream. The model relies on this to avoid double-rendering.
func TestCoordinator_CachedSearchDoesNotEmitPartial(t *testing.T) {
	collector := &mockRecursiveCollector{
		items: []Searchable{newFile("cache.go"), newFile("cached.go")},
	}
	cfg := FilePanelConfig()
	cfg.Timeout = 2 * time.Second
	c := NewCoordinator(cfg)
	c.SetCollectorForTest(func(maxDepth int, showHidden bool) streamingCollector {
		return collector
	})

	src := &mockRecursiveSource{baseDir: "/cache/stream/dir"}

	// Warm the cache.
	cmd := c.StartSearch("cache", src, true)
	require.NotNil(t, cmd)
	msg := cmd()
	_, ok := msg.(AsyncSearchCompleteMsg)
	require.True(t, ok, "first StartSearch must yield AsyncSearchCompleteMsg")

	// Drain any partial from the first (walk) call so we start clean.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		listen := c.ListenForPartial()
		_ = listen()
	}()
	select {
	case <-drained:
	case <-time.After(200 * time.Millisecond):
		// No partial queued — fine.
	}

	// Second search on the SAME dir — cache hit — must NOT emit a partial.
	partialSeen := make(chan AsyncSearchPartialMsg, 1)
	go func() {
		listen := c.ListenForPartial()
		if pm, ok := listen().(AsyncSearchPartialMsg); ok {
			partialSeen <- pm
		}
	}()

	cmd2 := c.StartSearch("cached", src, true)
	require.NotNil(t, cmd2)
	_ = cmd2()

	select {
	case pm := <-partialSeen:
		t.Fatalf("cache-hit search must NOT emit AsyncSearchPartialMsg; got %+v", pm)
	case <-time.After(150 * time.Millisecond):
		// Expected — no partial.
	}
}

// TestCoordinator_CancelSuppressesPartial verifies AC-CORRECT-3 for the
// partial path: a cancelled walk must emit NEITHER partial NOR complete for
// the cancelled query.
func TestCoordinator_CancelSuppressesPartial(t *testing.T) {
	slow := &twoStageCollector{
		firstBatch:      []Searchable{newFile("pending.go")},
		firstBatchDelay: 500 * time.Millisecond,
		completionDelay: 500 * time.Millisecond,
	}
	cfg := FilePanelConfig()
	cfg.Timeout = 5 * time.Second
	c := NewCoordinator(cfg)
	c.SetCollectorForTest(func(maxDepth int, showHidden bool) streamingCollector {
		return slow
	})

	src := &mockRecursiveSource{baseDir: "/cancel/stream/dir"}

	partialSeen := make(chan AsyncSearchPartialMsg, 1)
	go func() {
		listen := c.ListenForPartial()
		if pm, ok := listen().(AsyncSearchPartialMsg); ok {
			partialSeen <- pm
		}
	}()

	cmd := c.StartSearch("ghost", src, true)
	require.NotNil(t, cmd)
	finalCh := make(chan interface{}, 1)
	go func() { finalCh <- cmd() }()

	// Cancel before the first batch's delay expires.
	time.Sleep(50 * time.Millisecond)
	c.CancelActiveSearch()

	select {
	case pm := <-partialSeen:
		t.Errorf("cancelled walk emitted a partial msg: %+v", pm)
	case <-time.After(1 * time.Second):
		// Good — no partial.
	}

	select {
	case raw := <-finalCh:
		if cm, ok := raw.(AsyncSearchCompleteMsg); ok && cm.Error == nil {
			t.Errorf("cancelled walk emitted a no-error Complete msg — AC-CORRECT-3 violation: %+v", cm)
		}
	case <-time.After(500 * time.Millisecond):
		// Acceptable — preferred outcome.
	}
}
