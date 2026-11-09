package internal

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

func TestRenderUnifiedDiff(t *testing.T) {
	diff := `diff --git a/x b/y
--- a/x
+++ b/y
@@ -3,3 +3,4 @@ func f() {
 keep
-old line
+new line
+another
 tail
@@ -20,1 +21,1 @@
-a
+b
`
	lines, hunks, added, removed := renderUnifiedDiff(diff)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l.gutter + l.text)
	}
	assert.Equal(t, 3, added)
	assert.Equal(t, 2, removed)
	assert.Equal(t, []int{0, 6}, hunks, "where each change starts")
	assert.Equal(t, "   3    3  keep", plain[1])
	assert.Equal(t, "   4      -old line", plain[2])
	assert.Equal(t, "        4 +new line", plain[3])
	assert.Equal(t, "        5 +another", plain[4])
	assert.Equal(t, "   5    6  tail", plain[5])
}

// sessionFilesModel is a model whose first live session changed two files
// in dir: one it edited (with Claude Code's backup of the original) and one
// it created.
func sessionFilesModel(t *testing.T) (*model, string) {
	t.Helper()
	m := dockTestModel(t)
	dir := t.TempDir()
	edited, created := filepath.Join(dir, "src", "edited.go"), filepath.Join(dir, "new.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(edited), 0o755))
	require.NoError(t, os.WriteFile(edited, []byte("package a\n\nfunc A() int { return 2 }\n"), 0o600))
	require.NoError(t, os.WriteFile(created, []byte("package a\n"), 0o600))

	config := filepath.Dir(claudeProjectsDir())
	first := m.dockSessions()[0]
	history := filepath.Join(config, "file-history", first.ID)
	require.NoError(t, os.MkdirAll(history, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(history, "e1@v1"),
		[]byte("package a\n\nfunc A() int { return 1 }\n"), 0o600))
	transcript := filepath.Join(dir, "t.jsonl")
	require.NoError(t, os.WriteFile(transcript, []byte(`{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":`+
		`{"src/edited.go":{"backupFileName":"e1@v2"},"new.go":{"backupFileName":"n1@v2"}}}}`+"\n"), 0o600))
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == first.ID {
			m.claude.snapshot.Live[i].Cwd = dir
			m.claude.snapshot.Live[i].TranscriptPath = transcript
			m.claude.snapshot.Live[i].EditedFiles = []string{edited, created, filepath.Join(dir, "gone.go")}
		}
	}
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	return m, dir
}

func TestWListsTheFilesASessionChanged(t *testing.T) {
	m, _ := sessionFilesModel(t)

	pressKey(m, common.Hotkeys.ShowSessionFiles[0])

	require.NotNil(t, m.touchedLayer())
	panel := m.getFocusedFilePanel()
	names := []string{}
	for _, e := range panel.element {
		names = append(names, e.name)
	}
	assert.Equal(t, []string{"new.go", "src/edited.go"}, names, "named from its folder; a file gone since isn't listed")

	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	assert.Nil(t, m.touchedLayer(), "W again: the folder")
}

// The session's first file is in the panel's folder, so the panel can't tell
// from its list that W changed it: W must load the list itself, or the
// preview waits for the next refresh, up to seconds.
func TestWShowsTheFirstFilesDiffAtOnce(t *testing.T) {
	m, dir := sessionFilesModel(t)
	panel := m.getFocusedFilePanel()
	require.NoError(t, panel.updateCurrentFilePanelDir(dir))
	TeaUpdate(m, nil)
	require.NotEmpty(t, panel.element)

	cmd := m.toggleSessionFiles()

	require.NotEmpty(t, panel.element)
	assert.Equal(t, "new.go", panel.element[0].name, "the list, before any refresh")
	require.NotNil(t, cmd)
	msg, ok := cmd().(diffDocMsg)
	require.True(t, ok, "the preview asks for the diff, not the folder's file")
	assert.Equal(t, filepath.Join(dir, "new.go"), msg.path)

	m.toggleSessionFiles()
	assert.NotEqual(t, "new.go", panel.element[0].name, "W again lists the folder at once")
}

func TestTheDiffPreview(t *testing.T) {
	m, dir := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	layer := m.touchedLayer()
	require.NotNil(t, layer)
	config := filepath.Dir(claudeProjectsDir())

	edited := buildDiffView(config, layer.session, filepath.Join(dir, "src", "edited.go"), 60)
	view := diffText(edited)
	assert.Contains(t, view, "diff  edited.go +1 −1")
	assert.Contains(t, view, "as it was before")
	assert.Contains(t, view, "-func A() int { return 1 }")
	assert.Contains(t, view, "+func A() int { return 2 }")

	created := buildDiffView(config, layer.session, filepath.Join(dir, "new.go"), 60)
	view = diffText(created)
	assert.Contains(t, view, "created it")
	assert.Contains(t, view, "+package a")
}

// A file the session first changed in the turn under way: its backup is
// there, but the transcript names it only when the next turn starts.
func TestTheDiffOfAFileChangedThisTurn(t *testing.T) {
	m, dir := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	layer := m.touchedLayer()
	require.NotNil(t, layer)
	config := filepath.Dir(claudeProjectsDir())
	readme := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# a\n\nnew line\n"), 0o600))
	history := filepath.Join(config, "file-history", layer.session.id)
	require.NoError(t, os.WriteFile(filepath.Join(history, claudesession.PathHash(readme)+"@v1"),
		[]byte("# a\n"), 0o600))

	view := diffText(buildDiffView(config, layer.session, readme, 60))
	assert.Contains(t, view, "as it was before")
	assert.Contains(t, view, "+new line")
}

func TestAFileChangedByShellCommandsShowsThoseChanges(t *testing.T) {
	m, dir := sessionFilesModel(t)
	shelled := filepath.Join(dir, "shelled.go")
	require.NoError(t, os.WriteFile(shelled, []byte("package a\nvar y = 2\n"), 0o600))
	layerSession := touchedSessionRef{}
	for _, s := range m.claude.snapshot.Live {
		if s.Cwd == dir {
			layerSession = touchedSessionRef{id: s.ID, cwd: s.Cwd, transcript: s.TranscriptPath, title: s.DisplayTitle()}
		}
	}
	f, err := os.OpenFile(layerSession.transcript, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"timestamp":"2026-11-07T10:00:00Z","toolUseResult":{"bashEditDiff":{"files":[{"filePath":"` +
		shelled + `","hunks":[{"oldStart":2,"oldLines":1,"newStart":2,"newLines":1,"lines":["-var y = 1","+var y = 2"]}]}]}}}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	v := buildDiffView(filepath.Dir(claudeProjectsDir()), layerSession, shelled, 60)
	view := diffText(v)
	assert.Contains(t, view, "diff  shelled.go +1 −1")
	assert.Contains(t, view, "changed by shell commands: 1 change")
	assert.Contains(t, view, "-var y = 1")
	assert.Contains(t, view, "+var y = 2")
}

// diffText is a diff view's text, without colours.
func diffText(v diffView) string {
	lines := slices.Clone(v.header)
	for _, l := range v.body {
		lines = append(lines, l.gutter+l.text)
	}
	return ansi.Strip(strings.Join(lines, "\n"))
}

func TestTheDiffScrollsSideways(t *testing.T) {
	m, dir := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	path := filepath.Join(dir, "src", "edited.go")
	m.diff = diffView{path: path, body: []diffLine{
		{gutter: "   1    1 ", text: " " + strings.Repeat("a", 200) + "END"},
		{gutter: "   2    2 ", text: " short"},
	}}
	for i, e := range m.getFocusedFilePanel().element {
		if e.location == path {
			m.getFocusedFilePanel().cursor = i
		}
	}
	require.True(t, m.diffShown())

	m.drawDiff()
	assert.NotContains(t, ansi.Strip(m.fileModel.filePreview.GetContent()), "END")

	TeaUpdate(m, tea.MouseMsg{X: m.fullWidth - 3, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelRight})
	assert.NotEqual(t, ansi.Strip(m.fileModel.filePreview.GetContent()), "", "the wheel scrolls it sideways")
	m.fileModel.filePreview.ScrollBy(0, 1000)
	content := ansi.Strip(m.fileModel.filePreview.GetContent())
	assert.Contains(t, content, "END", "as far as its widest line goes")
	assert.Contains(t, content, "   1    1 ", "the line numbers stay")
}

func TestBracketsJumpBetweenChanges(t *testing.T) {
	m, dir := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	path := filepath.Join(dir, "src", "edited.go")
	var body []diffLine
	for i := range 200 {
		body = append(body, diffLine{gutter: "   . ", text: " line " + strconv.Itoa(i)})
	}
	m.diff = diffView{path: path, body: body, hunks: []int{0, 80, 160}}
	for i, e := range m.getFocusedFilePanel().element {
		if e.location == path {
			m.getFocusedFilePanel().cursor = i
		}
	}
	m.drawDiff()
	pv := &m.fileModel.filePreview

	pressKey(m, common.Hotkeys.DiffNextChange[0])
	assert.Equal(t, 80, pv.DocTop())
	pressKey(m, common.Hotkeys.DiffNextChange[0])
	assert.Equal(t, min(160, 200-(pv.InnerHeight())), pv.DocTop(), "as far as it scrolls")
	pressKey(m, common.Hotkeys.DiffPreviousChange[0])
	assert.Less(t, pv.DocTop(), 160)
}

func TestSearchingASessionsFilesFiltersThemAsYouType(t *testing.T) {
	m, _ := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	panel := m.getFocusedFilePanel()
	names := func(elems []element) []string {
		out := []string{}
		for _, e := range elems {
			out = append(out, e.name)
		}
		return out
	}

	panel.searchBar.SetValue("edit")
	assert.Equal(t, []string{"src/edited.go"}, names(m.getElementsForPanel(panel)), "as you type")
	panel.searchBar.SetValue("src")
	assert.Equal(t, []string{"src/edited.go"}, names(m.getElementsForPanel(panel)), "folders in the name match too")

	m.confirmSearch()
	top := panel.searchStack.Peek()
	require.NotNil(t, top)
	assert.Equal(t, searchLayerFilter, top.layerType, "enter keeps it as a filter of the session's files")
	assert.Equal(t, []string{"src/edited.go"}, names(m.getElementsForPanel(panel)))

	panel.searchStack.Pop()
	panel.recursiveSearch = true // f
	panel.searchBar.SetValue("new")
	assert.Equal(t, []string{"new.go"}, names(m.getElementsForPanel(panel)))
	m.confirmSearch()
	assert.Equal(t, searchLayerFilter, panel.searchStack.Peek().layerType, "f filters them too, not the folder")
}

func TestTheDiffHeaderWrapsInsteadOfCutting(t *testing.T) {
	m, dir := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	layer := m.touchedLayer()
	require.NotNil(t, layer)

	v := buildDiffView(filepath.Dir(claudeProjectsDir()), layer.session, filepath.Join(dir, "src", "edited.go"), 24)
	header := ansi.Strip(strings.Join(v.header, "\n"))
	assert.NotContains(t, header, "…", "nothing cut")
	assert.Contains(t, strings.ReplaceAll(header, "\n", " "), "first changed it", "the whole before line is there")
	for _, l := range v.header {
		assert.LessOrEqual(t, ansi.StringWidth(l), 24)
	}
}

// A search kept with enter is a filter over the session's files: the
// preview still shows each file's diff, after moving away and back too, and
// W leaves the filter with the files.
func TestAFilterOverASessionsFilesKeepsTheDiff(t *testing.T) {
	m, dir := sessionFilesModel(t)
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	panel := m.getFocusedFilePanel()
	panel.searchBar.SetValue(".go")
	m.confirmSearch()
	TeaUpdate(m, nil)
	require.Equal(t, searchLayerFilter, panel.searchStack.Peek().layerType)
	require.Len(t, panel.element, 2)

	require.NotNil(t, m.touchedLayer(), "the filter is over the session's files")
	panel.cursor = 1
	msg, ok := m.getFilePreviewCmd(true)().(diffDocMsg)
	require.True(t, ok, "the preview asks for a diff, not the file")
	assert.Equal(t, filepath.Join(dir, "src", "edited.go"), msg.path)

	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	assert.Nil(t, m.touchedLayer())
	assert.True(t, panel.searchStack.IsEmpty(), "W again: the folder, the filter gone with the files")
}
