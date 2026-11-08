package internal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func helpMenuTestModel(t *testing.T, width, height int) *model {
	t.Helper()
	m := defaultTestModel(t.TempDir())
	TeaUpdate(m, tea.WindowSizeMsg{Width: width, Height: height})
	m.openHelpMenu()
	require.True(t, m.helpMenu.open)
	return m
}

func TestHelpMenuWrapsLongDescriptions(t *testing.T) {
	m := helpMenuTestModel(t, 70, 30)
	lines, starts, counts := m.helpMenuLines()

	wrapped := -1
	for i, n := range counts {
		if n > 1 {
			wrapped = i
			break
		}
	}
	require.GreaterOrEqual(t, wrapped, 0, "some description is longer than the menu is wide")
	words := strings.Fields(m.helpMenu.filteredData[wrapped].description)
	last := ansi.Strip(lines[starts[wrapped]+counts[wrapped]-1])
	assert.Contains(t, last, words[len(words)-1], "the end of the description is shown, not cut")
	for _, l := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(l), m.helpMenu.width-2, "no line is wider than the menu")
	}
	assert.NotContains(t, ansi.Strip(m.helpMenuRender()), "...", "nothing is cut with ...")
}

func TestHelpMenuKeepsTheSelectedEntryInView(t *testing.T) {
	m := helpMenuTestModel(t, 70, 30)
	view := m.helpMenuViewLines()
	for range 40 {
		m.helpMenuListDown()
		_, starts, counts := m.helpMenuLines()
		c := m.helpMenu.cursor
		assert.GreaterOrEqual(t, starts[c], m.helpMenu.renderIndex)
		assert.LessOrEqual(t, starts[c]+counts[c], m.helpMenu.renderIndex+view, "entry %d fully in view", c)
	}
	m.helpMenu.cursor = 1
	m.helpMenuListUp() // wraps to the last entry
	lines, starts, counts := m.helpMenuLines()
	c := m.helpMenu.cursor
	assert.Equal(t, len(m.helpMenu.filteredData)-1, c)
	assert.Equal(t, starts[c]+counts[c], m.helpMenu.renderIndex+view, "the last entry at the bottom")
	assert.Equal(t, len(lines), starts[c]+counts[c])
}

func TestTheWheelScrollsTheHelpMenu(t *testing.T) {
	m := helpMenuTestModel(t, 70, 30)
	before := m.helpMenu.renderIndex

	TeaUpdate(m, tea.MouseMsg{X: 10, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	assert.Equal(t, before+wheelLines, m.helpMenu.renderIndex)

	_, starts, counts := m.helpMenuLines()
	c := m.helpMenu.cursor
	assert.GreaterOrEqual(t, starts[c], m.helpMenu.renderIndex, "the selection moved into view")
	assert.LessOrEqual(t, starts[c]+counts[c], m.helpMenu.renderIndex+m.helpMenuViewLines())

	TeaUpdate(m, tea.MouseMsg{X: 10, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	assert.Equal(t, before, m.helpMenu.renderIndex)
}
