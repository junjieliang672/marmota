package internal

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestTheSidebarPopup(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	pinned := filepath.Join(tempDir, "a-pinned-folder")
	utils.SetupDirectories(t, root, pinned)
	configureSidebarPinnedAndRecentForTest(t, tempDir, []sidebarTestEntry{{Location: pinned, Name: "a-pinned-folder"}}, nil)
	m := defaultTestModel(root)

	pressKey(m, common.Hotkeys.FocusOnSidebar[0])
	require.Equal(t, sidebarFocus, m.focusPanel)
	t.Log("cursor location:", m.sidebarModel.GetCurrentDirectoryLocation())
	popup := ansi.Strip(m.sidebarPopupRender())
	t.Log(popup)
	assert.Contains(t, popup, "a-pinned-folder")
	assert.Contains(t, popup, "path /", "the picked row's whole path, under the list")
	assert.Contains(t, popup, "folder", "wrapped, not cut")
	assert.Contains(t, ansi.Strip(m.View()), "path ", "drawn over the panels")

	pressKey(m, "esc")
	assert.Equal(t, nonePanelFocus, m.focusPanel, "esc closes it")
	assert.NotContains(t, ansi.Strip(m.View()), "path ")
}

func TestTheSidebarPopupListsSessionsAndEnterGoesToOne(t *testing.T) {
	ownAskHistory(t)
	m := dockTestModel(t)
	sessions := m.dockSessions()
	require.Len(t, sessions, 2)

	pressKey(m, common.Hotkeys.FocusOnSidebar[0])
	popup := ansi.Strip(m.sidebarPopupRender())
	assert.Contains(t, popup, "◆ Sessions")
	assert.Contains(t, popup, sessions[0].DisplayTitle())
	assert.Contains(t, popup, sessions[1].DisplayTitle())
	if recent := strings.Index(popup, "Recent"); recent >= 0 {
		assert.Less(t, strings.Index(popup, "◆ Sessions"), recent, "the sessions are above Recent")
	}

	m.sidebarModel.UpdateDirectoriesWithQueryForTest("ime hold")
	popup = ansi.Strip(m.sidebarPopupRender())
	assert.Contains(t, popup, "ime hold tests", "search finds a session by its name")
	assert.NotContains(t, popup, "provider registry")
	assert.Contains(t, popup, "enter go to ime hold tests", "the foot says what enter does")

	pressKey(m, "enter")
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "enter on a session gives the Sessions box the keyboard")
	s, ok := m.selectedSession()
	require.True(t, ok)
	assert.Equal(t, "ime hold tests", s.DisplayTitle(), "on that session")
}
