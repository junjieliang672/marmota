package internal

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
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

// s opens its popup from the Sessions box too; it goes to folders, so the
// files have the keyboard behind it. Closed without going anywhere, it gives
// the keyboard back to the Sessions box, on the same session.
func TestSOpensItsPopupFromTheSessionsBox(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	require.Equal(t, dockSessionsFocus, m.focusPanel)
	on := m.sessions.list.SelectedID()

	pressKey(m, common.Hotkeys.GoTo[0])
	assert.True(t, m.jumpUI.IsOpen(), "s: the go to popup")
	pressKey(m, "esc")
	assert.False(t, m.jumpUI.IsOpen())
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "esc: back to the Sessions box")
	assert.Equal(t, on, m.sessions.list.SelectedID())

	pressKey(m, "esc")
	require.Equal(t, nonePanelFocus, m.focusPanel)
	pressKey(m, common.Hotkeys.GoTo[0])
	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "opened from the files, it goes back to them")
	assert.True(t, m.getFocusedFilePanel().isFocused)
}

// A PDF's page, a picture or a video's frame is an image the terminal keeps
// on the screen until it is told to clear it: the session preview clears
// it, or it shows over the session, and so does a hidden preview.
func TestTheSessionPreviewClearsTheFilesImage(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WezTerm")
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	clear := "\x1b_Ga=d\x1b\\"

	bang(m)
	require.True(t, m.previewShowsASession())
	assert.Contains(t, m.View(), clear)

	// Hidden, the preview draws nothing but that.
	pressKey(m, "esc")
	m.fileModel.filePreview.SetContent("a page")
	m.toggleFilePreviewPanel()
	require.False(t, m.fileModel.filePreview.IsOpen())
	view := m.View()
	assert.NotContains(t, view, "a page")
	assert.Contains(t, view, clear)
}

func TestBracketsStepThroughTheSessions(t *testing.T) {
	m := dockTestModel(t)
	first, second := m.dockSessions()[0].ID, m.dockSessions()[1].ID
	next := func() { pressKey(m, common.Hotkeys.NextSession[0]) }
	previous := func() { pressKey(m, common.Hotkeys.PreviousSession[0]) }

	next()
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "the Sessions box has the keyboard")
	assert.Equal(t, first, m.sessions.list.SelectedID(), "never in the box yet: the first")
	next()
	assert.Equal(t, second, m.sessions.list.SelectedID())
	next()
	assert.Equal(t, first, m.sessions.list.SelectedID(), "round from the last to the first")
	previous()
	assert.Equal(t, second, m.sessions.list.SelectedID(), "and back")

	pressKey(m, "esc")
	require.Equal(t, nonePanelFocus, m.focusPanel)
	previous()
	assert.Equal(t, dockSessionsFocus, m.focusPanel)
	assert.Equal(t, first, m.sessions.list.SelectedID(), "from the files: before the one the box was last on")
}

func TestBracketsInASessionsFilesGoThroughTheDiff(t *testing.T) {
	m, _ := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	require.NotNil(t, m.touchedLayer())

	pressKey(m, common.Hotkeys.DiffNextChange[0])
	pressKey(m, common.Hotkeys.DiffPreviousChange[0])

	assert.Equal(t, nonePanelFocus, m.focusPanel, "the keys stayed with the diff")
	assert.NotNil(t, m.touchedLayer())
}

func TestTheSessionsBoxMarksRemoteControl(t *testing.T) {
	nerdfont := common.Config.Nerdfont
	t.Cleanup(func() { common.Config.Nerdfont = nerdfont })
	common.Config.Nerdfont = false
	m := dockTestModel(t)
	remote := m.dockSessions()[0]
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == remote.ID {
			m.claude.snapshot.Live[i].RemoteControl = true
		}
	}
	m.refreshSessionsList()

	box := ansi.Strip(m.dockSessionsRender())
	var marked []string
	for _, line := range strings.Split(box, "\n") {
		if strings.Contains(line, " rc ") {
			marked = append(marked, line)
		}
	}
	require.Len(t, marked, 1, "one row is marked:\n%s", box)
	assert.Contains(t, marked[0], remote.DisplayTitle(), "the row of the session with Remote Control")
}

func TestQuestionMarkOpensTheHelpFromTheSessionsBox(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	on := m.sessions.list.SelectedID()

	pressKey(m, common.Hotkeys.OpenHelpMenu[0])
	require.True(t, m.helpMenu.open)
	pressKey(m, common.Hotkeys.ListDown[0])
	assert.Equal(t, on, m.sessions.list.SelectedID(), "the help takes the keys while it is open")

	pressKey(m, "esc")
	pressKey(m, "esc")
	assert.False(t, m.helpMenu.open)
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "closed, it gives the keys back to the Sessions box")
	pressKey(m, common.Hotkeys.ListDown[0])
	assert.NotEqual(t, on, m.sessions.list.SelectedID())
}

func TestThePromptOpensFromTheSessionsBox(t *testing.T) {
	m := dockTestModel(t)
	bang(m)

	pressKey(m, common.Hotkeys.OpenAgentPrompt[0])

	assert.True(t, m.promptModal.IsOpen())
}

func TestPCopiesTheSessionsFolder(t *testing.T) {
	m := dockTestModel(t)
	var copied string
	setForTest(t, &writeClipboard, func(text string) error {
		copied = text
		return nil
	})
	bang(m)
	s, ok := m.selectedSession()
	require.True(t, ok)

	pressKey(m, common.Hotkeys.CopyPath[0])

	assert.Equal(t, s.Cwd, copied)
}

func TestYPutsTheFilesASessionChangedOnTheShelf(t *testing.T) {
	if runtime.GOOS != utils.OsDarwin {
		t.Skip("the shelf is macOS only")
	}
	m, dir := sessionFilesModel(t)
	setForTest(t, &common.Config.ShelfApp, "")
	setForTest(t, &shelfBinary, func() (string, error) { return "/bin/y-shelf", nil })
	var got []string
	setForTest(t, &startShelf, func(path string, args ...string) error {
		got = append([]string{path}, args...)
		return nil
	})
	require.Nil(t, m.focusDockSessionsOn(m.dockSessions()[0].ID))

	cmd := m.dockSessionsKey(utils.TeaKeyMsg(common.Hotkeys.SendToShelf[0]))
	require.NotNil(t, cmd)
	m.applyShelfSent(cmd().(shelfSentMsg))

	assert.Equal(t, []string{"/bin/y-shelf", filepath.Join(dir, "new.go"), filepath.Join(dir, "src", "edited.go")}, got,
		"the files it changed that are still there, as one pile")
	assert.Equal(t, "2 items → the shelf", m.claude.flash)
}
