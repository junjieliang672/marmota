package ask

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Tables, drawn as Claude Code draws them: in a box, the header bold, each
// cell wrapped to its column. Each column gets at least its longest word,
// and the room left goes to the columns that want it most. When that still
// makes a row too tall, each row becomes a short list instead.

type align int

const (
	alignLeft align = iota
	alignCenter
	alignRight
)

const (
	// minColumnCells is the narrowest a column gets before the table is
	// drawn as a list instead.
	minColumnCells = 3
	// maxWordCells caps the room a column's longest word asks for.
	maxWordCells = 16
	// maxRowLines is the tallest a row gets before the table is drawn as a
	// list instead.
	maxRowLines = 4
	// tableCellChrome is the border and padding around each cell: "│ " and
	// " ".
	tableCellChrome = 3
)

func isTableRow(line string) bool {
	return strings.HasPrefix(line, "|") && len(line) > 1
}

// splitRow cuts a table row into its cells: "| a | b |" is a and b. An
// escaped \| stays in its cell.
func splitRow(line string) []string {
	line = strings.TrimPrefix(strings.TrimSpace(line), "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// columnAligns reads the alignment of each column off the divider row:
// :--- left, :---: center, ---: right.
func columnAligns(divider string) []align {
	var out []align
	for _, c := range splitRow(divider) {
		left, right := strings.HasPrefix(c, ":"), strings.HasSuffix(c, ":")
		switch {
		case left && right:
			out = append(out, alignCenter)
		case right:
			out = append(out, alignRight)
		default:
			out = append(out, alignLeft)
		}
	}
	return out
}

// plainText is what a cell shows, without its Markdown marks.
func plainText(cell string) string {
	var b strings.Builder
	for _, s := range parseInline(cell) {
		b.WriteString(s.text)
	}
	return b.String()
}

func tableLines(rows [][]string, aligns []align, width int, st textStyles) []string {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return nil
	}
	natural, least := make([]int, cols), make([]int, cols)
	for _, r := range rows {
		for c, cell := range r {
			text := plainText(cell)
			natural[c] = max(natural[c], ansi.StringWidth(text))
			for _, word := range strings.Fields(text) {
				least[c] = max(least[c], min(maxWordCells, ansi.StringWidth(word)))
			}
		}
	}
	room := width - cols*tableCellChrome - 1
	if room < cols*minColumnCells {
		return tableAsList(rows, width, st)
	}
	widths := fitColumns(natural, least, room)

	header := st
	header.base, header.italic = st.bold, st.bold.Italic(true)
	headCells := rowCells(rows[0], widths, header)
	body := make([][][]string, 0, len(rows)-1)
	tall := false
	for _, r := range rows[1:] {
		cells := rowCells(r, widths, st)
		if rowHeight(cells) > maxRowLines {
			return tableAsList(rows, width, st)
		}
		tall = tall || rowHeight(cells) > 1
		body = append(body, cells)
	}
	edge := func(left, mid, right string) string {
		parts := make([]string, cols)
		for c, w := range widths {
			parts[c] = strings.Repeat("─", w+2) //nolint:mnd // the cell's padding
		}
		return st.border.Render(left + strings.Join(parts, mid) + right)
	}
	out := []string{edge("┌", "┬", "┐")}
	out = append(out, rowLines(headCells, widths, aligns, st)...)
	out = append(out, edge("├", "┼", "┤"))
	for i, cells := range body {
		if i > 0 && tall {
			out = append(out, edge("├", "┼", "┤"))
		}
		out = append(out, rowLines(cells, widths, aligns, st)...)
	}
	return append(out, edge("└", "┴", "┘"))
}

// fitColumns sizes the columns to fit in room. A column narrower than an
// equal share keeps its natural width, and the others share what is left,
// each at least its longest word (least). When even that doesn't fit, the
// widest columns shrink, a cell at a time.
func fitColumns(natural, least []int, room int) []int {
	widths, open, remaining := keepNarrowColumns(natural, room)
	if left := countOpen(open); left > 0 {
		share, extra := remaining/left, remaining%left
		for i := range widths {
			if !open[i] {
				continue
			}
			widths[i] = max(share, max(minColumnCells, least[i]))
			if extra > 0 && widths[i] == share {
				widths[i]++
				extra--
			}
		}
	}
	return shrinkToFit(widths, room)
}

// keepNarrowColumns gives each column narrower than an equal share of what
// is left its natural width, until none is; the others stay open, with
// remaining cells for them.
func keepNarrowColumns(natural []int, room int) ([]int, []bool, int) {
	widths := make([]int, len(natural))
	open := make([]bool, len(natural))
	for i := range open {
		open[i] = true
	}
	remaining := room
	for fixed := true; fixed; {
		fixed = false
		left := countOpen(open)
		if left == 0 {
			break
		}
		share := remaining / left
		for i, n := range natural {
			if open[i] && max(1, n) <= share {
				widths[i], open[i] = max(1, n), false
				remaining -= widths[i]
				fixed = true
			}
		}
	}
	return widths, open, remaining
}

func countOpen(open []bool) int {
	n := 0
	for _, o := range open {
		if o {
			n++
		}
	}
	return n
}

// shrinkToFit takes cells off the widest columns until they fit in room.
func shrinkToFit(widths []int, room int) []int {
	total := 0
	for _, w := range widths {
		total += w
	}
	for total > room {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= minColumnCells {
			break
		}
		widths[widest]--
		total--
	}
	return widths
}

// rowCells wraps each cell of a row to its column.
func rowCells(row []string, widths []int, st textStyles) [][]string {
	cells := make([][]string, len(widths))
	for c, w := range widths {
		text := ""
		if c < len(row) {
			text = row[c]
		}
		cells[c] = wrapSpans(parseInline(text), w, "", "", st)
	}
	return cells
}

func rowHeight(cells [][]string) int {
	h := 1
	for _, c := range cells {
		h = max(h, len(c))
	}
	return h
}

// rowLines draws a row's cells side by side, as many lines as its tallest
// cell.
func rowLines(cells [][]string, widths []int, aligns []align, st textStyles) []string {
	bar := st.border.Render("│")
	lines := make([]string, rowHeight(cells))
	for k := range lines {
		var b strings.Builder
		for c, w := range widths {
			text := ""
			if k < len(cells[c]) {
				text = cells[c][k]
			}
			a := alignLeft
			if c < len(aligns) {
				a = aligns[c]
			}
			b.WriteString(bar + st.base.Render(" ") + alignCell(text, w, a, st) + st.base.Render(" "))
		}
		lines[k] = b.String() + bar
	}
	return lines
}

func alignCell(text string, width int, a align, st textStyles) string {
	gap := max(0, width-ansi.StringWidth(text))
	switch a {
	case alignRight:
		return st.base.Render(strings.Repeat(" ", gap)) + text
	case alignCenter:
		left := gap / 2 //nolint:mnd // halves
		return st.base.Render(strings.Repeat(" ", left)) + text + st.base.Render(strings.Repeat(" ", gap-left))
	case alignLeft:
	}
	return text + st.base.Render(strings.Repeat(" ", gap))
}

// tableAsList draws a table too wide for the pane as a list: each row's
// first cell, then the others under their column's name.
func tableAsList(rows [][]string, width int, st textStyles) []string {
	head := rows[0]
	var out []string
	for i, r := range rows[1:] {
		if i > 0 {
			out = append(out, "")
		}
		bold := st
		bold.base = st.bold
		first := ""
		if len(r) > 0 {
			first = r[0]
		}
		out = append(out, wrapSpans(parseInline(first), width, "• ", "  ", bold)...)
		for c := 1; c < len(r); c++ {
			name := ""
			if c < len(head) {
				name = plainText(head[c]) + ": "
			}
			out = append(out, wrapSpans(append([]span{{text: name, kind: boldSpan}}, parseInline(r[c])...),
				width, "  ", "  ", st)...)
		}
	}
	return out
}
