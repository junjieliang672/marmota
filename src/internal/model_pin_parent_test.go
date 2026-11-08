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

func TestPinParentFolderHotkey_PinsParentOfSelectedFolder(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	child := filepath.Join(root, "child")
	utils.SetupDirectories(t, root, child)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	utils.SetupFilesWithData(t, []byte("[]"), pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), child)

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.PinParentFolder[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	loaded := pinnedMgr.Load()
	require.Len(t, loaded, 1)
	assert.Equal(t, root, loaded[0].Location)
}

func TestPinParentFolderHotkey_IgnoredWhenSearchResultsActive(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	child := filepath.Join(root, "child")
	utils.SetupDirectories(t, root, child)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	utils.SetupFilesWithData(t, []byte("[]"), pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	m.getFocusedFilePanel().searchStack.Push(searchLayer{
		layerType:  searchLayerRegular,
		searchTerm: "child",
	})

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.PinParentFolder[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	assert.Empty(t, pinnedMgr.Load())
}

func TestPinParentFolderHotkey_IgnoredInSelectMode(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	child := filepath.Join(root, "child")
	utils.SetupDirectories(t, root, child)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	utils.SetupFilesWithData(t, []byte("[]"), pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	m.getFocusedFilePanel().changeFilePanelMode()

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.PinParentFolder[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	assert.Empty(t, pinnedMgr.Load())
}

func TestPinParentFolderHotkey_PinsParentOfSelectedFile(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	filePath := filepath.Join(root, "file.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, filePath)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	initial, err := json.Marshal([]struct{}{})
	require.NoError(t, err)
	utils.SetupFilesWithData(t, initial, pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), filePath)

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.PinParentFolder[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	loaded := pinnedMgr.Load()
	require.Len(t, loaded, 1)
	assert.Equal(t, root, loaded[0].Location)
}
