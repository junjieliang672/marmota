package internal

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// fakeCodex stands in for Codex's server: it keeps what was sent and named.
type fakeCodex struct {
	mu      sync.Mutex
	sent    []string // id: prompt
	turns   []codexsession.TurnOptions
	renamed []string // id: name
}

func newFakeCodex(t *testing.T) *fakeCodex {
	t.Helper()
	f := &fakeCodex{}
	send, rename := codexSend, codexRename
	codexSend = func(_, id, text string, o codexsession.TurnOptions) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sent = append(f.sent, id+": "+text)
		f.turns = append(f.turns, o)
		return nil
	}
	codexRename = func(_, id, name string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.renamed = append(f.renamed, id+": "+name)
		return nil
	}
	t.Cleanup(func() { codexSend, codexRename = send, rename })
	return f
}

// runCmd runs a command and what it batches, feeding the messages back.
func runCmd(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range batchMsgs(t, cmd) {
		TeaUpdate(m, msg)
	}
}

func TestCodexPaneTitles(t *testing.T) {
	for title, want := range map[string][2]string{
		"Read a.txt and create made.txt | codexlab":                    {"Read a.txt and create made.txt", "codexlab"},
		"[ ! ] Action Required | Read a.txt and create made.txt | lab": {"Read a.txt and create made.txt", "lab"},
		"codexlab":          {"", "codexlab"},
		"⠋ Fix login | web": {"Fix login", "web"},
	} {
		name, folder, ok := codexPaneTitle(title)
		require.True(t, ok, title)
		assert.Equal(t, want, [2]string{name, folder}, title)
	}
	_, _, ok := codexPaneTitle("~/Developer/superfile")
	assert.False(t, ok, "a shell's title is a path")
}

// Codex sessions join the list: those in panes are listed like Claude
// Code's, one working with no window runs in the background, and one idle
// with no window is finished.
func TestCodexSessionsJoinTheList(t *testing.T) {
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	base := filepath.Base(dir)
	panes := []weztermPane{
		{PaneID: 10, TabID: 1, Size: weztermPaneSize{Cols: 100}},
		{PaneID: 30, TabID: 1, TTYName: "/dev/ttys030", Title: "[ ! ] Action Required | Fix login | " + base},
		{PaneID: 31, TabID: 7, TTYName: "/dev/ttys031", Title: base},
	}
	now := time.Now()
	codex := func(id, title string, status claudesession.Status) claudesession.Session {
		return claudesession.Session{ID: id, Agent: claudesession.AgentCodex, Title: title, Cwd: dir, Live: true,
			Status: status, StartedAt: now, LastActivity: now}
	}
	msg := claudeSessionsSnapshotMsg{folder: dir, panes: panes, panesListed: true, layoutGen: m.claude.layoutGen,
		codex: []claudesession.Session{
			codex("named", "Fix login", claudesession.StatusWaiting),
			codex("fresh", "", claudesession.StatusIdle),
			codex("closed", "Old work", claudesession.StatusIdle),
			codex("bg", "Runs alone", claudesession.StatusWorking),
		}}
	TeaUpdate(m, msg)

	live := map[string]claudesession.Session{}
	for _, s := range m.claude.snapshot.Live {
		live[s.ID] = s
	}
	require.Len(t, live, 3)
	assert.Equal(t, "/dev/ttys030", live["named"].TTY, "found by the title Codex gave its pane")
	assert.Equal(t, "/dev/ttys031", live["fresh"].TTY, "no name yet: its pane shows only the folder")
	assert.True(t, live["bg"].Background, "working with no window")
	closed, ok := m.claude.snapshot.Find("closed")
	require.True(t, ok)
	assert.False(t, closed.Live, "idle with no window: nothing runs")
	assert.Equal(t, claudesession.StatusEnded, closed.Status)

	bang(m)
	var rows []string
	for _, s := range m.dockSessions() {
		rows = append(rows, s.ID)
	}
	assert.ElementsMatch(t, []string{"named", "fresh"}, rows, "the Sessions box lists those in panes")
	assert.Equal(t, claudesession.StatusWaiting, live["named"].Status)

	now = time.Now()
	codexRow := ansi.Strip(strings.Join(m.dockRow(1, live["named"], false, 40, now), "\n"))
	assert.Regexp(t, `1 \S co Fix login`, codexRow, "co: Codex")
	claudeRow := ansi.Strip(strings.Join(m.dockRow(2, claudesession.Session{ID: "c", Title: "Tidy", Live: true},
		false, 40, now), "\n"))
	assert.Regexp(t, `2 \S cc Tidy`, claudeRow, "cc: Claude Code")
}

// Choosing Codex's model in the Ask box starts a Codex session. Its id
// isn't known until it is found, the first in its folder since it began;
// what you send it meanwhile goes to it through Codex once it is.
func TestTheAskBoxStartsACodexSession(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	wez := newFakeWezterm(t, superfileTab())
	cx := newFakeCodex(t)
	m.sessions.composer.SetStartsCodex(true)
	press(m, common.Hotkeys.OpenAskBar[0])
	assert.Contains(t, viewText(m), "model codex default · ctrl+t effort medium", "no model in Codex's config; medium")

	ask(t, m, "group the logos")

	command := startCommand(t, wez)
	assert.True(t, strings.HasSuffix(command, " -- codex --dangerously-bypass-approvals-and-sandbox"), command)
	require.Len(t, m.ask.starts, 1)
	var st *askStart
	for _, s := range m.ask.starts {
		st = s
	}
	require.True(t, st.codex)
	pending := st.id
	assert.Equal(t, pending, m.ask.folderSessions[dir])

	typeInto(m, "and the icons")
	assert.Nil(t, press(m, "enter"), "it waits for the session to be found")
	assert.Len(t, st.queued, 2, "the question, then this")

	// One that began before isn't it; the one that began since is.
	m.claude.snapshot.Live = []claudesession.Session{
		{ID: "older", Agent: claudesession.AgentCodex, Cwd: dir, Live: true, StartedAt: st.began.Add(-time.Hour)},
		{ID: "t1", Agent: claudesession.AgentCodex, Title: "Group the logos", Cwd: dir, Live: true,
			Status: claudesession.StatusWorking, StartedAt: st.began},
	}
	runCmd(t, m, m.applyAskStartTick(askStartTickMsg{id: pending}))

	assert.Empty(t, m.ask.starts, "found, it is no longer watched")
	assert.Equal(t, st.paneID, m.codex.panes["t1"], "its pane is known")
	assert.Equal(t, "t1", m.ask.folderSessions[dir], "it is the folder's session")
	require.Len(t, cx.sent, 2)
	assert.True(t, strings.HasPrefix(cx.sent[0], "t1: group the logos\n\n<marmota-context>"), cx.sent[0])
	assert.True(t, strings.HasPrefix(cx.sent[1], "t1: and the icons\n\n<marmota-context>"), cx.sent[1])
	assert.Equal(t, []codexsession.TurnOptions{{Effort: "medium"}, {Effort: "medium"}}, cx.turns,
		"medium, not the effort Codex's config.toml sets")
	assert.Equal(t, "Sent to Group the logos", m.claude.flash)
}

// Codex asks whether to trust a git repository it hasn't seen before its
// session starts: its row says so, as a Claude Code one's does, and its pane
// stays in the background.
func TestACodexStartAsksToTrustItsFolderInItsRow(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	m.sessions.composer.SetStartsCodex(true)
	ask(t, m, "group the logos")
	require.Len(t, m.ask.starts, 1)
	var st *askStart
	for _, s := range m.ask.starts {
		st = s
	}

	m.applyAskStartTick(askStartTickMsg{id: st.id, screen: codexTrustScreen})
	assert.NotEqual(t, st.paneID, wez.active, "the keyboard stays in Marmota")
	assert.Equal(t, managedAgentHiddenWorkspace, wez.panes[st.paneID].Workspace)
	rows := m.dockSessions()
	require.Len(t, rows, 1)
	assert.True(t, rows[0].IsCodex())
	assert.Equal(t, sessionsui.NeedsYou, m.sessionState(rows[0]))
	assert.Equal(t, "Trust this folder?", rows[0].PendingTool)
	assert.Equal(t, "The new session in "+filepath.Base(m.getFocusedFilePanel().location)+
		" asks: Trust this folder? "+common.Hotkeys.OpenAskBar[0]+" in the Sessions box answers it", m.claude.flash)
}

// tab chooses whether a new session runs in Claude Code or Codex. The box
// says which; once a session has started it goes back to Claude Code, and a
// running session's agent can't change.
func TestTheAskBoxChoosesTheAgentOfANewSession(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	press(m, common.Hotkeys.OpenAskBar[0])
	view := viewText(m)
	assert.Contains(t, view, "Ask → new Claude Code session in")
	assert.Contains(t, view, "tab Codex")

	press(m, "tab")
	view = viewText(m)
	assert.Contains(t, view, "Ask → new Codex session in")
	assert.Contains(t, view, "tab Claude Code")
	assert.Empty(t, m.sessions.composer.Value(), "tab types nothing")
	press(m, common.Hotkeys.AskChooseModel[0])
	typeInto(m, "group the logos")
	press(m, "enter")
	assert.False(t, m.sessions.composer.StartsCodex(), "the next new session is Claude Code's again")
	model, effort := m.sessions.composer.CodexChoices()
	assert.Equal(t, [2]string{"", ""}, [2]string{model, effort})

	d := dockTestModel(t)
	bang(d)
	press(d, common.Hotkeys.OpenAskBar[0])
	require.False(t, d.sessions.list.NewSelected(), "writing to a running session")
	press(d, "tab")
	assert.False(t, d.sessions.composer.StartsCodex(), "its agent is its own")
	assert.NotContains(t, viewText(d), "tab Codex")
	press(d, common.Hotkeys.AskChooseModel[0])
	model, _ = d.sessions.composer.Choices()
	assert.Empty(t, model, "the new session's choice stays")
	assert.Contains(t, viewText(d), "Claude Code would save a new model or effort as your", "said in the box")
}

// The model is a flag. An effort, or Codex reviewing the approval requests
// (auto), goes with the question, sent through Codex's server once the
// session is found: as flags, Codex would run the session on a server of
// its own, out of superfile's sight.
func TestACodexStartGetsTheChosenModelAndEffort(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	wez := newFakeWezterm(t, superfileTab())
	cx := newFakeCodex(t)
	m.sessions.composer.SetStartsCodex(true)
	m.sessions.composer.SetCodexChoices("gpt-6-luna", "max")
	m.sessions.composer.SetPermissionMode("auto")
	ask(t, m, "group the logos")

	command := startCommand(t, wez)
	assert.True(t, strings.HasSuffix(command,
		" -- codex --sandbox workspace-write --ask-for-approval on-request --model gpt-6-luna"), command)
	require.Len(t, m.ask.starts, 1)
	var st *askStart
	for _, s := range m.ask.starts {
		st = s
	}
	require.Len(t, st.queued, 1, "the question waits for the session")

	m.claude.snapshot.Live = []claudesession.Session{{ID: "t1", Agent: claudesession.AgentCodex, Cwd: dir,
		Live: true, Status: claudesession.StatusIdle, StartedAt: st.began}}
	runCmd(t, m, m.applyAskStartTick(askStartTickMsg{id: st.id}))
	require.Len(t, cx.sent, 1)
	assert.True(t, strings.HasPrefix(cx.sent[0], "t1: group the logos\n\n<marmota-context>"), cx.sent[0])
	assert.Equal(t, []codexsession.TurnOptions{{Effort: "max", Reviewer: "auto_review"}}, cx.turns)
}

// The folded Ask box says which program, model and effort the next message
// goes to: a running session's own, or what a new session gets.
func TestTheFoldedAskBoxSaysTheProgramAndModel(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	idle := func(s claudesession.Session) string {
		return ansi.Strip(strings.Join(m.dockAskIdleLines(s, 30, 8), "\n"))
	}
	assert.Contains(t, idle(claudesession.Session{}), "cc default · medium", "a new session: the defaults")
	m.sessions.composer.SetStartsCodex(true)
	assert.Contains(t, idle(claudesession.Session{}), "co default · medium")
	codex := claudesession.Session{ID: "t1", Agent: claudesession.AgentCodex, Title: "fix", Live: true,
		Model: "gpt-6.1-sol", Effort: "low"}
	assert.Contains(t, idle(codex), "co gpt-6.1-sol · low")

	codex.LastPrompt = "can you show a checkbox instead of me answering the question in words"
	lines := m.dockAskIdleLines(codex, 30, 6)
	assert.Len(t, lines, 6)
	assert.Contains(t, ansi.Strip(lines[5]), "replied", "how it stands keeps its row over the message's second")
}

// A second Codex start in a folder waits for the first to be found: they
// would be told apart only by luck.
func TestASecondCodexStartInAFolderWaits(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	m.sessions.composer.SetStartsCodex(true)
	ask(t, m, "first")
	m.sessions.pendingSelect = "" // as when you chose another row since
	m.sessions.list.SelectNew()
	m.sessions.composer.SetStartsCodex(true)
	typeInto(m, "second")
	press(m, "enter")
	assert.Len(t, m.ask.starts, 1)
	assert.Equal(t, "second", m.sessions.composer.Value(), "your text stays")
}

func TestSendingToACodexSessionGoesThroughCodex(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("notes"), 0o600))
	cx := newFakeCodex(t)
	m.claude.panes = []weztermPane{
		{PaneID: 10, TabID: 1, Size: weztermPaneSize{Cols: 100}},
		{PaneID: 30, TabID: 1, TTYName: "/dev/ttys030", Size: weztermPaneSize{Cols: 99}},
	}
	m.claude.snapshot.Live = []claudesession.Session{{ID: "t1", Agent: claudesession.AgentCodex, Title: "fix sidebar",
		Live: true, Status: claudesession.StatusWorking, TTY: "/dev/ttys030", Cwd: dir, Model: "gpt-6.1-sol",
		Effort: "low"}}

	bang(m)
	require.True(t, m.sessions.list.Select("t1"))
	press(m, common.Hotkeys.OpenAskBar[0])
	view := viewText(m)
	assert.Contains(t, view, "sent to it (beside Marmota)")
	assert.Contains(t, view, "ctrl+o model gpt-6.1-sol · ctrl+t effort low", "its own, which the keys change")

	// A model and effort chosen for it go with the next message.
	m.sessions.composer.SetCodexModels([]codexsession.Model{
		{ID: "gpt-6.1-sol", Efforts: []string{"low", "medium", "high"}},
		{ID: "gpt-6-luna", Efforts: []string{"low", "medium"}},
	}, "gpt-6.1-sol")
	press(m, common.Hotkeys.AskChooseModel[0])
	press(m, common.Hotkeys.AskChooseModel[0])
	press(m, common.Hotkeys.AskChooseEffort[0])
	press(m, common.Hotkeys.AskChooseEffort[0])
	view = viewText(m)
	assert.Contains(t, view, "model gpt-6-luna · ctrl+t effort medium · from your next message")
	typeInto(m, "also update the docs")
	runCmd(t, m, press(m, "enter"))
	assert.Equal(t, []codexsession.TurnOptions{{Model: "gpt-6-luna", Effort: "medium"}}, cx.turns)

	require.Len(t, cx.sent, 1, "not typed into its pane: Codex takes it while it works")
	assert.True(t, strings.HasPrefix(cx.sent[0], "t1: also update the docs\n\n<marmota-context>"))
	assert.Regexp(t, `notes: +`+regexp.QuoteMeta(filepath.Join(dir, "AGENTS.md")), cx.sent[0], "Codex's notes are AGENTS.md")
	assert.Equal(t, "Sent to fix sidebar", m.claude.flash)
}

func TestRenamingACodexSessionAsksCodex(t *testing.T) {
	m := newClaudeTestModel(t)
	cx := newFakeCodex(t)
	m.claude.snapshot.Live = []claudesession.Session{{ID: "t1", Agent: claudesession.AgentCodex, Title: "old",
		Live: true}}
	runCmd(t, m, m.renameClaudeSession("t1", "Login fix"))
	assert.Equal(t, []string{"t1: Login fix"}, cx.renamed)
	assert.Equal(t, "Renamed to Login fix", m.claude.flash)
	s, _ := m.claude.snapshot.Find("t1")
	assert.Equal(t, "Login fix", s.Title, "shown at once")
}

// What Codex draws when it asks for an approval, as captured from Codex
// 0.160.
const codexApprovalScreen = `› Read a.txt and tell me what it says. Then create a file made.txt containing …
• Explored
  └ Read a.txt
    + Show details
• Running printf hi > made.txt

  Would you like to run the following command?

  Environment: local
  Reason: Allow creating made.txt containing hi in this read-only workspace?

  $ printf hi > made.txt

› 1. Yes, proceed (y)
  2. Yes, and don't ask again for commands that start with ` + "`printf hi >" + `
     made.txt` + "`" + ` (p)
  3. No, and tell Codex what to do differently (esc)

  Press enter to confirm or esc to cancel
`

const codexPromptScreen = `• a.txt says hello.
  Worked for 1m 23s • 1:15 AM
› Ask Codex to do anything
  GPT-6.1-Sol low · /private/tmp/codexlab
  ← for agents · ? for shortcuts
`

// codexNarrowApprovalScreen is Codex asking in a pane 40 columns wide: the
// line that says how to answer wraps.
const codexNarrowApprovalScreen = `• Running printf yes > approved.txt


  Would you like to run the following
  command?

  $ printf yes > approved.txt

› 1. Yes, proceed (y)
  3. No, and tell Codex what to do
     differently (esc)

  Press enter to confirm or esc to
  cancel
`

func TestCodexQuestionsAreCutFromItsScreen(t *testing.T) {
	lines, over := codexQuestionLines(codexApprovalScreen)
	require.False(t, over)
	require.NotEmpty(t, lines)
	assert.Equal(t, "• Running printf hi > made.txt", lines[0])
	assert.Equal(t, "  Press enter to confirm or esc to cancel", lines[len(lines)-1])
	assert.Contains(t, lines, "› 1. Yes, proceed (y)")
	assert.NotContains(t, lines, "", "Codex's blank lines are left out, so its options fit")
	assert.Len(t, lines, 10)

	lines, over = codexQuestionLines(codexNarrowApprovalScreen)
	require.False(t, over)
	assert.Equal(t, []string{
		"• Running printf yes > approved.txt",
		"  Would you like to run the following",
		"  command?",
		"  $ printf yes > approved.txt",
		"› 1. Yes, proceed (y)",
		"  3. No, and tell Codex what to do",
		"     differently (esc)",
		"  Press enter to confirm or esc to",
		"  cancel",
	}, lines, "a wrapped hint is still the end")

	lines, over = codexQuestionLines(codexPromptScreen)
	assert.True(t, over, "its prompt is back: answered")
	assert.Empty(t, lines)
}

// A fork's pane has the forked session's title, so a title can't tell the
// two apart: the fork is known by the pane superfile started it in, and the
// session it was forked from keeps its own.
func TestACodexForkIsKnownByItsPane(t *testing.T) {
	m := newClaudeTestModel(t)
	dir := m.getFocusedFilePanel().location
	utils.SetupDirectories(t, filepath.Join(dir, ".git"))
	base := filepath.Base(dir)
	now := time.Now()
	orig := claudesession.Session{ID: "orig", Agent: claudesession.AgentCodex, Title: "Fix login", Cwd: dir,
		Live: true, Status: claudesession.StatusIdle, StartedAt: now.Add(-time.Hour), LastActivity: now}
	m.claude.snapshot.Live = []claudesession.Session{orig}

	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, Size: weztermPaneSize{Rows: 53, Cols: 223}},
		)},
		fakeExecResult{retCode: 0, output: "61\n"},
		fakeExecResult{retCode: 0, output: ""},
	)
	m.forkClaudeSession(orig)
	require.Len(t, *calls, 3)
	assert.Contains(t, (*calls)[1].cmd, "codex fork orig --worktree")
	require.Len(t, m.codex.forks, 1)
	assert.Equal(t, 61, m.codex.forks[0].paneID)

	worktree := filepath.Join(t.TempDir(), base) // Codex's worktree has the folder's name
	fork := orig
	fork.ID, fork.ForkedFrom, fork.Cwd, fork.StartedAt = "fork", "orig", worktree, now.Add(time.Second)
	title := "Fix login | " + base
	TeaUpdate(m, claudeSessionsSnapshotMsg{folder: dir, panesListed: true, layoutGen: m.claude.layoutGen,
		panes: []weztermPane{
			{PaneID: 10, TabID: 1, Size: weztermPaneSize{Cols: 100}},
			{PaneID: 30, TabID: 7, TTYName: "/dev/ttys030", Title: title},
			{PaneID: 61, TabID: 1, TTYName: "/dev/ttys061", Title: title},
		},
		codex: []claudesession.Session{fork, orig}})

	got := map[string]string{}
	for _, s := range m.claude.snapshot.Live {
		got[s.ID] = s.TTY
	}
	assert.Equal(t, map[string]string{"fork": "/dev/ttys061", "orig": "/dev/ttys030"}, got)
	assert.Empty(t, m.codex.forks, "found")

	// The next superfile reads the fork's pane back from the agent panes'
	// state, both idle and listed the other way round.
	m.codex = codexState{}
	TeaUpdate(m, claudeSessionsSnapshotMsg{folder: dir, panesListed: true, layoutGen: m.claude.layoutGen,
		panes: []weztermPane{
			{PaneID: 10, TabID: 1, Size: weztermPaneSize{Cols: 100}},
			{PaneID: 61, TabID: 1, TTYName: "/dev/ttys061", Title: title},
			{PaneID: 30, TabID: 7, TTYName: "/dev/ttys030", Title: title},
		},
		codex: []claudesession.Session{orig, fork}})
	got = map[string]string{}
	for _, s := range m.claude.snapshot.Live {
		got[s.ID] = s.TTY
	}
	assert.Equal(t, map[string]string{"fork": "/dev/ttys061", "orig": "/dev/ttys030"}, got, "after a restart")

	// With no state either, the pane that says it needs you is the waiting
	// session's.
	m.codex = codexState{restored: true}
	orig.Status = claudesession.StatusWaiting
	TeaUpdate(m, claudeSessionsSnapshotMsg{folder: dir, panesListed: true, layoutGen: m.claude.layoutGen,
		panes: []weztermPane{
			{PaneID: 10, TabID: 1, Size: weztermPaneSize{Cols: 100}},
			{PaneID: 61, TabID: 1, TTYName: "/dev/ttys061", Title: title},
			{PaneID: 30, TabID: 7, TTYName: "/dev/ttys030", Title: "[ ! ] Action Required | " + title},
		},
		codex: []claudesession.Session{orig, fork}})
	got = map[string]string{}
	for _, s := range m.claude.snapshot.Live {
		got[s.ID] = s.TTY
	}
	assert.Equal(t, map[string]string{"fork": "/dev/ttys061", "orig": "/dev/ttys030"}, got)
}

func TestACodexSessionsDiffIsWhatItRecorded(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello world\n"), 0o600))
	rollout := filepath.Join(dir, "rollout.jsonl")
	require.NoError(t, os.WriteFile(rollout, []byte(`{"timestamp":"2026-11-08T09:16:57Z","type":"event_msg",`+
		`"payload":{"type":"item_completed","item":{"type":"FileChange","changes":{"`+file+
		`":{"type":"update","unified_diff":"@@ -1 +1 @@\n-hello\n+hello world\n"}}}}}`+"\n"), 0o600))

	v := buildDiffView("", touchedSessionRef{id: "t1", transcript: rollout, title: "Edit a", codex: true}, file, 80)
	header := strings.Join(v.header, "\n")
	assert.Contains(t, header, "changed by Codex: 1 change, as they were made")
	var body []string
	for _, l := range v.body {
		body = append(body, l.text)
	}
	assert.Contains(t, strings.Join(body, "\n"), "hello world")
	assert.Len(t, v.hunks, 1)

	none := buildDiffView("", touchedSessionRef{transcript: rollout, codex: true}, filepath.Join(dir, "b.txt"), 80)
	assert.Contains(t, strings.Join(none.header, "\n"), "Codex recorded no change to this file.")
}

func TestCodexMentionsSayWhyNot(t *testing.T) {
	m := newClaudeTestModel(t)
	s := claudesession.Session{ID: "t1", Agent: claudesession.AgentCodex, Title: "x", Live: true}
	m.mentionClaudeSession(s)
	assert.Equal(t, "Codex sessions have no name other sessions can mention", m.claude.flash)
}

// = moves a Codex session through Codex's server, which has no /cd, and its
// pane stays its own though the pane's title still names the old folder.
func TestEqualsMovesACodexSessionHere(t *testing.T) {
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	here := m.getFocusedFilePanel().location
	pane := weztermPane{PaneID: 31, TabID: 5, Title: "fix the login | web", TTYName: "/dev/ttys031"}
	m.claude.panes = append(m.claude.panes, pane)
	s := claudesession.Session{ID: "t1", Agent: claudesession.AgentCodex, Title: "fix the login", Cwd: "/w/web",
		Live: true, Status: claudesession.StatusWorking, TTY: pane.TTYName}
	var moved []string
	move := codexMove
	t.Cleanup(func() { codexMove = move })
	codexMove = func(_, id, dir string) error {
		moved = append(moved, id+": "+dir)
		return nil
	}

	m.claude.snapshot.Live = []claudesession.Session{s}
	runCmd(t, m, m.moveClaudeSessionTo(s))
	assert.Equal(t, []string{"t1: " + here}, moved)
	assert.Equal(t, "fix the login moves to "+tildePath(here)+" once it's done replying", m.claude.flash,
		"from its next turn on")
	assert.Equal(t, 31, m.codex.panes["t1"], "its pane is known by the session")
	assert.Equal(t, here, m.claude.snapshot.Live[0].Cwd, "listed in its new folder at once")

	// Codex's list says the old folder until its next turn's record in its
	// rollout says the new one.
	m.claude.snapshot.Live = nil
	m.mergeCodexSessions([]claudesession.Session{s})
	require.Len(t, m.claude.snapshot.Live, 1)
	assert.Equal(t, here, m.claude.snapshot.Live[0].Cwd)
	moved1 := s
	moved1.Cwd = here
	m.claude.snapshot.Live = nil
	m.mergeCodexSessions([]claudesession.Session{moved1})
	assert.Empty(t, m.codex.moved, "the rollout caught up")

	codexMove = func(string, string, string) error { return errors.New("no server") }
	s.Status = claudesession.StatusIdle
	runCmd(t, m, m.moveClaudeSessionTo(s))
	assert.Equal(t, "Couldn't move fix the login: no server", m.claude.flash)
}
