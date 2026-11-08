package internal

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestGetMetadataCmd_UsesSidebarSelectionWhenSidebarFocused(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	olderPinned := filepath.Join(tempDir, "older")
	newestPinned := filepath.Join(tempDir, "newest")
	utils.SetupDirectories(t, root, olderPinned, newestPinned)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	pinnedData, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
	}{
		{Location: olderPinned, Name: "older", PinnedAt: 1},
		{Location: newestPinned, Name: "newest", PinnedAt: 2},
	})
	require.NoError(t, err)
	utils.SetupFilesWithData(t, pinnedData, pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	m.disableMetadata = false
	m.focusOnSideBar()

	require.Equal(t, sidebarFocus, m.focusPanel)
	require.Equal(t, olderPinned, m.sidebarModel.GetCurrentDirectoryLocation())

	cmd := m.getMetadataCmd()
	require.NotNil(t, cmd)

	msg := ExecuteTeaCmdWithTimeout(cmd, DefaultTestTimeout)
	require.NotNil(t, msg)

	TeaUpdate(m, msg)

	assert.Equal(t, olderPinned, m.fileMetaData.GetMetadata().GetPath())
}

func TestMetadataMsg_IgnoresStaleSidebarSelection(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	olderPinned := filepath.Join(tempDir, "older")
	newestPinned := filepath.Join(tempDir, "newest")
	utils.SetupDirectories(t, root, olderPinned, newestPinned)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	pinnedData, err := json.Marshal([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
	}{
		{Location: olderPinned, Name: "older", PinnedAt: 1},
		{Location: newestPinned, Name: "newest", PinnedAt: 2},
	})
	require.NoError(t, err)
	utils.SetupFilesWithData(t, pinnedData, pinnedFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)

	m := defaultTestModel(root)
	m.disableMetadata = false
	m.focusOnSideBar()

	firstPath := m.sidebarModel.GetCurrentDirectoryLocation()
	require.Equal(t, olderPinned, firstPath)

	oldCmd := m.getMetadataCmd()
	require.NotNil(t, oldCmd)

	m.sidebarModel.ListDown(m.mainPanelHeight)
	secondPath := m.sidebarModel.GetCurrentDirectoryLocation()
	require.Equal(t, newestPinned, secondPath)

	oldMsg := ExecuteTeaCmdWithTimeout(oldCmd, DefaultTestTimeout)
	require.NotNil(t, oldMsg)
	TeaUpdate(m, oldMsg)

	assert.NotEqual(t, firstPath, m.fileMetaData.GetMetadata().GetPath())

	newCmd := m.getMetadataCmd()
	require.NotNil(t, newCmd)
	newMsg := ExecuteTeaCmdWithTimeout(newCmd, DefaultTestTimeout)
	require.NotNil(t, newMsg)
	TeaUpdate(m, newMsg)

	assert.Equal(t, secondPath, m.fileMetaData.GetMetadata().GetPath())
}
