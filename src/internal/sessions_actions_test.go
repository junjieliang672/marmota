package internal

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/notify"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// sessionsTestModel has a session beside Marmota (pane 20), one in another
// WezTerm tab (pane 50) and one finished session of the current folder.
func sessionsTestModel(t *testing.T) *model {
	t.Helper()
	m := newClaudeTestModel(t)
	TeaUpdate(m, nil)
	m.claude.panes = tabPanes()
	m.claude.folder = "/repo"
	m.claude.snapshot = claudesession.Snapshot{
		Live: []claudesession.Session{
			{ID: "s20", Title: "provider registry", Live: true, TTY: "/dev/ttys020", Cwd: "/repo",
				Status: claudesession.StatusWorking, LastPrompt: "move the launchers into providers.toml",
				EditedFiles: []string{"/repo/a.go", "/repo/src/b.go"}},
			{ID: "s50", Title: "ime hold tests", Live: true, TTY: "/dev/ttys050", Cwd: "/elsewhere"},
		},
		Past: []claudesession.Session{{ID: "past1", Title: "claude rc launcher", Cwd: "/repo",
			Status: claudesession.StatusEnded}},
	}
	m.claude.loaded = true
	return m
}

// paneListResult answers the pane listing superfile makes before it switches
// WezTerm to a pane outside its own agent panes.
func paneListResult() fakeExecResult {
	return fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(tabPanes()...)}
}

func pressKey(m *model, key string) {
	TeaUpdate(m, utils.TeaKeyMsg(key))
}

func TestTheSessionsBoxListsLiveSessionsAndThePreviewTheSelectedOne(t *testing.T) {
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()

	bang(m)
	require.True(t, m.sessions.list.Select("s20"))
	TeaUpdate(m, nil)

	plain := ansi.Strip(m.View())
	for _, want := range []string{
		"✳ Sessions", "provider registry", "ime hold tests",
		"working · beside Marmota", "Changed files", "src/b.go", "a.go",
	} {
		assert.Contains(t, plain, want)
	}
	assert.NotContains(t, plain, "claude rc launcher", "a finished session isn't listed")
	assert.Less(t, strings.Index(plain, "src/b.go"), strings.Index(plain, " a.go"), "newest changed file first")
}

func TestEnterInTheSessionsBoxSwitchesToTheSession(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	require.True(t, m.sessions.list.Select("s50"))
	require.Equal(t, "s50", m.sessions.list.SelectedID())
	calls := installFakeExec(t, paneListResult(), fakeExecResult{retCode: 0, output: ""})

	pressKey(m, "enter")

	require.Len(t, *calls, 2)
	assert.Equal(t, "wezterm cli activate-pane --pane-id 50", (*calls)[1].cmd)
	assert.Equal(t, "Switched to ime hold tests", m.claude.flash)
}

// enter on the session beside Marmota puts the keyboard in it.
func TestEnterInTheSessionsBoxTypesInTheSessionBesideSuperfile(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	require.True(t, m.sessions.list.Select("s20"), "the one beside Marmota")
	calls := installFakeExec(t, fakeExecResult{retCode: 0, output: ""})

	pressKey(m, "enter")

	require.Len(t, *calls, 1)
	assert.Equal(t, "wezterm cli activate-pane --pane-id 20", (*calls)[0].cmd)
	assert.Equal(t, "Focused provider registry", m.claude.flash)
}

// The list shows only the sessions it can bring beside Marmota and type
// into: those running in a WezTerm pane.
func TestTheSessionsBoxListsOnlySessionsInWeztermPanes(t *testing.T) {
	m := sessionsTestModel(t)
	m.claude.snapshot.Live = append(m.claude.snapshot.Live,
		claudesession.Session{ID: "iterm", Title: "in iTerm", Live: true, PID: 42, TTY: "/dev/ttys099", Cwd: "/repo"},
		claudesession.Session{ID: "bg", Title: "in the background", Live: true, Background: true, Cwd: "/repo"},
		claudesession.Session{ID: "own", Title: "on superfile's terminal", Live: true, TTY: "/dev/ttys010",
			Cwd: "/repo"},
	)
	m.claude.panes[0].TTYName = "/dev/ttys010"

	var ids []string
	for _, s := range m.dockSessions() {
		ids = append(ids, s.ID)
	}
	assert.ElementsMatch(t, []string{"s20", "s50"}, ids,
		"not the finished one, nor those in iTerm, in the background or on superfile's terminal")
}

func TestTheSessionsBoxForksIntoAWorktree(t *testing.T) {
	m := dockTestModel(t)
	dir := t.TempDir()
	m.claude.snapshot.Live[0].Cwd = dir
	bang(m)
	require.True(t, m.sessions.list.Select("s20"))

	pressKey(m, common.Hotkeys.SessionFork[0])
	assert.Contains(t, m.claude.flash, "isn't in a git repository")

	utils.SetupDirectories(t, filepath.Join(dir, ".git"))
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, Size: weztermPaneSize{Rows: 53, Cols: 223}},
		)},
		fakeExecResult{retCode: 0, output: "61\n"},
		fakeExecResult{retCode: 0, output: ""},
	)
	pressKey(m, common.Hotkeys.SessionFork[0])

	require.Len(t, *calls, 3)
	assert.True(t, strings.HasSuffix((*calls)[1].cmd,
		"-- claude --resume s20 --fork-session --worktree --dangerously-skip-permissions"), (*calls)[1].cmd)
	assert.Equal(t, "Forked provider registry into a new worktree", m.claude.flash)
}

func TestTheSessionsBoxStopAsksThenClosesThePane(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	require.True(t, m.sessions.list.Select("s50"))

	pressKey(m, common.Hotkeys.SessionStop[0])
	require.True(t, m.notifyModel.IsOpen(), "it asks first")
	assert.Equal(t, notify.StopClaudeSessionAction, m.notifyModel.GetConfirmAction())
	assert.Contains(t, ansi.Strip(m.View()), "Stop “ime hold tests”?", "the question shows")

	calls := installFakeExec(t, fakeExecResult{retCode: 0, output: ""}, paneListResult(),
		fakeExecResult{retCode: 0, output: ""})
	pressKey(m, "enter")

	require.NotEmpty(t, *calls)
	assert.Equal(t, "wezterm cli kill-pane --pane-id 50", (*calls)[0].cmd)
	assert.Equal(t, "wezterm cli activate-pane --pane-id 10", (*calls)[len(*calls)-1].cmd,
		"the keyboard stays with superfile")
	assert.Equal(t, "Stopped ime hold tests", m.claude.flash)
	for _, s := range m.dockSessions() {
		assert.NotEqual(t, "s50", s.ID, "the box no longer lists it")
	}
}

// A stopped session moves to the finished sessions at once, and stays there
// while its process exits, even if a refresh still lists it.
func TestAStoppedSessionIsFinishedAtOnce(t *testing.T) {
	m := dockTestModel(t)
	m.claude.snapshot.Live[0].PID = 4242
	m.claude.snapshot.Live[0].TranscriptPath = "/projects/-repo/s20.jsonl"
	stillRunning := m.claude.snapshot
	stillRunning.Live = slices.Clone(stillRunning.Live)
	stillRunning.Past = slices.Clone(stillRunning.Past)
	bang(m)
	require.True(t, m.sessions.list.Select("s20"))
	pressKey(m, common.Hotkeys.SessionStop[0])
	installFakeExec(t, fakeExecResult{retCode: 0, output: ""}, paneListResult(), fakeExecResult{retCode: 0, output: ""})
	pressKey(m, "enter")
	require.Equal(t, "Stopped provider registry", m.claude.flash)

	finished := func() []string {
		var ids []string
		for _, s := range m.claude.snapshot.Past {
			ids = append(ids, s.ID)
		}
		return ids
	}
	assert.Equal(t, []string{"s20", "past1"}, finished(), "it is a finished session now")
	s, ok := m.claude.snapshot.Find("s20")
	require.True(t, ok)
	assert.False(t, s.Live)
	assert.Equal(t, claudesession.StatusEnded, s.Status)

	TeaUpdate(m, claudeSessionsSnapshotMsg{snapshot: stillRunning, folder: "/repo", layoutGen: m.claude.layoutGen})
	assert.Equal(t, []string{"s20", "past1"}, finished(), "a refresh made while it exits doesn't bring it back")

	resumed := stillRunning
	resumed.Live = slices.Clone(stillRunning.Live)
	resumed.Live[0].PID = 5151
	TeaUpdate(m, claudeSessionsSnapshotMsg{snapshot: resumed, folder: "/repo", layoutGen: m.claude.layoutGen})
	assert.Equal(t, []string{"past1"}, finished(), "a new process for it, as after resuming, is running")
	assert.Empty(t, m.claude.stopped, "and the stop is forgotten")
}

// esc leaves the Sessions box; q quits superfile there, as in the files,
// unless you're typing a name.
func TestTheSessionsBoxEscLeavesItAndQQuits(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	bang(m)
	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "esc goes back to the files")
	assert.Equal(t, notQuitting, m.modelQuitState)

	bang(m)
	pressKey(m, common.Hotkeys.SessionRename[0])
	pressKey(m, common.Hotkeys.Quit[0])
	assert.Equal(t, notQuitting, m.modelQuitState, "a q typed in a name is a q")
	pressKey(m, "esc")

	assert.True(t, IsTeaQuit(press(m, common.Hotkeys.Quit[0])), "q quits superfile")
}

// r renames a session in the Sessions box, as a file is renamed: the name is
// typed where the title was, and enter keeps it the way Claude Code's rename
// does.
func TestTheSessionsBoxRenamesASession(t *testing.T) {
	m := dockTestModel(t)
	pane := newFakeClaudePane(t)
	pane.set("half typed", nil)
	bang(m)
	require.True(t, m.sessions.list.Select("s20"))

	pressKey(m, common.Hotkeys.SessionRename[0])
	require.True(t, m.sessions.list.IsRenaming())
	assert.True(t, m.isTextInputActive(), "typing goes to the name, so the input method is let go")
	assert.Contains(t, ansi.Strip(m.dockStatusLine()), "rename")
	assert.Contains(t, ansi.Strip(m.dockStatusLine()), "cancel")
	TeaUpdate(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "providers in toml" {
		TeaUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	cmd := press(m, "enter")

	assert.False(t, m.sessions.list.IsRenaming())
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "the box keeps the keyboard")
	assert.Contains(t, ansi.Strip(m.View()), "providers in toml", "the box shows it at once")
	require.NotNil(t, cmd)
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(cmd, 2*time.Second))
	assert.Equal(t, "Renamed to providers in toml", m.claude.flash)
	input, _, sent := pane.state()
	assert.Equal(t, []string{"/rename providers in toml"}, sent,
		"a running session is renamed by its own /rename, so its pane shows the name at once")
	assert.Equal(t, "half typed", input, "what you had typed there stays")

	pressKey(m, common.Hotkeys.SessionRename[0])
	pressKey(m, "esc")
	assert.False(t, m.sessions.list.IsRenaming())
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "esc cancels the rename, and stays in the box")

	// superfile can't type into a pane it can't read; with no transcript
	// either, there is nowhere to keep the name yet.
	require.True(t, m.sessions.list.Select("s50"))
	pressKey(m, common.Hotkeys.SessionRename[0])
	TeaUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(press(m, "enter"), 2*time.Second))
	assert.Contains(t, m.claude.flash, "has no conversation yet")
}

func TestRenamingARunningSessionThatIsAskingSomething(t *testing.T) {
	m := dockTestModel(t)
	transcript := filepath.Join(t.TempDir(), "s20.jsonl")
	require.NoError(t, os.WriteFile(transcript, []byte(`{"type":"ai-title","aiTitle":"provider registry"}`+"\n"),
		0o600))
	m.claude.snapshot.Live[0].TranscriptPath = transcript
	pane := newFakeClaudePane(t)
	pane.question = true
	bang(m)
	require.True(t, m.sessions.list.Select("s20"))
	pressKey(m, common.Hotkeys.SessionRename[0])
	TeaUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(press(m, "enter"), 2*time.Second))

	assert.Empty(t, pane.keys, "enter would answer its question, so nothing is typed")
	assert.Equal(t, "Renamed to provider registrys. Its pane shows it once resumed: it is showing a question or a menu",
		m.claude.flash)
	data, err := os.ReadFile(transcript)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"customTitle":"provider registrys"`, "the name is kept all the same")
}
