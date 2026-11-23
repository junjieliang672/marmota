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

// askEditorOpenedAt is the line and column the fake editor was last opened at.
var askEditorOpenedAt [2]int //nolint:gochecknoglobals // what the fake editor was told

// askEditorLeftAt is the line and column the fake editor says its cursor
// was at as it closed ("2 5"); empty, it says nothing, as an editor without
// Marmota's plugin.
var askEditorLeftAt string //nolint:gochecknoglobals // what the fake editor tells

// Back from the editor, the question's cursor is where the editor's was, so
// the two go on from each other both ways; an editor that says nothing
// leaves the cursor at the question's end.
func TestBackFromTheEditorTheCursorIsWhereTheEditorsWas(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.openDockAsk("")
	m.sessions.composer.SetValue("one")
	fakeAskEditor(t, func(string) string { return "one\ntwo 字 words\nthree\n" }, nil)
	setForTest(t, &askEditorLeftAt, "2 6")

	pressAskEdit(t, m)
	require.Equal(t, "one\ntwo 字 words\nthree", m.sessions.composer.Value())
	line, col := m.sessions.composer.CursorAt()
	assert.Equal(t, [2]int{2, 6}, [2]int{line, col}, "after the Chinese character, five characters into line 2")

	pressAskEdit(t, m)
	assert.Equal(t, [2]int{2, 6}, askEditorOpenedAt, "and the editor opens there again")

	// On the empty last line an editor shows after the text: the question's end.
	askEditorLeftAt = "4 1"
	pressAskEdit(t, m)
	line, col = m.sessions.composer.CursorAt()
	assert.Equal(t, [2]int{3, 6}, [2]int{line, col})

	askEditorLeftAt = ""
	TeaUpdate(m, tea.KeyMsg{Type: tea.KeyHome})
	pressAskEdit(t, m)
	line, col = m.sessions.composer.CursorAt()
	assert.Equal(t, [2]int{3, 6}, [2]int{line, col}, "nothing said: the end, as before")
}

// The editor opens where the question's cursor is in the Ask box, not at the
// file's start: on its line, and as many characters into it.
func TestCtrlGOpensTheEditorWhereTheCursorIs(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.openDockAsk("")
	m.sessions.composer.SetValue("group the 标志\nand the icons\nlast")
	fakeAskEditor(t, func(q string) string { return q }, nil)

	pressAskEdit(t, m)
	assert.Equal(t, [2]int{3, 5}, askEditorOpenedAt,
		"at the question's end, where a question put in whole leaves the cursor")

	for range len("icons\nlast") {
		TeaUpdate(m, tea.KeyMsg{Type: tea.KeyLeft})
	}
	pressAskEdit(t, m)
	assert.Equal(t, [2]int{2, 9}, askEditorOpenedAt, "before icons, eight characters into the second line")

	// Back from the editor the cursor is at the question's end again.
	for range []rune("志\nand the icons\nlast") {
		TeaUpdate(m, tea.KeyMsg{Type: tea.KeyLeft})
	}
	pressAskEdit(t, m)
	assert.Equal(t, [2]int{1, 12}, askEditorOpenedAt, "a Chinese character is one character")

	// How each editor is told: micro, the wrapper that runs it for $EDITOR,
	// and vim's family; another editor opens as it did.
	assert.Equal(t, []string{"+2:9", "q.md"}, editorAt("/opt/homebrew/bin/micro", []string{"q.md"}, 2, 9))
	assert.Equal(t, []string{"+2:9", "q.md"},
		editorAt("/Users/me/.config/micro/plug/marmota/edit", []string{"q.md"}, 2, 9))
	assert.Equal(t, []string{"-u", "NONE", "+normal! 2G08l", "q.md"},
		editorAt("nvim", []string{"-u", "NONE", "q.md"}, 2, 9))
	assert.Equal(t, []string{"+normal! 1G0", "q.md"}, editorAt("vim", []string{"q.md"}, 1, 1))
	assert.Equal(t, []string{"--wait", "q.md"}, editorAt("code", []string{"--wait", "q.md"}, 2, 9))
}

// fakeAskEditor stands in for the editor: it gets the file with the
// question and leaves what edit returns in it, ending with err.
func fakeAskEditor(t *testing.T, edit func(question string) string, err error) *string {
	t.Helper()
	path := new(string)
	setForTest(t, &runAskEditor, func(file string, line, col int, env []string, done func(error) tea.Msg) tea.Cmd {
		*path = file
		askEditorOpenedAt = [2]int{line, col}
		if askEditorLeftAt != "" { // as micro does, with Marmota's plugin, when the file closes
			for _, e := range env {
				if out, named := strings.CutPrefix(e, editorCursorEnv+"="); named {
					require.NoError(t, os.WriteFile(out, []byte(askEditorLeftAt+" "+file+"\n"), 0o600))
				}
			}
		}
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
