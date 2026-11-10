package internal

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// resumeTestModel is a model with ended sessions to resume: two of Claude
// Code's, the older one in the panel's folder, and one of Codex's.
func resumeTestModel(t *testing.T) (*model, *fakeWezterm) {
	t.Helper()
	ownAskHistory(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	here := m.getFocusedFilePanel().location
	now := time.Now()
	m.claude.snapshot.Past = []claudesession.Session{
		{ID: "cc-else", Title: "tidy the release notes", Cwd: t.TempDir(), LastActivity: now.Add(-time.Hour)},
		{ID: "cx-here", Title: "port the parser", Cwd: here, Agent: claudesession.AgentCodex,
			LastActivity: now.Add(-90 * time.Minute)},
		{ID: "cc-here", Title: "group the logos", Cwd: here, LastActivity: now.Add(-2 * time.Hour)},
	}
	return m, wez
}

func jumpIDs(m *model) []string {
	var ids []string
	for _, it := range m.jumpUI.Visible() {
		ids = append(ids, it.Key)
	}
	return ids
}

// pickJumpRow picks row n of the open s list, as [n]g does.
func pickJumpRow(m *model, n int) {
	if !m.jumpUI.Picking() {
		press(m, "down")
	}
	typeInto(m, strconv.Itoa(n))
	press(m, "g")
}

func TestCtrlRListsTheEndedSessionsOfBothAgents(t *testing.T) {
	m, _ := resumeTestModel(t)
	press(m, common.Hotkeys.OpenAskBar[0])
	require.True(t, m.askSending().New)
	assert.Contains(t, viewText(m), "ctrl+r resume one")

	press(m, common.Hotkeys.AskResume[0])

	require.True(t, m.jumpUI.IsOpen())
	assert.Equal(t, []string{"cc-else", "cx-here", "cc-here"}, jumpIDs(m), "the Resume section alone, newest first")
	view := viewText(m)
	assert.Contains(t, view, "Resume")
	assert.Contains(t, view, "port the parser")
	assert.NotContains(t, view, "Pinned", "the other sections aren't listed")

	pickJumpRow(m, 2)
	press(m, "enter")
	assert.False(t, m.jumpUI.IsOpen())
	assert.Equal(t, dockAskFocus, m.focusPanel, "back in the Ask box")
	assert.True(t, m.sessions.composer.StartsCodex(), "the box starts the agent it ran in")
	assert.Contains(t, viewText(m), "resumes port the parser in the background")
	assert.Contains(t, viewText(m), "ctrl+r another")

	press(m, "tab")
	assert.Nil(t, m.ask.resume, "the other agent: a new session again")
}

func TestTheResumeSectionShowsTheFiveUsedLastOfEachAgent(t *testing.T) {
	m, _ := resumeTestModel(t)
	now := time.Now()
	m.claude.snapshot.Past = nil
	for i := range 7 {
		m.claude.snapshot.Past = append(m.claude.snapshot.Past, claudesession.Session{
			ID: "cc-" + strconv.Itoa(i), Title: "work " + strconv.Itoa(i), Cwd: t.TempDir(),
			LastActivity: now.Add(-time.Duration(i) * time.Hour),
		})
	}
	m.resume.ended = []claudesession.Session{
		{ID: "cc-old", Title: "last week", Cwd: t.TempDir(), LastActivity: now.Add(-7 * 24 * time.Hour)},
		{ID: "cc-recent", Title: "read for the list", Cwd: t.TempDir(), LastActivity: now.Add(-30 * time.Minute)},
		{ID: "cx-1", Title: "a codex one", Agent: claudesession.AgentCodex, LastActivity: now.Add(-3 * 24 * time.Hour)},
	}

	var ids []string
	for _, s := range m.endedSessions(false) {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{"cc-0", "cc-recent", "cc-1", "cc-2", "cc-3"}, ids)
	require.Len(t, m.endedSessions(true), 1)
}

func TestEnterResumesThePickedSessionWithTheDraft(t *testing.T) {
	m, wez := resumeTestModel(t)
	press(m, common.Hotkeys.OpenAskBar[0])
	press(m, common.Hotkeys.AskResume[0])
	pickJumpRow(m, 3) // the folder's Claude Code session
	press(m, "enter")
	typeInto(m, "now the icons")

	press(m, "enter")

	command := startCommand(t, wez)
	assert.Contains(t, command, " -- claude --resume cc-here --dangerously-skip-permissions --effort medium -- 'now the icons")
	assert.NotContains(t, command, "--session-id")
	assert.Equal(t, "Resuming group the logos in the background", m.claude.flash)
	assert.Equal(t, "cc-here", m.sessions.list.SelectedID(), "the Sessions box is on its row, starting")
	assert.Nil(t, m.ask.resume, "the next one starts a new session")
	assert.Empty(t, m.sessions.composer.Value())
}

func TestEnterResumesACodexSessionWithTheDraft(t *testing.T) {
	m, wez := resumeTestModel(t)
	newFakeCodex(t)
	press(m, common.Hotkeys.OpenAskBar[0])
	press(m, common.Hotkeys.AskResume[0])
	pickJumpRow(m, 2)
	press(m, "enter")
	typeInto(m, "and the lexer")

	press(m, "enter")

	command := startCommand(t, wez)
	assert.Contains(t, command, " resume cx-here ")
	assert.Contains(t, command, " -- 'and the lexer")
	assert.NotContains(t, command, " -c ", "a config flag would hide the session from Marmota")
	assert.Equal(t, "Resuming port the parser in the background", m.claude.flash)
}

func TestEnterAloneResumesThePickedSessionInTheBackground(t *testing.T) {
	m, wez := resumeTestModel(t)
	press(m, common.Hotkeys.OpenAskBar[0])
	press(m, common.Hotkeys.AskResume[0])
	pickJumpRow(m, 3)
	press(m, "enter")

	press(m, "enter")

	command := startCommand(t, wez)
	assert.Contains(t, command, managedAgentHiddenWorkspace, "not beside Marmota")
	assert.Contains(t, command, "claude --resume cc-here --dangerously-skip-permissions")
	assert.Equal(t, "Resuming group the logos in the background", m.claude.flash)
	assert.Equal(t, "cc-here", m.sessions.list.SelectedID(), "the Sessions box is on its row, starting")
	assert.Nil(t, m.ask.resume)
}

func TestEscClosesTheResumeListAndKeepsANewSession(t *testing.T) {
	m, _ := resumeTestModel(t)
	press(m, common.Hotkeys.OpenAskBar[0])
	press(m, common.Hotkeys.AskResume[0])

	press(m, "esc")

	assert.False(t, m.jumpUI.IsOpen())
	assert.Equal(t, dockAskFocus, m.focusPanel, "back in the Ask box")
	assert.Nil(t, m.ask.resume)
	assert.Contains(t, viewText(m), "starts in the background")
}

func TestEnterOnAnEndedSessionInTheSPopupResumesItInTheBackground(t *testing.T) {
	m, wez := resumeTestModel(t)
	press(m, common.Hotkeys.GoTo[0])
	press(m, "tab")
	press(m, "tab")
	press(m, "tab") // the Resume section alone
	require.Equal(t, []string{"cc-else", "cx-here", "cc-here"}, jumpIDs(m))
	pickJumpRow(m, 3)

	press(m, "enter")

	command := startCommand(t, wez)
	assert.Contains(t, command, managedAgentHiddenWorkspace, "not beside Marmota")
	assert.Contains(t, command, "claude --resume cc-here --dangerously-skip-permissions")
	assert.Equal(t, dockSessionsFocus, m.focusPanel)
	assert.Equal(t, "cc-here", m.sessions.list.SelectedID(), "the Sessions box is on its row, starting")
	assert.Contains(t, viewText(m), "group the logos")

}
