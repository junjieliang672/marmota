package internal

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestCopyPathHotkey_CopiesSidebarItemLocation(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	pinnedDir := filepath.Join(root, "pinned")
	utils.SetupDirectories(t, root, pinnedDir)

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

	var copied string
	originalWriteClipboard := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		writeClipboard = originalWriteClipboard
	}()

	m := defaultTestModel(root)
	m.focusOnSideBar()

	m.mainKey(common.Hotkeys.CopyPath[0])

	assert.Equal(t, pinnedDir, copied)
}

func TestCopyParentPathHotkey_CopiesFilePanelDirectory(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	filePath := filepath.Join(root, "file.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filePath)

	var copied string
	originalWriteClipboard := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		writeClipboard = originalWriteClipboard
	}()

	m := defaultTestModel(root)
	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), filePath)

	m.mainKey(common.Hotkeys.CopyParentPath[0])

	assert.Equal(t, root, copied)
}

func TestCopyParentPathHotkey_CopiesSidebarRecentItemParent(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	recentDir := filepath.Join(tempDir, "recent")
	recentFilePath := filepath.Join(recentDir, "recent.txt")
	utils.SetupDirectories(t, root, recentDir)
	utils.SetupFiles(t, recentFilePath)

	recentEntries, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		EditedAt int64  `json:"edited_at,omitempty"`
	}{
		{
			Location: recentFilePath,
			Name:     filepath.Base(recentFilePath),
			EditedAt: 1,
		},
	})
	require.NoError(t, err)

	recentFile := filepath.Join(tempDir, "recent.json")
	utils.SetupFilesWithData(t, recentEntries, recentFile)

	setForTest(t, &variable.RecentEditsFile, recentFile)

	var copied string
	originalWriteClipboard := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		writeClipboard = originalWriteClipboard
	}()

	m := defaultTestModel(root)
	m.focusOnSideBar()
	m.sidebarModel.ListBottom(m.mainPanelHeight)

	require.True(t, m.sidebarModel.IsCurrentItemRecentEdit())

	m.mainKey(common.Hotkeys.CopyParentPath[0])

	assert.Equal(t, recentDir, copied)
}
