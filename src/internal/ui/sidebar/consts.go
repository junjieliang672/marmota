package sidebar

// These are effectively consts
// Had to use `var` as go doesn't allows const structs
var pinnedDividerDir = directory{ //nolint: gochecknoglobals // This is more like a const.
	Name:     "",
	Location: "Pinned+-*/=?",
}

var recentDividerDir = directory{ //nolint: gochecknoglobals // This is more like a const.
	Name:     "",
	Location: "Recent+-*/=?",
}

// sessionsDividerDir heads the live Claude sessions, after Recent.
var sessionsDividerDir = directory{ //nolint: gochecknoglobals // This is more like a const.
	Name:     "",
	Location: "Sessions+-*/=?",
}

// SessionLocationPrefix marks a sidebar row that is a Claude session, not a
// path: what follows is the session's ID.
const SessionLocationPrefix = "claude-session:"

var locationsDividerDir = directory{ //nolint: gochecknoglobals // This is more like a const.
	Name:     "",
	Location: "Locations+-*/=?",
}

var diskDividerDir = directory{ //nolint: gochecknoglobals // This is more like a const.
	Name:     "",
	Location: "Disks+-*/=?",
}

// blank line + search bar (removed superfile logo)
const sideBarInitialHeight = 2

// UI dimension constants for sidebar
const (
	// searchBarPadding is the total padding for search bar (borders + prompt + extra char)
	searchBarPadding = 5 // 2 (borders) + 2 (prompt) + 1 (extra char)

	// dividerRenderHeight is the rows a section header takes: a blank line,
	// then the header.
	dividerRenderHeight = 2
)
