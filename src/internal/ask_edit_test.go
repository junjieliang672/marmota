package internal

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// fakeAskEditor stands in for the editor: it gets the file with the
// question and leaves what edit returns in it, ending with err.
func fakeAskEditor(t *testing.T, edit func(question string) string, err error) *string {
	t.Helper()
	path := new(string)
	setForTest(t, &runAskEditor, func(file string, _ []string, done func(error) tea.Msg) tea.Cmd {
		*path = file
		before, readErr := os.ReadFile(file)
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(file, []byte(edit(string(before))), 0o600))
		return func() tea.Msg { return done(err) }
	})
	return path
}

// pressAskEdit presses ctrl+g and has the editor close.
func pressAskEdit(t *testing.T, m *model) {
	t.Helper()
	cmd := TeaUpdate(m, utilsKey(common.Hotkeys.AskEdit[0]))
	require.NotNil(t, cmd)
	assert.True(t, m.externalProcessRunning, "the editor has the terminal")
	for _, msg := range batchMsgs(t, cmd) {
		if edited, ok := msg.(askDraftEditedMsg); ok {
			TeaUpdate(m, edited)
		}
	}
}

// ctrl+g in the Ask box opens the question in the editor; when it closes,
// what it wrote is the question, line breaks and all, and nothing is sent.
func TestCtrlGWritesTheQuestionInTheEditor(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.openDockAsk("")
	m.sessions.composer.SetValue("first draft")
	file := fakeAskEditor(t, func(q string) string {
		assert.Equal(t, "first draft", q, "the editor opens on what you had written")
		return q + "\nsecond line, from the editor\n"
	}, nil)

	pressAskEdit(t, m)
	assert.True(t, strings.HasSuffix(*file, ".md"))

	assert.Equal(t, "first draft\nsecond line, from the editor", m.sessions.composer.Value())
	assert.False(t, m.externalProcessRunning)
	assert.Equal(t, dockAskFocus, m.focusPanel, "the Ask box keeps the keyboard, and nothing was sent")
	assert.NoFileExists(t, *file, "its file is gone")
}

// An editor that ends with an error, as nvim does after :cq, changes nothing.
func TestAnEditorThatFailsLeavesTheQuestion(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.openDockAsk("")
	m.sessions.composer.SetValue("keep me")
	file := fakeAskEditor(t, func(string) string { return "thrown away" }, errors.New("exit status 1"))

	pressAskEdit(t, m)

	assert.Equal(t, "keep me", m.sessions.composer.Value())
	assert.Contains(t, m.claude.flash, "as it was")
	assert.NoFileExists(t, *file)
}
