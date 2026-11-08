package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

func readPromptTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "pkg", "claudeprompt", "testdata", name))
	require.NoError(t, err)
	return string(data)
}

func TestClaudeQuestionLinesCutTheDialogOut(t *testing.T) {
	lines, over := claudeQuestionLines(readPromptTestdata(t, "question.txt"))
	require.False(t, over)
	require.NotEmpty(t, lines)
	assert.Equal(t, "☐ Color", strings.TrimSpace(lines[0]), "it starts at the dialog")
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "Do you prefer red or blue?")
	assert.Contains(t, joined, "❯ 1. Red")
	assert.Contains(t, joined, "4. Chat about this")
	assert.Equal(t, "Enter to select · ↑/↓ to navigate · Esc to cancel", strings.TrimSpace(lines[len(lines)-1]))
	assert.NotContains(t, joined, "Stash test", "the rule with the session's name stays out")
	assert.NotContains(t, joined, "Session renamed", "and what was said before")

	lines, over = claudeQuestionLines(readPromptTestdata(t, "trust-question.txt"))
	require.False(t, over)
	assert.Equal(t, "Moving to a new directory:", strings.TrimSpace(lines[0]))
	assert.Contains(t, strings.Join(lines, "\n"), "Yes, move here")

	_, over = claudeQuestionLines(readPromptTestdata(t, "empty.txt"))
	assert.True(t, over, "a prompt box means there is no question")
}

// waitingTestModel has a session beside Marmota, in pane 20, asking a
// question.
func waitingTestModel(t *testing.T) (*model, *fakeClaudePane) {
	t.Helper()
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	m.claude.snapshot.Live[0].Status = claudesession.StatusWaiting
	m.claude.snapshot.Live[0].PendingTool = "Do you prefer red or blue?"
	pane := newFakeClaudePane(t)
	pane.question = true
	return m, pane
}

// runQuestionRead applies the read of the question a command starts, as
// the event loop would; the poll it schedules is left alone.
func runQuestionRead(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	for _, msg := range batchMsgs(t, cmd) {
		switch msg.(type) {
		case claudeQuestionMsg, claudeKeysSentMsg:
			runQuestionRead(t, m, TeaUpdate(m, msg))
		}
	}
}

func TestAQuestionIsAnsweredFromTheSessionsBox(t *testing.T) {
	m, pane := waitingTestModel(t)

	runQuestionRead(t, m, press(m, common.Hotkeys.JumpToUnreadSession[0]))
	require.Equal(t, "s20", m.sessions.list.SelectedID(), "! lands on the session that needs you")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "Question")
	assert.Contains(t, view, "Do you prefer red or blue?")
	assert.Contains(t, view, "❯ 1. Red")
	assert.Contains(t, view, common.Hotkeys.OpenAskBar[0]+" to answer")

	press(m, common.Hotkeys.OpenAskBar[0])
	require.Equal(t, sessionsFocusAnswer, m.sessions.focus, "K hands the keyboard to the question")
	assert.Contains(t, ansi.Strip(m.View()), "keys go to it · "+askBackKey()+" back")
	assert.True(t, m.isTextInputActive(), "an answer can be typed, so the input method is let go")

	runQuestionRead(t, m, press(m, "down"))
	runQuestionRead(t, m, press(m, "2"))
	runQuestionRead(t, m, press(m, " "))
	assert.Equal(t, []string{`key:"\x1b[B"`, `key:"2"`, `key:" "`}, pane.keys, "keys go to its pane as typed")
	assert.Equal(t, sessionsFocusAnswer, m.sessions.focus)

	press(m, askBackKey())
	assert.Equal(t, sessionsFocusList, m.sessions.focus, "ctrl+k gives the keyboard back: K would be typed")

	press(m, common.Hotkeys.OpenAskBar[0])
	pane.mu.Lock()
	pane.question = false
	pane.mu.Unlock()
	runQuestionRead(t, m, press(m, "enter"))
	assert.Equal(t, sessionsFocusList, m.sessions.focus, "once it is answered, the keyboard comes back")
	assert.Equal(t, "Answered", m.claude.flash)
	assert.NotContains(t, ansi.Strip(m.View()), "❯ 1. Red")
}

// A session that isn't in a pane superfile can read says what it asks, and
// how to get to it.
func TestQuestionReadsForAnotherSessionAreDropped(t *testing.T) {
	m, _ := waitingTestModel(t)
	cmd := press(m, common.Hotkeys.JumpToUnreadSession[0])
	require.NotNil(t, cmd)
	read := ExecuteTeaCmdWithTimeout(cmd, 2*time.Second)

	pressKey(m, common.Hotkeys.ListDown[0])
	TeaUpdate(m, read)

	assert.Empty(t, m.sessions.question.lines, "the read was for the session before")
}
