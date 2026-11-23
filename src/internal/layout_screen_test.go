package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/sysstat"
)

// The screen as it is drawn, against the layout: layout_test.go checks the
// arithmetic, these that the boxes are drawn where it says.

// screenTestModel is the dock's two sessions on a screen of width × 56, with
// the preview, Details and the Ask box.
func screenTestModel(t *testing.T, width int) *model {
	t.Helper()
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	TeaUpdate(m, tea.WindowSizeMsg{Width: width, Height: 56})
	return m
}

// folderOf makes a folder of n files.
func folderOf(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := range n {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%03d.txt", i)), nil, 0o600))
	}
	return dir
}

// screenLines is the screen, a string a row, every row as wide as the screen.
func screenLines(t *testing.T, m *model) []string {
	t.Helper()
	lines := strings.Split(viewText(m), "\n")
	require.Len(t, lines, m.fullHeight, "a row of the screen each")
	for i, l := range lines {
		require.Equal(t, m.fullWidth, ansi.StringWidth(l), "row %d: %q", i, l)
	}
	return lines
}

// at is what row y shows from column x on.
func at(lines []string, x, y int) string {
	return ansi.Cut(lines[y], x, ansi.StringWidth(lines[y]))
}

func TestTheScreenIsTheSidebarAndThePreviewOverTheSessions(t *testing.T) {
	m := screenTestModel(t, 111)
	lines := screenLines(t, m)
	l := m.lay
	require.NoError(t, m.validateLayout())

	panel := m.getFocusedFilePanel()
	assert.Equal(t, l.body-l.details.h, l.panels[0].h, "the one file panel has the sidebar's rows above Details")
	assert.Equal(t, 40, l.sidebar.w)
	assert.True(t, strings.HasPrefix(lines[0], "╭─ "+filepath.Base(panel.location)), lines[0])
	assert.Equal(t, l.details.y, l.panels[0].h, "and goes down to Details")
	assert.True(t, strings.HasPrefix(at(lines, l.ask.x, l.ask.y), "╭─ Ask → "), "the Ask box under the preview: %q",
		lines[l.ask.y])
	assert.Equal(t, rect{l.preview.x, l.preview.h, l.preview.w, askMinHeight}, l.ask)
	assert.Equal(t, l.ask.y+l.ask.h, l.sessions.y, "over the Sessions box")
	assert.True(t, strings.HasPrefix(lines[l.details.y], "╭─ Details "), "Details at the sidebar's foot: %q",
		lines[l.details.y])
	assert.Equal(t, l.body, l.details.y+l.details.h)

	assert.True(t, strings.HasPrefix(at(lines, l.preview.x, 0), "╭─ Preview "), "the preview right of the sidebar")
	assert.Contains(t, at(lines, l.sessions.x, l.sessions.y), "Sessions ─", "the Sessions box under the preview")
	assert.Equal(t, len(m.dockSessions())+common.BorderPadding, l.sessions.h, "a row a session")
	assert.Equal(t, l.body, l.sessions.y+l.sessions.h)
	assert.Contains(t, at(lines, l.sessions.x, l.sessions.y+1), "provider registry")
	assert.Contains(t, at(lines, l.sessions.x, l.sessions.y+2), "ime hold tests")
}

func TestASessionsRowSaysWhatItDoesAndAnIdleOneWhatYouAskedIt(t *testing.T) {
	m := screenTestModel(t, 111)
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == "s50" {
			m.claude.snapshot.Live[i].LastPrompt = "hold the input\nmethod in ASCII"
		}
	}
	idle, ok := m.findSession("s50")
	require.True(t, ok)
	require.Equal(t, sessionsui.Idle, m.sessionState(idle))
	assert.Equal(t, "you: hold the input method in ASCII", m.dockDoing(idle, sessionsui.Idle))
	assert.Equal(t, "waiting for your next prompt",
		m.dockDoing(claudesession.Session{ID: "new"}, sessionsui.Idle), "nothing asked yet")

	var row string
	for _, line := range strings.Split(ansi.Strip(sessionsBoxRender(m)), "\n") {
		if strings.Contains(line, "ime hold tests") {
			row = line
		}
	}
	assert.Regexp(t, `2 \S cc ime hold tests +you: hold the input method in ASCII`, row,
		"title, then what it says, in one row")
}

// What a session does starts in one column, after the widest title. A row
// whose text doesn't fit there starts it further left, after its own title,
// rather than cut it.
func TestALongRowBorrowsTheRoomAfterItsTitle(t *testing.T) {
	m := screenTestModel(t, 111)
	now := time.Now()
	long := "offline · redteam-root · arch/agent-swarm" // 41 columns
	row := func(title string) string {
		s := claudesession.Session{ID: title, Title: title, Named: true,
			App: &claudesession.AppSession{Doing: long, Offline: true}}
		return ansi.Strip(m.dockRow(7, 11, s, false, 69, 17, now))
	}

	// The room after the prefix, a space and the cloud at the end is 57:
	// the column leaves 57 - 17 - 2 = 38 of it, three short of the text.
	short := row("b1-swarm")
	assert.Contains(t, short, "b1-swarm"+strings.Repeat(" ", 17-len("b1-swarm")+2-3)+long+" ",
		"whole, three columns further left: %q", short)
	assert.Equal(t, 69, ansi.StringWidth(short))

	wide := row("b2-implementation")
	assert.Contains(t, wide, "b2-implementation  offline · redteam-root · arch/agent",
		"its title fills the column: %q", wide)
	assert.Contains(t, wide, "…", "so its text is cut")
	assert.Equal(t, 69, ansi.StringWidth(wide))

	fits := ansi.Strip(m.dockRow(7, 11, claudesession.Session{ID: "b2", Title: "b2", Named: true,
		App: &claudesession.AppSession{Doing: "marmota · dev"}}, false, 69, 17, now))
	assert.Contains(t, fits, "b2"+strings.Repeat(" ", 17)+"marmota · dev",
		"text that fits stays in the column: %q", fits)
}

func TestFilePanelsAreOneUnderAnother(t *testing.T) {
	m := screenTestModel(t, 111)
	one := m.lay
	require.NoError(t, m.createNewFilePanel(folderOf(t, 3)))
	TeaUpdate(m, nil)
	lines := screenLines(t, m)
	l := m.lay
	require.NoError(t, m.validateLayout())

	require.Len(t, l.panels, 2)
	first := m.fileModel.filePanels[0]
	assert.Equal(t, max(len(first.element), 1)+filePanelChromeRows+common.BorderPadding, l.panels[0].h,
		"the first is as tall as its items, one row when it has none")
	assert.Equal(t, l.panels[0].h, l.panels[1].y, "the second under it")
	assert.Equal(t, l.details.y, l.panels[1].y+l.panels[1].h, "and down to Details, with the rows no file needs")
	assert.True(t, strings.HasPrefix(lines[l.panels[1].y], "╭─ "), lines[l.panels[1].y])
	assert.Equal(t, one.preview, l.preview, "the preview stays as it is")
	assert.Equal(t, one.sessions, l.sessions, "and so does the Sessions box")
	assert.Equal(t, one.details, l.details)
	assert.Equal(t, one.ask, l.ask, "and the Ask box")
	assert.Equal(t, 1, m.filePanelFocusIndex, "the new panel has the keyboard")
	assert.Equal(t, l.panels[1].h-common.BorderPadding, m.mainPanelHeight, "and its rows are what the keys move in")
}

func TestAsManyFilePanelsAsHaveEightRowsEach(t *testing.T) {
	m := screenTestModel(t, 111)
	limit := m.fileModel.maxFilePanel
	require.Equal(t, m.lay.maxPanels, limit)
	dir := folderOf(t, 40)
	for len(m.fileModel.filePanels) < limit {
		require.NoError(t, m.createNewFilePanel(dir))
		TeaUpdate(m, nil)
	}
	require.Error(t, m.createNewFilePanel(dir), "one more than the sidebar takes")
	screenLines(t, m)
	require.NoError(t, m.validateLayout())
	for i, p := range m.lay.panels[1:] {
		assert.GreaterOrEqual(t, p.h, panelRoomEach, "panel %d, of 40 files, shows four at least", i+1)
	}
}

func TestAFilePanelSaysHowManyOfItsItemsShow(t *testing.T) {
	m := screenTestModel(t, 111)
	require.NoError(t, m.createNewFilePanel(folderOf(t, 200)))
	TeaUpdate(m, nil)
	lines := screenLines(t, m)
	big := m.lay.panels[1]
	shown := big.h - common.BorderPadding - filePanelChromeRows
	require.Less(t, shown, 200)
	assert.Contains(t, lines[big.y+big.h-1], fmt.Sprintf(" %d/200 ", shown), "on its bottom edge")
	assert.NotContains(t, lines[m.lay.panels[0].h-1], "/", "a panel that shows all its items says nothing")
}

// Whichever box has the keyboard, its frame is the one focus color, and no
// other box's is: a file panel, the Sessions box, the Ask box, Details, the
// s list.
func TestTheBoxWithTheKeyboardHasTheFocusColor(t *testing.T) {
	ownAskHistory(t)
	withTheme(t, func(th *common.ThemeType) {
		th.FooterBorderActive, th.FilePanelBorderActive = "#00ff00", "#ff0000"
		th.FooterBorder, th.FilePanelBorder = "#111111", "#222222"
	})
	m := screenTestModel(t, 111)
	focus := sgr(lipgloss.NewStyle().Foreground(common.FocusBorderColor()))
	require.Equal(t, "38;2;0;255;0", focus, "the theme's footer_border_active")
	// lit is the boxes whose top edge is drawn in the focus color, by title.
	lit := func() []string {
		m.relayout()
		var out []string
		for _, line := range strings.Split(m.View(), "\n") {
			pieces := strings.Split(line, "╭")
			for i, piece := range pieces[1:] {
				// The corner is drawn in the color set last before it.
				before := pieces[i]
				if at := strings.LastIndex(before, "\x1b["); at >= 0 && strings.Contains(before[at:], focus) {
					out = append(out, strings.Fields(ansi.Strip(piece))[1])
				}
			}
		}
		return out
	}
	folder := filepath.Base(m.getFocusedFilePanel().location)

	assert.Equal(t, []string{folder}, lit(), "the files: the file panel")
	bang(m)
	assert.Equal(t, []string{"✳"}, lit(), "!: the Sessions box alone")
	pressKey(m, common.Hotkeys.OpenAskBar[0])
	assert.Equal(t, []string{"Ask"}, lit(), "K: the Ask box alone")
	pressKey(m, "esc")
	pressKey(m, "esc")
	pressKey(m, common.Hotkeys.FocusOnMetaData[0])
	assert.Equal(t, []string{"Details"}, lit(), "M: Details alone")
	pressKey(m, common.Hotkeys.FocusOnMetaData[0])
	pressKey(m, common.Hotkeys.GoTo[0])
	assert.Equal(t, []string{"go"}, lit(), "s: its list")
	assert.NotContains(t, m.View(), "38;2;255;0;0m╭", "no frame in the file panel's own active color any more")
}

// Marmota starts with the boxes where they will be: the sessions, the file's
// facts and the machine's readings each arrive a moment after the first
// frame, and none of them moves a box.
func TestNoBoxMovesWhileTheFirstDataArrives(t *testing.T) {
	setForTest(t, &appSessionsAutoRefresh, true) // claude.ai is asked, and hasn't answered
	m := screenTestModel(t, 111)
	for i := range 4 {
		m.claude.snapshot.Live = append(m.claude.snapshot.Live, claudesession.Session{
			ID: fmt.Sprintf("more%d", i), Title: fmt.Sprintf("more %d", i), Live: true,
			TTY: "/dev/ttys020", Cwd: "/repo",
		})
	}
	m.claude.loaded, m.app.answered = true, true
	m.refreshSessionsList()
	listed := m.claude.snapshot
	sessions := m.dockSessionLineCount()
	require.Greater(t, sessions, dockNoSessionsLines)
	runCmd(t, m, m.rememberDockRows()) // what the last run left
	data, err := os.ReadFile(variable.SessionsRowsFile)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(sessions), strings.TrimSpace(string(data)),
		"the box's lines are kept for the next start")
	t.Cleanup(func() { _ = os.Remove(variable.SessionsRowsFile) })

	// The next start: nothing has arrived yet.
	m.dock.rows, m.dock.rowsRead = 0, false
	m.claude.loaded, m.claude.snapshot, m.app.answered = false, claudesession.Snapshot{}, false
	m.macStats = nil
	m.refreshSessionsList()
	first := screenLines(t, m)
	start := m.lay
	assert.Equal(t, sessions+common.BorderPadding, start.sessions.h, "the Sessions box is as tall as last time")
	assert.Contains(t, at(first, start.sessions.x, start.sessions.y+1), "Listing the sessions…")
	assert.Equal(t, detailsFileLines+1+detailsMacLines+common.BorderPadding, start.details.h,
		"Details has FILE's and MAC's lines before either is read")

	// This computer's sessions, then the machine's readings, then claude.ai.
	m.claude.loaded, m.claude.snapshot = true, listed
	m.refreshSessionsList()
	screenLines(t, m)
	assert.Equal(t, start, m.lay, "this computer's sessions are listed: nothing moves")
	m.macStats = &sysstat.Stats{CPUOK: true}
	screenLines(t, m)
	assert.Equal(t, start, m.lay, "the machine is read: nothing moves")
	TeaUpdate(m, appSessionsMsg{err: errors.New("offline")})
	require.True(t, m.sessionsListed())
	screenLines(t, m)
	assert.Equal(t, sessions+1, m.dockSessionLineCount(), "claude.ai couldn't be asked: a line says so")
}

func TestWritingInTheAskBoxMovesNothing(t *testing.T) {
	ownAskHistory(t)
	m := screenTestModel(t, 111)
	before := m.lay

	pressKey(m, common.Hotkeys.OpenAskBar[0])
	lines := screenLines(t, m)
	l := m.lay
	require.NoError(t, m.validateLayout())

	assert.Equal(t, before, l, "K: every box is where it was")
	top := at(lines, l.ask.x, l.ask.y)
	assert.True(t, strings.HasPrefix(top, "╭─ Ask → "), top)
	assert.Contains(t, at(lines, l.ask.x, l.ask.y+1), "› ", "its line is the one written in")
	assert.Equal(t, l.preview.h, m.fileModel.filePreview.GetHeight(), "the preview is told its height")
	assert.Contains(t, lines[len(lines)-1], "enter", "the status line has its keys")

	pressKey(m, "esc")
	assert.Equal(t, before, m.lay, "closed, everything is where it was")
}

func TestFPutsDetailsAndTheAskBoxAway(t *testing.T) {
	m := screenTestModel(t, 111)
	before := m.lay

	pressKey(m, common.Hotkeys.ToggleFooter[0])
	lines := screenLines(t, m)
	l := m.lay
	require.NoError(t, m.validateLayout())

	assert.False(t, l.ask.shown())
	assert.False(t, l.details.shown())
	assert.Equal(t, l.body, l.panels[0].h, "the file panel takes the sidebar")
	assert.NotContains(t, strings.Join(lines, "\n"), "─ Details ─")
	assert.NotContains(t, strings.Join(lines, "\n"), "─ Ask → ")
	assert.Equal(t, before.sessions, l.sessions, "the Sessions box stays")
	assert.Equal(t, before.preview.h+before.ask.h, l.preview.h, "the preview has the Ask box's rows")

	pressKey(m, common.Hotkeys.OpenAskBar[0])
	require.NoError(t, m.validateLayout())
	assert.Equal(t, before.ask, m.lay.ask, "written in, the Ask box is back for as long")
	assert.False(t, m.toggleFooter, "Details stays away")
	pressKey(m, "esc")
	assert.Equal(t, l, m.lay, "and away again")

	bang(m)
	assert.False(t, m.toggleFooter, "the Sessions box doesn't need them back")
	assert.Equal(t, dockSessionsFocus, m.focusPanel)
}

func TestTheWholeWindowHasTwoColumnsOfSessions(t *testing.T) {
	m := screenTestModel(t, 223)
	for i := range 11 {
		m.claude.snapshot.Live = append(m.claude.snapshot.Live, claudesession.Session{
			ID: fmt.Sprintf("more%d", i), Title: fmt.Sprintf("more %d", i), Live: true,
			TTY: "/dev/ttys020", Cwd: "/repo",
		})
	}
	m.refreshSessionsList()
	TeaUpdate(m, nil)
	sessions := m.dockSessions()
	require.Greater(t, len(sessions), sessionsOneColumn)
	lines := screenLines(t, m)
	l := m.lay

	assert.Equal(t, 2, l.sessionCols)
	assert.Equal(t, (len(sessions)+1)/2+common.BorderPadding, l.sessions.h, "half the rows")
	first := at(lines, l.sessions.x, l.sessions.y+1)
	assert.Contains(t, first, sessions[0].DisplayTitle())
	assert.Contains(t, first, sessions[(len(sessions)+1)/2].DisplayTitle(), "the second column beside the first")
}

func TestTheSListTakesTheSidebar(t *testing.T) {
	m := screenTestModel(t, 111)
	before := m.lay

	pressKey(m, common.Hotkeys.GoTo[0])
	lines := screenLines(t, m)

	assert.True(t, strings.HasPrefix(lines[0], "╭─ go to "), lines[0])
	assert.Equal(t, before, m.lay, "nothing right of it moves")
	assert.NotContains(t, strings.Join(lines, "\n"), "─ Details ─", "it has the sidebar's rows")
	assert.Equal(t, "╰", string([]rune(lines[m.lay.body-1])[0]), "down to the status line")
}
