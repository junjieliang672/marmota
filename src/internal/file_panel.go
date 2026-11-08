package internal

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/icloud"
)

func (panel *filePanel) getSelectedItem() element {
	if panel.cursor < 0 || len(panel.element) <= panel.cursor {
		return element{}
	}
	return panel.element[panel.cursor]
}

func (panel *filePanel) resetSelected() {
	clear(panel.selected)
}

// invalidateElementCache marks the directory element cache as stale,
// forcing the next getElementsForPanel call to re-read from disk.
func (panel *filePanel) invalidateElementCache() {
	panel.elementCacheValid = false
}

// selectedToSlice returns selected items as a sorted slice for operations that need []string
// Sorting ensures deterministic behavior for operations like compression where the first item
// determines the archive name.
func (panel *filePanel) selectedToSlice() []string {
	items := make([]string, 0, len(panel.selected))
	for path := range panel.selected {
		items = append(items, path)
	}
	sort.Strings(items)
	return items
}

// liveLayerFilter is a search layer's list filtered by what is being typed
// over it, worked out again only when the term or the list changes.
func (panel *filePanel) liveLayerFilter(elems []element, term string) []element {
	c := &panel.layerFilter
	if c.term != term || c.base != len(elems) {
		c.term, c.base, c.results = term, len(elems), filterElementsBySearchString(elems, term)
	}
	return c.results
}

// actionTargets is what an action on "these files" acts on: the selected
// items, else the item under the cursor, else nothing.
func (panel *filePanel) actionTargets() []string {
	if len(panel.selected) > 0 {
		return panel.selectedToSlice()
	}
	if item := panel.getSelectedItem(); item.location != "" {
		return []string{item.location}
	}
	return nil
}

// selectedFiles is the selected items that aren't folders, in the order the
// panel shows them.
func (panel *filePanel) selectedFiles() []string {
	var files []string
	for _, e := range panel.element {
		if _, ok := panel.selected[e.location]; ok && !e.directory {
			files = append(files, e.location)
		}
	}
	return files
}

// For modification. Make sure to do a nil check
func (panel *filePanel) getSelectedItemPtr() *element {
	if panel.cursor < 0 || len(panel.element) <= panel.cursor {
		return nil
	}
	return &panel.element[panel.cursor]
}

// Note : This will soon be moved to its own package.
func (panel *filePanel) changeFilePanelMode() {
	switch panel.panelMode {
	case selectMode:
		clear(panel.selected)
		panel.panelMode = browserMode
	case browserMode:
		panel.panelMode = selectMode
	default:
		slog.Error("Unexpected panelMode", "panelMode", panel.panelMode)
	}
}

// This should be the function that is always called whenever we are updating a directory.
func (panel *filePanel) updateCurrentFilePanelDir(path string) error {
	slog.Debug("updateCurrentFilePanelDir", "panel.location", panel.location, "path", path)
	// In case non Absolute path is passed, make sure to resolve it.
	path = utils.ResolveAbsPath(panel.location, path)

	// Ignore if its the same directory. It prevents resetting of searchBar
	if path == panel.location {
		return nil
	}

	// NOTE: This could be a configurable feature
	// Update the cursor and render status in case we switch back to this.
	panel.directoryRecords[panel.location] = directoryRecord{
		directoryCursor: panel.cursor,
		directoryRender: panel.render,
	}

	if info, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s : no such file or directory, stats err : %w", path, err)
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	// Switch to "path"
	panel.location = path
	panel.invalidateElementCache()

	// Update iCloud folder status
	panel.isICloudFolder = icloud.IsICloudFolder(path)

	// Clear iCloud filter when leaving iCloud folders
	if !panel.isICloudFolder {
		panel.iCloudFilter.selected = 0 // FilterAll
		panel.iCloudFilter.cursor = 0
	}

	// TODO(BUG) : We are fetching the cursor and render from cache, but this could become invalid
	// in case user deletes some items in the directory via another file manager and then switch back
	// Basically this directoryRecords cache can be invalid. On each Update(), we must validate
	// the cursor and render values.
	curDirectoryRecord, hasRecord := panel.directoryRecords[panel.location]
	if hasRecord {
		panel.cursor = curDirectoryRecord.directoryCursor
		panel.render = curDirectoryRecord.directoryRender
	} else {
		panel.cursor = 0
		panel.render = 0
	}

	slog.Debug("updateCurrentFilePanelDir : After update", "cursor", panel.cursor, "render", panel.render)

	// Reset the searchbar Value and search stack
	// TODO(Refactoring) : Have a common searchBar type for sidebar and this search bar.
	panel.searchBar.SetValue("")
	panel.searchStack.Clear() // Clear the entire search hierarchy

	// OLD: Keep temporarily during migration (dual-write)
	panel.recursiveSearch = false
	panel.recursiveBaseElems = nil // Clear stored recursive results
	panel.recursiveSearchTerm = "" // Clear cached search term
	panel.lastFilterTerm = ""      // Clear filter cache
	panel.lastFilterResults = nil

	panel.followFolderSort()
	return nil
}

// followFolderSort gives the panel the sort of the folder it shows: the
// Downloads folder opens newest first, and leaving it gives the panel back
// the sort it has for other folders. A sort chosen in Downloads lasts until
// the panel leaves it.
func (panel *filePanel) followFolderSort() {
	data := &panel.sortOptions.data
	inDownloads := isDownloadsFolder(panel.location)
	switch {
	case inDownloads && panel.sortElsewhere == nil:
		byDate := slices.Index(data.options, string(sortingDateModified))
		if byDate < 0 {
			return
		}
		elsewhere := *data
		panel.sortElsewhere = &elsewhere
		// Not reversed, Date Modified is newest first.
		data.selected, data.reversed = byDate, false
	case !inDownloads && panel.sortElsewhere != nil:
		*data = *panel.sortElsewhere
		panel.sortElsewhere = nil
	default:
		return
	}
	panel.sortOptions.cursor = data.selected
}

// ownSort is the panel's sort for folders other than Downloads.
func (panel *filePanel) ownSort() sortOptionsModelData {
	if panel.sortElsewhere != nil {
		return *panel.sortElsewhere
	}
	return panel.sortOptions.data
}

// isDownloadsFolder reports whether dir is the Downloads folder itself, not
// one inside it.
func isDownloadsFolder(dir string) bool {
	return variable.DownloadsDir != "" && filepath.Clean(dir) == filepath.Clean(variable.DownloadsDir)
}

func (panel *filePanel) parentDirectory() error {
	return panel.updateCurrentFilePanelDir("..")
}

func (panel *filePanel) handleResize(height int) {
	// Min render cursor that keeps the cursor in view
	minVisibleRenderCursor := panel.cursor - panel.elementHeight(height) + 1
	// Max render cursor. This ensures all elements are rendered if there is space
	maxRenderCursor := max(len(panel.element)-panel.elementHeight(height), 0)

	if panel.render > maxRenderCursor {
		panel.render = maxRenderCursor
	}
	if panel.render < minVisibleRenderCursor {
		panel.render = minVisibleRenderCursor
	}
}
