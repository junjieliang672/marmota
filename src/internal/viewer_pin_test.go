package internal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func pinKey(m *model) { pressKey(m, common.Hotkeys.PinViewer[0]) }

// previewText is the screen without its colors.
func previewText(m *model) string {
	return ansi.Strip(m.View())
}

func TestVInTheFilesPinsTheAskBoxsSession(t *testing.T) {
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	target := m.dockAskTarget()
	s, ok := m.claude.snapshot.Find(target)
	require.True(t, ok)

	pinKey(m)

	assert.Equal(t, nonePanelFocus, m.focusPanel, "the keyboard stays in the files")
	assert.True(t, m.viewerPinned())
	assert.Equal(t, target, m.sessions.list.SelectedID())
	assert.True(t, m.sessionsShown(), "its conversation and questions are read while it shows")
	screen := previewText(m)
	assert.Contains(t, screen, s.DisplayTitle())
	assert.Contains(t, screen, "pinned")
	assert.Contains(t, screen, "V unpin", "its key at the foot, as the dock's boxes have")

	pinned := m.dockSessionPreview(m.fileModel.filePreview.GetWidth(), m.fileModel.filePreview.GetHeight())
	assert.Contains(t, pinned, sgrFG(common.ClaudeAgentColor), "the pinned preview's frame is Claude's orange")

	pinKey(m)
	assert.False(t, m.viewerPinned(), "V again unpins")
	assert.NotContains(t, previewText(m), "V unpin")
}

func TestVInTheSessionsBoxPinsTheSelectedOneAndGoesBackToTheFiles(t *testing.T) {
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	bang(m)
	pressKey(m, "j")
	picked := m.sessions.list.SelectedID()

	pinKey(m)

	assert.Equal(t, nonePanelFocus, m.focusPanel)
	assert.True(t, m.viewerPinned())
	s, _ := m.claude.snapshot.Find(picked)
	assert.Contains(t, previewText(m), s.DisplayTitle())
}

func TestWAndTheSessionEndingUnpin(t *testing.T) {
	m, _ := sessionFilesModel(t)
	pinKey(m)
	require.True(t, m.viewerPinned())
	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	assert.False(t, m.viewerPinned(), "W: the preview shows the diffs")

	pressKey(m, common.Hotkeys.ShowSessionFiles[0])
	pinKey(m)
	require.True(t, m.viewerPinned())
	id := m.sessions.list.SelectedID()
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == id {
			m.claude.snapshot.Live[i].Live = false
		}
	}
	m.keepViewerPin()
	assert.False(t, m.viewerPinned(), "its session ended")
}

// sgrFG is how lipgloss starts text in color, to find it in a render.
func sgrFG(color string) string {
	out := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("x")
	return out[:strings.Index(out, "x")]
}
