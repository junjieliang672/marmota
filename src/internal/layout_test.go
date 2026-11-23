package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The numbers are the plan's pictures: a pane of 111×56 beside a session and
// a window of 223×56, with a status line.

// screen is a folder of files files, 13 sessions, Details' ten lines and the
// Ask box with nothing written in it.
func screen(width int, files ...int) layoutIn {
	in := layoutIn{width: width, height: 56, statusRows: 1, footer: true, preview: true,
		detailsLines: 10, sessionLines: 13, askLines: 1}
	for _, f := range files {
		in.panelRows = append(in.panelRows, f+filePanelChromeRows)
	}
	return in
}

func heights(rs []rect) []int {
	out := make([]int, len(rs))
	for i, r := range rs {
		out[i] = r.h
	}
	return out
}

func TestLayoutOfThePaneBesideASession(t *testing.T) {
	l := computeLayout(screen(111, 18))
	assert.Equal(t, []rect{{0, 0, 40, 43}}, l.panels, "the panel goes down to Details")
	assert.Equal(t, rect{0, 43, 40, 12}, l.details)
	assert.Equal(t, rect{40, 0, 71, 37}, l.preview)
	assert.Equal(t, rect{40, 37, 71, 3}, l.ask, "the Ask box: its frame and a line, under the preview")
	assert.Equal(t, rect{40, 40, 71, 15}, l.sessions, "a row a session")
	assert.Equal(t, 1, l.sessionCols)
}

func TestLayoutWithFewSessionsGivesThePreviewTheirRows(t *testing.T) {
	in := screen(111, 18)
	in.sessionLines = 3
	l := computeLayout(in)
	assert.Equal(t, rect{40, 50, 71, 5}, l.sessions)
	assert.Equal(t, rect{40, 47, 71, 3}, l.ask)
	assert.Equal(t, rect{40, 0, 71, 47}, l.preview)
}

func TestLayoutOfAFolderWithManyFiles(t *testing.T) {
	l := computeLayout(screen(111, 138))
	assert.Equal(t, []int{43}, heights(l.panels), "39 files show")
	assert.Equal(t, rect{0, 43, 40, 12}, l.details)
}

func TestLayoutStacksFilePanels(t *testing.T) {
	for name, tc := range map[string]struct {
		files []int
		want  []int
	}{
		"both fit: the last has the rows left": {[]int{18, 6}, []int{22, 21}},
		"neither fits":                         {[]int{138, 16}, []int{23, 20}},
		"three":                                {[]int{18, 15, 138}, []int{14, 14, 15}},
		"four":                                 {[]int{18, 6, 3, 138}, []int{13, 10, 7, 13}},
		"an empty folder":                      {[]int{0, 18}, []int{4, 39}},
		"the small ones first":                 {[]int{138, 2}, []int{37, 6}},
	} {
		l := computeLayout(screen(111, tc.files...))
		assert.Equal(t, tc.want, heights(l.panels), name)
		assert.Equal(t, rect{40, 0, 71, 37}, l.preview, "%s: the preview stays as it is", name)
		assert.Equal(t, rect{40, 37, 71, 3}, l.ask, name)
		assert.Equal(t, rect{40, 40, 71, 15}, l.sessions, name)
		assert.Equal(t, rect{0, 43, 40, 12}, l.details, name)
	}
	assert.Equal(t, 5, computeLayout(screen(111, 18)).maxPanels, "43 rows, eight a panel")
}

func TestLayoutWhileTheAskBoxIsWrittenIn(t *testing.T) {
	in := screen(111, 18)
	idle := computeLayout(in)
	in.askLines = 1 // a line written
	assert.Equal(t, idle, computeLayout(in), "a line written: every box is where it was")

	in.askLines = 3
	l := computeLayout(in)
	assert.Equal(t, rect{40, 0, 71, 35}, l.preview, "a row each line more, of the preview's")
	assert.Equal(t, rect{40, 35, 71, 5}, l.ask)
	assert.Equal(t, rect{40, 40, 71, 15}, l.sessions)
	assert.Equal(t, idle.panels, l.panels, "the sidebar stays as it is")

	in.askLines = 30
	assert.Equal(t, 12, computeLayout(in).ask.h, "ten lines at most")
}

func TestLayoutOfTheWholeWindow(t *testing.T) {
	in := screen(223, 18)
	in.detailsLines = 7 // no file operations
	l := computeLayout(in)
	assert.Equal(t, []rect{{0, 0, 40, 46}}, l.panels)
	assert.Equal(t, rect{0, 46, 40, 9}, l.details, "Details is as tall as its lines")
	assert.Equal(t, 2, l.sessionCols, "the sessions have two columns")
	assert.Equal(t, rect{40, 46, 183, 9}, l.sessions)
	assert.Equal(t, rect{40, 43, 183, 3}, l.ask)
	assert.Equal(t, rect{40, 0, 183, 43}, l.preview)

	in.panelRows = []int{18 + filePanelChromeRows, 138 + filePanelChromeRows}
	l = computeLayout(in)
	assert.Equal(t, []int{22, 24}, heights(l.panels))
	assert.Equal(t, rect{40, 0, 183, 43}, l.preview, "the preview stays as it is")
}

func TestLayoutShowsTwentySessionsAtMost(t *testing.T) {
	in := screen(111, 18)
	in.sessionLines = 27
	l := computeLayout(in)
	assert.Equal(t, 22, l.sessions.h, "40% of the 55 rows")
	assert.Equal(t, 30, l.preview.h)
}

func TestLayoutWithDetailsAndAskPutAway(t *testing.T) {
	in := screen(111, 18, 6)
	in.footer, in.askLines = false, 0
	l := computeLayout(in)
	assert.Equal(t, []int{22, 33}, heights(l.panels), "the last panel goes down to the foot")
	assert.False(t, l.ask.shown())
	assert.False(t, l.details.shown())
	assert.Equal(t, rect{40, 0, 71, 40}, l.preview, "the preview has the Ask box's rows")
	assert.Equal(t, rect{40, 40, 71, 15}, l.sessions, "the Sessions box stays")

	in.askLines = 1 // K: it is written in
	l = computeLayout(in)
	assert.Equal(t, rect{40, 37, 71, 3}, l.ask, "written in, it is back for as long")
	assert.Equal(t, []int{22, 33}, heights(l.panels))
}

func TestLayoutWithoutThePreview(t *testing.T) {
	in := screen(111, 18)
	in.preview = false
	l := computeLayout(in)
	assert.False(t, l.preview.shown())
	assert.Equal(t, rect{40, 0, 71, 3}, l.ask)
	assert.Equal(t, rect{40, 3, 71, 52}, l.sessions, "the Sessions box has the preview's rows")
	in.askLines = 4
	l = computeLayout(in)
	assert.Equal(t, rect{40, 0, 71, 6}, l.ask)
	assert.Equal(t, rect{40, 6, 71, 49}, l.sessions)
}

func TestLayoutOfANarrowScreen(t *testing.T) {
	l := computeLayout(screen(80, 18))
	assert.Equal(t, 30, l.sidebar.w, "36% is 28: thirty at least")
	assert.Equal(t, 35, computeLayout(screen(99, 18)).sidebar.w)
	assert.Equal(t, 40, computeLayout(screen(100, 18)).sidebar.w)
}

// Whatever there is to show, the boxes cover the screen above the status
// line, each cell once.
func TestLayoutCoversTheScreenOnce(t *testing.T) {
	for _, width := range []int{60, 80, 111, 140, 223} {
		for _, height := range []int{24, 30, 40, 56, 80} {
			for _, files := range [][]int{{0}, {3}, {18}, {500}, {18, 6}, {300, 300}, {1, 2, 3, 400}} {
				in := layoutIn{width: width, height: height, statusRows: 1, detailsLines: 10}
				for _, f := range files {
					in.panelRows = append(in.panelRows, f+filePanelChromeRows)
				}
				requireCoveredWhateverShows(t, in)
			}
		}
	}
}

// requireCoveredWhateverShows checks in with few and many sessions, and with
// Details, the preview and the Ask box each there or not, the Ask box with
// a line and with several.
func requireCoveredWhateverShows(t *testing.T, in layoutIn) {
	t.Helper()
	for _, sessions := range []int{0, 1, 5, 13, 60} {
		for _, shown := range [][2]bool{{true, true}, {false, true}, {true, false}, {false, false}} {
			for _, ask := range []int{0, 1, 5, 40} {
				in.sessionLines, in.footer, in.preview, in.askLines = sessions, shown[0], shown[1], ask
				requireCovered(t, in)
			}
		}
	}
}

func requireCovered(t *testing.T, in layoutIn) {
	t.Helper()
	l := computeLayout(in)
	boxes := append([]rect{l.details, l.preview, l.ask, l.sessions}, l.panels...)
	seen := make([]int, in.width*l.body)
	for _, b := range boxes {
		if !b.shown() {
			continue
		}
		require.GreaterOrEqual(t, b.x, 0, "%+v: %+v", in, b)
		require.GreaterOrEqual(t, b.y, 0, "%+v: %+v", in, b)
		require.LessOrEqual(t, b.x+b.w, in.width, "%+v: %+v", in, b)
		require.LessOrEqual(t, b.y+b.h, l.body, "%+v: %+v", in, b)
		for y := b.y; y < b.y+b.h; y++ {
			for x := b.x; x < b.x+b.w; x++ {
				seen[y*in.width+x]++
			}
		}
	}
	for i, n := range seen {
		require.Equal(t, 1, n, "%+v: the cell %d, %d", in, i%in.width, i/in.width)
	}
	for i, p := range l.panels {
		require.GreaterOrEqual(t, p.h, panelFloorHeight, "%+v: panel %d", in, i)
	}
}
