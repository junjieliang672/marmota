package ask

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/ui"
	"github.com/junjieliang672/marmota/src/internal/ui/rendering"
)

// A cell is one of the boxes the preview is cut into while the keyboard is
// away from Marmota and several sessions have something to show: a
// session's conversation, its last lines, in a frame of the session's own
// color, with its number in the Sessions box before its name on the bottom
// edge. A cell scrolls on its own.

// Cell is how a cell is drawn, beside the session it shows.
type Cell struct {
	// Number is the session's number in the Sessions box.
	Number int
	// Color is the session's color: the frame's and the name's.
	Color lipgloss.TerminalColor
	// Asks draws the name on a band of Color: the session needs you.
	Asks bool
	// Scroll moves the conversation back by that many lines.
	Scroll int
	// More is how many sessions had no room for a cell; the last cell
	// says so.
	More int
}

// RenderCell draws session s in a cell, width wide and height tall.
func RenderCell(s Session, c Cell, width, height int) string {
	r := ui.SessionCellRenderer(height, width, c.Color)
	if s.Glyph != "" {
		r.SetBorderTitleGlyph(s.Glyph, s.GlyphColor)
	}
	r.SetBorderTitle(strconv.Itoa(c.Number) + " " + s.Title)
	r.SetBorderTitleSuffix(s.State)
	r.SetBorderTitleAtFoot()
	if c.Asks {
		r.SetBorderTitleReverse()
	}
	lines := cellLines(s, width)
	room := max(1, height-composerChrome)
	scroll := min(max(0, c.Scroll), max(0, len(lines)-room))
	var info []string
	if scroll > 0 {
		info = append(info, "↑ "+strconv.Itoa(scroll)+" lines")
	}
	if c.More > 0 {
		info = append(info, "+"+strconv.Itoa(c.More)+" more")
	}
	if len(info) > 0 {
		r.SetBorderInfoItems(info...)
	}
	end := len(lines) - scroll
	shown := lines[max(0, end-room):end]
	// its last line on the cell's last row: what is new comes in from below
	for range room - len(shown) {
		r.AddLineWithCustomTruncate("", rendering.TailsTruncateRight)
	}
	for _, l := range shown {
		r.AddLineWithCustomTruncate(l, rendering.TailsTruncateRight)
	}
	return r.Render()
}

// CellLines is how many lines s's conversation has in a cell width wide.
func CellLines(s Session, width int) int {
	return len(cellLines(s, width))
}

// CellScrollLimit is how far back a cell's conversation can be scrolled.
func CellScrollLimit(s Session, width, height int) int {
	return max(0, CellLines(s, width)-max(1, height-composerChrome))
}

// cellLines is what a cell shows of s: its turns, newest last, what it does
// now, and what it asks you.
func cellLines(s Session, width int) []string {
	inner := max(1, width-composerChrome)
	st := newPaneStyles()
	lines := conversationLines(s, inner, st)
	if len(lines) > 0 {
		lines = lines[1:] // without the heading: the cell's edge names the session
	}
	if s.QuestionNote != "" {
		lines = append(lines, st.ask.Render(ansi.Truncate(questionBar+s.QuestionNote, inner, "…")))
	}
	return lines
}
