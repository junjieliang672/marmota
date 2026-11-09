package sidebar

import (
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/junjieliang672/marmota/src/internal/search"
)

type directory struct {
	Location string `json:"location"`
	Name     string `json:"name"`
	PinnedAt int64  `json:"pinned_at,omitempty"`
	EditedAt int64  `json:"edited_at,omitempty"`
	// Codex is set for a session row that runs in Codex.
	Codex bool `json:"-"`
	// Remote is set for a session row with Remote Control on.
	Remote bool `json:"-"`
}

// SortInfo holds the active sort state for display in the sidebar.

// fuzzyCache memoises the result of the sidebar's fuzzy search so that
// repeated UpdateDirectories calls with an unchanged search bar value and
// unchanged base directory list (pinned + well-known + external-media)
// short-circuit both the FZF work AND the cost of reloading the base
// lists from disk. See spec-006 task T6.
//
// baseHash is a cheap FNV-1a digest of invalidation signals that can be
// gathered WITHOUT a full pinned-file parse (pinned file path + mtime +
// size, plus the OS-derived well-known list). When the signals match the
// cached value we return the cached result immediately. Only on a miss
// do we pay for pinnedMgr.Load() + external-media enumeration.
type fuzzyCache struct {
	query    string
	baseHash uint64 // hash of cheap invalidation signals for (pinned+wellknown+external-media)
	result   []directory
}

type Model struct {
	directories []directory
	renderIndex int
	cursor      int
	rename      textinput.Model
	renaming    bool
	searchBar   textinput.Model
	pinnedMgr   *PinnedManager
	recentMgr   *RecentEditsManager
	moveState   pinnedMoveState

	// fuzzyCache is a private, single-entry cache used by UpdateDirectories
	// to avoid re-running fuzzySearch on every Bubble Tea tick when the
	// sidebar search bar value is unchanged. See spec-006 T6.
	fuzzyCache *fuzzyCache

	// NEW: Unified search coordinator for recursive FZF search
	searchCoordinator *search.Coordinator
	searchResults     []directory // Cached search results
	inSearchMode      bool        // True when displaying search results

	// Git status for current directory
	gitStatus GitStatus
	// sessions are the live Claude sessions the popup lists after Recent.
	sessions []directory

	// Sort info for current focused panel
}

type pinnedMoveState struct {
	active   bool
	location string
	input    string
	original []directory
	preview  []directory
}
