package internal

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// followTestModel is a screen with four sessions on this computer: s20
// beside Marmota, and s50, s60 and s70 in panes of other tabs, idle, s50 the
// latest to do something. The Sessions box has the keyboard, on s20.
func followTestModel(t *testing.T) *model {
	t.Helper()
	m := appTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	TeaUpdate(m, tea.WindowSizeMsg{Width: 160, Height: 56})
	now := time.Now()
	m.claude.panes = append(m.claude.panes,
		weztermPane{WindowID: 3, TabID: 8, PaneID: 60, TTYName: "/dev/ttys060"},
		weztermPane{WindowID: 3, TabID: 9, PaneID: 70, TTYName: "/dev/ttys070"})
	m.claude.snapshot.Live = append(m.claude.snapshot.Live,
		claudesession.Session{ID: "s60", Title: "wheel in a cell", Live: true, TTY: "/dev/ttys060", Cwd: "/repo"},
		claudesession.Session{ID: "s70", Title: "names at the foot", Live: true, TTY: "/dev/ttys070", Cwd: "/repo"})
	for i := range m.claude.snapshot.Live {
		s := &m.claude.snapshot.Live[i]
		s.Status = ""
		s.LastActivity = now.Add(-time.Duration(i+1) * time.Hour)
	}
	m.sessionsChanged("")
	toSessions(m)
	require.True(t, m.sessions.list.Select("s20"))
	require.Equal(t, "s20", m.visibleSessionID(), "s20 is beside Marmota")
	return m
}

// setStatus gives session id a status, and lists the sessions anew.
func setStatus(m *model, id string, status claudesession.Status) {
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == id {
			m.claude.snapshot.Live[i].Status = status
		}
	}
	m.sessionsChanged("")
}

func enterOnTheSessionBeside(t *testing.T, m *model) {
	t.Helper()
	installFakeExec(t, fakeExecResult{retCode: 0, output: ""})
	pressKey(m, "enter")
}

// previewRows is the preview's rows as the screen draws them.
func previewRows(t *testing.T, m *model) []string {
	t.Helper()
	pv := &m.fileModel.filePreview
	lines := screenLines(t, m)[:pv.GetHeight()]
	for i := range lines {
		lines[i] = ansi.Cut(lines[i], m.fullWidth-pv.GetWidth(), m.fullWidth)
	}
	return lines
}

// Enter brings a session beside Marmota, where you read it: the preview
// goes on to the session most worth seeing of the others on this computer,
// and never to one on another computer, whose conversation it can't show.
func TestEnterSendsThePreviewToTheSessionMostWorthSeeing(t *testing.T) {
	t.Run("an idle one, the latest to do something, when nothing else stands out", func(t *testing.T) {
		m := followTestModel(t)
		enterOnTheSessionBeside(t, m)
		assert.Equal(t, "s50", m.sessions.list.SelectedID())
	})
	t.Run("one working before an idle one", func(t *testing.T) {
		m := followTestModel(t)
		setStatus(m, "s70", claudesession.StatusWorking)
		enterOnTheSessionBeside(t, m)
		assert.Equal(t, "s70", m.sessions.list.SelectedID())
	})
	t.Run("one that replied unread before one working", func(t *testing.T) {
		m := followTestModel(t)
		setStatus(m, "s70", claudesession.StatusWorking)
		m.claude.unread = map[string]bool{"s60": true}
		enterOnTheSessionBeside(t, m)
		assert.Equal(t, "s60", m.sessions.list.SelectedID())
		assert.True(t, m.claude.unread["s60"], "shown on its own, it is still unread")
	})
	t.Run("one that needs you before all", func(t *testing.T) {
		m := followTestModel(t)
		m.claude.unread = map[string]bool{"s60": true}
		setStatus(m, "s70", claudesession.StatusWaiting)
		enterOnTheSessionBeside(t, m)
		assert.Equal(t, "s70", m.sessions.list.SelectedID())
	})
	t.Run("with no other session on this computer it stays", func(t *testing.T) {
		m := followTestModel(t)
		m.claude.snapshot.Live = m.claude.snapshot.Live[:1]
		m.sessionsChanged("")
		require.NotEmpty(t, m.appSessions(), "one on another computer needs you")
		enterOnTheSessionBeside(t, m)
		assert.Equal(t, "s20", m.sessions.list.SelectedID())
	})
}

// While the keyboard is away, the preview goes to a session of a higher
// tier than the one it shows, and to no other.
func TestThePreviewFollowsWhileTheKeyboardIsAway(t *testing.T) {
	m := followTestModel(t)
	enterOnTheSessionBeside(t, m)
	require.Equal(t, "s50", m.sessions.list.SelectedID())

	setStatus(m, "s60", claudesession.StatusWaiting)
	assert.Equal(t, "s50", m.sessions.list.SelectedID(), "not while Marmota has the keyboard")
	setStatus(m, "s60", "")

	TeaUpdate(m, tea.BlurMsg{})
	setStatus(m, "s70", claudesession.StatusWorking)
	assert.Equal(t, "s70", m.sessions.list.SelectedID(), "an idle session gives way to one working")
	setStatus(m, "s60", claudesession.StatusWaiting)
	assert.Equal(t, "s60", m.sessions.list.SelectedID(), "and that to one that needs you")
	setStatus(m, "s50", claudesession.StatusWaiting)
	assert.Equal(t, "s60", m.sessions.list.SelectedID(), "not to another of its own tier")

	t.Run("a pinned preview stays", func(t *testing.T) {
		m := followTestModel(t)
		require.True(t, m.sessions.list.Select("s50"))
		m.dock.pinned = true
		TeaUpdate(m, tea.BlurMsg{})
		setStatus(m, "s60", claudesession.StatusWaiting)
		assert.Equal(t, "s50", m.sessions.list.SelectedID())
	})
}

// Several sessions with something to show have a cell each while the
// keyboard is away, in the Sessions box's order; back in Marmota the
// preview is one session, the one most worth seeing.
func TestThePreviewIsCutIntoCellsWhileTheKeyboardIsAway(t *testing.T) {
	m := followTestModel(t)
	enterOnTheSessionBeside(t, m)
	setStatus(m, "s60", claudesession.StatusWorking)
	setStatus(m, "s70", claudesession.StatusWorking)
	cells, _ := m.previewCells()
	require.Empty(t, cells, "one preview while Marmota has the keyboard")

	TeaUpdate(m, tea.BlurMsg{})
	cells, more := m.previewCells()
	require.Len(t, cells, 2, "the two working; the idle one and the one beside Marmota have none")
	assert.Equal(t, []string{"s60", "s70"}, []string{cells[0].ID, cells[1].ID})
	assert.Zero(t, more)

	rows := previewRows(t, m)
	half := len(rows) / 2
	assert.Contains(t, rows[half-1], "wheel in a cell", "the first cell's name on its bottom edge")
	assert.Contains(t, rows[len(rows)-1], "names at the foot", "the second's on the preview's last row")
	assert.True(t, strings.HasPrefix(rows[half], "╭"), "a frame each: %q", rows[half])
	number := 1 + slices.IndexFunc(m.dockSessions(), func(s claudesession.Session) bool { return s.ID == "s60" })
	assert.Contains(t, rows[half-1], fmt.Sprintf(" %d wheel in a cell", number), "with its number in the Sessions box")
	assert.NotEqual(t, m.cellColor("s60"), m.cellColor("s70"), "and a color of its own")
	assert.Equal(t, m.cellColor("s60"), m.follow.marks["s60"], "which its row in the Sessions box has too")

	t.Run("a cell keeps its place and its color when another session changes", func(t *testing.T) {
		color := m.cellColor("s70")
		setStatus(m, "s70", claudesession.StatusWaiting)
		cells, _ := m.previewCells()
		assert.Equal(t, []string{"s60", "s70"}, []string{cells[0].ID, cells[1].ID})
		assert.Equal(t, color, m.cellColor("s70"))
		assert.Equal(t, "s70", m.sessions.list.SelectedID(), "the Sessions box is on the one that needs you")
	})

	TeaUpdate(m, tea.FocusMsg{})
	cells, _ = m.previewCells()
	assert.Empty(t, cells, "one preview again")
	assert.Equal(t, "s70", m.sessions.list.SelectedID(), "the session that needs you")
	rows = previewRows(t, m)
	assert.Contains(t, rows[len(rows)-1], "names at the foot")
	assert.True(t, strings.HasPrefix(rows[half], "│"), "no cell's frame: %q", rows[half])
}

// A preview too short for a cell each shows those most worth seeing, and
// says how many more there are.
func TestCellsThatHaveNoRoomAreCounted(t *testing.T) {
	m := followTestModel(t)
	enterOnTheSessionBeside(t, m)
	for _, id := range []string{"s50", "s60"} {
		setStatus(m, id, claudesession.StatusWorking)
	}
	setStatus(m, "s70", claudesession.StatusWaiting)
	pv := &m.fileModel.filePreview
	for height := 56; pv.GetHeight() >= 3*cellMinRows; height-- {
		TeaUpdate(m, tea.WindowSizeMsg{Width: 160, Height: height})
	}
	require.GreaterOrEqual(t, pv.GetHeight(), 2*cellMinRows, "a preview of two cells' rows")
	TeaUpdate(m, tea.BlurMsg{})

	cells, more := m.previewCells()
	require.Len(t, cells, 2, "two cells of eight rows at least")
	assert.ElementsMatch(t, []string{"s50", "s70"}, []string{cells[0].ID, cells[1].ID},
		"the one that needs you, and of the two working the latest to do something")
	assert.Equal(t, 1, more)
	rows := previewRows(t, m)
	assert.Contains(t, rows[len(rows)-1], "+1 more")
}

// The wheel scrolls the cell under the pointer, and no other. A click on a
// cell picks the session the preview goes to when the keyboard comes back,
// scrolled as its cell was.
func TestTheWheelScrollsOneCellAndAClickPicksIt(t *testing.T) {
	m := followTestModel(t)
	for i := range m.claude.snapshot.Live {
		m.claude.snapshot.Live[i].TranscriptPath = longTranscript(t)
	}
	enterOnTheSessionBeside(t, m)
	setStatus(m, "s60", claudesession.StatusWorking)
	setStatus(m, "s70", claudesession.StatusWorking)
	TeaUpdate(m, tea.BlurMsg{})
	cells, _ := m.previewCells()
	require.Len(t, cells, 2)
	pv := &m.fileModel.filePreview
	x, half := m.fullWidth-pv.GetWidth()+5, pv.GetHeight()/2

	wheel(m, tea.MouseButtonWheelUp, x, half+3)
	assert.Equal(t, wheelLines, m.follow.scroll["s70"], "the cell under the pointer")
	assert.Zero(t, m.follow.scroll["s60"], "the other stays")
	rows := previewRows(t, m)
	assert.Contains(t, rows[len(rows)-1], "↑ 3 lines", "a scrolled cell says so")
	assert.Contains(t, strings.Join(rows[:half], "\n"), "answer 29", "the other is on its last lines")
	assert.NotContains(t, strings.Join(rows[half:], "\n"), "answer 29", "which this one scrolled away")
	wheel(m, tea.MouseButtonWheelUp, x, 3)
	wheel(m, tea.MouseButtonWheelUp, x, 3)
	assert.Equal(t, 2*wheelLines, m.follow.scroll["s60"])
	assert.Equal(t, wheelLines, m.follow.scroll["s70"])

	t.Run("a scrolled cell stays on what it shows when its session writes on", func(t *testing.T) {
		screenLines(t, m)
		require.NotEqual(t, "s70", m.sessions.list.SelectedID())
		c := m.cellConvo("s70")
		c.turns = append(slices.Clone(c.turns), claudesession.Turn{Prompt: "one more", Reply: "and its answer"})
		m.follow.convos["s70"] = c
		before := m.follow.scroll["s70"]
		screenLines(t, m)
		assert.Greater(t, m.follow.scroll["s70"], before)
	})

	scrolled := m.follow.scroll["s70"]
	TeaUpdate(m, tea.MouseMsg{X: x, Y: half + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	TeaUpdate(m, tea.FocusMsg{})
	assert.Equal(t, "s70", m.sessions.list.SelectedID(), "the cell clicked, not the latest to do something")
	assert.Equal(t, scrolled, m.sessions.scroll, "scrolled as its cell was")

	t.Run("a click that arrives after the keyboard is back picks its cell too", func(t *testing.T) {
		TeaUpdate(m, tea.BlurMsg{})
		TeaUpdate(m, tea.FocusMsg{})
		require.Equal(t, "s60", m.sessions.list.SelectedID(), "the latest to do something")
		TeaUpdate(m, tea.MouseMsg{X: x, Y: half + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		assert.Equal(t, "s70", m.sessions.list.SelectedID())
		assert.False(t, m.sessions.selection.on, "and selects nothing")
	})
}
