package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

// editorBesideModel is a model in WezTerm pane 10 with two files, the editor
// set to micro beside Marmota, and what is typed into panes caught in typed.
func editorBesideModel(t *testing.T) (*model, *fakeWezterm, *[]string) {
	t.Helper()
	dir := t.TempDir()
	utils.SetupFiles(t, filepath.Join(dir, "a.md"), filepath.Join(dir, "it's b.md"))
	t.Setenv("WEZTERM_PANE", "10")
	setForTest(t, &common.Config.Editor, "micro")
	setForTest(t, &common.Config.EditorBeside, true)
	wez := newFakeWezterm(t, superfileTab())
	typed := &[]string{}
	setForTest(t, &weztermSendKeys, func(pane int, keys string) error {
		*typed = append(*typed, keys)
		return nil
	})
	m := defaultTestModel(dir)
	TeaUpdate(m, nil)
	return m, wez, typed
}

// With editor_beside, e opens the editor in a pane on Marmota's right, in
// the panel's folder, and gives it the keyboard. A file opened while that
// pane is there goes to the same micro, in a tab of its own.
func TestEOpensTheEditorBesideMarmota(t *testing.T) {
	m, wez, typed := editorBesideModel(t)
	dir := m.getFocusedFilePanel().location

	cmd := m.openFileWithEditor()
	assert.Nil(t, cmd, "Marmota keeps its place: nothing runs in it")
	require.NotZero(t, m.editorPane)
	split := wez.commands[len(wez.commands)-2]
	assert.Contains(t, split, "split-pane --pane-id 10 --right --percent 60 --cwd "+shellQuote(dir)+" -- ")
	assert.Contains(t, split, " micro a.md'", "the file by its path from the folder")
	assert.Contains(t, split, "exec env ", "told of Marmota")
	assert.Contains(t, split, "MARMOTA_BIN=")
	assert.NotContains(t, split, "MARMOTA_SESSION", "no session to write to")
	assert.Equal(t, m.editorPane, wez.active, "the editor has the keyboard")
	assert.Empty(t, *typed)

	m.getFocusedFilePanel().cursor = 1
	require.Nil(t, m.openFileWithEditor())
	require.Len(t, *typed, 1, "the second file is typed into micro's command bar")
	assert.Equal(t, "\x1bxtab 'it'\\''s b.md'\r", (*typed)[0])
	panes := 0
	for range wez.panes {
		panes++
	}
	assert.Equal(t, 2, panes, "no second pane")

	// The pane closed: the next file opens a new one.
	wez.closePane(m.editorPane)
	require.Nil(t, m.openFileWithEditor())
	assert.Len(t, *typed, 1)
	assert.NotZero(t, m.editorPane)
}

// Outside WezTerm, the editor opens in Marmota's place as before.
func TestTheEditorOpensInPlaceOutsideWezterm(t *testing.T) {
	m, _, _ := editorBesideModel(t)
	require.NoError(t, os.Unsetenv("WEZTERM_PANE"))

	cmd := m.openFileWithEditor()

	assert.NotNil(t, cmd, "the editor runs in Marmota's place")
	assert.Zero(t, m.editorPane)
	assert.True(t, m.externalProcessRunning)
}

func TestMicroQuote(t *testing.T) {
	assert.Equal(t, "'plain.md'", microQuote("plain.md"))
	assert.Equal(t, `'it'\''s.md'`, microQuote("it's.md"))
	assert.True(t, strings.HasPrefix(microCommandBar, "\x1b"))
}

// The editor is told which session the Ask box writes to: questions asked
// from it go there, and who runs it, since the editor words a question for
// Codex as Codex reads it.
func TestTheEditorIsToldTheSessionTheAskBoxWritesTo(t *testing.T) {
	m := dockTestModel(t)
	bang(m)
	require.True(t, m.sessions.list.Select("s50"))

	env := strings.Join(m.editorEnv(), "\n")
	assert.Contains(t, env, "MARMOTA_SESSION=s50")
	assert.Contains(t, env, "MARMOTA_SESSION_TITLE=ime hold tests")
	assert.Contains(t, env, "MARMOTA_SESSION_HOST=here")
	assert.Contains(t, env, "MARMOTA_SESSION_AGENT=cc")

	for i := range m.claude.snapshot.Live {
		m.claude.snapshot.Live[i].Agent = "codex"
	}
	env = strings.Join(m.editorEnv(), "\n")
	assert.Contains(t, env, "MARMOTA_SESSION=s50", "a Codex session too")
	assert.Contains(t, env, "MARMOTA_SESSION_AGENT=co")
}
