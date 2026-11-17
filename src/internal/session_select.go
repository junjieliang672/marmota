package internal

import (
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Selecting text in the session the preview shows, with the mouse, as in a
// terminal: press, drag, and letting go copies what is between, as the
// preview draws it, without its border. The selection stays lit until a key,
// a click or a scroll. It reaches as far as the preview shows: scroll first
// to copy older lines.

// previewSelection is the text selected in the session preview, by its two
// ends: cells of the preview's box, from its top-left corner.
type previewSelection struct {
	on, dragging bool
	from, to     previewCell
}

type previewCell struct{ row, col int }

func (c previewCell) before(o previewCell) bool {
	return c.row < o.row || c.row == o.row && c.col < o.col
}

// ordered is the selection's ends in reading order.
func (s previewSelection) ordered() (previewCell, previewCell) {
	if s.to.before(s.from) {
		return s.to, s.from
	}
	return s.from, s.to
}

// selectInSessionPreview takes the left button over the session preview. It
// reports whether it took the event.
func (m *model) selectInSessionPreview(msg tea.MouseMsg) (tea.Cmd, bool) {
	sel := &m.sessions.selection
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		*sel = previewSelection{} // a click puts the last selection away
		if m.previewUnder(msg.X, msg.Y) != sessionPreviewArea {
			return nil, false
		}
		at := m.sessionPreviewCell(msg.X, msg.Y)
		*sel = previewSelection{on: true, dragging: true, from: at, to: at}
		return nil, true
	case msg.Action == tea.MouseActionMotion && sel.dragging:
		sel.to = m.sessionPreviewCell(msg.X, msg.Y)
		return nil, true
	case msg.Action == tea.MouseActionRelease && sel.dragging:
		sel.dragging = false
		sel.to = m.sessionPreviewCell(msg.X, msg.Y)
		if sel.from == sel.to {
			*sel = previewSelection{} // a click, not a selection
			return nil, true
		}
		m.copySessionSelection()
		return nil, true
	}
	return nil, false
}

// sessionPreviewCell is the cell of the preview's box at screen cell x, y,
// kept inside its border.
func (m *model) sessionPreviewCell(x, y int) previewCell {
	pv := &m.fileModel.filePreview
	left := m.fullWidth - pv.GetWidth()
	return previewCell{
		row: min(max(y, 1), pv.GetHeight()-2),
		col: min(max(x-left, 1), pv.GetWidth()-2),
	}
}

// copySessionSelection copies the selected text to the clipboard.
func (m *model) copySessionSelection() {
	pv := &m.fileModel.filePreview
	text := selectedText(m.dockSessionPreview(pv.GetWidth(), pv.GetHeight()), m.sessions.selection)
	if text == "" {
		return
	}
	if err := writeClipboard(text); err != nil {
		m.flashClaude("Couldn't copy: " + err.Error())
		return
	}
	lines := strings.Count(text, "\n") + 1
	if lines == 1 {
		m.flashClaude("Copied " + strconv.Itoa(len([]rune(text))) + " characters")
		return
	}
	m.flashClaude("Copied " + strconv.Itoa(lines) + " lines")
}

// selectedText is the text sel spans in a box drawn as rendered, each row
// without the spaces that end it.
func selectedText(rendered string, sel previewSelection) string {
	lines := strings.Split(rendered, "\n")
	from, to := sel.ordered()
	var out []string
	for row := from.row; row <= to.row && row < len(lines); row++ {
		start, end := rowSpan(row, from, to, ansi.StringWidth(lines[row]))
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(lines[row], start, end)), " "))
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// rowSpan is the cells of row a selection from..to covers, end excluded:
// all of a row between, from the start or to the end on the first and the
// last, never the border.
func rowSpan(row int, from, to previewCell, width int) (int, int) {
	start, end := 1, width-1
	if row == from.row {
		start = from.col
	}
	if row == to.row {
		end = to.col + 1
	}
	return start, max(start, end)
}

// sgrSequence is an escape sequence that sets how text is drawn.
var sgrSequence = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// highlightSelection lights the selected cells of a box drawn as rendered,
// in reverse, their colors kept. The cells after them are drawn as they
// were: the settings before them are given again.
func highlightSelection(rendered string, sel previewSelection) string {
	if !sel.on {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	from, to := sel.ordered()
	for row := from.row; row <= to.row && row < len(lines); row++ {
		line := lines[row]
		width := ansi.StringWidth(line)
		start, end := rowSpan(row, from, to, width)
		mid := ansi.Cut(line, start, end)
		if ansi.StringWidth(mid) == 0 {
			continue
		}
		// A setting inside the cells, such as a color's end, would end the
		// reverse too: it is given again after each.
		mid = reverseOn + sgrSequence.ReplaceAllStringFunc(mid, func(s string) string { return s + reverseOn }) +
			reverseOff
		upTo := ansi.Cut(line, 0, end)
		next := ansi.Cut(line, 0, start) + mid + strings.Join(sgrSequence.FindAllString(upTo, -1), "") +
			ansi.Cut(line, end, width)
		if ansi.StringWidth(next) == width { // a wide character cut in two would shift the border
			lines[row] = next
		}
	}
	return strings.Join(lines, "\n")
}

const (
	reverseOn  = "\x1b[7m"
	reverseOff = "\x1b[27m"
)
