package internal

import (
	"os"
	"path/filepath"
	"runtime"
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

// ! goes to a session that needs you, and again to the next one, round at
// the end; it never takes you back to the files.
func TestBangGoesToEachSessionThatNeedsYou(t *testing.T) {
	m := dockTestModel(t)
	for i := range m.claude.snapshot.Live {
		m.claude.snapshot.Live[i].Status = claudesession.StatusWaiting
	}
	m.refreshSessionsList()
	ids := m.dockSessions()

	bang(m)
	assert.Equal(t, dockSessionsFocus, m.focusPanel)
	assert.Equal(t, ids[0].ID, m.sessions.list.SelectedID(), "the first that needs you")
	bang(m)
	assert.Equal(t, ids[1].ID, m.sessions.list.SelectedID(), "again: the next")
	bang(m)
	assert.Equal(t, ids[0].ID, m.sessions.list.SelectedID(), "round at the end")
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "still in the Sessions box")

	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "esc goes back to the files")
	bang(m)
	assert.Equal(t, ids[0].ID, m.sessions.list.SelectedID(), "from the files, the first that needs you again")
}

// With nothing that needs you, ! goes to the first session, wherever the
// box was.
func TestBangElseTheFirstSession(t *testing.T) {
	m := dockTestModel(t)
	first := m.dockSessions()[0].ID

	bang(m)
	assert.Equal(t, first, m.sessions.list.SelectedID())
	pressKey(m, common.Hotkeys.ListDown[0])
	require.NotEqual(t, first, m.sessions.list.SelectedID())

	bang(m)
	assert.Equal(t, first, m.sessions.list.SelectedID(), "in the box too")
	assert.Equal(t, dockSessionsFocus, m.focusPanel)

	// One needs you: ! goes there, and stays there.
	waiting := m.dockSessions()[1].ID
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == waiting {
			m.claude.snapshot.Live[i].Status = claudesession.StatusWaiting
		}
	}
	m.refreshSessionsList()
	bang(m)
	assert.Equal(t, waiting, m.sessions.list.SelectedID())
	bang(m)
	assert.Equal(t, waiting, m.sessions.list.SelectedID())
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

	pressKey(m, "esc")
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
	m.fileModel.filePreview.SetOpen(true)
	first := m.dockSessions()[0]
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == first.ID {
			m.claude.snapshot.Live[i].TranscriptPath = longTranscript(t)
			m.claude.snapshot.Live[i].Status = claudesession.StatusIdle
		}
	}

	m.relayout()
	lines := strings.Split(ansi.Strip(m.dockAskRender()), "\n")
	box := strings.Join(lines, "\n")

	require.Len(t, lines, 3, "its frame and one line")
	assert.Equal(t, askMinHeight, m.lay.ask.h)
	assert.Contains(t, lines[0], "Ask → "+first.DisplayTitle(), "nothing chosen yet: the first session")
	assert.Contains(t, lines[1], "“question 29”", "your last message to it")
	assert.Contains(t, lines[2], "cc Claude Code", "the program it runs in; its model and effort aren't known")
	assert.Contains(t, lines[2], common.Hotkeys.OpenAskBar[0]+" write")
	assert.NotContains(t, box, "answer 29", "not its answer: the preview has that")
	assert.NotContains(t, box, "question 28", "nor what you asked before: the preview has that too")
	assert.Equal(t, m.lay.preview.w, ansi.StringWidth(lines[0]), "as wide as the preview")
	assert.Equal(t, m.lay.preview.h, m.lay.ask.y, "right under it")
	assert.Equal(t, m.lay.ask.y+m.lay.ask.h, m.lay.sessions.y, "and over the Sessions box")
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

// K writes in the Ask box where it is: no box moves or changes size for
// it. The box grows a row with each line more of what is written, on rows
// of the preview's.
func TestKWritesInTheAskBoxWhereItIs(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	first := m.dockSessions()[0]
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == first.ID {
			m.claude.snapshot.Live[i].TranscriptPath = longTranscript(t)
			m.claude.snapshot.Live[i].Status = claudesession.StatusIdle
		}
	}
	m.relayout()
	before := m.lay

	pressKey(m, common.Hotkeys.OpenAskBar[0])
	require.Equal(t, first.ID, m.sessions.list.SelectedID())
	assert.Equal(t, before.ask, m.lay.ask, "the Ask box is where it was")
	assert.Equal(t, before.preview, m.lay.preview, "and so is the preview")
	assert.Equal(t, before.sessions, m.lay.sessions)
	lines := strings.Split(ansi.Strip(m.dockAskRender()), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "Ask → "+first.DisplayTitle())
	assert.Contains(t, lines[1], "› ", "the line is the one you write in")
	assert.NotContains(t, lines[2], "write", "its edge no longer says the key that opens it")
	assert.NotContains(t, strings.Join(lines, "\n"), "question 2", "what you asked before stays in the preview")

	typeInto(m, "one line")
	assert.Equal(t, before.preview, m.lay.preview, "a line of draft: nothing moves")
	typeInto(m, strings.Repeat(" and a good deal more", 4))
	draft := m.dockAskLineCount(m.lay.ask.w)
	require.Greater(t, draft, 1, "the draft wraps")
	assert.Equal(t, draft+2, m.lay.ask.h, "a row each line")
	assert.Equal(t, before.preview.h-(draft-1), m.lay.preview.h, "on rows of the preview's")
	assert.Len(t, strings.Split(ansi.Strip(m.dockAskRender()), "\n"), m.lay.ask.h)

	typeInto(m, strings.Repeat(" and still more to say about it", 40))
	assert.Equal(t, askMaxLines+2, m.lay.ask.h, "ten lines at most; the draft scrolls in them")
}

// The keys of the files that act on no file work while the Sessions box has
// the keyboard: those that change what the screen shows leave it there.
func TestTheSessionsBoxTakesTheKeysThatChangeTheScreen(t *testing.T) {
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	bang(m)
	require.Equal(t, dockSessionsFocus, m.focusPanel)
	selected := m.sessions.list.SelectedID()

	pressKey(m, common.Hotkeys.ToggleFooter[0])
	assert.False(t, m.toggleFooter, "F puts Details and the Ask box away")
	assert.False(t, m.lay.details.shown())
	assert.False(t, m.lay.ask.shown())
	pressKey(m, common.Hotkeys.ToggleFooter[0])
	assert.True(t, m.lay.details.shown(), "and brings them back")

	pressKey(m, common.Hotkeys.ToggleFilePreviewPanel[0])
	assert.False(t, m.fileModel.filePreview.IsOpen(), "ctrl+f closes the preview")
	assert.Equal(t, m.lay.body-m.lay.ask.h, m.lay.sessions.h, "the Sessions box has its rows")
	pressKey(m, common.Hotkeys.ToggleFilePreviewPanel[0])
	assert.True(t, m.fileModel.filePreview.IsOpen())

	dots := m.toggleDotFile
	pressKey(m, common.Hotkeys.ToggleDotFile[0])
	assert.NotEqual(t, dots, m.toggleDotFile, ". shows or hides the dot files")

	pressKey(m, common.Hotkeys.BackToEditor[0])
	assert.Contains(t, m.claude.flash, "No editor is put away", "b is the editor's key here too")

	assert.Equal(t, dockSessionsFocus, m.focusPanel, "the keyboard stayed in the Sessions box")
	assert.Equal(t, selected, m.sessions.list.SelectedID(), "on the same session")
}

// The keys that go to another box take the keyboard there from the Sessions
// box.
func TestTheSessionsBoxLetsGoForTheKeysThatGoElsewhere(t *testing.T) {
	m := dockTestModel(t)
	require.NoError(t, m.createNewFilePanel(t.TempDir()))
	require.Equal(t, 1, m.filePanelFocusIndex)

	bang(m)
	pressKey(m, common.Hotkeys.FocusOnMetaData[0])
	assert.Equal(t, metadataFocus, m.focusPanel, "M: Details")

	bang(m)
	pressKey(m, common.Hotkeys.NextFilePanel[0])
	assert.Equal(t, nonePanelFocus, m.focusPanel, "tab: the files")
	assert.Equal(t, 0, m.filePanelFocusIndex, "the next panel")
	assert.True(t, m.getFocusedFilePanel().isFocused)

	bang(m)
	pressKey(m, common.Hotkeys.PreviousFilePanel[0])
	assert.Equal(t, nonePanelFocus, m.focusPanel)
	assert.Equal(t, 1, m.filePanelFocusIndex, "the one before")
}

// The keys that act on the file under a panel's cursor do nothing in the
// Sessions box: you aren't looking at that file.
func TestTheSessionsBoxLeavesTheFilesAlone(t *testing.T) {
	m := dockTestModel(t)
	require.NoError(t, m.createNewFilePanel(folderOf(t, 3)))
	TeaUpdate(m, nil)
	require.Len(t, m.getFocusedFilePanel().element, 3, "the cursor is on a file")
	dir := m.getFocusedFilePanel().location
	names := func() []string {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return out
	}
	before := names()
	require.NotEmpty(t, before)
	bang(m)
	selected := m.sessions.list.SelectedID()

	for _, keys := range [][]string{common.Hotkeys.DeleteItems, common.Hotkeys.PermanentlyDeleteItems,
		common.Hotkeys.OpenFileWithEditor, common.Hotkeys.OpenFileWithSecondEditor, common.Hotkeys.PasteItems,
		common.Hotkeys.CutItems, common.Hotkeys.FilePanelItemCreate, common.Hotkeys.ExtractFile,
		common.Hotkeys.CompressFile, common.Hotkeys.OpenSortOptionsMenu, common.Hotkeys.SearchBar,
		common.Hotkeys.ParentDirectory, common.Hotkeys.PinnedDirectory} {
		pressKey(m, keys[0])
		assert.Equal(t, dockSessionsFocus, m.focusPanel, "%s: the keyboard stays", keys[0])
		assert.Equal(t, selected, m.sessions.list.SelectedID(), keys[0])
		assert.False(t, m.typingModal.open, "%s opens nothing", keys[0])
		assert.False(t, m.notifyModel.IsOpen(), "%s asks nothing", keys[0])
	}
	assert.Equal(t, before, names(), "no file was made, removed or renamed")
	assert.Equal(t, dir, m.getFocusedFilePanel().location, "and the panel is where it was")
}

// The Ask box's one line, in each state the session it writes to can be in.
func TestTheAskBoxsLineSaysHowYourLastMessageStands(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	line := func(s claudesession.Session) string {
		l := m.dockAskIdleLine(s, 60)
		require.Equal(t, 60, ansi.StringWidth(l), "one line, as wide as the box: %q", ansi.Strip(l))
		return strings.TrimRight(ansi.Strip(l), " ")
	}
	s := claudesession.Session{ID: "a1", Title: "fix", Live: true, LastPrompt: "run the tests"}

	assert.Equal(t, "starts in the background", line(claudesession.Session{}), "no session: a new one")
	assert.Equal(t, "you: “run the tests”", line(s), "replied: its row and the preview say so")
	s.LastPrompt = ""
	assert.Equal(t, "nothing sent yet", line(s))
	s.LastPrompt = "run the tests"

	s.Status = claudesession.StatusWorking
	assert.True(t, strings.HasSuffix(line(s), "✳ working"), line(s))
	s.Status, s.PendingTool = claudesession.StatusWaiting, "Bash(go test ./...)"
	assert.True(t, strings.HasSuffix(line(s), "◉ asks you: Bash(go test ./...)"), line(s))
	s.PendingTool = "Bash(" + strings.Repeat("a very long command ", 6) + ")"
	asks := line(s)
	assert.Contains(t, asks, "you: “run the", "what it asks takes half the line at most: your message keeps the rest")
	assert.Contains(t, asks, "◉ asks you: Bash(a very")

	remote := claudesession.Session{ID: "app:cse_1", Title: "b1", Live: true,
		App: &claudesession.AppSession{Doing: "redteam-root · arch/agent-swarm"}}
	assert.Equal(t, "redteam-root · arch/agent-swarm", line(remote),
		"a session of the Remote section: what claude.ai says")
	assert.Contains(t, m.dockAskEdge(remote, 60), "on another computer · enter: open in Claude app")

	m.sessions.composer.SetValue("half a\nthought")
	assert.True(t, strings.HasPrefix(line(s), "draft: half a thought"),
		"a draft that waits shows in the message's place, its lines as one: %q", line(s))
}

// While the Ask box is written in, a line under the draft says what sending
// does when that isn't the usual: here, that the session asks you something.
func TestTheAskBoxSaysWhenSendingIsntTheUsual(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	bang(m)
	selected, ok := m.selectedSession()
	require.True(t, ok)
	pressKey(m, common.Hotkeys.OpenAskBar[0])
	require.True(t, m.askSending().Usual, m.askSending().Text)
	assert.Equal(t, askMinHeight, m.lay.ask.h, "the usual: the draft's line alone")

	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == selected.ID {
			m.claude.snapshot.Live[i].Status = claudesession.StatusWaiting
		}
	}
	m.refreshSessionsList()
	m.relayout()
	require.False(t, m.askSending().Usual, m.askSending().Text)
	lines := strings.Split(ansi.Strip(m.dockAskRender()), "\n")
	require.Len(t, lines, askMinHeight+1, "a line more")
	assert.Contains(t, lines[2], "→ it asks you something")
}

func TestEscFromTheAskBoxGoesBackToTheFiles(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	pressKey(m, common.Hotkeys.OpenAskBar[0])
	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "opened from the files, esc goes back to them")
}

// s opens its list from the Sessions box too; it goes to folders, so the
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

	box := ansi.Strip(sessionsBoxRender(m))
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

// The status line ends with this computer's time, with its day and zone.
func TestTheStatusLineEndsWithTheTime(t *testing.T) {
	m := dockTestModel(t)
	zone := time.FixedZone("PST", -8*60*60)
	setForTest(t, &statusNow, func() time.Time { return time.Date(2026, 11, 17, 15, 4, 0, 0, zone) })

	line := strings.TrimRight(ansi.Strip(m.statusLineRender()), " ")
	assert.True(t, strings.HasSuffix(line, "✳ 1 working   Tue 15:04 PST"), line)

	bang(m)
	line = strings.TrimRight(ansi.Strip(m.statusLineRender()), " ")
	assert.True(t, strings.HasSuffix(line, "Tue 15:04 PST"), "in the Sessions box too: %s", line)
	assert.LessOrEqual(t, ansi.StringWidth(m.statusLineRender()), m.fullWidth)
}
