package claudeprompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A prompt as Claude Code draws it in a pane 100 columns wide, the second
// line of the text folded at a space: taken from a real one.
const cursorScreen = ` ▐▛███▛█   Claude Code v2.1.326
▝▜██████▀  Fable 5.2 with high effort
 ▝▝   ▝▝   ~/Developer/marmota


────────────────────────────────────────────────────────────────────────────────────────────────────
❯ first line of the prompt with some words
  second line 中文字 here and it is long enough to wrap around the edge of the box when the pane
  is one hundred columns wide
  third
────────────────────────────────────────────────────────────────────────────────────────────────────
  [Fable 5.2] marmota (dev)                                             ctrl+g to edit in micro
  ⏵⏵ auto mode on (shift+tab to cycle)`

const cursorText = "first line of the prompt with some words\n" +
	"second line 中文字 here and it is long enough to wrap around the edge of the box when the pane " +
	"is one hundred columns wide\nthird"

// The editor opens where the prompt's cursor was: the place in the text is
// read off the screen as it was when the key was pressed.
func TestTheCursorsPlaceInThePromptsText(t *testing.T) {
	rows := strings.Split(cursorScreen, "\n")
	at := func(x, y int) [2]int {
		line, col, ok := CursorIn(rows, x, y, cursorText)
		require.True(t, ok, "%d, %d", x, y)
		return [2]int{line, col}
	}
	// Row 6 is the text's first line; the prompt's mark and a space take
	// two columns.
	assert.Equal(t, [2]int{1, 1}, at(2, 6), "at the text's start")
	assert.Equal(t, [2]int{1, 39}, at(40, 6), "two characters before the first line's end")
	assert.Equal(t, [2]int{1, 41}, at(42, 6), "at the first line's end")
	assert.Equal(t, [2]int{2, 1}, at(2, 7), "at the second line's start")
	// A Chinese character takes two columns and is one character: after
	// "second line 中文字" the cursor is in column 2+12+6, 15 characters in.
	assert.Equal(t, [2]int{2, 16}, at(20, 7))
	// The folded line's second row: its characters go on from the first
	// row's, the space the fold took counted.
	first := len([]rune("second line 中文字 here and it is long enough to wrap around the edge of the box when the pane "))
	assert.Equal(t, [2]int{2, first + 1}, at(2, 8), "the folded row's start")
	assert.Equal(t, [2]int{2, first + 4}, at(5, 8))
	assert.Equal(t, [2]int{3, 6}, at(7, 9), "at the text's end")
}

// Lines that repeat fit several rows: the cursor's line is counted from the
// prompt's first row, not from the nearest row that looks like the start.
func TestTheCursorsLineIsCountedFromThePromptsTop(t *testing.T) {
	rows := []string{"──────", "› again", "  again", "  again", "──────"}
	line, col, ok := CursorIn(rows, 4, 3, "again\nagain\nagain")
	require.True(t, ok)
	assert.Equal(t, [2]int{3, 3}, [2]int{line, col})

	// An empty line between two, and spaces the text keeps at a line's end.
	rows = []string{"> one  ", "", "  two"}
	line, col, ok = CursorIn(rows, 2, 1, "one  \n\ntwo")
	require.True(t, ok)
	assert.Equal(t, [2]int{2, 1}, [2]int{line, col}, "on the empty line")
	line, col, ok = CursorIn(rows, 7, 0, "one  \n\ntwo")
	require.True(t, ok)
	assert.Equal(t, [2]int{1, 6}, [2]int{line, col}, "after the spaces at the first line's end")
}

// What isn't the text on the screen gives no place: the editor then opens
// at the start, as before.
func TestNoPlaceWhenTheScreenIsNotTheText(t *testing.T) {
	rows := strings.Split(cursorScreen, "\n")
	for name, c := range map[string]struct {
		x, y int
		text string
	}{
		"another text":             {4, 6, "something else entirely"},
		"a pasted piece, folded":   {4, 6, cursorText + "\nand forty lines more that the box shows as one mark"},
		"the cursor above the box": {4, 2, cursorText},
		"the cursor under the box": {4, 11, cursorText},
		"no such row":              {4, 99, cursorText},
		"no text":                  {4, 6, ""},
	} {
		_, _, ok := CursorIn(rows, c.x, c.y, c.text)
		assert.False(t, ok, name)
	}
}
