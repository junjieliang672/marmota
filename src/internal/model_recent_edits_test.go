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
