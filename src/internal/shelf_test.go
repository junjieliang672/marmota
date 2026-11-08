package internal

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestDirEditorCommand(t *testing.T) {
	tests := []struct {
		name     string
		editor   string
		goos     string
		targets  []string
		wantCmd  string
		wantArgs []string
	}{
		{"macOS reveals the item under the cursor", "", utils.OsDarwin, []string{"/d/a.txt"},
			"open", []string{"-R", "/d/a.txt"}},
		{"macOS reveals every selected item", "", utils.OsDarwin, []string{"/d/a.txt", "/d/b.txt"},
			"osascript", append(finderRevealScript(), "/d/a.txt", "/d/b.txt")},
		{"macOS empty directory opens the directory", "", utils.OsDarwin, nil, "open", []string{"/d"}},
		{"dir_editor set keeps opening the directory", "code -n", utils.OsDarwin, []string{"/d/a.txt"},
			"code", []string{"-n", "/d"}},
		{"windows", "", utils.OsWindows, []string{"/d/a.txt"}, "explorer", []string{"/d"}},
		{"linux", "", "linux", []string{"/d/a.txt"}, "vi", []string{"/d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := dirEditorCommand(tt.editor, tt.goos, "/d", tt.targets)
			assert.Equal(t, tt.wantCmd, cmd)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

func fakeShelfCommand(t *testing.T, err error) *[]string {
	t.Helper()
	var got []string
	original := runShelfCommand
	runShelfCommand = func(name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		if err != nil {
			return []byte("Unable to find application named 'Yoink'"), err
		}
		return nil, nil
	}
	t.Cleanup(func() { runShelfCommand = original })
	return &got
}

func TestSendToShelf(t *testing.T) {
	if runtime.GOOS != utils.OsDarwin {
		t.Skip("the shelf app is macOS only")
	}
	root := filepath.Join(t.TempDir(), "root")
	a, b := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	utils.SetupDirectories(t, root)
	utils.SetupFiles(t, a, b)
	setForTest(t, &common.Config.ShelfApp, "Yoink")

	t.Run("sends the item under the cursor", func(t *testing.T) {
		got := fakeShelfCommand(t, nil)
		m := defaultTestModel(root)
		setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), b)

		cmd := m.mainKey(common.Hotkeys.SendToShelf[0])
		require.NotNil(t, cmd)
		m.applyShelfSent(cmd().(shelfSentMsg))

		assert.Equal(t, []string{"open", "-g", "-a", "Yoink", b}, *got)
		assert.Equal(t, "1 item → Yoink", m.claude.flash)
	})

	t.Run("sends every selected item", func(t *testing.T) {
		got := fakeShelfCommand(t, nil)
		m := defaultTestModel(root)
		panel := m.getFocusedFilePanel()
		panel.selected[a] = struct{}{}
		panel.selected[b] = struct{}{}

		m.applyShelfSent(m.mainKey(common.Hotkeys.SendToShelf[0])().(shelfSentMsg))

		assert.Equal(t, []string{"open", "-g", "-a", "Yoink", a, b}, *got)
		assert.Equal(t, "2 items → Yoink", m.claude.flash)
	})

	t.Run("says when the app can't be opened", func(t *testing.T) {
		fakeShelfCommand(t, errors.New("exit status 1"))
		m := defaultTestModel(root)

		m.applyShelfSent(m.mainKey(common.Hotkeys.SendToShelf[0])().(shelfSentMsg))

		assert.Contains(t, m.claude.flash, "Unable to find application named 'Yoink'")
	})
}
