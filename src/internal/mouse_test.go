package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

// Outside the previews the mouse does nothing: here over the sidebar, at the
// top left corner.
func TestTheMouseDoesNothingOutsideThePreviews(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	utils.SetupDirectories(t, child)
	for i := range 5 {
		utils.SetupFiles(t, filepath.Join(dir, fmt.Sprintf("file%d.txt", i)))
	}
	m := defaultTestModel(dir)
	TeaUpdate(m, nil)
	panel := m.getFocusedFilePanel()
	require.Equal(t, 0, panel.cursor)

	for _, button := range []tea.MouseButton{tea.MouseButtonWheelDown, tea.MouseButtonWheelUp,
		tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight, tea.MouseButtonLeft} {
		assert.Nil(t, TeaUpdate(m, tea.MouseMsg{Button: button, Action: tea.MouseActionPress}))
	}
	assert.Equal(t, 0, m.getFocusedFilePanel().cursor, "scrolling doesn't move the cursor")
	assert.Equal(t, dir, m.getFocusedFilePanel().location, "nor change the folder")
}

func TestMouseReportingComesBackAfterTheEditor(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	TeaUpdate(m, nil)
	m.externalProcessRunning = true

	cmd := TeaUpdate(m, editorFinishedMsg{})
	assert.False(t, m.externalProcessRunning)
	assert.True(t, batchRuns(cmd, restoreMouseReporting),
		"bubbletea leaves mouse reporting off after the editor; without it WezTerm sends scrolling as arrow keys")
	assert.IsType(t, tea.EnableMouseCellMotion(), restoreMouseReporting())

	m.externalProcessRunning = true
	cmd = TeaUpdate(m, sudoDeleteFinishedMsg{})
	assert.True(t, batchRuns(cmd, restoreMouseReporting), "and after sudo")
}

// batchRuns reports whether cmd is want or a batch that holds it.
func batchRuns(cmd, want tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	wantPtr := reflect.ValueOf(want).Pointer()
	if reflect.ValueOf(cmd).Pointer() == wantPtr {
		return true
	}
	// A batch returns its commands at once; anything else is no match.
	batch, ok := ExecuteTeaCmdWithTimeout(cmd, 10*time.Millisecond).(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, c := range batch {
		if c != nil && reflect.ValueOf(c).Pointer() == wantPtr {
			return true
		}
	}
	return false
}

func wheel(m *model, button tea.MouseButton, x, y int) tea.Cmd {
	return TeaUpdate(m, tea.MouseMsg{X: x, Y: y, Button: button, Action: tea.MouseActionPress})
}

// The wheel scrolls the session the preview shows. Over the file panel or
// the Sessions box it does nothing, and so do a sideways scroll and a click.
func TestTheWheelScrollsTheSessionPreview(t *testing.T) {
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	m.claude.snapshot.Live[0].TranscriptPath = longTranscript(t)
	bang(m)
	require.True(t, m.sessions.list.Select("s20"))
	m.sessionsSelectionChanged()
	x := m.fullWidth - m.fileModel.filePreview.GetWidth() + 5

	wheel(m, tea.MouseButtonWheelUp, x, 5)
	assert.Equal(t, wheelLines, m.sessions.scroll, "up, to older turns")
	wheel(m, tea.MouseButtonWheelDown, x, 5)
	wheel(m, tea.MouseButtonWheelDown, x, 5)
	assert.Equal(t, 0, m.sessions.scroll, "not past the latest line")

	cursor := m.getFocusedFilePanel().cursor
	for _, at := range [][2]int{{2, 2}, {2, m.mainPanelHeight + 4}} { // the file panel, the Sessions box
		wheel(m, tea.MouseButtonWheelUp, at[0], at[1])
		wheel(m, tea.MouseButtonWheelDown, at[0], at[1])
	}
	wheel(m, tea.MouseButtonWheelLeft, x, 5)
	TeaUpdate(m, tea.MouseMsg{X: x, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	assert.Equal(t, 0, m.sessions.scroll)
	assert.Equal(t, "s20", m.sessions.list.SelectedID(), "the list's cursor stays")
	assert.Equal(t, cursor, m.getFocusedFilePanel().cursor, "and the file panel's")

	m.helpMenu.open = true
	wheel(m, tea.MouseButtonWheelUp, x, 5)
	assert.Equal(t, 0, m.sessions.scroll, "nothing scrolls under a modal")
}

// Over the file preview the wheel scrolls it. The first scroll down draws
// the file past what fits, in the background; the rest scroll at once.
func TestTheWheelScrollsTheFilePreview(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&b, "row %03d\n", i)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0o600))
	m := defaultTestModel(dir)
	TeaUpdate(m, nil)
	if !m.fileModel.filePreview.IsOpen() {
		m.toggleFilePreviewPanel()
	}
	setFilePanelSelectedItemByName(t, m.getFocusedFilePanel(), "long.txt")
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(m.getFilePreviewCmd(true), 2*time.Second))
	require.Contains(t, ansi.Strip(m.View()), "row 001")
	x, y := m.fullWidth-3, 3

	cmd := wheel(m, tea.MouseButtonWheelDown, x, y)
	require.NotNil(t, cmd, "the first scroll down draws the file past what fits")
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(cmd, 2*time.Second))
	view := ansi.Strip(m.View())
	assert.NotContains(t, view, "row 001")
	assert.Contains(t, view, "row 004", "three rows down")

	assert.Nil(t, wheel(m, tea.MouseButtonWheelUp, x, y), "the rest scroll at once")
	assert.Contains(t, ansi.Strip(m.View()), "row 001")
	for range 100 {
		wheel(m, tea.MouseButtonWheelDown, x, y)
	}
	view = ansi.Strip(m.View())
	assert.Contains(t, view, "row 200", "down to the end")
	assert.NotContains(t, view, "row 001")

	cursor := m.getFocusedFilePanel().cursor
	wheel(m, tea.MouseButtonWheelUp, 30, y) // over the file panel
	assert.Contains(t, ansi.Strip(m.View()), "row 200", "the file panel takes nothing from the wheel")
	assert.Equal(t, cursor, m.getFocusedFilePanel().cursor)
}

// Sideways the wheel shows what the file preview cuts off of long lines,
// with a trackpad's sideways scroll or with shift. A sideways scroll WezTerm
// sends from outside superfile's screen does nothing.
func TestTheWheelScrollsTheFilePreviewSideways(t *testing.T) {
	dir := t.TempDir()
	wide := "start " + strings.Repeat("-", 300) + " far end"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wide.txt"), []byte(wide+"\nshort\n"), 0o600))
	m := defaultTestModel(dir)
	TeaUpdate(m, nil)
	if !m.fileModel.filePreview.IsOpen() {
		m.toggleFilePreviewPanel()
	}
	setFilePanelSelectedItemByName(t, m.getFocusedFilePanel(), "wide.txt")
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(m.getFilePreviewCmd(true), 2*time.Second))
	require.Contains(t, ansi.Strip(m.View()), "start ---")
	x, y := m.fullWidth-3, 3

	assert.Nil(t, wheel(m, tea.MouseButtonWheelRight, m.fullWidth+5, y), "outside the screen")
	cmd := wheel(m, tea.MouseButtonWheelRight, x, y)
	require.NotNil(t, cmd, "the first scroll right draws the file's whole lines")
	TeaUpdate(m, ExecuteTeaCmdWithTimeout(cmd, 2*time.Second))
	assert.NotContains(t, ansi.Strip(m.View()), "start ---", "six columns right")

	for range 100 {
		TeaUpdate(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Shift: true,
			Action: tea.MouseActionPress})
	}
	view := ansi.Strip(m.View())
	assert.Contains(t, strings.Split(view, "\n")[1], "far end",
		"shift and the wheel scroll sideways too, not down: the long line is still the first")
	for range 100 {
		wheel(m, tea.MouseButtonWheelLeft, x, y)
	}
	assert.Contains(t, ansi.Strip(m.View()), "start ---")
}
