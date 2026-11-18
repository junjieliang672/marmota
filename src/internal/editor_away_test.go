//go:build unix

package internal

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

// An editor that stops itself, as micro does on cmd+M (SIGSTOP) and vim on
// ctrl+z (SIGTSTP), is put away: Run comes back with the process kept, and
// Run again continues it from where it stopped.
func TestAnEditorThatStopsIsPutAwayAndGoesOnWhenContinued(t *testing.T) {
	for _, signal := range []string{"STOP", "TSTP"} {
		t.Run(signal, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			job := &editorJob{name: "/bin/sh", env: os.Environ(), noTerminal: true,
				args: []string{"-c", `echo started >> "$0"; kill -` + signal + ` $$; echo continued >> "$0"`, out}}

			require.NoError(t, job.Run())
			assert.True(t, job.stopped, "it stopped: put away")
			said, err := os.ReadFile(out)
			require.NoError(t, err)
			assert.Equal(t, "started\n", string(said), "and went no further")

			require.NoError(t, job.Run())
			assert.False(t, job.stopped, "continued, it ended")
			said, err = os.ReadFile(out)
			require.NoError(t, err)
			assert.Equal(t, "started\ncontinued\n", string(said))
		})
	}
}

func TestAnEditorThatEndsIsNotPutAway(t *testing.T) {
	job := &editorJob{name: "/bin/sh", args: []string{"-c", "exit 0"}, env: os.Environ(), noTerminal: true}
	require.NoError(t, job.Run())
	assert.False(t, job.stopped)

	job = &editorJob{name: "/bin/sh", args: []string{"-c", "exit 3"}, env: os.Environ(), noTerminal: true}
	err := job.Run()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 3")
	assert.False(t, job.stopped)

	job = &editorJob{name: "/bin/sh", args: []string{"-c", "kill -KILL $$"}, env: os.Environ(), noTerminal: true}
	require.Error(t, job.Run())
	assert.False(t, job.stopped)

	job = &editorJob{name: filepath.Join(t.TempDir(), "no-such-editor"), env: os.Environ(), noTerminal: true}
	require.Error(t, job.Run())
}

// awayTestModel is a model whose editor writes what it holds in a file of
// the test's.
func awayTestModel(t *testing.T) (*model, string) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "editor")
	old := editorStatePath
	editorStatePath = func() string { return state }
	t.Cleanup(func() { editorStatePath = old })
	return defaultTestModel(t.TempDir()), state
}

// putAway has the model's editor stop, having said it holds files, unsaved
// of them not saved.
func putAway(t *testing.T, m *model, state, holds string) *editorJob {
	t.Helper()
	job := m.newEditorJob("/usr/local/bin/micro", []string{"plan.md"})
	if holds != "" {
		require.NoError(t, os.WriteFile(state, []byte(holds+"\n"), 0o600))
	}
	job.stopped = true
	m.externalProcessRunning = true
	TeaUpdate(m, editorAwayMsg{job: job})
	return job
}

func TestAnEditorPutAwayIsNamedInTheStatusLineAndBBringsItBack(t *testing.T) {
	m, state := awayTestModel(t)
	job := putAway(t, m, state, "3 1")

	require.NotNil(t, m.editorAway)
	assert.False(t, m.externalProcessRunning, "Marmota has the terminal again")
	assert.Equal(t, "✎ micro · 3 files · b back", ansi.Strip(m.editorAwayStatus()))
	assert.Contains(t, ansi.Strip(m.View()), "✎ micro · 3 files · b back")

	// b gives it the terminal again, with nothing asked of it.
	require.NoError(t, os.WriteFile(state+editorOrderSuffix, []byte("quit\n"), 0o600))
	cmd := TeaUpdate(m, utils.TeaKeyMsg("b"))
	require.NotNil(t, cmd)
	assert.True(t, m.externalProcessRunning)
	assert.NoFileExists(t, state+editorOrderSuffix, "an order left from before isn't given again")
	assert.Empty(t, m.editorAwayStatus(), "not while it runs")

	// It ends: nothing is put away, and its files are gone.
	TeaUpdate(m, editorJobEndedMsg{job: job})
	assert.Nil(t, m.editorAway)
	assert.False(t, m.externalProcessRunning)
	assert.NoFileExists(t, state)
}

func TestWhatAnEditorPutAwaySaysItHolds(t *testing.T) {
	for holds, want := range map[string]string{
		"1 0": "✎ micro · 1 file · b back",
		"2 0": "✎ micro · 2 files · b back",
		"4 2": "✎ micro · 4 files · b back", // what isn't saved is q's to know, not the line's
		"":    "✎ micro · b back",           // an editor that says nothing, as vim
	} {
		m, state := awayTestModel(t)
		putAway(t, m, state, holds)
		assert.Equal(t, want, ansi.Strip(m.editorAwayStatus()), holds)
	}
}

func TestBWithNoEditorPutAwaySaysSo(t *testing.T) {
	m, _ := awayTestModel(t)
	cmd := m.backToEditor()
	assert.Nil(t, cmd)
	assert.Contains(t, m.claude.flash, "No editor is put away")
	assert.False(t, m.externalProcessRunning)
}

// e stays what it was while an editor is put away: a new editor, which is
// told it can't be put away too.
func TestASecondEditorIsToldOneIsPutAwayAlready(t *testing.T) {
	m, state := awayTestModel(t)

	first := m.newEditorJob("micro", nil)
	assert.Contains(t, first.env, editorStateEnv+"="+state)
	assert.False(t, slices.ContainsFunc(first.env, func(v string) bool { return strings.HasPrefix(v, editorAwayEnv+"=") }))

	kept := putAway(t, m, state, "1 0")
	second := m.newEditorJob("micro", nil)
	assert.Contains(t, second.env, editorAwayEnv+"=1")
	assert.False(t, slices.ContainsFunc(second.env, func(v string) bool { return strings.HasPrefix(v, editorStateEnv+"=") }))
	assert.FileExists(t, state, "what the first one wrote stays")

	// One that stops all the same, as vim on ctrl+z, goes on: one is kept.
	second.stopped = true
	m.externalProcessRunning = true
	cmd := TeaUpdate(m, editorAwayMsg{job: second})
	require.NotNil(t, cmd)
	assert.True(t, m.externalProcessRunning)
	assert.Same(t, kept, m.editorAway.job)

	// It ends: the first is still put away.
	TeaUpdate(m, editorJobEndedMsg{job: second})
	require.NotNil(t, m.editorAway)
	assert.Same(t, kept, m.editorAway.job)
	assert.FileExists(t, state)
}

// q with an editor put away that holds something unsaved doesn't quit: the
// editor comes back, told to say why.
func TestQBringsBackAnEditorWithSomethingUnsaved(t *testing.T) {
	for _, holds := range []string{"2 1", ""} { // "" an editor that didn't say
		m, state := awayTestModel(t)
		putAway(t, m, state, holds)

		cmd := TeaUpdate(m, utils.TeaKeyMsg("q"))

		assert.False(t, IsTeaQuit(cmd), holds)
		assert.Equal(t, notQuitting, m.modelQuitState)
		assert.True(t, m.externalProcessRunning, "the editor has the terminal")
		assert.Nil(t, m.quitAfterEditor)
		order, err := os.ReadFile(state + editorOrderSuffix)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(string(order), editorOrderSay+"Marmota didn't quit"), string(order))
	}
}

// q with an editor put away that holds nothing unsaved ends the editor
// first, and quits when it has.
func TestQEndsAnEditorWithNothingUnsavedAndThenQuits(t *testing.T) {
	m, state := awayTestModel(t)
	job := putAway(t, m, state, "2 0")

	cmd := TeaUpdate(m, utils.TeaKeyMsg("q"))
	assert.False(t, IsTeaQuit(cmd), "not before the editor has ended")
	assert.True(t, m.externalProcessRunning)
	order, err := os.ReadFile(state + editorOrderSuffix)
	require.NoError(t, err)
	assert.Equal(t, editorOrderQuit+"\n", string(order))

	cmd = TeaUpdate(m, editorJobEndedMsg{job: job})
	assert.True(t, IsTeaQuit(cmd))
	assert.Equal(t, quitDone, m.modelQuitState)
}

// An editor continued to end for q that is put away again instead keeps
// Marmota: a later end of it doesn't quit.
func TestAnEditorPutAwayAgainInsteadOfEndingKeepsMarmota(t *testing.T) {
	m, state := awayTestModel(t)
	job := putAway(t, m, state, "2 0")
	TeaUpdate(m, utils.TeaKeyMsg("q"))
	require.NotNil(t, m.quitAfterEditor)

	TeaUpdate(m, editorAwayMsg{job: job})
	assert.Nil(t, m.quitAfterEditor)
	require.NotNil(t, m.editorAway)

	TeaUpdate(m, utils.TeaKeyMsg("b"))
	cmd := TeaUpdate(m, editorJobEndedMsg{job: job})
	assert.False(t, IsTeaQuit(cmd))
	assert.Equal(t, notQuitting, m.modelQuitState)
}
