package sidebar

type directory struct {
	Location string `json:"location"`
	Name     string `json:"name"`
	PinnedAt int64  `json:"pinned_at,omitempty"`
	EditedAt int64  `json:"edited_at,omitempty"`
}

// Model keeps the pinned folders and files, the files edited lately, and
// the git status of the focused panel's folder. The s list
// (src/internal/ui/jump) lists the first two; the file rows and Details
// show the third.
type Model struct {
	pinnedMgr *PinnedManager
	recentMgr *RecentEditsManager
	gitStatus GitStatus
}
