package claudeprompt

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Where a prompt's cursor was, in the text the prompt held. A program that
// opens its prompt in an editor (Claude Code and Codex on ctrl+G) hands the
// editor the text and nothing about the cursor, and has redrawn its screen by
// the time the editor starts. So the screen and the terminal's cursor are
// kept as the key is pressed (Marmota's tmux does that), and the place in the
// text is worked out from them here: the rows of the prompt, read one under
// another, are the text, a row ending where a line of the text ends or where
// the box folded a long line.

const (
	// cursorRowsAbove is how many rows above the cursor's the prompt's
	// first row is looked for in.
	cursorRowsAbove = 200
	// cursorMaxIndent is how many columns a prompt's text may start in from:
	// Claude Code's and Codex's mark and a space take two.
	cursorMaxIndent = 4
)

// CursorIn reports the line and the column of text, both from 1 and counted
// in characters, that the cursor was at: rows is the screen as it was, x and
// y the cursor's column and row on it, from 0. It is not ok when the rows
// around the cursor aren't text, as with a prompt that shows a pasted piece
// folded, or one taller than the screen.
func CursorIn(rows []string, x, y int, text string) (int, int, bool) {
	at, ok := CursorOffset(rows, x, y, text)
	if !ok {
		return 0, 0, false
	}
	line, col := LineCol(text, at)
	return line, col, true
}

// CursorOffset is CursorIn's place as how many characters of text the cursor
// is past.
func CursorOffset(rows []string, x, y int, text string) (int, bool) {
	want := []rune(strings.ReplaceAll(text, "\r\n", "\n"))
	if y < 0 || y >= len(rows) || x < 0 || len(want) == 0 {
		return 0, false
	}
	// The topmost row the text can start in: a text whose lines repeat fits
	// lower rows too, with the cursor in the wrong one of them.
	for top := max(0, y-cursorRowsAbove); top <= y; top++ {
		for _, indent := range []int{2, 0, 1, 3, cursorMaxIndent} {
			if at, ok := alignRows(rows, top, indent, x, y, want); ok {
				return at, true
			}
		}
	}
	return 0, false
}

// LineCol is the line and the column, both from 1 and in characters, of the
// place off characters into text.
func LineCol(text string, off int) (int, int) {
	line, col := 1, 1
	for i, r := range []rune(strings.ReplaceAll(text, "\r\n", "\n")) {
		if i >= off {
			break
		}
		col++
		if r == '\n' {
			line, col = line+1, 1
		}
	}
	return line, col
}

// Offset is how many characters into text the place at line and col is,
// both from 1: LineCol the other way. A line or a column past what text has
// is its last.
func Offset(text string, line, col int) int {
	runes := []rune(strings.ReplaceAll(text, "\r\n", "\n"))
	off := 0
	for l := 1; l < line; l++ {
		next := slices.Index(runes[off:], '\n')
		if next < 0 {
			return len(runes)
		}
		off += next + 1
	}
	end := slices.Index(runes[off:], '\n')
	if end < 0 {
		end = len(runes) - off
	}
	return off + min(max(col-1, 0), end)
}

// EditorCursor reads where an editor left its cursor in file, from the file
// at path that Marmota's micro plugin writes as file closes (MARMOTA_CURSOR_FILE:
// the line, the column, the file's name), and removes it. It isn't ok when the
// editor wrote nothing, or wrote of another file.
func EditorCursor(path, file string) (int, int, bool) {
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return 0, 0, false
	}
	var line, col int
	at, of, _ := strings.Cut(strings.TrimSpace(string(data)), " /") // a name may hold spaces
	if _, err := fmt.Sscan(at, &line, &col); err != nil {
		return 0, 0, false
	}
	// The name alone: /tmp is /private/tmp on a Mac, and each program
	// writes the one it was given.
	return line, col, line >= 1 && col >= 1 && filepath.Base(of) == filepath.Base(file)
}

// alignRows reads text off the rows from top down, each from the column
// indent on, and reports how far into text the cursor at x, y is. It isn't ok
// unless the rows hold all of text, and the cursor's row is one of them.
func alignRows(rows []string, top, indent, x, y int, text []rune) (int, bool) {
	off, cursor := 0, -1
	for r := top; r < len(rows); r++ {
		cells, ok := afterColumns(rows[r], indent)
		if !ok {
			return 0, false
		}
		if r > top {
			off = afterRowEnd(text, off)
		}
		shown := []rune(strings.TrimRight(string(cells), " "))
		if r == top && len(shown) == 0 {
			return 0, false // a prompt's first row holds its first words
		}
		if off+len(shown) > len(text) || string(text[off:off+len(shown)]) != string(shown) {
			return 0, false
		}
		if r == y {
			cursor = off + cursorInRow(cells, shown, x-indent, text[off+len(shown):])
		}
		off += len(shown)
		if off == len(text) {
			return cursor, cursor >= 0
		}
	}
	return 0, false
}

// afterRowEnd is where in text the next row starts, off being where a row's
// last shown character ends: a row ends where a line does, or where the box
// folded one, at a space the fold took.
func afterRowEnd(text []rune, off int) int {
	for off < len(text) && text[off] == ' ' {
		off++
	}
	if off < len(text) && text[off] == '\n' {
		off++
	}
	return off
}

// afterColumns is a row from the display column n on. It isn't ok when a
// wide character lies across that column.
func afterColumns(row string, n int) ([]rune, bool) {
	cells := []rune(row)
	width := 0
	for i, r := range cells {
		if width == n {
			return cells[i:], true
		}
		if width > n {
			return nil, false
		}
		width += ansi.StringWidth(string(r))
	}
	return nil, width <= n
}

// cursorInRow is how many characters of a row the cursor at display column x
// is past: within what the row shows, or in the spaces the text has after it
// (rest is the text from the row's end on).
func cursorInRow(cells, shown []rune, x int, rest []rune) int {
	n, width := 0, 0
	for n < len(cells) && width < x {
		width += ansi.StringWidth(string(cells[n]))
		n++
	}
	n += max(0, x-width) // past what the screen holds of the row
	if n <= len(shown) {
		return n
	}
	spaces := 0
	for spaces < len(rest) && rest[spaces] == ' ' {
		spaces++
	}
	return len(shown) + min(n-len(shown), spaces)
}
