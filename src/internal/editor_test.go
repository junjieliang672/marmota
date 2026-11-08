package internal

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestEditorCommand(t *testing.T) {
	paths := []string{"/d/a.txt", "/d/b.txt"}
	tests := []struct {
		name, configured, env, goos string
		wantCmd                     string
		wantArgs                    []string
	}{
		{"configured editor gets every path", "nvim -p", "vi", utils.OsDarwin, "nvim", []string{"-p", "/d/a.txt", "/d/b.txt"}},
		{"else $EDITOR", "", "hx", utils.OsDarwin, "hx", paths},
		{"else nvim on macOS", "", "", utils.OsDarwin, "nvim", paths},
		{"else nano on linux", "", "", "linux", "nano", paths},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := editorCommand(tt.configured, tt.env, tt.goos, paths)
			assert.Equal(t, tt.wantCmd, cmd)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

func setupSelectedEdit(t *testing.T) (*model, string, string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	a, b, sub := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt"), filepath.Join(root, "sub")
	utils.SetupDirectories(t, root, sub)
	utils.SetupFiles(t, a, b)
	return defaultTestModel(root), a, b, sub
}

func TestSelectedFilesInPanelOrderWithoutFolders(t *testing.T) {
	m, a, b, sub := setupSelectedEdit(t)
	setupPanelModeAndSelection(t, m, true, "", []string{b, sub, a})

	assert.Equal(t, []string{a, b}, m.getFocusedFilePanel().selectedFiles())
}

func TestEditKeyInSelectMode(t *testing.T) {
	t.Run("opens every selected file", func(t *testing.T) {
		m, a, b, _ := setupSelectedEdit(t)
		setupPanelModeAndSelection(t, m, true, "", []string{a, b})

		cmd := m.mainKey(common.Hotkeys.OpenFileWithEditor[0])

		require.NotNil(t, cmd)
		assert.True(t, m.externalProcessRunning, "e should start the editor, not extract")
	})

	t.Run("only folders selected: nothing opens", func(t *testing.T) {
		m, _, _, sub := setupSelectedEdit(t)
		setupPanelModeAndSelection(t, m, true, "", []string{sub})

		m.mainKey(common.Hotkeys.OpenFileWithEditor[0])

		assert.False(t, m.externalProcessRunning)
		assert.Equal(t, "No files selected to edit", m.claude.flash)
	})

	t.Run("extract is no longer on e", func(t *testing.T) {
		assert.NotContains(t, common.Hotkeys.SelectModeExtract, "e")
		assert.Contains(t, common.Hotkeys.SelectModeExtract, "ctrl+e")
	})
}

func TestExtractFallsBackToTheCursorsArchive(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	a, zip := filepath.Join(root, "a.txt"), filepath.Join(root, "b.zip")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, a, zip)
	m := defaultTestModel(root)
	setupPanelModeAndSelection(t, m, true, "", []string{a})
	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), zip)

	assert.Equal(t, []string{zip}, extractableFiles(m.getFocusedFilePanel()),
		"no selected archive, so the cursor's archive")
}
