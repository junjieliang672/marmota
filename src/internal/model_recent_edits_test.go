package internal

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestOpenFileWithEditor_RecordsRecentEdit(t *testing.T) {
	editorPath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true command not available")
	}

	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	filePath := filepath.Join(root, "file.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filePath)

	originalEditor := common.Config.Editor
	common.Config.Editor = editorPath
	setForTest(t, &variable.RecentEditsFile, filepath.Join(tempDir, "recent.json"))
	defer func() {
		common.Config.Editor = originalEditor
	}()

	m := defaultTestModel(root)
	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), filePath)

	cmd := m.openFileWithEditor()
	require.NotNil(t, cmd)

	msg := ExecuteTeaCmdWithTimeout(cmd, DefaultTestTimeout)
	require.NotNil(t, msg)

	data, err := os.ReadFile(variable.RecentEditsFile)
	require.NoError(t, err)

	var entries []struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		EditedAt int64  `json:"edited_at,omitempty"`
	}
	require.NoError(t, json.Unmarshal(data, &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, filePath, entries[0].Location)
}

func TestSidebarOpenRecentFileWithEditor_ReturnsCommandForRecentItem(t *testing.T) {
	editorPath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true command not available")
	}

	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	filePath := filepath.Join(tempDir, "recent.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filePath)

	recentEntries, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		EditedAt int64  `json:"edited_at,omitempty"`
	}{
		{
			Location: filePath,
			Name:     filepath.Base(filePath),
			EditedAt: 1,
		},
	})
	require.NoError(t, err)

	recentFile := filepath.Join(tempDir, "recent.json")
	utils.SetupFilesWithData(t, recentEntries, recentFile)

	originalEditor := common.Config.Editor
	common.Config.Editor = editorPath
	setForTest(t, &variable.RecentEditsFile, recentFile)
	defer func() {
		common.Config.Editor = originalEditor
	}()

	m := defaultTestModel(root)
	m.focusOnSideBar()
	m.sidebarModel.ListBottom(m.mainPanelHeight)

	require.True(t, m.sidebarModel.IsCurrentItemRecentEdit())

	cmd := m.openSidebarRecentFileWithEditor()
	require.NotNil(t, cmd)

	msg := ExecuteTeaCmdWithTimeout(cmd, DefaultTestTimeout)
	require.NotNil(t, msg)
}

func TestSidebarPinnedDirectoryHotkey_RemovesSelectedRecentItem(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	filePath := filepath.Join(tempDir, "recent.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filePath)

	recentEntries, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		EditedAt int64  `json:"edited_at,omitempty"`
	}{
		{
			Location: filePath,
			Name:     filepath.Base(filePath),
			EditedAt: 1,
		},
	})
	require.NoError(t, err)

	recentFile := filepath.Join(tempDir, "recent.json")
	utils.SetupFilesWithData(t, recentEntries, recentFile)

	setForTest(t, &variable.RecentEditsFile, recentFile)

	m := defaultTestModel(root)
	m.focusOnSideBar()
	m.sidebarModel.ListBottom(m.mainPanelHeight)

	require.True(t, m.sidebarModel.IsCurrentItemRecentEdit())
	assert.Equal(t, filePath, m.sidebarModel.GetCurrentDirectoryLocation())

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.PinnedDirectory[0]))

	recentMgr := sidebar.NewRecentEditsManager(recentFile)
	assert.Empty(t, recentMgr.Load())
	assert.Equal(t, sidebarFocus, m.focusPanel)
	assert.Equal(t, root, m.getFocusedFilePanel().location)
}
