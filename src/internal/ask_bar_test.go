package internal

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

func askTestModel(t *testing.T) (*model, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# demo\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "logo.png"), []byte("png"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "logos"), 0o755))
	m := defaultTestModel(dir)
	TeaUpdate(m, nil) // load the folder
	return m, dir
}

func press(m *model, key string) tea.Cmd {
	return m.handleKeyInput(utils.TeaKeyMsg(key))
}

func typeInto(m *model, text string) {
	for _, r := range text {
		m.handleKeyInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// ask writes a question in the Ask bar, opening it with alt+k first, and
// sends it.
func ask(t *testing.T, m *model, question string) {
	t.Helper()
	if m.focusPanel != dockAskFocus {
		press(m, common.Hotkeys.OpenAskBar[0])
	}
	require.Equal(t, sessionsFocusAsk, m.sessions.focus)
	press(m, common.Hotkeys.AskClear[0])
	typeInto(m, question)
	press(m, "enter")
}

// newAskID makes the sessions the Ask bar starts take id.
func newAskID(t *testing.T, id string) {
	t.Helper()
	original := newAskSessionID
	newAskSessionID = func() string { return id }
	t.Cleanup(func() { newAskSessionID = original })
}

// ownAskHistory gives the test an Ask history of its own.
func ownAskHistory(t *testing.T) {
	t.Helper()
	history := variable.AskHistoryFile
	variable.AskHistoryFile = filepath.Join(t.TempDir(), "ask_history.json") //nolint:reassign // its own history
	t.Cleanup(func() { variable.AskHistoryFile = history })                  //nolint:reassign // restored
}

func viewText(m *model) string {
	return ansi.Strip(m.View())
}

func TestKOpensTheAskBoxInTheDock(t *testing.T) {
	ownAskHistory(t)
	m, dir := askTestModel(t)
	setFilePanelSelectedItemByName(t, m.getFocusedFilePanel(), "logo.png")

	press(m, common.Hotkeys.OpenAskBar[0])

	require.Equal(t, dockAskFocus, m.focusPanel)
	assert.Equal(t, sessionsFocusAsk, m.sessions.focus)
	assert.True(t, m.sessions.list.NewSelected(), "no live session: a new one")
	assert.Empty(t, m.sessions.composer.Value(), "the cursor's item goes along as context, not as a mention")
	assert.True(t, m.isTextInputActive(), "typing goes to the bar, so the input method is let go")
	view := viewText(m)
	assert.Contains(t, view, "Ask → new Claude Code session in "+filepath.Base(dir))
	assert.Contains(t, view, "starts in the background, ⏵⏵ bypass")
	assert.NotContains(t, view, "─ Details ─", "the Ask box grows over Details")

	typeInto(m, "hi")
	press(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "esc closes it")
	assert.False(t, m.isTextInputActive())
	assert.Equal(t, "hi", m.sessions.composer.Value(), "the draft stays")
	assert.Contains(t, viewText(m), "draft: hi", "the idle box says a draft waits")

	press(m, common.Hotkeys.AskClear[0]) // not typing: does nothing
	press(m, common.Hotkeys.SessionMention[0])
	assert.Equal(t, "hi @logo.png ", m.sessions.composer.Value(), "m in the file panel mentions the item")
}

// The Ask bar starts its sessions in the background: the session beside
// superfile, and the keyboard, stay where they are.
func TestANewSessionStartsInTheBackground(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	beside := weztermPane{PaneID: 20, TabID: 1, WindowID: 1, Size: weztermPaneSize{Rows: 50, Cols: 99}}
	wez := newFakeWezterm(t, superfileTab(), beside)
	newAskID(t, "n-1")

	ask(t, m, "group the logos")

	require.Len(t, wez.panes, 3, "a session pane was started: %v", wez.commands)
	assert.Contains(
		t,
		startCommand(t, wez),
		" -- claude --session-id n-1 --dangerously-skip-permissions --effort medium -- 'group the logos\n\n<superfile-context>\ncwd:",
	)
	assert.Equal(t, managedAgentHiddenWorkspace, wez.panes[wez.nextPane].Workspace, "in the hidden workspace")
	assert.Equal(t, 1, wez.panes[20].TabID, "the session beside Marmota stays")
	assert.Equal(t, 10, wez.active, "the keyboard stays with superfile")
	assert.Equal(t, sessionsFocusAsk, m.sessions.focus, "in the Ask bar")
	assert.Equal(t, "Started a background session in "+filepath.Base(m.getFocusedFilePanel().location),
		m.claude.flash)
	assert.Equal(t, "n-1", m.ask.folderSessions[m.getFocusedFilePanel().location], "it is the folder's session now")
	assert.Empty(t, m.sessions.composer.Value(), "sent, the bar is empty again")
	assert.Equal(t, []string{"group the logos"}, m.sessions.composer.History())
	assert.Equal(t, dockAskFocus, m.focusPanel, "the Ask box keeps the keyboard")
}

func TestASessionStartsInTheChosenPermissionMode(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	press(m, common.Hotkeys.OpenAskBar[0])
	press(m, common.Hotkeys.AskClear[0])
	typeInto(m, "group the logos")
	assert.Contains(t, viewText(m), "⏵⏵ bypass", "bypass, superfile's default")
	press(m, common.Hotkeys.AskChoosePermissionMode[0])
	assert.Contains(t, viewText(m), "⏵⏵ accept edits")

	press(m, "enter")

	assert.Contains(t, startCommand(t, wez), " --permission-mode acceptEdits --effort medium -- 'group the logos")
	m.sessions.composer.SetPermissionMode("")
	m.ask.loaded = false
	m.loadAskHistory()
	assert.Equal(t, "acceptEdits", m.sessions.composer.PermissionMode(), "kept with the history, as the model is")
}

// A finished session isn't listed, so when the folder's session has ended,
// alt+k starts a new one.
func TestKStartsANewSessionWhenTheFoldersSessionEnded(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	m.claude.snapshot.Past = []claudesession.Session{{ID: "s-7", Title: "an earlier talk", Cwd: dir}}
	m.ask.loaded = true
	m.ask.folderSessions = map[string]string{dir: "s-7"}
	wez := newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")

	press(m, common.Hotkeys.OpenAskBar[0])
	require.Equal(t, sessionsFocusAsk, m.sessions.focus)
	assert.True(t, m.sessions.list.NewSelected(), "the folder's session ended: a new one")
	assert.Empty(t, m.dockSessions(), "an ended session isn't listed")
	ask(t, m, "now do it")

	started := startCommand(t, wez)
	assert.Contains(t, started, "--cwd \""+dir+"\"")
	assert.Contains(t, started, " -- claude --session-id n-1 --dangerously-skip-permissions --effort medium -- 'now do it")
	assert.Equal(t, "n-1", m.ask.folderSessions[dir], "the new session is the folder's now")
}

// When the session the Ask bar writes to leaves the list, as when it ends,
// the question goes to a new session, not to the row the cursor lands on.
func TestTheAskBoxWritesToANewSessionWhenItsSessionLeaves(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	bang(m)
	require.True(t, m.sessions.list.Select("s20"))
	press(m, common.Hotkeys.OpenAskBar[0]) // writes to it
	press(m, common.Hotkeys.AskClear[0])
	typeInto(m, "keep me")
	require.False(t, m.sessions.list.NewSelected())

	ended := m.claude.snapshot
	gone := ended.Live[0]
	gone.Live, gone.Status = false, claudesession.StatusEnded
	ended.Live = slices.Clone(ended.Live[1:])
	ended.Past = append([]claudesession.Session{gone}, ended.Past...)
	TeaUpdate(m, claudeSessionsSnapshotMsg{snapshot: ended, folder: "/repo", layoutGen: m.claude.layoutGen})

	assert.True(t, m.sessions.list.NewSelected())
	assert.Equal(t, "provider registry left the list: your question now starts a new session", m.claude.flash)
	assert.Equal(t, "keep me", m.sessions.composer.Value())
	assert.Contains(t, viewText(m), "starts in the background")
}

// The folder's session is where alt+k writes next time.
func TestKWritesToTheSessionItStarted(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "first")

	// The next refresh lists it, running in its pane.
	m.claude.panes = append(m.claude.panes, weztermPane{PaneID: 100, TabID: 1, TTYName: "/dev/ttys100"})
	m.claude.snapshot.Live = []claudesession.Session{{ID: "n-1", Title: "first", Cwd: dir, Live: true,
		TTY: "/dev/ttys100"}}
	m.refreshSessionsList()
	assert.Equal(t, "n-1", m.sessions.list.SelectedID(), "the list goes to it once it is listed")
	press(m, "esc")
	require.Equal(t, nonePanelFocus, m.focusPanel)

	press(m, common.Hotkeys.OpenAskBar[0])
	assert.Equal(t, "n-1", m.sessions.list.SelectedID(), "K writes to the session it started")
	assert.False(t, m.sessions.list.NewSelected())
}

func TestASendThatCantStartKeepsYourText(t *testing.T) {
	ownAskHistory(t)
	m, _ := askTestModel(t) // not in WezTerm

	ask(t, m, "will this work?")

	assert.Equal(t, sessionsFocusAsk, m.sessions.focus)
	assert.Equal(t, "will this work?", m.sessions.composer.Value())
	assert.Contains(t, viewText(m), "starting a session needs Marmota running in WezTerm")
	assert.Empty(t, m.ask.folderSessions)
}

// The question and what was sent are kept for the next superfile; the
// model, effort and agent aren't: a new session starts from the defaults.
func TestTheQuestionIsKeptAndTheChoicesStartFromTheDefaults(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	ask(t, m, "remember me")
	press(m, common.Hotkeys.AskChooseModel[0])
	typeInto(m, "half written")
	press(m, "esc")
	press(m, "esc")
	require.NotEqual(t, dockAskFocus, m.focusPanel)

	again, _ := askTestModel(t)
	press(again, common.Hotkeys.OpenAskBar[0])
	assert.Equal(t, "half written", again.sessions.composer.Value(), "the question waits where you left it")
	press(again, common.Hotkeys.AskPrevious[0])
	assert.Equal(t, "remember me", again.sessions.composer.Value())
	press(again, common.Hotkeys.AskNext[0])
	assert.Equal(t, "half written", again.sessions.composer.Value())
	model, effort := again.sessions.composer.Choices()
	assert.Equal(t, [2]string{"", "medium"}, [2]string{model, effort}, "the default model, medium")
	assert.False(t, again.sessions.composer.StartsCodex(), "Claude Code")

	press(again, common.Hotkeys.AskClear[0])
	assert.Empty(t, again.sessions.composer.Value())
}

func TestSendingToARunningSessionTypesIntoItsPane(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	m.claude.panes = []weztermPane{
		{PaneID: 10, TabID: 1, Size: weztermPaneSize{Cols: 100}},
		{PaneID: 20, TabID: 1, TTYName: "/dev/ttys020", Size: weztermPaneSize{Cols: 99}},
	}
	m.claude.snapshot.Live = []claudesession.Session{{ID: "s20", Title: "fix sidebar", Live: true,
		TTY: "/dev/ttys020", Cwd: m.getFocusedFilePanel().location}}
	pane := newFakeClaudePane(t)
	pane.set("half typed", nil)

	bang(m)
	require.True(t, m.sessions.list.Select("s20"))
	press(m, common.Hotkeys.OpenAskBar[0])
	assert.Contains(t, viewText(m), "typed into its pane")
	typeInto(m, "also update the docs")
	for _, msg := range batchMsgs(t, press(m, "enter")) {
		TeaUpdate(m, msg)
	}

	require.NotEmpty(t, pane.keys)
	assert.True(t, strings.HasPrefix(pane.keys[1], "paste:also update the docs\n\n<superfile-context>"),
		pane.keys[1])
	input, _, sent := pane.state()
	require.Len(t, sent, 1, "then enter sends it")
	assert.True(t, strings.HasPrefix(sent[0], "[Pasted text #1 +"), "alone: %q", sent[0])
	assert.Equal(t, "half typed", input, "what you had typed there stays")
	assert.Equal(t, "Sent to fix sidebar", m.claude.flash)
	assert.Empty(t, m.sessions.composer.Value())

	pane.question = true // enter would answer it
	typeInto(m, "lost?")
	for _, msg := range batchMsgs(t, press(m, "enter")) {
		TeaUpdate(m, msg)
	}
	assert.Contains(t, m.claude.flash, "Couldn't send to fix sidebar")
	assert.Equal(t, "lost?", m.sessions.composer.Value(), "a failed send gives your text back")
}

func TestMentionsAreWrittenForTheSessionTheyGoTo(t *testing.T) {
	ownAskHistory(t)
	m, dir := askTestModel(t)
	setFilePanelSelectedItemByName(t, m.getFocusedFilePanel(), "logo.png")
	press(m, common.Hotkeys.SessionMention[0]) // m mentions it in the draft
	press(m, common.Hotkeys.OpenAskBar[0])
	typeInto(m, "what is in it?")
	question := m.sessions.composer.Value()
	require.Equal(t, "@logo.png what is in it?", question)

	assert.Equal(t, "@"+filepath.Base(dir)+"/logo.png what is in it?", m.questionFor(question, filepath.Dir(dir)))
	assert.Equal(t, "@"+filepath.Join(dir, "logo.png")+" what is in it?", m.questionFor(question, "/elsewhere"))

	press(m, "esc")
	require.Equal(t, nonePanelFocus, m.focusPanel)
	setFilePanelSelectedItemByName(t, m.getFocusedFilePanel(), "logos")
	press(m, common.Hotkeys.SessionMention[0])
	assert.Equal(t, "@logo.png what is in it? @logos/ ", m.sessions.composer.Value(), "m adds to the draft")
	assert.Contains(t, m.claude.flash, "Added to the Ask draft")
}

func TestAskContext(t *testing.T) {
	m, dir := askTestModel(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("notes"), 0o600))
	panel := m.getFocusedFilePanel()
	setupPanelModeAndSelection(t, m, true, "", []string{filepath.Join(dir, "logo.png"), filepath.Join(dir, "logos")})

	c := m.askContext()
	assert.Equal(t, dir, c.Folder)
	assert.Equal(t, []string{"logos/", "logo.png"}, c.Selected, "in panel order, folders first")
	assert.Equal(t, filepath.Join(dir, "CLAUDE.md"), c.Notes)
	assert.Equal(t, selectMode, panel.panelMode)
}

// startCommand is the command that started the Ask bar's last session: a
// pane spawned in the hidden workspace.
func startCommand(t *testing.T, wez *fakeWezterm) string {
	t.Helper()
	for i := len(wez.commands) - 1; i >= 0; i-- {
		if strings.HasPrefix(wez.commands[i], "wezterm cli spawn --new-window --workspace "+
			managedAgentHiddenWorkspace+" ") {
			return wez.commands[i]
		}
	}
	t.Fatalf("no session was started: %v", wez.commands)
	return ""
}

// In a folder Claude Code doesn't trust yet, the new session first asks
// whether to: its pane comes in beside Marmota, with the keyboard, to be
// answered.
func TestANewSessionAskingToTrustItsFolderComesBesideSuperfile(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	require.Contains(t, m.ask.starts, "n-1")
	paneID := m.ask.starts["n-1"].paneID
	require.NotZero(t, paneID)

	trust := askStartTickMsg{id: "n-1", screen: "Quick safety check\n ❯ No, exit\n   Yes, I trust this folder"}
	assert.NotNil(t, m.applyAskStartTick(trust), "it is watched until listed")

	assert.Equal(t, 1, wez.panes[paneID].TabID, "it is beside Marmota")
	assert.Equal(t, paneID, wez.active, "with the keyboard")
	assert.Equal(t, "The new session asks whether you trust "+filepath.Base(m.getFocusedFilePanel().location),
		m.claude.flash)
	commands := len(wez.commands)
	m.applyAskStartTick(trust)
	assert.Len(t, wez.commands, commands, "it is brought in once")
}

// Once Claude Code lists the new session it answers in the background; one
// that isn't listed in time comes in beside Marmota all the same.
func TestTheAskBarWatchesItsNewSessionUntilItIsListed(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	paneID := m.ask.starts["n-1"].paneID

	assert.NotNil(t, m.applyAskStartTick(askStartTickMsg{id: "n-1", screen: "starting"}), "it looks again")
	m.claude.snapshot.Live = []claudesession.Session{{ID: "n-1", Live: true}}
	assert.Nil(t, m.applyAskStartTick(askStartTickMsg{id: "n-1", screen: "working"}))
	assert.Empty(t, m.ask.starts, "listed, it is no longer watched")
	assert.Equal(t, managedAgentHiddenWorkspace, wez.panes[paneID].Workspace, "and stays in the background")

	newAskID(t, "n-2")
	m.claude.snapshot.Live = nil
	ask(t, m, "group the icons")
	slow := m.ask.starts["n-2"].paneID
	m.ask.starts["n-2"].polls = askStartPolls - 1
	TeaUpdate(m, askStartTickMsg{id: "n-2", screen: "starting"})
	assert.Equal(t, 1, wez.panes[slow].TabID, "not listed in time, it comes in beside Marmota")
	assert.Equal(t, slow, wez.active, "with the keyboard")
	assert.Equal(t, "The new session hasn't started yet: here it is", m.claude.flash)
}

// What you send while the new session starts waits for its first reply, then
// is typed into it, in the order you sent it: it doesn't start another
// session, and isn't typed into the screen Claude Code draws as it starts.
func TestARequestSentWhileTheNewSessionStartsWaitsForItsReply(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	wez := newFakeWezterm(t, superfileTab())
	pane := newFakeClaudePane(t)
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	paneID := m.ask.starts["n-1"].paneID
	pane.paneID = paneID
	view := viewText(m)
	assert.Contains(t, view, "→ sent once the new session has replied")
	assert.Contains(t, view, "What you send it now waits")

	typeInto(m, "and the icons")
	assert.Nil(t, press(m, "enter"))
	assert.Len(t, wez.panes, 2, "no other session was started: %v", wez.commands)
	assert.Empty(t, m.sessions.composer.Value())
	assert.Equal(t, []string{"group the logos", "and the icons"}, m.sessions.composer.History())
	assert.Equal(t, "Queued for the new session until it has replied", m.claude.flash)

	// Listed while it works on its first reply, it is selected; what you
	// send waits behind what waits already.
	wez.panes[paneID].TTYName = "/dev/ttys101"
	m.claude.panes = wez.paneList()
	working := claudesession.Session{ID: "n-1", Title: "logos", Live: true, Status: claudesession.StatusWorking,
		TTY: "/dev/ttys101", Cwd: dir}
	listed := claudesession.Snapshot{Live: []claudesession.Session{working}}
	TeaUpdate(m, claudeSessionsSnapshotMsg{snapshot: listed, folder: dir, layoutGen: m.claude.layoutGen})
	require.Equal(t, "n-1", m.sessions.list.SelectedID(), "listed, it is selected")
	assert.NotNil(t, m.applyAskStartTick(askStartTickMsg{id: "n-1"}), "it is watched until it has replied")
	typeInto(m, "and the fonts")
	assert.Nil(t, press(m, "enter"))
	assert.Empty(t, pane.keys, "nothing is typed before its first reply")

	replied := working
	replied.Status, replied.LastTurnEnd = claudesession.StatusIdle, time.Now()
	m.claude.snapshot.Live = []claudesession.Session{replied}
	for _, msg := range batchMsgs(t, m.applyAskStartTick(askStartTickMsg{id: "n-1"})) {
		TeaUpdate(m, msg)
	}
	var pasted []string
	for _, k := range pane.keys {
		if text, ok := strings.CutPrefix(k, "paste:"); ok {
			pasted = append(pasted, strings.SplitN(text, "\n", 2)[0])
		}
	}
	assert.Equal(t, []string{"and the icons", "and the fonts"}, pasted, "in the order you sent them")
	_, _, sent := pane.state()
	assert.Len(t, sent, 2)
	assert.Equal(t, "Sent to logos", m.claude.flash)
	assert.Empty(t, m.ask.starts, "sent, it is no longer watched")
	assert.Contains(t, viewText(m), "typed into its pane", "what you send now goes straight in")
}

// What waits for a new session that ends before its first reply comes back
// to the bar.
func TestARequestForANewSessionThatEndsComesBack(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	typeInto(m, "and the icons")
	press(m, "enter")
	m.claude.snapshot.Live = []claudesession.Session{{ID: "n-1", Live: true, Status: claudesession.StatusWorking}}
	m.applyAskStartTick(askStartTickMsg{id: "n-1"})

	m.claude.snapshot.Live = nil
	m.applyAskStartTick(askStartTickMsg{id: "n-1"})

	assert.Equal(t, "and the icons", m.sessions.composer.Value())
	assert.Contains(t, m.claude.flash, "ended first")
	assert.Empty(t, m.ask.starts)
}

// A request waiting for a session that closes before it starts comes back
// to the bar.
func TestARequestForANewSessionThatClosesComesBack(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	typeInto(m, "and the icons")
	press(m, "enter")

	m.applyAskStartTick(askStartTickMsg{id: "n-1", err: errors.New("no pane")})

	assert.Equal(t, "and the icons", m.sessions.composer.Value())
	assert.Contains(t, m.claude.flash, "closed before it started")
	assert.Empty(t, m.ask.starts)
	assert.Empty(t, m.sessions.pendingSelect)
	assert.Contains(t, viewText(m), "starts in the background", "the next send starts a session")
}

// K from the files writes to the folder's session while it starts.
func TestAltKWritesToTheFoldersSessionWhileItStarts(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	press(m, "esc")
	press(m, "esc")
	require.NotEqual(t, dockAskFocus, m.focusPanel)

	press(m, common.Hotkeys.OpenAskBar[0])
	assert.Contains(t, viewText(m), "→ sent once the new session has replied")
}

// A row you choose while the box's new session starts stays chosen when the
// session is listed: the box writes where you chose.
func TestTheListKeepsTheRowYouChoseWhileANewSessionStarts(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	newFakeWezterm(t, tabPanes()...)
	newAskID(t, "n-1")
	bang(m)
	press(m, common.Hotkeys.SessionNew[0])
	typeInto(m, "group the logos")
	press(m, "enter")
	require.Contains(t, m.ask.starts, "n-1")

	press(m, "esc") // back to the Sessions box
	require.Equal(t, dockSessionsFocus, m.focusPanel)
	require.True(t, m.sessions.list.Select("s50"))
	press(m, common.Hotkeys.OpenAskBar[0]) // and write to s50
	require.Equal(t, "s50", m.sessions.list.SelectedID())

	listed := m.claude.snapshot
	listed.Live = append(slices.Clone(listed.Live), claudesession.Session{ID: "n-1", Title: "logos", Live: true,
		TTY: "/dev/ttys030", Cwd: "/repo"})
	TeaUpdate(m, claudeSessionsSnapshotMsg{snapshot: listed, folder: "/repo", layoutGen: m.claude.layoutGen})

	assert.True(t, slices.ContainsFunc(m.dockSessions(), func(s claudesession.Session) bool { return s.ID == "n-1" }),
		"it is listed")
	assert.Equal(t, "s50", m.sessions.list.SelectedID(), "the row you chose stays")
	assert.NotContains(t, m.claude.flash, "left the list")
}

// A process on superfile's own terminal is no session pane. Were it taken
// for one, @ and the Ask bar would type into superfile itself.
func TestAProcessOnSuperfilesTerminalHasNoSessionPane(t *testing.T) {
	m := newClaudeTestModel(t)
	m.claude.panes = []weztermPane{
		{PaneID: 10, TabID: 1, TTYName: "/dev/ttys010", Size: weztermPaneSize{Cols: 100}},
		{PaneID: 20, TabID: 1, TTYName: "/dev/ttys020", Size: weztermPaneSize{Cols: 99}},
	}
	ask := claudesession.Session{ID: "ask", Live: true, TTY: "/dev/ttys010"}
	shown := claudesession.Session{ID: "shown", Live: true, TTY: "/dev/ttys020"}
	m.claude.snapshot.Live = []claudesession.Session{ask, shown}

	_, ok := m.weztermPaneForSession(ask)
	assert.False(t, ok)
	s, pane, ok := m.visibleClaudeSessionPane()
	require.True(t, ok)
	assert.Equal(t, "shown", s.ID)
	assert.Equal(t, 20, pane.PaneID)
}

func TestTheBarReadsGitStatusAgain(t *testing.T) {
	ownAskHistory(t)
	m, dir := askTestModel(t)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	m.sidebarModel.SetGitStatus(sidebar.FetchGitStatus(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed by a session\n"), 0o600))
	assert.Equal(t, 0, m.askContext().GitChanged, "what superfile knew before")

	for _, msg := range batchMsgs(t, press(m, common.Hotkeys.OpenAskBar[0])) {
		TeaUpdate(m, msg)
	}
	assert.Equal(t, 1, m.askContext().GitChanged)
}

// batchMsgs runs a command and the commands it batches.
func batchMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := ExecuteTeaCmdWithTimeout(cmd, 2*time.Second)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, batchMsgs(t, c)...)
	}
	return msgs
}
