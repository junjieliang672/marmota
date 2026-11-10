package internal

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

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
