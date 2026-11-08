package sidebar

import "strings"

func (d directory) IsDivider() bool {
	return d == pinnedDividerDir || d == recentDividerDir || d == locationsDividerDir || d == diskDividerDir ||
		d == sessionsDividerDir
}
func (d directory) RequiredHeight() int {
	if d.IsDivider() {
		return dividerRenderHeight
	}
	return 1
}

// True if only dividers are in directories slice,
// but no actual directories
// This will be pretty quick. But we can replace it with
// len(s.directories) <= 2 - More hacky and hardcoded-like, but faster
func (s *Model) NoActualDir() bool {
	for _, d := range s.directories {
		if !d.IsDivider() {
			return false
		}
	}
	return true
}

func (s *Model) isCursorInvalid() bool {
	return s.cursor < 0 || s.cursor >= len(s.directories) || s.directories[s.cursor].IsDivider()
}

func (s *Model) resetCursor() {
	s.cursor = 0
	// Move to first non Divider dir
	for i, d := range s.directories {
		if !d.IsDivider() {
			s.cursor = i
			return
		}
	}
	// If all directories are divider, code will reach here. and s.cursor will stay 0
	// Or s.directories is empty
}

// SearchBarFocused returns whether the search bar is focused
func (s *Model) SearchBarFocused() bool {
	return s.searchBar.Focused()
}

// SearchBarBlur removes focus from the search bar
func (s *Model) SearchBarBlur() {
	s.searchBar.Blur()
	if s.searchCoordinator != nil {
		s.searchCoordinator.SetTyping(false)
	}
}

// SearchBarFocus sets focus on the search bar
func (s *Model) SearchBarFocus() {
	s.searchBar.Focus()
	if s.searchCoordinator != nil {
		s.searchCoordinator.SetTyping(true)
	}
}

// IsSearchActive returns whether there is an active search filter
func (s *Model) IsSearchActive() bool {
	return s.searchBar.Value() != ""
}

// ClearSearch clears the search filter and restores all directories
func (s *Model) ClearSearch() {
	s.searchBar.SetValue("")
	s.UpdateDirectories()
}

// IsRenaming returns whether the sidebar is currently in renaming mode
func (s *Model) IsRenaming() bool {
	return s.renaming
}

// GetCurrentDirectoryLocation returns the location of the currently selected directory
func (s *Model) GetCurrentDirectoryLocation() string {
	if s.isCursorInvalid() || s.NoActualDir() {
		return ""
	}
	return s.directories[s.cursor].Location
}

// IsCurrentDirectoryPinned reports whether the currently selected sidebar
// directory is present in the pinned file.
func (s *Model) IsCurrentDirectoryPinned() bool {
	current := s.GetCurrentDirectoryLocation()
	if current == "" || s.pinnedMgr == nil {
		return false
	}

	for _, pinned := range s.pinnedMgr.Load() {
		if pinned.Location == current {
			return true
		}
	}

	return false
}

func (s *Model) pinnedIndexRange() (int, int) {
	return s.sectionRange(pinnedDividerDir)
}

// sessionsIndexRange is where the live sessions are, between Pinned and
// Recent.
func (s *Model) sessionsIndexRange() (int, int) {
	return s.sectionRange(sessionsDividerDir)
}

func (s *Model) recentEditsIndexRange() (int, int) {
	return s.sectionRange(recentDividerDir)
}

// sectionRange is where the rows under divider are: up to the next divider.
func (s *Model) sectionRange(divider directory) (int, int) {
	idx := s.findDividerIndex(divider)
	if idx == -1 {
		return 0, -1
	}
	end := idx + 1
	for end < len(s.directories) && !s.directories[end].IsDivider() {
		end++
	}
	return idx + 1, end - 1
}

// SessionID is the session a sidebar row's location names, when it is one.
func SessionID(location string) (string, bool) {
	return strings.CutPrefix(location, SessionLocationPrefix)
}

// indexedItemsRange is from the first numbered row to the last: the
// sections' rows, numbered on across their dividers.
func (s *Model) indexedItemsRange() (int, int) {
	first, last := 0, -1
	for _, section := range []func() (int, int){s.pinnedIndexRange, s.sessionsIndexRange, s.recentEditsIndexRange} {
		start, end := section()
		if start > end {
			continue
		}
		if last < first {
			first, last = start, end
		}
		first, last = min(first, start), max(last, end)
	}
	return first, last
}

func (s *Model) IsCurrentItemIndexed() bool {
	start, end := s.indexedItemsRange()
	return start <= end && s.cursor >= start && s.cursor <= end
}

func sidebarIndexedDisplayNumber(directories []directory, idx int) int {
	if idx < 0 || idx >= len(directories) {
		return 0
	}

	count := 0
	for i := range idx + 1 {
		if !directories[i].IsDivider() {
			count++
		}
	}
	return count
}

func (s *Model) IsCurrentItemRecentEdit() bool {
	current := s.GetCurrentDirectoryLocation()
	if current == "" || s.recentMgr == nil {
		return false
	}

	for _, recent := range s.recentMgr.Load() {
		if recent.Location == current {
			return true
		}
	}

	return false
}

func (s *Model) sectionStartIndexes() []int {
	var starts []int
	if start, end := s.pinnedIndexRange(); start <= end {
		starts = append(starts, start)
	}
	if start, end := s.sessionsIndexRange(); start <= end {
		starts = append(starts, start)
	}
	if start, end := s.recentEditsIndexRange(); start <= end {
		starts = append(starts, start)
	}
	return starts
}

func (s *Model) findDividerIndex(target directory) int {
	for i, d := range s.directories {
		if d == target {
			return i
		}
	}
	return -1
}

// ============= Search Mode Accessors =============

// IsInSearchMode returns whether sidebar is displaying search results
func (s *Model) IsInSearchMode() bool {
	return s.inSearchMode
}

// GetSearchResults returns the current search results
func (s *Model) GetSearchResults() []directory {
	return s.searchResults
}

// GetCursor returns the current cursor position
func (s *Model) GetCursor() int {
	return s.cursor
}

// ExitSearchMode clears search state and returns to normal mode
func (s *Model) ExitSearchMode() {
	s.inSearchMode = false
	s.searchResults = nil
	s.searchBar.SetValue("")
	if s.searchCoordinator != nil {
		s.searchCoordinator.Clear()
	}
}
