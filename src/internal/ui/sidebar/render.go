package sidebar

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/ui"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/rendering"
)

// Render returns the rendered sidebar string
func (s *Model) Render(mainPanelHeight int, sidebarFocussed bool, currentFilePanelLocation string,
	footer ...string) string {
	slog.Debug("Rendering sidebar.", "cursor", s.cursor,
		"renderIndex", s.renderIndex, "dirs count", len(s.directories),
		"sidebar focused", sidebarFocussed)

	r := ui.PopupRenderer(
		mainPanelHeight+common.BorderPadding+len(footer),
		common.SidebarWidth+common.BorderPadding, firstKey(common.Hotkeys.FocusOnSidebar), "sidebar")
	s.searchBar.Width = common.SidebarWidth - searchBarPadding

	if s.searchBar.Focused() || s.searchBar.Value() != "" || sidebarFocussed {
		r.AddLines(s.searchBar.View())
	}

	if s.NoActualDir() {
		r.AddLines(common.SideBarNoneText)
	} else {
		s.directoriesRender(mainPanelHeight, currentFilePanelLocation, sidebarFocussed, r)
	}

	// The sort order is on each file panel's edge; footer lines (the popup's
	// path line) go under the list.
	if len(footer) > 0 {
		for range max(0, mainPanelHeight-r.ContentLines()) {
			r.AddLines("")
		}
		r.AddLines(footer...)
	}
	return r.Render()
}

func (s *Model) directoriesRender(mainPanelHeight int, curFilePanelFileLocation string,
	sideBarFocussed bool, r *rendering.Renderer) {
	// Cursor should always point to a valid directory at this point
	if s.isCursorInvalid() {
		slog.Error("Unexpected situation in sideBar Model. "+
			"Cursor is at invalid position, while there are valid directories", "cursor", s.cursor,
			"directory count", len(s.directories))
	}

	// TODO : This is not true when searchbar is not rendered(totalHeight is 2, not 3),
	// so we end up underutilizing one line for our render. But it wont break anything.
	totalHeight := sideBarInitialHeight
	numbering := s.numbering()
	for i := s.renderIndex; i < len(s.directories); i++ {
		requiredHeight := s.directories[i].RequiredHeight()

		// Allow rendering if it fits exactly, not just if there's space remaining
		if totalHeight+requiredHeight > mainPanelHeight {
			// Log when truncating to help debug
			if i == s.renderIndex {
				slog.Warn("Sidebar truncated - insufficient height",
					"mainPanelHeight", mainPanelHeight,
					"requiredHeight", requiredHeight,
					"totalHeight", totalHeight)
			}
			break
		}

		totalHeight += requiredHeight

		switch s.directories[i] {
		case pinnedDividerDir:
			r.AddLines("", common.SideBarPinnedDivider)
		case recentDividerDir:
			r.AddLines("", common.SideBarRecentDivider)
		case sessionsDividerDir:
			r.AddLines("", common.SideBarSessionsDivider)
		default:
			if s.renaming && s.cursor == i {
				r.AddLines(s.rename.View())
			} else {
				line := s.directoryLine(i, numbering, curFilePanelFileLocation, sideBarFocussed)
				r.AddLineWithCustomTruncate(line, rendering.TailsTruncateRight)
			}
		}
	}
}

// sidebarNumbering is which of the sidebar's rows are numbered, and how
// wide the numbers are.
type sidebarNumbering struct {
	start, end, width int
}

func (s *Model) numbering() sidebarNumbering {
	start, end := s.indexedItemsRange()
	count := 0
	if start <= end {
		count = end - start + 1
	}
	return sidebarNumbering{start: start, end: end, width: len(strconv.Itoa(max(count, 1)))}
}

// directoryLine is the row of the sidebar's directory i: the cursor, its
// number, and its name, highlighted when the file panel shows it or it is
// being moved.
func (s *Model) directoryLine(i int, n sidebarNumbering, curFilePanelFileLocation string,
	sideBarFocussed bool) string {
	picked := s.cursor == i && sideBarFocussed && !s.searchBar.Focused()
	st := common.PopupStyles(picked)
	cursor := " "
	if picked && !common.PopupHasBar() {
		cursor = icon.Cursor // a theme without a bar: the cursor marks the row
	}
	renderStyle := st.Text
	session := strings.HasPrefix(s.directories[i].Location, SessionLocationPrefix)
	switch {
	case picked:
		renderStyle = st.Picked
	case session:
		renderStyle = st.Session.Foreground(common.AgentColor(s.directories[i].Codex))
	case s.directories[i].Location == curFilePanelFileLocation:
		renderStyle = common.SidebarSelectedStyle.Background(st.Text.GetBackground())
	}
	name := s.directories[i].Name
	if s.IsMovingPinnedLocation(s.directories[i].Location) {
		renderStyle = common.SidebarSelectedStyle
		name += s.MovingPinnedLabelSuffix(s.directories[i].Location)
	}
	lineNumber := ""
	if n.start <= i && i <= n.end && !s.directories[i].IsDivider() {
		lineNumber = fmt.Sprintf("%*d ", n.width, sidebarIndexedDisplayNumber(s.directories, i))
	}
	tag := ""
	if session { // cc or co: which program the session runs in
		tag = common.AgentTag(s.directories[i].Codex, renderStyle.GetBackground()) + renderStyle.Render(" ")
	}
	line := st.Text.Render(cursor+" ") + st.Faint.Render(lineNumber) + tag + renderStyle.Render(name)
	if pad := common.SidebarWidth - ansi.StringWidth(line); picked && pad > 0 {
		line += st.Text.Render(strings.Repeat(" ", pad)) // the bar spans the row
	}
	return line
}

// firstKey is the first of a hotkey's keys, for a title.
func firstKey(keys []string) string {
	for _, k := range keys {
		if k != "" {
			return k
		}
	}
	return ""
}
