package sidebar

import (
	"hash/fnv"
	"log/slog"
	"slices"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
)

// Rename file where the cursor is located
func (s *Model) PinnedItemRename() {
	pinnedBegin, pinnedEnd := s.pinnedIndexRange()
	// We have not selected a pinned directory, rename is not allowed
	if s.cursor < pinnedBegin || s.cursor > pinnedEnd {
		return
	}

	nameLen := len(s.directories[s.cursor].Name)
	cursorPos := nameLen

	s.renaming = true
	s.rename = common.GeneratePinnedRenameTextInput(cursorPos, s.directories[s.cursor].Name)
}

// Cancel rename pinned directory
func (s *Model) CancelSidebarRename() {
	s.rename.Blur()
	s.renaming = false
}

// Confirm rename pinned directory
func (s *Model) ConfirmSidebarRename() {
	itemLocation := s.directories[s.cursor].Location
	newItemName := s.rename.Value()
	// This is needed to update the current pinned directory data loaded into memory
	s.directories[s.cursor].Name = newItemName

	// recover the state of rename
	s.CancelSidebarRename()

	pinnedDirs := s.pinnedMgr.Load()
	// Call getPinnedDirectories, instead of using what is stored in sidebar.directories
	// sidebar.directories could have less directories in case a search filter is used
	for i := range pinnedDirs {
		// Considering the situation when many
		if pinnedDirs[i].Location == itemLocation {
			pinnedDirs[i].Name = newItemName
		}
	}

	if err := s.pinnedMgr.Save(pinnedDirs); err != nil {
		slog.Error("error saving pinned directories", "error", err)
	}
}

// UpdateState handles the sidebar's state updates
func (s *Model) UpdateState(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch {
	case s.moveState.active:
		return nil
	case s.renaming:
		s.rename, cmd = s.rename.Update(msg)
	case s.searchBar.Focused():
		s.searchBar, cmd = s.searchBar.Update(msg)
	}

	if s.cursor < 0 {
		s.cursor = 0
	}
	return cmd
}

// HandleSearchBarKey handles key events for the sidebar search bar
func (s *Model) HandleSearchBarKey(msg string) tea.Cmd {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		s.SearchBarBlur()
		s.searchBar.SetValue("")
		s.UpdateDirectories()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		s.SearchBarBlur()
		s.resetCursor()
	}
	return nil
}

// UpdateDirectories updates the directories list based on search value.
//
// Because the Bubble Tea model calls this on every tick (see
// model.updateModelStateAfterMsg), a non-empty search bar used to force a
// fresh fuzzySearch on every keypress anywhere in the app. To avoid that,
// we keep a single-entry cache keyed on (query, baseHash) — baseHash is a
// cheap FNV-1a digest (see hashSidebarInvalidationSignal) that lets us
// skip reloading the pinned file + re-enumerating external media when
// nothing relevant has changed. See spec-006 T6.
// SessionEntry is a live session for the sidebar to list, Claude Code's or
// Codex's.
type SessionEntry struct {
	ID, Title string
	Codex     bool
	Remote    bool // Remote Control is on
}

// SetSessions sets the live sessions listed after Recent.
func (s *Model) SetSessions(entries []SessionEntry) {
	s.sessions = s.sessions[:0]
	for _, e := range entries {
		s.sessions = append(s.sessions, directory{Name: e.Title, Location: SessionLocationPrefix + e.ID,
			Codex: e.Codex, Remote: e.Remote})
	}
}

// withSessions adds the live sessions matching query ("" for all) between
// Pinned and Recent.
func (s *Model) withSessions(dirs []directory, query string) []directory {
	sessions := s.sessions
	if query != "" {
		sessions = fuzzySearch(query, sessions)
	}
	if len(sessions) == 0 {
		return dirs
	}
	at := slices.Index(dirs, recentDividerDir) // above Recent
	if at == -1 {
		at = len(dirs)
	}
	return slices.Concat(dirs[:at], []directory{sessionsDividerDir}, sessions, dirs[at:])
}

// sessionsSignal changes when the listed sessions do, for the search cache.
func (s *Model) sessionsSignal() uint64 {
	h := fnv.New64a()
	for _, d := range s.sessions {
		_, _ = h.Write([]byte(d.Location + "\x00" + d.Name + "\x00" + strconv.FormatBool(d.Codex) + "\x00" +
			strconv.FormatBool(d.Remote) + "\x00"))
	}
	return h.Sum64()
}

func (s *Model) UpdateDirectories() {
	if s.moveState.active {
		s.reapplyPinnedPreview()
		return
	}

	query := s.searchBar.Value()
	if query == "" {
		s.directories = s.withSessions(getDirectories(s.pinnedMgr, s.recentMgr), "")
		// Drop any stale cache so the next non-empty query recomputes cleanly.
		s.fuzzyCache = nil
		if s.isCursorInvalid() {
			s.resetCursor()
		}
		return
	}

	baseHash := hashSidebarInvalidationSignal(s.pinnedMgr, s.recentMgr) ^ s.sessionsSignal()

	if s.fuzzyCache != nil &&
		s.fuzzyCache.query == query &&
		s.fuzzyCache.baseHash == baseHash {
		slog.Debug("sidebar fuzzy cache hit",
			"operation", "sidebar_fuzzy_cache_hit",
			"query", query,
			"base_count", len(s.fuzzyCache.result),
		)
		s.directories = s.fuzzyCache.result
		if s.isCursorInvalid() {
			s.resetCursor()
		}
		return
	}

	result := s.withSessions(getFilteredDirectories(query, s.pinnedMgr, s.recentMgr), query)
	s.directories = result
	s.fuzzyCache = &fuzzyCache{
		query:    query,
		baseHash: baseHash,
		result:   result,
	}

	// This is needed, as due to filtering, the cursor might be invalid
	if s.isCursorInvalid() {
		s.resetCursor()
	}
}

// UpdateDirectoriesWithQueryForTest is a TEST-ONLY helper that sets the
// sidebar's search-bar value to `query` (via the underlying textinput
// model) and runs UpdateDirectories. It exists solely to exercise the
// UpdateDirectories caching path in sidebar_perf_test.go without pulling
// in the full Model construction (which depends on XDG directories).
//
// Do NOT use this in production code.
func (s *Model) UpdateDirectoriesWithQueryForTest(query string) {
	s.searchBar.SetValue(query)
	s.UpdateDirectories()
}

func (s *Model) TogglePinnedDirectory(dir string) error {
	return s.pinnedMgr.Toggle(dir)
}

func (s *Model) RecordRecentEdit(filePath string) error {
	if s.recentMgr == nil {
		return nil
	}
	return s.recentMgr.Add(filePath)
}

func (s *Model) RemoveRecentEdit(filePath string) error {
	if s.recentMgr == nil {
		return nil
	}
	return s.recentMgr.Remove(filePath)
}

// SetGitStatus updates the git status displayed in sidebar
func (s *Model) SetGitStatus(status GitStatus) {
	s.gitStatus = status
}

// GetGitStatus returns the current git status
func (s *Model) GetGitStatus() GitStatus {
	return s.gitStatus
}

// ClearGitStatusCache clears the cached git status to force a refresh
func (s *Model) ClearGitStatusCache() {
	s.gitStatus.LastPath = ""
}

// New creates a new sidebar model with the given parameters
func New() Model {
	// pinnedMgr is created here, can be done higher up in the call chain
	pinnedMgr := NewPinnedFileManager(variable.PinnedFile)
	recentMgr := NewRecentEditsManager(variable.RecentEditsFile)
	res := Model{
		renderIndex: 0,
		directories: getDirectories(&pinnedMgr, &recentMgr),
		searchBar:   common.GenerateSearchBar(),
		pinnedMgr:   &pinnedMgr,
		recentMgr:   &recentMgr,
	}

	// Excluding borders(2), Searchbar Prompt(2), and one extra character than is appended
	// by searchBar.View()
	res.searchBar.Width = common.SidebarWidth - searchBarPadding
	res.searchBar.Placeholder = "(" + common.Hotkeys.SearchBar[0] + ")" + " Search"
	return res
}
