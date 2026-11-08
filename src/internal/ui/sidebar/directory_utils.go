package sidebar

import (
	"fmt"
	"hash"
	"hash/fnv"
	"os"
	"slices"
	"sync"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

// filteredDirsCache is a package-level single-entry memo for
// getFilteredDirectories. The sidebar calls getFilteredDirectories on every
// Bubble Tea tick while a search is active, even when nothing changed.
var (
	filteredDirsCacheMu sync.Mutex  //nolint:gochecknoglobals // guards the memo below
	filteredDirsCache   *fuzzyCache //nolint:gochecknoglobals // a package-level memo, by design
)

// Fuzzy search function for a list of directories.
func fuzzySearch(query string, dirs []directory) []directory {
	if len(dirs) == 0 {
		return []directory{}
	}

	var filteredDirs []directory

	haystack := make([]string, len(dirs))
	dirMap := make(map[string][]directory, len(dirs))
	for i, dir := range dirs {
		haystack[i] = dir.Name
		dirMap[dir.Name] = append(dirMap[dir.Name], dir)
	}

	for _, match := range utils.FzfSearch(query, haystack) {
		if entries, ok := dirMap[match.Key]; ok && len(entries) > 0 {
			filteredDirs = append(filteredDirs, entries[0])
			dirMap[match.Key] = entries[1:]
		}
	}

	return filteredDirs
}

// Return all sidebar directories.
func getDirectories(pinnedMgr *PinnedManager, recentMgrs ...*RecentEditsManager) []directory {
	pinned := []directory{}
	if pinnedMgr != nil {
		pinned = pinnedMgr.Load()
	}
	recent := []directory{}
	if len(recentMgrs) > 0 && recentMgrs[0] != nil {
		recent = recentMgrs[0].Load()
	}
	return formSidebarSections(pinned, recent)
}

// Get filtered directories using fuzzy search over the pinned and recent
// sections only.
func getFilteredDirectories(query string, pinnedMgr *PinnedManager, recentMgrs ...*RecentEditsManager) []directory {
	baseHash := hashSidebarInvalidationSignal(pinnedMgr, recentMgrs...)

	filteredDirsCacheMu.Lock()
	if filteredDirsCache != nil &&
		filteredDirsCache.query == query &&
		filteredDirsCache.baseHash == baseHash {
		cached := filteredDirsCache.result
		filteredDirsCacheMu.Unlock()
		return cached
	}
	filteredDirsCacheMu.Unlock()

	pinned := []directory{}
	if pinnedMgr != nil {
		pinned = pinnedMgr.Load()
	}
	recent := []directory{}
	if len(recentMgrs) > 0 && recentMgrs[0] != nil {
		recent = recentMgrs[0].Load()
	}

	result := formSidebarSections(fuzzySearch(query, pinned), fuzzySearch(query, recent))

	filteredDirsCacheMu.Lock()
	filteredDirsCache = &fuzzyCache{
		query:    query,
		baseHash: baseHash,
		result:   result,
	}
	filteredDirsCacheMu.Unlock()

	return result
}

func hashSidebarInvalidationSignal(pinnedMgr *PinnedManager, recentMgrs ...*RecentEditsManager) uint64 {
	h := fnv.New64a()
	if pinnedMgr != nil {
		writeSidebarFileHash(h, pinnedMgr.filePath)
	}
	if len(recentMgrs) > 0 && recentMgrs[0] != nil {
		writeSidebarFileHash(h, recentMgrs[0].filePath)
	}
	return h.Sum64()
}

func writeSidebarFileHash(h hash.Hash64, filePath string) {
	_, _ = h.Write([]byte(filePath))
	_, _ = h.Write([]byte{0})
	if info, err := os.Stat(filePath); err == nil {
		_, _ = fmt.Fprintf(h, "%d\x00%d", info.Size(), info.ModTime().UnixNano())
	}
}

func formSidebarSections(pinnedDirectories []directory, recentEditedFiles []directory) []directory {
	return slices.Concat([]directory{pinnedDividerDir}, pinnedDirectories,
		[]directory{recentDividerDir}, recentEditedFiles)
}

func formDirctorySlice(homeDirectories []directory, pinnedDirectories []directory,
	diskDirectories []directory) []directory {
	return slices.Concat([]directory{pinnedDividerDir}, pinnedDirectories,
		[]directory{locationsDividerDir}, homeDirectories,
		[]directory{diskDividerDir}, diskDirectories)
}
