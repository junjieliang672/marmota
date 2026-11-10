package internal

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

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
