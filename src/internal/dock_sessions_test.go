package internal

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// dockTestModel is sessionsTestModel with the dock on screen: s20 working
// in /repo, s50 idle elsewhere, both in WezTerm panes.
func dockTestModel(t *testing.T) *model {
	t.Helper()
	m := sessionsTestModel(t)
	if !m.toggleFooter {
		m.toggleFooterController()
	}
	m.refreshSessionsList()
	require.Len(t, m.dockSessions(), 2)
	return m
}

func bang(m *model) { pressKey(m, common.Hotkeys.JumpToUnreadSession[0]) }

func TestBangLandsOnTheNewestSessionThatNeedsYou(t *testing.T) {
	m := dockTestModel(t)
	now := time.Now()
	for i := range m.claude.snapshot.Live {
		m.claude.snapshot.Live[i].Status = claudesession.StatusWaiting
	}
	m.claude.waitingSince = map[string]time.Time{"s20": now.Add(-10 * time.Minute), "s50": now.Add(-time.Minute)}

	bang(m)

	assert.Equal(t, dockSessionsFocus, m.focusPanel)
	assert.Equal(t, "s50", m.sessions.list.SelectedID(), "s50 started needing you most recently")
}

func TestBangElseTheLastSessionElseTheFirst(t *testing.T) {
	m := dockTestModel(t)
	first := m.dockSessions()[0].ID

	bang(m)
	assert.Equal(t, first, m.sessions.list.SelectedID(), "nothing waits and none was visited: the first")

	pressKey(m, common.Hotkeys.ListDown[0])
	second := m.sessions.list.SelectedID()
	require.NotEqual(t, first, second)

	bang(m)
	assert.Equal(t, nonePanelFocus, m.focusPanel, "! again goes back to the files")
	assert.True(t, m.getFocusedFilePanel().isFocused)

	bang(m)
	assert.Equal(t, second, m.sessions.list.SelectedID(), "the session you were last on")
}

func TestDockJumpsByNumber(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	ids := m.dockSessions()

	pressKey(m, "2")
	pressKey(m, common.Hotkeys.ListTop[0])
	assert.Equal(t, ids[1].ID, m.sessions.list.SelectedID(), "2g")

	pressKey(m, common.Hotkeys.ListTop[0])
	assert.Equal(t, ids[0].ID, m.sessions.list.SelectedID(), "g")

	pressKey(m, common.Hotkeys.ListBottom[0])
	assert.Equal(t, ids[1].ID, m.sessions.list.SelectedID(), "G")

	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "esc goes back too")
}

func TestDockShowsSessionsAndThePreviewTheSelectedOne(t *testing.T) {
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()

	screen := ansi.Strip(m.View())
	assert.Contains(t, screen, "Sessions")
	assert.Contains(t, screen, "provider registry")
	assert.Contains(t, screen, "ime hold tests")

	bang(m)
	sel, ok := m.selectedSession()
	require.True(t, ok)
	preview := ansi.Strip(m.dockSessionPreview(m.fileModel.filePreview.GetWidth(),
		m.fileModel.filePreview.GetHeight()))
	assert.Contains(t, preview, sel.DisplayTitle(), "the preview shows the session")

	bang(m)
	assert.NotContains(t, ansi.Strip(m.filePreviewPanelRender()), "Ask bar",
		"back in the files, the preview is the file's again")
}

func TestBangWithNoSessions(t *testing.T) {
	m := dockTestModel(t)
	m.claude.snapshot.Live = nil

	bang(m)

	assert.Equal(t, nonePanelFocus, m.focusPanel)
	assert.Contains(t, m.claude.flash, "No live sessions")
}

func TestTheIdleAskBoxSaysWhereYourNextMessageGoes(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	first := m.dockSessions()[0]
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == first.ID {
			m.claude.snapshot.Live[i].TranscriptPath = longTranscript(t)
			m.claude.snapshot.Live[i].Status = claudesession.StatusIdle
		}
	}

	box := ansi.Strip(m.dockAskRender(40))

	assert.Contains(t, box, "→ "+first.DisplayTitle(), "nothing chosen yet: the first session")
	assert.Contains(t, box, common.Hotkeys.OpenAskBar[0]+" write")
	assert.Contains(t, box, "“question 29”", "your last message to it")
	assert.NotContains(t, box, "answer 29", "not its answer: the preview has that")
	assert.Contains(t, box, "cc Claude Code", "the program it runs in; its model and effort aren't known")
	assert.Contains(t, box, "› question 28", "your messages before fill the room left")
	assert.Len(t, strings.Split(box, "\n"), m.footerHeight+common.BorderPadding, "within the box")
}

func TestKFromTheSessionsBoxWritesToTheSelectedSession(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	bang(m)
	pressKey(m, common.Hotkeys.ListDown[0])
	second := m.sessions.list.SelectedID()

	pressKey(m, common.Hotkeys.OpenAskBar[0])

	require.Equal(t, dockAskFocus, m.focusPanel)
	assert.Equal(t, second, m.sessions.list.SelectedID())
	assert.Contains(t, ansi.Strip(m.View()), "Ask → ")
	assert.Empty(t, m.opsStatus(), "no operations, so nothing on the status line")

	pressKey(m, "esc")
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "esc goes back to the Sessions box it came from")
	assert.Equal(t, second, m.sessions.list.SelectedID(), "on the same session")

	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel)
	assert.Equal(t, second, m.dockAskTarget(), "and the next K writes to the same session")
}

func TestTheOpenAskBoxListsYourQuestions(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	first := m.dockSessions()[0]
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == first.ID {
			m.claude.snapshot.Live[i].TranscriptPath = longTranscript(t)
			m.claude.snapshot.Live[i].Status = claudesession.StatusIdle
		}
	}
	m.footerHeight = 10

	pressKey(m, common.Hotkeys.OpenAskBar[0])
	require.Equal(t, first.ID, m.sessions.list.SelectedID())
	lines := strings.Split(ansi.Strip(m.dockAskRender(100)), "\n")
	box := strings.Join(lines, "\n")

	newest := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "› question 29") })
	older := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "› question 28") })
	require.GreaterOrEqual(t, newest, 0, box)
	assert.Greater(t, older, newest, "newest first")
	assert.Contains(t, lines[newest], "replied")
	assert.NotContains(t, box, "answer 29", "no answers: the preview shows them")
}

func TestEscFromTheAskBoxGoesBackToTheFiles(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	pressKey(m, common.Hotkeys.OpenAskBar[0])
	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "opened from the files, esc goes back to them")
}

// s and z open their popups from the Sessions box too; the popups go to
// folders, so the files have the keyboard behind them.
func TestSAndZOpenTheirPopupsFromTheSessionsBox(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	require.Equal(t, dockSessionsFocus, m.focusPanel)

	pressKey(m, common.Hotkeys.FocusOnSidebar[0])
	assert.Equal(t, sidebarFocus, m.focusPanel, "s: the sidebar popup")
	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "esc: the files")

	bang(m)
	pressKey(m, common.Hotkeys.OpenZoxide[0])
	assert.True(t, m.zoxideModal.IsOpen(), "z: the zoxide popup")
	assert.Equal(t, nonePanelFocus, m.focusPanel)
}
