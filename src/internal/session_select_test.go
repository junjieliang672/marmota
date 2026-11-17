package internal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sessionPreviewModel has the preview on s20's conversation, the Sessions
// box with the keyboard, and the clipboard caught in copied.
func sessionPreviewModel(t *testing.T) (*model, *string) {
	t.Helper()
	m := dockTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	m.claude.snapshot.Live[0].TranscriptPath = longTranscript(t)
	bang(m)
	require.True(t, m.sessions.list.Select("s20"))
	m.sessionsSelectionChanged()
	copied := new(string)
	setForTest(t, &writeClipboard, func(text string) error {
		*copied = text
		return nil
	})
	return m, copied
}

func mouse(m *model, action tea.MouseAction, x, y int) {
	TeaUpdate(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action})
}

// Dragging over the session preview selects what it draws, as a terminal
// does, and letting go copies it. The box's border isn't part of it.
func TestDraggingInTheSessionPreviewCopiesWhatItSelects(t *testing.T) {
	m, copied := sessionPreviewModel(t)
	pv := &m.fileModel.filePreview
	left := m.fullWidth - pv.GetWidth()
	rows := strings.Split(ansi.Strip(m.dockSessionPreview(pv.GetWidth(), pv.GetHeight())), "\n")
	require.Equal(t, "│› question 21", strings.TrimRight(rows[5], " │"))
	require.Equal(t, "│⏺ answer 21", strings.TrimRight(rows[6], " │"))

	mouse(m, tea.MouseActionPress, left+3, 5) // on the q of question 21
	mouse(m, tea.MouseActionMotion, left+20, 8)
	mouse(m, tea.MouseActionMotion, left+8, 6)
	view := m.View()
	assert.Contains(t, view, "\x1b[7m", "what it selects is lit while you drag")
	assert.Empty(t, *copied, "nothing is copied before you let go")
	mouse(m, tea.MouseActionRelease, left+8, 6) // the r of answer
	assert.Equal(t, "question 21\n⏺ answer", *copied)
	assert.Contains(t, m.claude.flash, "Copied 2 lines")

	// Up and over the border: from the start of the row, the border left out.
	mouse(m, tea.MouseActionPress, left+10, 6)
	mouse(m, tea.MouseActionRelease, left-5, 5)
	assert.Equal(t, "› question 21\n⏺ answer 2", *copied, "backwards works too")
	assert.True(t, m.sessions.selection.on, "the selection stays lit")

	TeaUpdate(m, utilsKey("j"))
	assert.False(t, m.sessions.selection.on, "a key puts it away")

	*copied = ""
	mouse(m, tea.MouseActionPress, left+3, 5)
	mouse(m, tea.MouseActionRelease, left+3, 5)
	assert.Empty(t, *copied, "a click copies nothing")
	assert.False(t, m.sessions.selection.on)

	mouse(m, tea.MouseActionPress, left+3, 5)
	mouse(m, tea.MouseActionRelease, left+9, 5)
	wheel(m, tea.MouseButtonWheelUp, left+3, 5)
	assert.False(t, m.sessions.selection.on, "scrolling puts it away: the lines it lit moved")
}

// Over the file preview, or with the preview on a file, the mouse selects
// nothing: only the session preview takes it.
func TestOnlyTheSessionPreviewSelects(t *testing.T) {
	m, copied := sessionPreviewModel(t)
	pv := &m.fileModel.filePreview
	left := m.fullWidth - pv.GetWidth()

	mouse(m, tea.MouseActionPress, 3, 5) // the file panel
	mouse(m, tea.MouseActionRelease, 20, 6)
	assert.Empty(t, *copied)

	TeaUpdate(m, utilsKey("esc")) // back to the files: the preview is the file's
	require.False(t, m.previewShowsASession())
	mouse(m, tea.MouseActionPress, left+3, 5)
	mouse(m, tea.MouseActionRelease, left+9, 6)
	assert.Empty(t, *copied)
	assert.False(t, m.sessions.selection.on)
}

// The lit cells keep the box as it was: the same width, the same text, and
// what comes after them drawn as before.
func TestTheSelectionKeepsTheBox(t *testing.T) {
	box := strings.Join([]string{
		"╭────────╮",
		"│\x1b[31mred\x1b[0m 中文│",
		"│\x1b[32mgreen\x1b[0m   │",
		"╰────────╯",
	}, "\n")
	sel := previewSelection{on: true, from: previewCell{1, 2}, to: previewCell{2, 3}}
	lit := highlightSelection(box, sel)
	assert.Equal(t, ansi.Strip(box), ansi.Strip(lit))
	for i, line := range strings.Split(lit, "\n") {
		assert.Equal(t, ansi.StringWidth(strings.Split(box, "\n")[i]), ansi.StringWidth(line), "row %d", i)
	}
	assert.Equal(t, "ed 中文\ngre", selectedText(box, sel))
	assert.Contains(t, strings.Split(lit, "\n")[1], "\x1b[31m\x1b[7med", "lit, its color kept")

	// A selection that starts on a wide character's second cell lights the
	// whole character, as a terminal does, and the border stays put.
	half := highlightSelection(box, previewSelection{on: true, from: previewCell{1, 6}, to: previewCell{1, 6}})
	row := strings.Split(half, "\n")[1]
	assert.Equal(t, ansi.Strip(strings.Split(box, "\n")[1]), ansi.Strip(row))
	assert.Equal(t, 10, ansi.StringWidth(row))
	assert.Contains(t, row, "\x1b[7m中")
}
