package internal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zoxidelib "github.com/lazysegtree/go-zoxide"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func setupProgAndOpenZoxide(t *testing.T, zClient *zoxidelib.Client, dir string) *TeaProg {
	t.Helper()
	common.Config.ZoxideSupport = true
	m := defaultTestModelWithZClient(zClient, dir)
	p := NewTestTeaProgWithEventLoop(t, m)

	p.SendKey(common.Hotkeys.OpenZoxide[0])
	assert.Eventually(t, func() bool {
		return p.getModel().zoxideModal.IsOpen()
	}, zoxideTestTimeout, DefaultTestTick, "Zoxide modal should open")
	return p
}

func updateCurrentFilePanelDirOfTestModel(t *testing.T, p *TeaProg, dir string) {
	err := p.getModel().updateCurrentFilePanelDir(dir)
	require.NoError(t, err, "Failed to navigate to %s", dir)
	assert.Equal(t, dir, p.getModel().getFocusedFilePanel().location, "Should be in %s after navigation", dir)
}

// zoxideTestTimeout is longer than the default because every query runs the
// zoxide binary, which can take over a second on a busy machine.
const zoxideTestTimeout = 3 * time.Second

func TestZoxide(t *testing.T) {
	//nolint:usetesting // t.TempDir's cleanup fails while zoxide is still writing
	zoxideDataDir, err := os.MkdirTemp("", "spf-zoxide-test-")
	require.NoError(t, err)
	// zoxide can still be writing its database as the test ends, which makes
	// t.TempDir's cleanup fail with "directory not empty". Retry instead.
	t.Cleanup(func() {
		assert.Eventually(t, func() bool { return os.RemoveAll(zoxideDataDir) == nil },
			zoxideTestTimeout, DefaultTestTick, "remove zoxide data dir")
	})
	zClient, err := zoxidelib.New(zoxidelib.WithDataDir(zoxideDataDir))
	if err != nil {
		if runtime.GOOS != utils.OsLinux {
			t.Skipf("Skipping zoxide tests in non-Linux because zoxide client cannot be initialized")
		} else {
			t.Fatalf("zoxide initialization failed")
		}
	}

	originalZoxideSupport := common.Config.ZoxideSupport
	defer func() {
		common.Config.ZoxideSupport = originalZoxideSupport
	}()

	curTestDir := filepath.Join(testDir, "TestZoxide")
	dir1 := filepath.Join(curTestDir, "dir1")
	dir2 := filepath.Join(curTestDir, "dir2")
	dir3 := filepath.Join(curTestDir, "dir3")
	multiSpaceDir := filepath.Join(curTestDir, "test  dir")
	utils.SetupDirectories(t, curTestDir, dir1, dir2, dir3, multiSpaceDir)

	t.Run("Zoxide tracking and navigation", func(t *testing.T) {
		p := setupProgAndOpenZoxide(t, zClient, dir1)
		updateCurrentFilePanelDirOfTestModel(t, p, dir2)
		updateCurrentFilePanelDirOfTestModel(t, p, dir3)

		p.SendKey("dir2")
		assert.Eventually(t, func() bool {
			results := p.getModel().zoxideModal.GetResults()
			return len(results) == 1 && results[0].Path == dir2
		}, zoxideTestTimeout, DefaultTestTick, "dir2 should be found by zoxide UI search")

		// Press enter to navigate to dir2
		p.SendKey(common.Hotkeys.ConfirmTyping[0])
		assert.Eventually(t, func() bool {
			return !p.getModel().zoxideModal.IsOpen()
		}, zoxideTestTimeout, DefaultTestTick, "Zoxide modal should close after navigation")
		// The modal closes a moment before the panel changes folder.
		assert.Eventually(t, func() bool {
			return p.getModel().getFocusedFilePanel().location == dir2
		}, zoxideTestTimeout, DefaultTestTick, "Should navigate back to dir2 after zoxide selection")
	})

	t.Run("Zoxide disabled shows no results", func(t *testing.T) {
		common.Config.ZoxideSupport = false
		m := defaultTestModelWithZClient(zClient, dir1)

		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenZoxide[0]))
		assert.True(t, m.zoxideModal.IsOpen(), "Zoxide modal should open even when ZoxideSupport is disabled")

		results := m.zoxideModal.GetResults()
		assert.Empty(t, results, "Zoxide modal should show no results when ZoxideSupport is disabled")
	})

	t.Run("Zoxide modal is the s popup's size on window resize", func(t *testing.T) {
		p := setupProgAndOpenZoxide(t, zClient, dir1)

		// Through the event loop: updating the model from the test goroutine
		// while the loop runs is a data race.
		p.Send(tea.WindowSizeMsg{Width: 2 * DefaultTestModelWidth, Height: 2 * DefaultTestModelHeight})

		assert.Eventually(t, func() bool {
			m := p.getModel()
			return m.fullWidth == 2*DefaultTestModelWidth &&
				m.zoxideModal.GetWidth() == m.popupWidth()+common.BorderPadding &&
				m.zoxideModal.GetMaxHeight() == m.sidebarListHeight()
		}, zoxideTestTimeout, DefaultTestTick, "as wide as the s popup, and its list area as tall")
	})

	t.Run("Zoxide 'z' key suppression on open", func(t *testing.T) {
		p := setupProgAndOpenZoxide(t, zClient, dir1)
		assert.Empty(t, p.getModel().zoxideModal.GetTextInputValue(),
			"The 'z' key should not be added to textInput")
		p.SendKey("abc")
		assert.Eventually(t, func() bool {
			return p.getModel().zoxideModal.GetTextInputValue() == "abc"
		}, zoxideTestTimeout, DefaultTestTick, "typed text should be exactly 'abc', got %q",
			p.getModel().zoxideModal.GetTextInputValue())
	})

	t.Run("Multi-space directory name navigation", func(t *testing.T) {
		common.Config.ZoxideSupport = true
		p := NewTeaProg(defaultTestModelWithZClient(zClient, dir1), false)
		// Visit the folders before the event loop starts: changing the model
		// from the test goroutine while the loop runs is a data race.
		updateCurrentFilePanelDirOfTestModel(t, p, multiSpaceDir)
		updateCurrentFilePanelDirOfTestModel(t, p, dir1)
		p.StartEventLoop()
		t.Cleanup(p.Close)
		openZoxide := func() {
			p.SendKey(common.Hotkeys.OpenZoxide[0])
			assert.Eventually(t, func() bool {
				return p.getModel().zoxideModal.IsOpen()
			}, zoxideTestTimeout, DefaultTestTick, "Zoxide modal should open")
		}
		openZoxide()

		p.SendKey(filepath.Base(multiSpaceDir))
		assert.Eventually(t, func() bool {
			results := p.getModel().zoxideModal.GetResults()
			for _, result := range results {
				if result.Path == multiSpaceDir {
					return true
				}
			}
			return false
		}, zoxideTestTimeout, DefaultTestTick, "Multi-space directory should be found by zoxide")

		// Reset the text input by closing and opening the modal again
		p.SendKey(common.Hotkeys.CancelTyping[0])
		assert.Eventually(t, func() bool {
			return !p.getModel().zoxideModal.IsOpen()
		}, zoxideTestTimeout, DefaultTestTick, "Zoxide modal should close")
		openZoxide()
		p.SendKey("di r 1")
		assert.Eventually(t, func() bool {
			results := p.getModel().zoxideModal.GetResults()
			for _, result := range results {
				if result.Path == dir1 {
					return true
				}
			}
			return false
		}, zoxideTestTimeout, DefaultTestTick, "dir1 should be found by zoxide")
	})

	t.Run("Zoxide escape key closes modal", func(t *testing.T) {
		p := setupProgAndOpenZoxide(t, zClient, dir1)
		p.SendKey(common.Hotkeys.CancelTyping[0])
		assert.Eventually(t, func() bool {
			return !p.getModel().zoxideModal.IsOpen()
		}, zoxideTestTimeout, DefaultTestTick, "Zoxide modal should close on escape key")
	})
}
