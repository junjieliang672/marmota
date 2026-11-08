package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestFilePanelCountJump_JumpsToIndexedItem(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	for i := range 12 {
		utils.SetupFiles(t, filepath.Join(root, fileNameForIndex(i+1)))
	}

	m := defaultTestModel(root)

	TeaUpdate(m, utils.TeaRuneKeyMsg("1"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("0"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("G"))

	panel := m.getFocusedFilePanel()
	require.NotNil(t, panel)
	assert.Equal(t, 9, panel.cursor)
	assert.Equal(t, fileNameForIndex(10), panel.getSelectedItem().name)
	assert.Empty(t, m.filePanelJumpCount)
}

func TestFilePanelCountJump_LowercaseGJumpsToIndexedItem(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	for i := range 12 {
		utils.SetupFiles(t, filepath.Join(root, fileNameForIndex(i+1)))
	}

	m := defaultTestModel(root)

	TeaUpdate(m, utils.TeaRuneKeyMsg("1"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("0"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("g"))

	panel := m.getFocusedFilePanel()
	require.NotNil(t, panel)
	assert.Equal(t, 9, panel.cursor)
	assert.Equal(t, fileNameForIndex(10), panel.getSelectedItem().name)
	assert.Empty(t, m.filePanelJumpCount)
}

func TestFilePanelCountJump_PlainGStillGoesToBottom(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	for i := range 12 {
		utils.SetupFiles(t, filepath.Join(root, fileNameForIndex(i+1)))
	}

	m := defaultTestModel(root)

	TeaUpdate(m, utils.TeaRuneKeyMsg("G"))

	panel := m.getFocusedFilePanel()
	require.NotNil(t, panel)
	assert.Equal(t, len(panel.element)-1, panel.cursor)
	assert.Equal(t, fileNameForIndex(12), panel.getSelectedItem().name)
}

func TestFilePanelCountJump_IgnoredWhenSearchBarFocused(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	for i := range 12 {
		utils.SetupFiles(t, filepath.Join(root, fileNameForIndex(i+1)))
	}

	m := defaultTestModel(root)
	m.searchBarFocus()
	m.firstTextInput = false

	TeaUpdate(m, utils.TeaRuneKeyMsg("1"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("0"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("G"))

	panel := m.getFocusedFilePanel()
	require.NotNil(t, panel)
	assert.Equal(t, 0, panel.cursor)
	assert.Empty(t, m.filePanelJumpCount)
	assert.Equal(t, "10G", panel.searchBar.Value())
}

func TestFilePanelCountJump_WorksWhenSearchResultsAreConfirmed(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	for i := range 12 {
		utils.SetupFiles(t, filepath.Join(root, fileNameForIndex(i+1)))
	}

	m := defaultTestModel(root)
	m.getFocusedFilePanel().searchStack.Push(searchLayer{
		layerType:  searchLayerRegular,
		searchTerm: "file",
	})

	TeaUpdate(m, utils.TeaRuneKeyMsg("1"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("0"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("G"))

	panel := m.getFocusedFilePanel()
	require.NotNil(t, panel)
	assert.Equal(t, 9, panel.cursor)
	assert.Equal(t, fileNameForIndex(10), panel.getSelectedItem().name)
	assert.Empty(t, m.filePanelJumpCount)
}

func TestFilePanelRender_ShowsLineNumbers(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	for i := range 12 {
		utils.SetupFiles(t, filepath.Join(root, fileNameForIndex(i+1)))
	}

	m := defaultTestModel(root)

	plain := ansi.Strip(m.filePanelRender())
	assert.Regexp(t, `(?m)\s1 .*file01\.txt`, plain)
	assert.Regexp(t, `(?m)\s10 .*file10\.txt`, plain)
}

func TestFilePreviewPanelRender_PausesPDFPreviewDuringRename(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	pdfPath := filepath.Join(root, "doc.pdf")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, pdfPath)

	m := defaultTestModel(root)
	m.fileModel.filePreview.Open()
	m.fileModel.filePreview.SetContentWithRenderText("expensive cached preview")
	m.fileModel.renaming = true

	rendered := ansi.Strip(m.filePreviewPanelRender())
	assert.Contains(t, rendered, "Preview paused while renaming PDF...")
	assert.NotContains(t, rendered, "expensive cached preview")
}

func TestSchedulePreviewRefresh_DebouncesRequests(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filepath.Join(root, "file.txt"))

	m := defaultTestModel(root)
	m.fileModel.filePreview.Open()

	firstCmd := schedulePreviewRefresh(m)
	require.NotNil(t, firstCmd)
	assert.True(t, m.previewRefreshPending)

	secondCmd := schedulePreviewRefresh(m)
	assert.Nil(t, secondCmd)
	assert.True(t, m.previewRefreshPending)
}

func TestPreviewDebounceMsgClearsPendingFlag(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)
	firstPath := filepath.Join(root, "a.txt")
	secondPath := filepath.Join(root, "b.txt")
	require.NoError(t, os.WriteFile(firstPath, []byte("alpha"), 0o644))
	require.NoError(t, os.WriteFile(secondPath, []byte("beta"), 0o644))

	m := defaultTestModel(root)
	m.fileModel.filePreview.Open()
	m.fileModel.filePreview.SetLocation("")
	m.previewRenderInFlight = false
	m.previewRenderQueued = false
	m.previewRenderForced = false

	panel := m.getFocusedFilePanel()
	require.GreaterOrEqual(t, len(panel.element), 2)
	panel.cursor = 1
	m.previewRefreshPending = true

	TeaUpdate(m, previewDebounceMsg{})
	assert.False(t, m.previewRefreshPending)
}

func TestRequestFilePreviewRender_RendersLatestHoveredItem(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)
	firstPath := filepath.Join(root, "a.txt")
	secondPath := filepath.Join(root, "b.txt")
	require.NoError(t, os.WriteFile(firstPath, []byte("alpha"), 0o644))
	require.NoError(t, os.WriteFile(secondPath, []byte("beta"), 0o644))

	m := defaultTestModel(root)
	m.fileModel.filePreview.Open()
	m.fileModel.filePreview.SetLocation("")
	m.previewRenderInFlight = false
	m.previewRenderQueued = false
	m.previewRenderForced = false

	panel := m.getFocusedFilePanel()
	require.GreaterOrEqual(t, len(panel.element), 2)
	panel.cursor = 1

	cmd := m.requestFilePreviewRender(true)
	require.NotNil(t, cmd)

	msg := ExecuteTeaCmdWithTimeout(cmd, time.Second)
	previewMsg, ok := msg.(FilePreviewUpdateMsg)
	require.True(t, ok)
	assert.Equal(t, panel.getSelectedItem().location, previewMsg.location)
}

func TestFilePreviewUpdateMsg_StartsQueuedLatestRender(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	firstPath := filepath.Join(root, "a.txt")
	secondPath := filepath.Join(root, "b.txt")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(firstPath, []byte("alpha"), 0o644))
	require.NoError(t, os.WriteFile(secondPath, []byte("beta"), 0o644))

	m := defaultTestModel(root)
	m.fileModel.filePreview.Open()
	panel := m.getFocusedFilePanel()
	require.GreaterOrEqual(t, len(panel.element), 2)

	panel.cursor = 1
	m.previewRenderInFlight = true
	m.previewRenderQueued = true

	cmd := NewFilePreviewUpdateMsg(firstPath, "stale preview", 1).ApplyToModel(m)
	require.NotNil(t, cmd)
	assert.True(t, m.previewRenderInFlight)
	assert.False(t, m.previewRenderQueued)

	msg := ExecuteTeaCmdWithTimeout(cmd, time.Second)
	previewMsg, ok := msg.(FilePreviewUpdateMsg)
	require.True(t, ok)
	assert.Equal(t, secondPath, previewMsg.location)
}

func fileNameForIndex(i int) string {
	return fmt.Sprintf("file%02d.txt", i)
}
