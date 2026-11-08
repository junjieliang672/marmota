package internal

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

// Cancel typing modal e.g. create file or directory
func (m *model) cancelTypingModal() {
	m.typingModal.textInput.Blur()
	m.typingModal.open = false
}

// Confirm to create file or directory
func (m *model) createItem() {
	if err := checkFileNameValidity(m.typingModal.textInput.Value()); err != nil {
		m.typingModal.errorMesssage = err.Error()
		slog.Error("Errow while createItem during item creation", "error", err)

		return
	}

	defer func() {
		m.typingModal.errorMesssage = ""
		m.typingModal.open = false
		m.typingModal.textInput.Blur()
	}()

	path := filepath.Join(m.typingModal.location, m.typingModal.textInput.Value())
	if !strings.HasSuffix(m.typingModal.textInput.Value(), string(filepath.Separator)) {
		path, _ = renameIfDuplicate(path)
		if err := os.MkdirAll(filepath.Dir(path), utils.UserDirPerm); err != nil {
			slog.Error("Error while createItem during directory creation", "error", err)
			return
		}
		f, err := os.Create(path)
		if err != nil {
			slog.Error("Error while createItem during file creation", "error", err)
			return
		}
		defer f.Close()
	} else {
		err := os.MkdirAll(path, utils.UserDirPerm)
		if err != nil {
			slog.Error("Error while createItem during directory creation", "error", err)
			return
		}
	}
}

// Cancel rename file or directory
func (m *model) cancelRename() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	panel.rename.Blur()
	panel.renaming = false
	m.fileModel.renaming = false
}

// Connfirm rename file or directory
func (m *model) confirmRename() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	// Although we dont expect this to happen based on our current flow
	// Just adding it here to be safe
	if len(panel.element) == 0 {
		slog.Error("confirmRename called on empty panel")
		return
	}

	oldPath := panel.element[panel.cursor].location
	newPath := filepath.Join(panel.location, panel.rename.Value())

	// Rename the file
	err := os.Rename(oldPath, newPath)
	if err != nil {
		slog.Error("Error while confirmRename during rename", "error", err)
		// Dont return. We have to also reset the panel and model information
	}
	m.fileModel.renaming = false
	panel.rename.Blur()
	panel.renaming = false
}

func (m *model) openSortOptionsMenu() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	panel.sortOptions.open = true
}

func (m *model) cancelSortOptions() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	panel.sortOptions.cursor = panel.sortOptions.data.selected
	panel.sortOptions.open = false
}

func (m *model) confirmSortOptions() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	panel.sortOptions.data.selected = panel.sortOptions.cursor
	panel.sortOptions.open = false
	panel.invalidateElementCache()

	// If a search layer is active, flip it into user-selected sort order.
	// Search results default to FZF-relevance order; the explicit `o` sort
	// choice only takes effect for the current layer (and any filter layers
	// built on top of it) for the rest of its lifetime. Popping the layer
	// (ESC) resets the flag via Go's zero value.
	if top := panel.searchStack.Peek(); top != nil {
		top.manualSortOverride = true
		if len(top.cachedElems) > 0 {
			sortElements(top.cachedElems, panel.sortOptions.data)
		}
	} else {
		// No active search — invalidate the directory-listing cache so the
		// next render picks up the new sort options.
		panel.searchStack.InvalidateCache()
	}
}

// Move the cursor up in the sort options menu
func (m *model) sortOptionsListUp() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	if panel.sortOptions.cursor > 0 {
		panel.sortOptions.cursor--
	} else {
		panel.sortOptions.cursor = len(panel.sortOptions.data.options) - 1
	}
}

// Move the cursor down in the sort options menu
func (m *model) sortOptionsListDown() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	if panel.sortOptions.cursor < len(panel.sortOptions.data.options)-1 {
		panel.sortOptions.cursor++
	} else {
		panel.sortOptions.cursor = 0
	}
}

func (m *model) toggleReverseSort() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	panel.sortOptions.data.reversed = !panel.sortOptions.data.reversed
	panel.invalidateElementCache()

	// Toggling reverse order is a manual sort action: if a search layer is
	// active, flip it into user-selected order (same semantics as
	// confirmSortOptions).
	if top := panel.searchStack.Peek(); top != nil {
		top.manualSortOverride = true
		if len(top.cachedElems) > 0 {
			sortElements(top.cachedElems, panel.sortOptions.data)
		}
	} else {
		panel.searchStack.InvalidateCache()
	}
}

// Cancel search, this will pop one layer from the search stack
func (m *model) cancelSearch() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	m.clearFilePanelJumpCount()

	// If search bar is focused (user is typing), blur it and clear value
	if panel.searchBar.Focused() {
		panel.searchBar.Blur()
		panel.searchBar.SetValue("")
	}

	// Pop one layer from the stack (only if not empty)
	if !panel.searchStack.IsEmpty() {
		panel.searchStack.Pop()
	}

	// Recalculate heights (footer may need to show)
	m.setHeightValues(m.fullHeight)

	// OLD: Keep old behavior during migration (dual-write)
	if len(panel.recursiveBaseElems) > 0 {
		panel.lastFilterTerm = ""
		panel.lastFilterResults = nil
		if panel.recursiveSearch {
			panel.recursiveSearch = false
		}
	}

	// Clear everything in old system
	panel.recursiveSearch = false
	panel.recursiveBaseElems = nil
	panel.recursiveSearchTerm = ""
	panel.lastFilterTerm = ""
	panel.lastFilterResults = nil
}

// Confirm search. This will push a new layer to the stack with the search term
func (m *model) confirmSearch() tea.Cmd {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	m.clearFilePanelJumpCount()

	var cmd tea.Cmd
	searchTerm := panel.searchBar.Value()
	if searchTerm != "" {
		// Check if search term has file type filter prefix (f: or d:)
		// If so, automatically treat it as a recursive search
		filterType, _ := parseSearchTerm(searchTerm)
		hasFileTypeFilter := filterType != ""

		// Determine layer type based on current state
		var layerType searchLayerType
		top := panel.searchStack.Peek()
		switch {
		case top != nil && top.layerType == searchLayerTouched:
			// A session's files span folders already: f, or f:/d:, filters
			// them as / does rather than walking the folder.
			layerType = searchLayerFilter
		case panel.recursiveSearch || hasFileTypeFilter:
			// Recursive search: either user pressed 'f' key OR used f:/d: prefix
			layerType = searchLayerRecursive
		case !panel.searchStack.IsEmpty():
			layerType = searchLayerFilter
		default:
			layerType = searchLayerRegular
		}

		// Get parent elements for filter layers
		var parentElems []element
		if layerType == searchLayerFilter && !panel.searchStack.IsEmpty() {
			top := panel.searchStack.Peek()
			if top != nil {
				parentElems = top.cachedElems
			}
		}

		// Push new layer
		layer := searchLayer{
			layerType:   layerType,
			searchTerm:  searchTerm,
			parentElems: parentElems,
			searching:   layerType == searchLayerRecursive, // Mark as searching for recursive
		}
		panel.searchStack.Push(layer)

		// Launch async search for recursive layers
		if layerType == searchLayerRecursive {
			cmd = startAsyncRecursiveSearch(
				m,
				panel.location,
				m.toggleDotFile,
				searchTerm,
			)
		}

		// Recalculate heights (footer may need to hide)
		m.setHeightValues(m.fullHeight)
	}

	panel.searchBar.Blur()
	panel.searchBar.SetValue("") // Clear the search bar for next input

	// Reset recursiveSearch flag so next '/' will create a filter layer
	panel.recursiveSearch = false

	return cmd
}

// Help menu panel list up
func (m *model) helpMenuListUp() {
	if m.helpMenu.cursor > 1 {
		m.helpMenu.cursor--
		if m.helpMenu.filteredData[m.helpMenu.cursor].subTitle != "" {
			m.helpMenu.cursor--
		}
	} else {
		// Wrap to the last item. max(..., 0) keeps the cursor from going
		// negative when the filtered list is empty.
		m.helpMenu.cursor = max(len(m.helpMenu.filteredData)-1, 0)
	}
	m.helpMenuScrollToCursor()
}

// Help menu panel list down
func (m *model) helpMenuListDown() {
	if len(m.helpMenu.filteredData) == 0 {
		return
	}

	if m.helpMenu.cursor < len(m.helpMenu.filteredData)-1 {
		// Compute the next selectable row (skip subtitles).
		next := m.helpMenu.cursor + 1
		for next < len(m.helpMenu.filteredData) && m.helpMenu.filteredData[next].subTitle != "" {
			next++
		}
		if next >= len(m.helpMenu.filteredData) {
			// Wrap if no more selectable rows.
			m.helpMenu.cursor = 1
			m.helpMenu.renderIndex = 0
			return
		}
		m.helpMenu.cursor = next
		m.helpMenuScrollToCursor()
	} else {
		m.helpMenu.cursor = 1
		m.helpMenu.renderIndex = 0
	}
}

func removeOrphanSections(items []helpMenuModalData) []helpMenuModalData {
	var result []helpMenuModalData
	// Since we can't know beforehand which section are we actually filtering
	// we may end up in a scenario where there are two sections (General, Panel navigation)
	// with no hotkeys between them, so we need to remove the section which its hotkeys was
	// completely filtered out (Orphan sections)
	for i := range items {
		if items[i].subTitle != "" {
			// Look ahead: is the next item a real hotkey?
			if i+1 < len(items) && items[i+1].subTitle == "" {
				result = append(result, items[i])
			}
			// Else: skip this subtitle because no children
		} else {
			result = append(result, items[i])
		}
	}
	return result
}

func (m *model) filterHelpMenu(query string) {
	filtered := fuzzySearch(query, m.helpMenu.data)
	filtered = removeOrphanSections(filtered)

	m.helpMenu.filteredData = filtered
	if len(filtered) == 0 {
		m.helpMenu.cursor = 0
	} else {
		m.helpMenu.cursor = 1
	}
	m.helpMenu.renderIndex = 0
}

// Fuzzy search function for a list of helpMenuModalData.
// inspired from: sidebar/directory_utils.go
func fuzzySearch(query string, data []helpMenuModalData) []helpMenuModalData {
	if len(data) == 0 {
		return []helpMenuModalData{}
	}

	// Optimization - This haystack can be kept precomputed based on description
	// instead of re computing it in each call
	haystack := []string{}
	idxMap := []int{}
	for i, item := range data {
		var searchText string
		if item.subTitle != "" {
			// Include section headers in search
			searchText = item.subTitle
		} else {
			searchText = strings.Join(item.hotkey, " ") + " " + item.description
		}
		haystack = append(haystack, searchText)
		idxMap = append(idxMap, i)
	}

	matchedIdx := map[int]struct{}{}
	matchedSections := map[int]struct{}{} // Track which sections matched directly
	for _, match := range utils.FzfSearch(query, haystack) {
		idx := idxMap[match.HayIndex]
		matchedIdx[idx] = struct{}{}
		if data[idx].subTitle != "" {
			matchedSections[idx] = struct{}{}
		}
	}

	// For matched sections, include all items under that section
	currentSection := -1
	sectionMatched := false
	for i, d := range data {
		if d.subTitle != "" {
			currentSection = i
			_, sectionMatched = matchedSections[i]
		} else if sectionMatched {
			// Include all items under a matched section
			matchedIdx[i] = struct{}{}
		}
		_ = currentSection // silence unused warning
	}

	results := []helpMenuModalData{}
	for i, d := range data {
		_, isMatch := matchedIdx[i]
		if d.subTitle != "" || isMatch {
			results = append(results, d)
		}
	}

	return results
}

// Toggle help menu
func (m *model) openHelpMenu() {
	if m.helpMenu.open {
		m.helpMenu.searchBar.Reset()
		m.helpMenu.open = false
		return
	}

	// Reset filteredData to the full data whenever the helpMenu is opened
	m.helpMenu.filteredData = m.helpMenu.data
	m.helpMenu.open = true
	// Auto-focus the search bar so users can type immediately
	m.helpMenu.searchBar.Focus()
}

// Quit help menu
func (m *model) quitHelpMenu() {
	m.helpMenu.searchBar.Reset()
	m.helpMenu.open = false
}
