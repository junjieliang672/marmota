package search

// Tests for spec-006 Coordinator cancellation slot.
//
// RED PHASE: these tests reference
//   - Coordinator.CancelActiveSearch
// and assume the same injection seam documented in coordinator_cache_test.go
// (SetCollectorForTest).
//
// Invariants from spec:
//   - AC-CORRECT-3: a cancelled walk MUST NOT emit an AsyncSearchCompleteMsg
//     for the cancelled query; only the new query's msgs reach the model.
//   - AC-PERF-6: cancellation latency < 50ms (from a new StartSearch or an
//     explicit CancelActiveSearch call to the walk goroutine exiting).

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// A cancellable slow collector — blocks in CollectStream until told to
// release OR until ctx is cancelled.
// ---------------------------------------------------------------------------

type slowRecursiveCollector struct {
	callCount  int32
	released   chan struct{} // closed to release non-cancelled calls
	exitedCnt  int32
	lastCtxErr atomic.Value // error (or nil)
}

func newSlowCollector() *slowRecursiveCollector {
	return &slowRecursiveCollector{released: make(chan struct{})}
}

func (s *slowRecursiveCollector) CollectStream(
	ctx context.Context,
	root, filter string,
	out chan<- []Searchable,
) (bool, error) {
	atomic.AddInt32(&s.callCount, 1)
	defer close(out)
	defer atomic.AddInt32(&s.exitedCnt, 1)

	select {
	case <-ctx.Done():
		s.lastCtxErr.Store(ctx.Err())
		return false, ctx.Err()
	case <-s.released:
		// Emit one batch and return normally.
		select {
		case out <- []Searchable{newFile("completed.txt")}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		return false, nil
	}
}

func (s *slowRecursiveCollector) Collect(ctx context.Context, root, filter string) ([]Searchable, bool, error) {
	// Not used in these tests.
	return nil, false, nil
}

func (s *slowRecursiveCollector) CallCount() int { return int(atomic.LoadInt32(&s.callCount)) }
func (s *slowRecursiveCollector) ExitCount() int { return int(atomic.LoadInt32(&s.exitedCnt)) }

// newSlowCoordinator wires a Coordinator to use our slow collector.
func newSlowCoordinator(t *testing.T, slow *slowRecursiveCollector) *Coordinator {
	t.Helper()
	cfg := FilePanelConfig()
	cfg.Timeout = 5 * time.Second

	c := NewCoordinator(cfg)
	c.SetCollectorForTest(func(maxDepth int, showHidden bool) streamingCollector {
		return slow
	})
	return c
}

// runCmdInBackground runs the tea.Cmd in a goroutine and returns a channel
// that receives the final tea.Msg (as interface{}) when the cmd returns.
func runCmdInBackground(cmd func() interface{}) <-chan interface{} {
	ch := make(chan interface{}, 1)
	go func() { ch <- cmd() }()
	return ch
}

// ---------------------------------------------------------------------------
// Unit: Coordinator.CancelActiveSearch cancels the in-flight goroutine
// ---------------------------------------------------------------------------

func TestCoordinator_CancelActiveSearch(t *testing.T) {
	slow := newSlowCollector()
	c := newSlowCoordinator(t, slow)
	src := &mockRecursiveSource{baseDir: "/slow/dir"}

	cmd := c.StartSearch("pending", src, true)
	require.NotNil(t, cmd, "StartSearch must return a Cmd for recursive search")

	// Kick the cmd off in the background so it blocks inside our slow
	// collector. We convert tea.Cmd (func() tea.Msg) to an interface{} returning
	// shim so we don't depend on the exact type here.
	msgCh := runCmdInBackground(func() interface{} { return cmd() })

	// Wait for the collector to actually be entered.
	deadline := time.Now().Add(2 * time.Second)
	for slow.CallCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("collector never invoked")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancelStart := time.Now()
	c.CancelActiveSearch()

	// Wait for the collector goroutine to exit.
	exited := false
	for time.Since(cancelStart) < 500*time.Millisecond {
		if slow.ExitCount() == 1 {
			exited = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	elapsed := time.Since(cancelStart)
	assert.True(t, exited, "collector must exit within 500ms of CancelActiveSearch (got %s)", elapsed)
	// AC-PERF-6: aim for <50ms, but allow slack for scheduling on CI.
	assert.Less(t, elapsed, 200*time.Millisecond,
		"cancellation latency should be <200ms (target <50ms per AC-PERF-6)")

	// Drain the cmd's result — it may emit a Complete msg with Error set, OR
	// it may emit nothing (preferred — AC-CORRECT-3 says cancelled walk must
	// not emit a final msg for the cancelled query). Either way we shouldn't
	// block forever.
	select {
	case raw := <-msgCh:
		// If a Complete msg IS emitted, the spec says it must NOT be for the
		// cancelled query being reflected to the stack. The Coordinator may
		// choose to swallow the msg internally; but if it propagates, we
		// accept nil (no msg) or a msg whose Error is context.Canceled.
		if msg, ok := raw.(AsyncSearchCompleteMsg); ok {
			if msg.Error != nil {
				// Acceptable — documents the cancellation.
				t.Logf("cancel produced Complete msg with Error=%v", msg.Error)
			} else {
				// If the msg carries the cancelled query and no error, the
				// stack should at least reflect that the layer is no longer
				// "searching". A no-error Complete for the cancelled query
				// is a SPEC VIOLATION — flag it as a failure so implementers
				// must handle cancellation explicitly (e.g. return nil msg).
				t.Errorf("cancelled walk emitted Complete msg with no error for query %q — AC-CORRECT-3 violation",
					msg.Query)
			}
		}
	case <-time.After(500 * time.Millisecond):
		// No msg emitted — preferred outcome per AC-CORRECT-3.
	}
}

// ---------------------------------------------------------------------------
// Unit: a NEW StartSearch cancels the previous StartSearch
// ---------------------------------------------------------------------------

func TestCoordinator_NewSearchCancelsPrevious(t *testing.T) {
	slow := newSlowCollector()
	c := newSlowCoordinator(t, slow)
	src := &mockRecursiveSource{baseDir: "/slow/dir2"}

	// First search — starts but blocks.
	cmd1 := c.StartSearch("old", src, true)
	require.NotNil(t, cmd1)
	msgCh1 := runCmdInBackground(func() interface{} { return cmd1() })

	// Wait for the collector to be entered for the first search.
	deadline := time.Now().Add(2 * time.Second)
	for slow.CallCount() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("first collector call never happened")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second search — should cancel the first and start a new walk.
	cancelStart := time.Now()
	cmd2 := c.StartSearch("new", src, true)
	require.NotNil(t, cmd2)

	// The first goroutine must have exited promptly.
	exitedDeadline := cancelStart.Add(500 * time.Millisecond)
	for time.Now().Before(exitedDeadline) {
		if slow.ExitCount() >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	elapsed := time.Since(cancelStart)
	assert.GreaterOrEqual(t, slow.ExitCount(), 1,
		"previous search goroutine must exit when a new StartSearch arrives (got ExitCount=%d after %s)",
		slow.ExitCount(), elapsed)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"cancellation latency should be <200ms (target <50ms per AC-PERF-6)")

	// Release the slow collector so the SECOND cmd can complete.
	// Note: the collector is shared between the two cmds, so either the
	// first or second call will pick up the release. Close the channel to
	// unblock any still-waiting call.
	close(slow.released)

	// The second cmd must eventually emit a final Complete msg for query "new".
	msgCh2 := runCmdInBackground(func() interface{} { return cmd2() })

	select {
	case raw := <-msgCh2:
		msg, ok := raw.(AsyncSearchCompleteMsg)
		require.True(t, ok, "second cmd must emit AsyncSearchCompleteMsg, got %T", raw)
		// Spec says: only the NEW query's msg reaches the model.
		assert.Equal(t, "new", msg.Query, "final msg must carry the new query")
	case <-time.After(3 * time.Second):
		t.Fatalf("second StartSearch cmd never produced a final message")
	}

	// The FIRST cmd — if it ever produces a msg, must not be a no-error
	// Complete for the "old" query.
	select {
	case raw := <-msgCh1:
		if msg, ok := raw.(AsyncSearchCompleteMsg); ok {
			if msg.Error == nil && msg.Query == "old" {
				t.Errorf("cancelled (old) search produced a no-error Complete msg — AC-CORRECT-3 violation")
			}
		}
	case <-time.After(200 * time.Millisecond):
		// Ideal outcome — no message at all for the cancelled cmd.
	}
}
