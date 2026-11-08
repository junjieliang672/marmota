package internal

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestSidebarPinnedDirectoryHotkey_UnpinsSelectedPinnedDirectory(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	pinnedDir := filepath.Join(root, "pinned")
	otherDir := filepath.Join(root, "other")
	utils.SetupDirectories(t, root, pinnedDir, otherDir)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	data, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
	}{
		{
			Location: pinnedDir,
			Name:     "pinned",
			PinnedAt: 1,
		},
	})
	require.NoError(t, err)
	utils.SetupFilesWithData(t, data, pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	m.focusOnSideBar()

	assert.Equal(t, sidebarFocus, m.focusPanel)
	assert.Equal(t, pinnedDir, m.sidebarModel.GetCurrentDirectoryLocation())

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.PinnedDirectory[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	assert.Empty(t, pinnedMgr.Load())
	assert.Equal(t, sidebarFocus, m.focusPanel)
	assert.Equal(t, root, m.getFocusedFilePanel().location)
}

func TestPinnedDirectoryHotkey_PinsSelectedFile(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	filePath := filepath.Join(root, "pinned.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filePath)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	utils.SetupFilesWithData(t, []byte("[]"), pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), filePath)

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.PinnedDirectory[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	loaded := pinnedMgr.Load()
	require.Len(t, loaded, 1)
	assert.Equal(t, filePath, loaded[0].Location)
	assert.Equal(t, filepath.Base(filePath), loaded[0].Name)
}

func TestSidebarSelectDirectory_OnPinnedFileTargetsFileInParentDirectory(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	filePath := filepath.Join(root, "pinned.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filePath)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	data, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
	}{
		{
			Location: filePath,
			Name:     filepath.Base(filePath),
			PinnedAt: 1,
		},
	})
	require.NoError(t, err)
	utils.SetupFilesWithData(t, data, pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(tempDir)
	m.focusOnSideBar()

	require.Equal(t, filePath, m.sidebarModel.GetCurrentDirectoryLocation())

	m.sidebarSelectDirectory()
	TeaUpdate(m, nil)

	panel := m.getFocusedFilePanel()
	assert.Equal(t, nonePanelFocus, m.focusPanel)
	assert.True(t, panel.isFocused)
	assert.Equal(t, root, panel.location)
	assert.Equal(t, filepath.Base(filePath), panel.getSelectedItem().name)
}
