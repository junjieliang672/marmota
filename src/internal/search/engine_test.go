package search

// Extension tests for spec-006 FzfExecutor.ExecuteWithCap.
//
// Note: the existing FzfExecutor tests live in search_test.go. The spec asked
// us to "EXTEND existing file engine_test.go" but that file did not exist on
// main@HEAD. Rather than reorganise search_test.go (risk: introducing churn
// into an 80+ test suite), we add a NEW engine_test.go dedicated to the new
// API so the original file stays untouched.
//
// RED PHASE: these tests reference FzfExecutor.ExecuteWithCap and
// DefaultMaxResults which do NOT exist yet.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Unit: FzfExecutor.ExecuteWithCap truncates to maxResults
// ---------------------------------------------------------------------------

func TestFzfExecutor_ExecuteWithCap_Truncation(t *testing.T) {
	// Generate 10k items, all matching the query "abc" (we embed "abc" in
	// every filename so fzf scores all of them).
	const n = 10_000
	items := make([]Searchable, n)
	for i := range n {
		items[i] = &mockSearchable{
			name:      fmt.Sprintf("abc_%06d.txt", i),
			location:  fmt.Sprintf("/synthetic/abc_%06d.txt", i),
			directory: false,
		}
	}

	executor := NewFzfExecutor(10 * time.Second)
	ctx := context.Background()

	cases := []struct {
		name string
		cap  int
	}{
		{"cap_1", 1},
		{"cap_10", 10},
		{"cap_100", 100},
		{"cap_1000", 1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := executor.ExecuteWithCap(ctx, "abc", items, tc.cap)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(results), tc.cap,
				"ExecuteWithCap must return at most maxResults items")

			// All returned items must be in the input set — no hallucinations.
			nameSet := make(map[string]struct{}, n)
			for _, it := range items {
				nameSet[it.GetName()] = struct{}{}
			}
			for _, r := range results {
				_, ok := nameSet[r.Item.GetName()]
				assert.True(t, ok, "result %q not in input set", r.Item.GetName())
			}
		})
	}
}

// Top-N preservation: the cap=100 results must be the same as the top-100
// prefix of the uncapped Execute() results. This guards against buggy
// implementations that truncate BEFORE scoring (returning a random 100)
// rather than after.
func TestFzfExecutor_ExecuteWithCap_KeepsTopScoring(t *testing.T) {
	// Mix of matching and non-matching filenames so fzf ordering is meaningful.
	items := make([]Searchable, 0, 500)
	for i := range 200 {
		items = append(items, &mockSearchable{
			name:      fmt.Sprintf("abc%03d.txt", i), // strong match
			location:  fmt.Sprintf("/m/abc%03d.txt", i),
			directory: false,
		})
	}
	for i := range 300 {
		items = append(items, &mockSearchable{
			name:      fmt.Sprintf("xyz%03d.txt", i), // weak/no match for "abc"
			location:  fmt.Sprintf("/m/xyz%03d.txt", i),
			directory: false,
		})
	}

	executor := NewFzfExecutor(10 * time.Second)
	ctx := context.Background()

	fullResults, err := executor.Execute(ctx, "abc", items)
	require.NoError(t, err)
	require.NotEmpty(t, fullResults, "Execute must return results")

	capped, err := executor.ExecuteWithCap(ctx, "abc", items, 50)
	require.NoError(t, err)
	require.LessOrEqual(t, len(capped), 50)

	// The capped results must be a prefix of the full results — same order,
	// same scores, just truncated.
	require.LessOrEqual(t, len(capped), len(fullResults))
	for i := range capped {
		assert.Equal(t, fullResults[i].Item.GetName(), capped[i].Item.GetName(),
			"capped[%d] should equal fullResults[%d] — cap must keep TOP scores, not a random slice", i, i)
		assert.Equal(t, fullResults[i].Score, capped[i].Score,
			"capped[%d].Score should equal full[%d].Score", i, i)
	}
}

// ---------------------------------------------------------------------------
// Unit: FzfExecutor.ExecuteWithCap with maxResults=0 matches Execute
// ---------------------------------------------------------------------------

func TestFzfExecutor_ExecuteWithCap_ZeroMeansUncapped(t *testing.T) {
	items := []Searchable{
		newFile("main.go"),
		newFile("engine.go"),
		newFile("coordinator.go"),
		newFile("types.go"),
		newFile("stack.go"),
	}

	executor := NewFzfExecutor(5 * time.Second)
	ctx := context.Background()

	plain, err := executor.Execute(ctx, "g", items)
	require.NoError(t, err)

	capped, err := executor.ExecuteWithCap(ctx, "g", items, 0)
	require.NoError(t, err)

	require.Len(t, capped, len(plain), "maxResults=0 must match Execute length exactly")
	for i := range plain {
		assert.Equal(t, plain[i].Item.GetName(), capped[i].Item.GetName(),
			"maxResults=0 must yield identical ordering to Execute")
		assert.Equal(t, plain[i].Score, capped[i].Score,
			"maxResults=0 must yield identical scores")
	}
}

// Parity/regression: negative maxResults is treated as 0 (uncapped). The spec
// says "0 = no cap"; callers may pass negative values accidentally.
func TestFzfExecutor_ExecuteWithCap_NegativeIsUncapped(t *testing.T) {
	items := []Searchable{
		newFile("alpha.go"),
		newFile("beta.go"),
		newFile("gamma.go"),
	}
	executor := NewFzfExecutor(5 * time.Second)
	ctx := context.Background()

	plain, err := executor.Execute(ctx, "a", items)
	require.NoError(t, err)

	capped, err := executor.ExecuteWithCap(ctx, "a", items, -5)
	require.NoError(t, err)

	assert.Len(t, capped, len(plain), "negative cap must behave as uncapped")
}

// Sanity: DefaultMaxResults is set to the spec'd 5000.
func TestFzfExecutor_DefaultMaxResultsConstant(t *testing.T) {
	// Spec 006: FzfExecutor.DefaultMaxResults default 5000.
	assert.Equal(t, 5000, DefaultMaxResults,
		"DefaultMaxResults should be 5000 per spec-006")
}
