package internal

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestSidebarMovePinnedItem_ReordersWithArrowsAndShowsMarker(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	first := filepath.Join(tempDir, "first")
	second := filepath.Join(tempDir, "second")
	third := filepath.Join(tempDir, "third")
	utils.SetupDirectories(t, root, first, second, third)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	pinnedData, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
	}{
		{Location: first, Name: "first", PinnedAt: 1},
		{Location: second, Name: "second", PinnedAt: 2},
		{Location: third, Name: "third", PinnedAt: 3},
	})
	require.NoError(t, err)
	utils.SetupFilesWithData(t, pinnedData, pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	m.focusOnSideBar()

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.MovePinnedItem[0]))
	require.True(t, m.sidebarModel.IsMovingPinned())
	assert.Contains(t, ansi.Strip(m.sidebarPopupRender()), "first [move]")

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.ListDown[0]))
	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.ConfirmTyping[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	loaded := pinnedMgr.Load()
	require.Len(t, loaded, 3)
	assert.Equal(t, []string{second, first, third}, []string{
		loaded[0].Location,
		loaded[1].Location,
		loaded[2].Location,
	})
	assert.Equal(t, first, m.sidebarModel.GetCurrentDirectoryLocation())
}

func TestSidebarMovePinnedItem_ReordersToTypedIndex(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	first := filepath.Join(tempDir, "first")
	second := filepath.Join(tempDir, "second")
	third := filepath.Join(tempDir, "third")
	utils.SetupDirectories(t, root, first, second, third)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	pinnedData, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
	}{
		{Location: first, Name: "first", PinnedAt: 1},
		{Location: second, Name: "second", PinnedAt: 2},
		{Location: third, Name: "third", PinnedAt: 3},
	})
	require.NoError(t, err)
	utils.SetupFilesWithData(t, pinnedData, pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	m.focusOnSideBar()

	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.MovePinnedItem[0]))
	TeaUpdate(m, utils.TeaRuneKeyMsg("3"))
	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.ConfirmTyping[0]))

	pinnedMgr := sidebar.NewPinnedFileManager(pinnedFile)
	loaded := pinnedMgr.Load()
	require.Len(t, loaded, 3)
	assert.Equal(t, []string{second, third, first}, []string{
		loaded[0].Location,
		loaded[1].Location,
		loaded[2].Location,
	})
	assert.Equal(t, first, m.sidebarModel.GetCurrentDirectoryLocation())
}
