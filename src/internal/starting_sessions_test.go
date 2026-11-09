package internal

import (
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// The screens Claude Code 2.1 and Codex 0.160 drew in a folder they hadn't
// seen, read with wezterm cli get-text.
const (
	claudeTrustScreen = `
────────────────────────────────────────────────────────────────────────────────
 Do you trust the files in this folder?

 /w/logos

 Claude Code may read, write, or execute files contained in this directory.
 This can pose security risks, so only use files from trusted sources.

 Learn more

 ❯ 1. Yes, proceed
   2. No, exit

 Enter to confirm · Esc to cancel
`
	claudeBypassScreen = `╭──────────────────────────────────────────────────────────────────────────────╮
│                                                                              │
│ WARNING: Claude Code running in Bypass Permissions mode                      │
│                                                                              │
│ In Bypass Permissions mode, Claude Code will not ask for your approval       │
│ before running potentially dangerous commands.                               │
│                                                                              │
│ ❯ 1. No, exit                                                                │
│   2. Yes, I accept                                                           │
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯
   Enter to confirm · Esc to cancel
`
	codexTrustScreen = `
  Folder access
  /w/logos

  Trust this folder? Codex can read, edit, and run files here, subject to your
  permission settings. Folder settings can run code automatically, even
  without a model request. Continue only if you trust these files. Your trust
  decision will be saved.

› 1. Trust and continue
  2. Back to Agent Command Center

  enter continue · esc back
`
)

// Only what a starting session asks, and its choices, come out of its
// screen.
func TestStartDialogCutsOutTheQuestion(t *testing.T) {
	for name, c := range map[string]struct {
		screen, question string
		choices          []string
	}{
		"claude trust":  {claudeTrustScreen, "Do you trust the files in this folder?", []string{"❯ 1. Yes, proceed", "  2. No, exit"}},
		"claude bypass": {claudeBypassScreen, "WARNING: Claude Code running in Bypass Permissions mode", []string{"❯ 1. No, exit", "  2. Yes, I accept"}},
		"codex trust":   {codexTrustScreen, "Trust this folder?", []string{"› 1. Trust and continue", "  2. Back to Agent Command Center"}},
		"claude, unnumbered": {readPromptTestdata(t, "trust-question.txt"),
			"Is this a directory you created or one you trust?", []string{"❯ No, stay put", "  Yes, move here"}},
	} {
		lines, question := startDialog(c.screen)
		assert.Equal(t, c.question, question, name)
		assert.Equal(t, append([]string{c.question, ""}, c.choices...), lines, name)
	}
	for name, screen := range map[string]string{
		"its prompt":          readPromptTestdata(t, "empty.txt"),
		"a list in its reply": "⏺ Two ways:\n ❯ 1. move them\n   2. copy them\n",
	} {
		lines, question := startDialog(screen)
		assert.Nil(t, lines, name)
		assert.Empty(t, question, name)
	}
}

// startPane stands in for the pane of a session the Ask box started: what it
// shows, and the keys typed into it.
type startPane struct {
	mu     sync.Mutex
	id     int
	screen string
	keys   []string
}

func newStartPane(t *testing.T, id int, screen string) *startPane {
	t.Helper()
	p := &startPane{id: id, screen: screen}
	getText, sendKeys := weztermGetText, weztermSendKeys
	weztermGetText = func(id int) (string, error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		require.Equal(t, p.id, id)
		return p.screen, nil
	}
	weztermSendKeys = func(id int, keys string) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		require.Equal(t, p.id, id)
		p.keys = append(p.keys, keys)
		if keys == claudeKeyEnter {
			p.screen = "starting" // answered
		}
		return nil
	}
	t.Cleanup(func() { weztermGetText, weztermSendKeys = getText, sendKeys })
	return p
}

// A new session that first asks whether to trust its folder stays in the
// background: its row says what it asks, the preview shows the question and
// its choices, and K answers it there.
func TestANewSessionsTrustQuestionIsAnsweredInThePreview(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	wez := newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	paneID := m.ask.starts["n-1"].paneID
	pane := newStartPane(t, paneID, claudeTrustScreen)

	runCmd(t, m, m.applyAskStartTick(askStartTickMsg{id: "n-1", screen: claudeTrustScreen}))
	assert.Equal(t, managedAgentHiddenWorkspace, wez.panes[paneID].Workspace, "it stays in the background")
	assert.NotEqual(t, paneID, wez.active, "and the keyboard in Marmota")
	rows := m.dockSessions()
	require.Len(t, rows, 1)
	assert.Equal(t, sessionsui.NeedsYou, m.sessionState(rows[0]))
	assert.Equal(t, "Do you trust the files in this folder?", rows[0].PendingTool)
	assert.Equal(t, "n-1", m.sessions.list.SelectedID(), "the Ask box writes to its row")
	view := viewText(m)
	assert.Contains(t, view, "asks: Do you trust")
	assert.Contains(t, view, "▎ Do you trust the files in this folder?")
	assert.Contains(t, view, "❯ 1. Yes, proceed")
	assert.NotContains(t, view, "Claude Code may read", "only the question and its choices")

	press(m, "esc")
	runQuestionRead(t, m, press(m, common.Hotkeys.JumpToUnreadSession[0]))
	require.Equal(t, dockSessionsFocus, m.focusPanel)
	require.Equal(t, "n-1", m.sessions.list.SelectedID(), "! lands on it")
	press(m, common.Hotkeys.OpenAskBar[0])
	require.Equal(t, sessionsFocusAnswer, m.sessions.focus, "K hands the keyboard to the question")
	runQuestionRead(t, m, press(m, "enter"))
	assert.Equal(t, []string{claudeKeyEnter}, pane.keys)
	assert.Equal(t, sessionsFocusList, m.sessions.focus, "answered, the keyboard comes back")
	assert.Equal(t, "Answered", m.claude.flash)
	assert.Equal(t, sessionsui.Working, m.sessionState(m.dockSessions()[0]))
	assert.NotContains(t, ansi.Strip(m.View()), "❯ 1. Yes, proceed")

	// Listed, its row is the session's.
	m.claude.snapshot.Live = []claudesession.Session{{ID: "n-1", Live: true, Status: claudesession.StatusWorking}}
	m.applyAskStartTick(askStartTickMsg{id: "n-1"})
	assert.Empty(t, m.ask.starts)
}

// enter on a starting session's row brings it beside Marmota, to see all it
// shows.
func TestEnterBringsAStartingSessionBesideMarmota(t *testing.T) {
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	newAskID(t, "n-1")
	ask(t, m, "group the logos")
	paneID := m.ask.starts["n-1"].paneID
	press(m, "esc")
	press(m, common.Hotkeys.JumpToUnreadSession[0])
	require.Equal(t, "n-1", m.sessions.list.SelectedID())

	press(m, common.Hotkeys.SessionRename[0])
	assert.True(t, strings.HasPrefix(m.claude.flash, "It hasn't started yet"), m.claude.flash)
	press(m, "enter")
	assert.Equal(t, 1, wez.panes[paneID].TabID, "beside Marmota")
	assert.Equal(t, paneID, wez.active, "with the keyboard")
}
