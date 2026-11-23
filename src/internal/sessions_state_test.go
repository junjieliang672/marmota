package internal

import (
	"fmt"
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

// p puts a running session's mention in the question and copies it, for
// pasting into another session: Claude Code messages the session it names.
func TestTheSessionsBoxMentionsARunningSession(t *testing.T) {
	m := dockTestModel(t)
	m.claude.snapshot.Live[0].Name = "provider-registry"
	m.claude.snapshot.Live[1].Name = "ime hold"
	var copied []string
	original := writeClipboard
	writeClipboard = func(text string) error {
		copied = append(copied, text)
		return nil
	}
	t.Cleanup(func() { writeClipboard = original })
	toSessions(m)
	require.True(t, m.sessions.list.Select("s20"))

	pressKey(m, common.Hotkeys.SessionMention[0])
	require.True(t, m.sessions.list.Select("s50"))
	pressKey(m, common.Hotkeys.SessionMention[0])

	assert.Equal(t, []string{"@provider-registry", `@"ime hold"`}, copied)
	assert.Equal(t, `@provider-registry @"ime hold" `, m.sessions.composer.Value())
	assert.Equal(t, `Added @"ime hold" to your question, and copied it`, m.claude.flash)
}

func TestNInTheSessionsBoxStartsANewSessionInTheAskBox(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	toSessions(m)

	pressKey(m, common.Hotkeys.SessionNew[0])

	assert.Equal(t, dockAskFocus, m.focusPanel)
	assert.Equal(t, sessionsFocusAsk, m.sessions.focus)
	assert.True(t, m.sessions.list.NewSelected())
	assert.Contains(t, ansi.Strip(m.View()), "Ask → new Claude Code session in")
}

// longTranscript writes a transcript of 30 turns, more than the right side
// shows at once.
func longTranscript(t *testing.T) string {
	t.Helper()
	transcript := filepath.Join(t.TempDir(), "s20.jsonl")
	var lines []string
	for i := range 30 {
		question := `{"type":"user","message":{"role":"user","content":"question %d"}}`
		answer := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"answer %d"}]}}`
		lines = append(lines, fmt.Sprintf(question, i), fmt.Sprintf(answer, i))
	}
	require.NoError(t, os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return transcript
}

// Up and down in the Ask box step through what was said to the session it
// writes to, as in its own Claude Code; past the latest, what you were
// writing comes back. Questions sent to other sessions aren't among them.
// Who it writes to stays the list's choice, and the session in the preview
// scrolls with pgup and pgdown (and the wheel), not with them.
func TestUpAndDownInTheAskBoxRecallTheSessionsOwnPrompts(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	m.claude.snapshot.Live[0].TranscriptPath = longTranscript(t)
	m.sessions.composer.Sent("wow, to another session")
	toSessions(m)
	require.True(t, m.sessions.list.Select("s20"))
	pressKey(m, common.Hotkeys.OpenAskBar[0]) // writes to it
	require.Equal(t, sessionsFocusAsk, m.sessions.focus)
	typeInto(m, "keep me")

	pressKey(m, "up")
	assert.Equal(t, "question 29", m.sessions.composer.Value(), "its latest prompt, not the other session's")
	for range 30 {
		pressKey(m, "up")
	}
	assert.Equal(t, "question 10", m.sessions.composer.Value(), "no further back than the turns read")
	pressKey(m, "down")
	assert.Equal(t, "question 11", m.sessions.composer.Value())
	for range 30 {
		pressKey(m, "down")
	}
	assert.Equal(t, "keep me", m.sessions.composer.Value(), "past the latest, what you were writing")
	assert.Equal(t, 0, m.sessions.scroll, "the session in the preview doesn't scroll")
	assert.Equal(t, "s20", m.sessions.list.SelectedID(), "it still writes to the same session")
	assert.False(t, m.sessions.list.NewSelected())
	assert.Contains(t, askKeys(m), "↑↓ history")

	pressKey(m, common.Hotkeys.PageUp[0])
	assert.Positive(t, m.sessions.scroll, "pgup scrolls it a page")

	pressKey(m, "esc")
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "esc goes back to the Sessions box")
	assert.Equal(t, sessionsFocusList, m.sessions.focus)
	pressKey(m, common.Hotkeys.SessionNew[0])
	require.True(t, m.sessions.list.NewSelected(), "n chooses a new session again")
	pressKey(m, common.Hotkeys.AskClear[0])
	pressKey(m, "up")
	assert.Equal(t, "wow, to another session", m.sessions.composer.Value(),
		"a new session has no prompts of its own: what the box sent")
}

// = moves the selected session to the file panel's folder, with /cd.
func TestTheSessionsBoxMovesTheSelectedSessionHere(t *testing.T) {
	m := dockTestModel(t)
	pane := newFakeClaudePane(t)
	toSessions(m)
	require.True(t, m.sessions.list.Select("s20"))

	cmd := press(m, common.Hotkeys.MoveSessionHere[0])
	require.NotNil(t, cmd)
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(cmd, 2*time.Second))

	_, _, sent := pane.state()
	assert.Equal(t, []string{"/cd " + m.getFocusedFilePanel().location}, sent)
}

func TestSessionsWithNoFolderAreListedLast(t *testing.T) {
	m := dockTestModel(t)
	m.claude.snapshot.Live = append(m.claude.snapshot.Live, claudesession.Session{ID: "nowhere", Title: "lost",
		Live: true, TTY: "/dev/ttys030"})
	toSessions(m)
	pressKey(m, common.Hotkeys.ListBottom[0])
	assert.Equal(t, "nowhere", m.sessions.list.SelectedID())
}

// The preview reads a transcript's tail at first; scrolled up to its first
// turn, it reads further back, in the background, and stays where it was.
func TestThePreviewLoadsOlderTurnsAsYouScrollUp(t *testing.T) {
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	transcript := filepath.Join(t.TempDir(), "long.jsonl")
	big := strings.Repeat("x", sessionsConversationTail+100)
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"first question"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first answer"}]}}`,
		`{"type":"user","message":{"role":"user","content":"second question"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + big + `"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"second answer"}]}}`,
		`{"type":"user","message":{"role":"user","content":"third question"}}`,
	}
	require.NoError(t, os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	m.claude.snapshot.Live[0].TranscriptPath = transcript
	toSessions(m)
	require.True(t, m.sessions.list.Select("s20"))
	m.loadSessionsConversation()

	require.Len(t, m.sessions.convo.turns, 2, "the tail: the turn it starts in, and the next")
	assert.Equal(t, "second question", m.sessions.convo.turns[0].Prompt)
	assert.Contains(t, ansi.Strip(m.View()), "older turns load as you scroll up")

	var cmd tea.Cmd
	for range 20 {
		if cmd = press(m, common.Hotkeys.PageUp[0]); cmd != nil {
			break
		}
	}
	require.NotNil(t, cmd, "scrolled up to the first turn, it reads further back")
	assert.True(t, m.sessions.convo.loading)
	assert.Nil(t, m.loadOlderTurns(), "once at a time")
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(cmd, 2*time.Second))

	require.Len(t, m.sessions.convo.turns, 3)
	assert.Equal(t, "first question", m.sessions.convo.turns[0].Prompt)
	assert.False(t, m.sessions.convo.hasOlderTurns(), "the whole transcript is read")
	assert.NotContains(t, ansi.Strip(m.View()), "older turns")

	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"third answer"}]}}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	m.loadSessionsConversation()
	require.Len(t, m.sessions.convo.turns, 3)
	assert.Equal(t, "third answer", m.sessions.convo.turns[2].Reply, "what is written next is read on from there")
	assert.Equal(t, "first question", m.sessions.convo.turns[0].Prompt)
}

// A session the preview goes to is read in the background: the preview says
// so meanwhile, and shows the conversation once it is read. Going back to a
// session read before shows it at once.
func TestTheSelectedSessionIsReadInTheBackground(t *testing.T) {
	conversationsInBackground = true
	defer func() { conversationsInBackground = false }()
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	m.claude.snapshot.Live[0].TranscriptPath = longTranscript(t)
	toSessions(m)
	require.True(t, m.sessions.list.Select("s50"))
	assert.Nil(t, m.loadSessionsConversation(), "no transcript: nothing to read")

	require.True(t, m.sessions.list.Select("s20"))
	cmd := m.loadSessionsConversation()
	require.NotNil(t, cmd)
	assert.True(t, m.sessions.convo.reading)
	assert.Empty(t, m.sessions.convo.turns)
	assert.Equal(t, "reading the conversation…", m.sessionsPaneView().Older)
	assert.NotContains(t, ansi.Strip(m.View()), "Nothing said yet", "it isn't said there is nothing yet")

	runCmds(m, cmd)
	require.Len(t, m.sessions.convo.turns, sessionsConversationTurns)
	assert.Contains(t, ansi.Strip(m.View()), "answer 29")
	assert.NotContains(t, ansi.Strip(m.View()), "reading the conversation")

	require.True(t, m.sessions.list.Select("s50"))
	m.loadSessionsConversation()
	require.True(t, m.sessions.list.Select("s20"))
	assert.Nil(t, m.loadSessionsConversation(), "nothing to read")
	assert.False(t, m.sessions.convo.reading, "kept from before")
	assert.Len(t, m.sessions.convo.turns, sessionsConversationTurns)
}
