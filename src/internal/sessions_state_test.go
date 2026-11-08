package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	bang(m)
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
	bang(m)

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
	bang(m)
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
	assert.Contains(t, ansi.Strip(m.View()), "↑↓ history")

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
	bang(m)
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
	bang(m)
	pressKey(m, common.Hotkeys.ListBottom[0])
	assert.Equal(t, "nowhere", m.sessions.list.SelectedID())
}
