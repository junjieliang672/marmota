package icloud

import (
	"sync"
	"time"
)

type statusCacheEntry struct {
	status    Status
	expiresAt time.Time
}

var (
	statusCacheMu      sync.Mutex                          //nolint:gochecknoglobals // guards statusCache
	statusCache        = make(map[string]statusCacheEntry) //nolint:gochecknoglobals // shared by every panel
	statusCacheTTL     = 5 * time.Second                   //nolint:gochecknoglobals // test seam
	batchStatusFetchFn = GetBatchStatus                    //nolint:gochecknoglobals // test seam
)

// GetBatchStatusCached returns iCloud statuses with a short TTL to avoid
// repeatedly shelling out while the user is simply moving the cursor.
func GetBatchStatusCached(paths []string) map[string]Status {
	result := make(map[string]Status, len(paths))
	if len(paths) == 0 {
		return result
	}

	now := time.Now()
	missing := make([]string, 0, len(paths))

	statusCacheMu.Lock()
	for _, path := range paths {
		entry, ok := statusCache[path]
		if ok && now.Before(entry.expiresAt) {
			result[path] = entry.status
			continue
		}

		delete(statusCache, path)
		missing = append(missing, path)
	}
	statusCacheMu.Unlock()

	if len(missing) == 0 {
		return result
	}

	fetched := batchStatusFetchFn(missing)
	expiresAt := now.Add(statusCacheTTL)

	statusCacheMu.Lock()
	defer statusCacheMu.Unlock()
	for _, path := range missing {
		status, ok := fetched[path]
		if !ok {
			status = StatusUnknown
		}
		statusCache[path] = statusCacheEntry{
			status:    status,
			expiresAt: expiresAt,
		}
		result[path] = status
	}

	return result
}

// ClearStatusCache forces the next cached status lookup to hit the helper again.
func ClearStatusCache() {
	statusCacheMu.Lock()
	defer statusCacheMu.Unlock()
	statusCache = make(map[string]statusCacheEntry)
}
