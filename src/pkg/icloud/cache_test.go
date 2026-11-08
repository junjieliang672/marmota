package icloud

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBatchStatusCached_CachesResults(t *testing.T) {
	originalFetch := batchStatusFetchFn
	originalTTL := statusCacheTTL
	ClearStatusCache()
	t.Cleanup(func() {
		batchStatusFetchFn = originalFetch
		statusCacheTTL = originalTTL
		ClearStatusCache()
	})

	var calls int
	batchStatusFetchFn = func(paths []string) map[string]Status {
		calls++
		return map[string]Status{
			"/tmp/a": StatusSynced,
			"/tmp/b": StatusCloudOnly,
		}
	}
	statusCacheTTL = time.Minute

	first := GetBatchStatusCached([]string{"/tmp/a", "/tmp/b"})
	second := GetBatchStatusCached([]string{"/tmp/a", "/tmp/b"})

	assert.Equal(t, 1, calls)
	assert.Equal(t, first, second)
	assert.Equal(t, StatusSynced, first["/tmp/a"])
	assert.Equal(t, StatusCloudOnly, first["/tmp/b"])
}

func TestGetBatchStatusCached_OnlyFetchesMisses(t *testing.T) {
	originalFetch := batchStatusFetchFn
	originalTTL := statusCacheTTL
	ClearStatusCache()
	t.Cleanup(func() {
		batchStatusFetchFn = originalFetch
		statusCacheTTL = originalTTL
		ClearStatusCache()
	})

	var requested [][]string
	batchStatusFetchFn = func(paths []string) map[string]Status {
		cloned := append([]string(nil), paths...)
		requested = append(requested, cloned)

		result := make(map[string]Status, len(paths))
		for _, path := range paths {
			result[path] = StatusSynced
		}
		return result
	}
	statusCacheTTL = time.Minute

	_ = GetBatchStatusCached([]string{"/tmp/a", "/tmp/b"})
	got := GetBatchStatusCached([]string{"/tmp/a", "/tmp/c"})

	require.Len(t, requested, 2)
	assert.ElementsMatch(t, []string{"/tmp/a", "/tmp/b"}, requested[0])
	assert.ElementsMatch(t, []string{"/tmp/c"}, requested[1])
	assert.Equal(t, StatusSynced, got["/tmp/a"])
	assert.Equal(t, StatusSynced, got["/tmp/c"])
}

func TestClearStatusCache_ForcesRefetch(t *testing.T) {
	originalFetch := batchStatusFetchFn
	originalTTL := statusCacheTTL
	ClearStatusCache()
	t.Cleanup(func() {
		batchStatusFetchFn = originalFetch
		statusCacheTTL = originalTTL
		ClearStatusCache()
	})

	var calls int
	batchStatusFetchFn = func(paths []string) map[string]Status {
		calls++
		return map[string]Status{
			"/tmp/a": StatusSynced,
		}
	}
	statusCacheTTL = time.Minute

	_ = GetBatchStatusCached([]string{"/tmp/a"})
	ClearStatusCache()
	_ = GetBatchStatusCached([]string{"/tmp/a"})

	assert.Equal(t, 2, calls)
}
