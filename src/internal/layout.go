package internal

import (
	"slices"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// The screen is a sidebar and, right of it, the preview over the Ask box over
// the Sessions box. The sidebar holds the file panels, one under another,
// each as tall as its files, the last down to Details, which is at its foot,
// as tall as its lines. The Sessions box is as tall as its sessions, the Ask
// box as what is written in it, three rows with nothing, and the preview
// takes the rest.
//
// computeLayout works every box's place out of what each has to show, and
// nothing else: it is the whole of the arithmetic, so it is tested on its own.

const (
	// sidebarWidth holds Details' longest line (the disk's, 38 columns) and
	// file names up to 28 columns.
	sidebarWidth = 40
	// A screen narrower than wideScreen gives the sidebar sidebarShare
	// percent of it, sidebarMinWidth at least.
	wideScreen      = 100
	sidebarShare    = 36
	sidebarMinWidth = 30

	// detailsMaxHeight is Details' height at most: its frame, FILE's four
	// lines, MAC's three, two file operations and the lines between them.
	// detailsShare is the percent of a short screen it takes at most.
	detailsMaxHeight = 13
	detailsMinHeight = 4
	detailsShare     = 24
	// panelFloorHeight is the height a file panel is never under: its
	// frame and one row. panelRoomEach is the height each panel has at least
	// when as many are open as the sidebar takes: four items.
	panelFloorHeight = 3
	panelRoomEach    = 8

	// sessionsShare is the percent of the screen's height the Sessions box
	// takes at most.
	sessionsShare     = 40
	sessionsMinHeight = 3
	// sessionColumn is the width a session's row needs to say everything;
	// a box with room for two of them, and the gap, has two columns.
	sessionColumn    = 64
	sessionColumnGap = 3
	// sessionsOneColumn is how many rows stay in one column whatever the
	// width.
	sessionsOneColumn = 4

	// The Ask box: its frame and a line, up to its frame and askMaxLines of
	// what is written. previewMinHeight is the rows the preview keeps.
	askMinHeight     = 3
	askMaxLines      = 10
	previewMinHeight = 5
)

// rect is a box's place on the screen, borders included. One of no height
// isn't drawn.
type rect struct{ x, y, w, h int }

func (r rect) shown() bool { return r.w > 0 && r.h > 0 }

// contains reports whether the screen cell x, y is in the box.
func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// layoutIn is what the boxes have to show.
type layoutIn struct {
	width, height int // the screen
	statusRows    int // the status line's rows at the foot
	// panelRows is, for each file panel, the rows it wants inside its frame:
	// its path and divider, its search row, and a row an item.
	panelRows []int
	// footer is false while Details is put away (F): the file panels take
	// the sidebar.
	footer       bool
	detailsLines int // the lines Details has
	sessionLines int // the lines the Sessions box has
	// askLines is the lines the Ask box has, 0 while it is put away.
	askLines int
	preview  bool // the preview is open
}

// layout is where every box is.
type layout struct {
	body    int // the rows above the status line
	sidebar rect
	panels  []rect
	details rect
	preview rect
	ask     rect // the Ask box, under the preview
	// sessions is the Sessions box, and sessionCols its columns.
	sessions    rect
	sessionCols int
	// maxPanels is how many file panels the sidebar takes.
	maxPanels int
}

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

func computeLayout(in layoutIn) layout {
	l := layout{body: max(0, in.height-in.statusRows)}
	side := sidebarWidth
	if in.width < wideScreen {
		side = max(sidebarMinWidth, in.width*sidebarShare/100) //nolint:mnd // percent
	}
	side = min(side, in.width)
	l.sidebar = rect{0, 0, side, l.body}

	detailsH := 0
	if in.footer {
		detailsCap := clampInt(l.body*detailsShare/100, detailsMinHeight, detailsMaxHeight) //nolint:mnd // percent
		detailsH = clampInt(in.detailsLines+2, detailsMinHeight, detailsCap)                //nolint:mnd // its frame
	}
	room := max(0, l.body-detailsH)
	l.maxPanels = max(1, room/panelRoomEach)
	wants := make([]int, len(in.panelRows))
	for i, rows := range in.panelRows {
		wants[i] = max(rows, 1) + 2 //nolint:mnd // its frame
	}
	heights := sharePanels(wants, room)
	if len(heights) > 0 {
		// The rows the files don't need stay in the last panel: it goes down
		// to Details.
		used := 0
		for _, h := range heights {
			used += h
		}
		heights[len(heights)-1] += max(0, room-used)
	}
	y := 0
	for _, h := range heights {
		l.panels = append(l.panels, rect{0, y, side, h})
		y += h
	}
	if in.footer {
		l.details = rect{0, l.body - detailsH, side, detailsH}
	}

	right := in.width - side
	l.sessionCols = 1
	// Two columns when the box, without its frame, holds two rows side by side.
	if right-common.BorderPadding >= 2*sessionColumn+sessionColumnGap && in.sessionLines > sessionsOneColumn {
		l.sessionCols = 2
	}
	rows := (max(in.sessionLines, 1) + l.sessionCols - 1) / l.sessionCols
	sessionsCap := max(sessionsMinHeight, l.body*sessionsShare/100)            //nolint:mnd // percent
	sessionsH := min(clampInt(rows+2, sessionsMinHeight, sessionsCap), l.body) //nolint:mnd // its frame
	askH := 0
	if in.askLines > 0 {
		askH = clampInt(in.askLines, 1, askMaxLines) + 2 //nolint:mnd // its frame
	}
	if !in.preview {
		// No preview: the Sessions box has the rows it would take.
		askH = min(askH, max(0, l.body-sessionsMinHeight))
		sessionsH = l.body - askH
	} else if over := sessionsH + askH + previewMinHeight - l.body; over > 0 {
		// A short screen: the Ask box gives rows up, then the Sessions box.
		cut := min(over, max(0, askH-askMinHeight))
		askH -= cut
		sessionsH = max(sessionsMinHeight, sessionsH-(over-cut))
	}
	top := max(0, l.body-sessionsH-askH)
	if in.preview {
		l.preview = rect{side, 0, right, top}
	}
	l.ask = rect{side, top, right, askH}
	l.sessions = rect{side, top + askH, right, l.body - top - askH}
	return l
}

// sharePanels is each file panel's height out of room rows: what its files
// want when they all fit. When they don't, the panels with the fewest files
// have theirs first and the others share the rest evenly.
func sharePanels(wants []int, room int) []int {
	out := make([]int, len(wants))
	order := make([]int, len(wants))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return wants[a] - wants[b] })
	left := room
	for n, i := range order {
		share := left / (len(order) - n)
		out[i] = max(min(wants[i], share), min(panelFloorHeight, wants[i]))
		left -= out[i]
	}
	if left > 0 && len(order) > 0 {
		// Rows the division left over go to the panel that wants the most.
		if last := order[len(order)-1]; wants[last] > out[last] {
			out[last] += min(left, wants[last]-out[last])
		}
	}
	return out
}

// statusRows is the status line's rows at the foot of the screen.
func (m *model) statusRows() int {
	if common.Config.ShowStatusLine && m.statusLineModel.IsEnabled() {
		return m.statusLineModel.GetHeight()
	}
	return 0
}

// sidebarWidthFor is the sidebar's width on a screen width wide.
func sidebarWidthFor(width int) int {
	return computeLayout(layoutIn{width: width}).sidebar.w
}

// layoutInput is what the boxes have to show now.
func (m *model) layoutInput() layoutIn {
	in := layoutIn{
		width: m.fullWidth, height: m.fullHeight, statusRows: m.statusRows(),
		footer: m.toggleFooter, preview: m.fileModel.filePreview.IsOpen(),
		sessionLines: m.dockSessionLineCount(),
	}
	for i := range m.fileModel.filePanels {
		in.panelRows = append(in.panelRows, m.fileModel.filePanels[i].rowsWanted())
	}
	if in.footer {
		in.detailsLines = detailsLineCount(m.detailsInput())
	}
	if in.footer || m.dockAskOpen() {
		in.askLines = m.dockAskLineCount(m.fullWidth - sidebarWidthFor(m.fullWidth))
	}
	return in
}

// rowsWanted is the rows the panel wants inside its frame: its path and
// divider, its search row, and a row for each item, one when it has none.
// While a search narrows the panel it keeps the rows it had, so the boxes
// under it stay where they are as you type.
func (panel *filePanel) rowsWanted() int {
	rows := filePanelChromeRows + max(len(panel.element), 1)
	if panel.showsSearchRow() {
		return max(rows+1, panel.rowsBeforeSearch+1)
	}
	panel.rowsBeforeSearch = rows
	return rows
}

// relayout works out where every box is from what each has to show, and
// gives the boxes that keep their own size theirs. It is run after every
// message. It reports whether the preview's size changed, which is when
// what it shows has to be drawn again.
func (m *model) relayout() bool {
	if len(m.fileModel.filePanels) == 0 {
		return false
	}
	m.lay = computeLayout(m.layoutInput())
	l := m.lay

	m.fileModel.width = max(0, l.sidebar.w-common.BorderPadding)
	m.fileModel.maxFilePanel = min(l.maxPanels, common.FilePanelMax)
	for i := range m.fileModel.filePanels {
		panel := &m.fileModel.filePanels[i]
		panel.searchBar.Width = m.fileModel.width - common.InnerPadding
		panel.handleResize(m.panelRows(i))
	}
	m.mainPanelHeight = m.panelRows(m.filePanelFocusIndex)
	m.footerHeight = max(0, l.details.h-common.BorderPadding)
	m.details.SetDimensions(l.details.w, l.details.h)
	m.setJumpSize()

	pv := &m.fileModel.filePreview
	width, height := 0, l.preview.h
	if pv.IsOpen() {
		width = l.preview.w
	}
	changed := pv.GetWidth() != width || pv.GetHeight() != height
	pv.SetWidth(width)
	pv.SetHeight(height)
	return changed && pv.IsOpen()
}

// panelRows is the rows inside file panel i's frame.
func (m *model) panelRows(i int) int {
	if i < 0 || i >= len(m.lay.panels) {
		return 0
	}
	return max(0, m.lay.panels[i].h-common.BorderPadding)
}
