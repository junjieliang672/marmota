package jump

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
)

// columnGap is what separates the two columns: " │ ".
const columnGap = 3

// narrowWidth is the inner width under which the sections stack in one
// column.
const narrowWidth = 100

// slot is one row of a column: a section's title, an item, or a note
// such as "none".
type slot struct {
	header bool
	kind   Kind
	item   int // in visible(), for an item
	note   string
	more   int // items of the section that didn't fit
	total  int
}

// grid is the two columns, and the items in them in the order the arrows
// go: the left column's top to bottom, then the right's.
type grid struct {
	cols  [2][]slot
	items []Item
	at    []struct{ col, row int }
}

func (m *Model) innerWidth() int  { return max(20, m.width-common.BorderPadding) } //nolint:mnd // narrowest
func (m *Model) twoColumns() bool { return m.innerWidth() >= narrowWidth }

func (m *Model) columnWidth() int {
	if !m.twoColumns() {
		return m.innerWidth()
	}
	return max(10, (m.innerWidth()-columnGap)/2) //nolint:mnd // two columns
}

// chromeRows is the popup's rows that aren't the columns: its frame, the
// input and its rule, and the rule, path line and keys at the foot.
const chromeRows = 7

func (m *Model) bodyRows() int { return max(3, m.height-chromeRows) } //nolint:mnd // a row or two of items

func (m *Model) visible() []Item { return m.layout().items }

// layout places the sections shown in the two columns, each section as
// many of its items as fit, the rows shared out evenly among them.
func (m *Model) layout() grid {
	var g grid
	rows := m.bodyRows()
	typed := strings.TrimSpace(m.input.Value()) != ""
	if m.scope >= 0 {
		m.layoutOne(&g, rows)
		return g
	}
	for col := range 2 {
		var secs []Section
		for _, s := range m.shown {
			inCol := !m.twoColumns() && col == 0 || m.twoColumns() && s.Kind.leftColumn() == (col == 0)
			if inCol && (len(s.Items) > 0 || !typed) {
				secs = append(secs, s)
			}
		}
		if len(secs) == 0 {
			continue
		}
		// Each section's title, a blank line between sections, "none" for
		// an empty one: what is left goes to the items.
		free := rows - 2*len(secs) + 1
		counts := make([]int, len(secs))
		for i, s := range secs {
			counts[i] = len(s.Items)
			if counts[i] == 0 {
				free--
			}
		}
		shares := share(counts, free)
		for i, s := range secs {
			if i > 0 {
				g.cols[col] = append(g.cols[col], slot{note: " "})
			}
			g.cols[col] = append(g.cols[col], slot{header: true, kind: s.Kind, more: len(s.Items) - shares[i],
				total: len(s.Items)})
			if len(s.Items) == 0 {
				g.cols[col] = append(g.cols[col], slot{note: "none"})
			}
			for _, it := range s.Items[:shares[i]] {
				g.add(col, it)
			}
		}
	}
	return g
}

// layoutOne fills both columns with the one section shown: its title over
// the left column, its items down the left, then down the right.
func (m *Model) layoutOne(g *grid, rows int) {
	var s Section
	for _, sec := range m.shown {
		s = sec
	}
	s.Kind = Kinds[m.scope]
	per := rows - 1
	cols := 1
	if m.twoColumns() {
		cols = 2
		g.cols[1] = append(g.cols[1], slot{note: " "})
	}
	fit := min(len(s.Items), cols*per)
	g.cols[0] = append(g.cols[0], slot{header: true, kind: s.Kind, more: len(s.Items) - fit, total: len(s.Items)})
	if len(s.Items) == 0 {
		g.cols[0] = append(g.cols[0], slot{note: "none"})
	}
	for i, it := range s.Items[:fit] {
		g.add(i/per, it)
	}
}

func (g *grid) add(col int, it Item) {
	g.at = append(g.at, struct{ col, row int }{col, len(g.cols[col])})
	g.cols[col] = append(g.cols[col], slot{item: len(g.items)})
	g.items = append(g.items, it)
}

// share gives out rows to sections of counts items, a row each in turn,
// so a long section doesn't crowd out the others.
func share(counts []int, rows int) []int {
	out := make([]int, len(counts))
	for rows > 0 {
		gave := false
		for i := range counts {
			if rows > 0 && out[i] < counts[i] {
				out[i]++
				rows--
				gave = true
			}
		}
		if !gave {
			break
		}
	}
	return out
}

// otherColumn is the item across from item i, on the nearest row; i when
// the other column has none.
func (m *Model) otherColumn(i int) int {
	g := m.layout()
	if i >= len(g.at) {
		return i
	}
	from := g.at[i]
	best, bestDist := i, -1
	for j, at := range g.at {
		if at.col == from.col {
			continue
		}
		dist := at.row - from.row
		if dist < 0 {
			dist = -dist
		}
		if bestDist < 0 || dist < bestDist {
			best, bestDist = j, dist
		}
	}
	return best
}

// Render draws the popup.
func (m *Model) Render() string {
	width := m.innerWidth()
	st := common.PopupStyles(false)
	g := m.layout()
	lines := []string{m.topLine(width, st), st.Rule.Render(strings.Repeat("─", width))}

	target, hasTarget := m.Picked()
	colW := m.columnWidth()
	m.numWidth = len(strconv.Itoa(max(1, len(g.items))))
	for r := range m.bodyRows() {
		line := m.slotLine(g, 0, r, colW, target, hasTarget)
		if m.twoColumns() {
			line += st.Faint.Render(" │ ") + m.slotLine(g, 1, r, colW, target, hasTarget)
		}
		lines = append(lines, line)
	}
	if len(g.items) == 0 && strings.TrimSpace(m.input.Value()) != "" {
		lines[2] = padTo(st.Muted.Render("  Nothing matches"), width)
	}
	lines = append(lines, st.Rule.Render(strings.Repeat("─", width)), m.pathLine(width, st, target, hasTarget),
		m.keysLine(width, st, target, hasTarget))

	key := ""
	if len(common.Hotkeys.GoTo) > 0 {
		key = common.Hotkeys.GoTo[0]
	}
	r := ui.PopupRenderer(len(lines)+common.BorderPadding, m.width, key, "go to")
	r.AddLines(lines...)
	return r.Render()
}

// topLine is what you type, and the sections tab goes through on the
// right.
func (m *Model) topLine(width int, st common.PopupStyleSet) string {
	var chips []string
	if m.only {
		chips = []string{st.Key.Render(Kinds[m.scope].Title())}
	} else {
		for i := -1; i < len(Kinds); i++ {
			name := "All"
			if i >= 0 {
				name = Kinds[i].Title()
			}
			if i == m.scope {
				chips = append(chips, st.Key.Render(name))
			} else {
				chips = append(chips, st.Faint.Render(name))
			}
		}
	}
	right := strings.Join(chips, st.Faint.Render(" · "))
	m.input.Width = max(1, width-ansi.StringWidth(right)-5) //nolint:mnd // "› ", the cursor, a gap
	input := st.Key.Render("› ") + m.input.View()
	if m.picking {
		input = st.Muted.Render("› " + m.input.Value())
	}
	gap := width - ansi.StringWidth(input) - ansi.StringWidth(right)
	if gap < 1 {
		return ansi.Truncate(input, width, "…")
	}
	return input + strings.Repeat(" ", gap) + right
}

func (m *Model) slotLine(g grid, col, row, width int, target Item, hasTarget bool) string {
	st := common.PopupStyles(false)
	if row >= len(g.cols[col]) {
		return strings.Repeat(" ", width)
	}
	s := g.cols[col][row]
	switch {
	case s.header:
		title := st.Section.Render(s.kind.Title())
		count := ""
		if s.more > 0 {
			count = strconv.Itoa(s.total-s.more) + " of " + strconv.Itoa(s.total)
		} else if s.total > 0 {
			count = strconv.Itoa(s.total)
		}
		gap := max(1, width-ansi.StringWidth(title)-len(count))
		return title + strings.Repeat(" ", gap) + st.Faint.Render(count)
	case s.note != "":
		return padTo(st.Faint.Render("  "+strings.TrimSpace(s.note)), width)
	}
	it := g.items[s.item]
	picked := m.picking && s.item == m.cursor
	first := !m.picking && hasTarget && it.Kind == target.Kind && it.Key == target.Key
	return m.itemLine(it, s.item+1, picked, first, width)
}

// itemLine is one item: what it is (cc or co for a session, ▸ a folder,
// · a file), its name, where it is, and on the right its age or state.
func (m *Model) itemLine(it Item, n int, picked, first bool, width int) string {
	st := common.PopupStyles(picked)
	lead := "  "
	switch {
	case first:
		lead = "↵ " // where enter goes
	case picked && !common.PopupHasBar():
		lead = "› "
	}
	mark := st.Faint.Render("· ")
	switch {
	case it.Tag != "":
		mark = st.Session.Render(it.Tag) + st.Text.Render(" ")
	case it.Dir:
		mark = st.Accent.Render("▸ ")
	}
	head := st.Key.Render(lead) + st.Faint.Render(fmtNum(n, m.numWidth)+" ") + mark
	where := it.Where
	if m.ShortPath != nil && where != "" {
		where = m.ShortPath(where)
	}
	room := width - ansi.StringWidth(head) - len(it.Meta) - 2 //nolint:mnd // the gaps
	title := ansi.Truncate(it.Title, max(4, room*3/5), "…")   //nolint:mnd // the name gets most of it
	whereRoom := room - ansi.StringWidth(title) - 1
	if whereRoom < 4 { //nolint:mnd // too narrow to say where
		where = ""
	} else {
		where = cutFromLeft(where, whereRoom)
	}
	titleStyle := st.Text
	if picked {
		titleStyle = st.Picked
	}
	gap := max(1, width-ansi.StringWidth(head)-ansi.StringWidth(title)-ansi.StringWidth(where)-len(it.Meta)-1)
	line := head + titleStyle.Render(title) + st.Text.Render(" ") + st.Muted.Render(where) +
		st.Text.Render(strings.Repeat(" ", gap)) + st.Faint.Render(it.Meta)
	return padTo(ansi.Truncate(line, width, "…"), width)
}

// pathLine says where enter goes.
func (m *Model) pathLine(width int, st common.PopupStyleSet, target Item, ok bool) string {
	if m.renaming {
		m.rename.Width = max(10, width-12) //nolint:mnd // "rename › " and the cursor
		return st.Key.Render("rename › ") + m.rename.View()
	}
	text := ""
	switch {
	case ok && (target.Kind == Session || target.Kind == Ended):
		text = target.Title
		if target.Where != "" {
			text = m.short(target.Where) + " · " + target.Title
		}
	case ok:
		text = m.short(target.Key)
	case strings.TrimSpace(m.input.Value()) == "" && !m.only:
		text = m.HomeText
	}
	return ansi.Truncate(st.Muted.Render(text), width, "…")
}

func (m *Model) short(path string) string {
	if m.ShortPath != nil {
		return m.ShortPath(path)
	}
	return path
}

func (m *Model) keysLine(width int, st common.PopupStyleSet, target Item, ok bool) string {
	var keys []string
	switch {
	case m.renaming:
		keys = []string{"enter", "rename", "esc", "cancel"}
	case m.picking && ok && m.Keys != nil:
		keys = append(m.Keys(target), "esc", "search")
	case m.picking:
		keys = []string{"esc", "search"}
	default:
		keys = []string{"enter", "go", "↓", "pick"}
		if !m.only {
			keys = append(keys, "tab", "section")
		}
		if m.input.Value() != "" {
			keys = append(keys, "esc", "clear")
		} else {
			keys = append(keys, "esc", "close")
		}
	}
	if m.picking && m.count != "" {
		keys = append([]string{m.count + "g", "row " + m.count}, keys...)
	}
	var parts []string
	for i := 0; i+1 < len(keys); i += 2 {
		part := st.Key.Render(keys[i])
		if keys[i+1] != "" {
			part += st.Text.Render(" " + keys[i+1])
		}
		parts = append(parts, part)
	}
	return ansi.Truncate(strings.Join(parts, st.Faint.Render(" · ")), width, "…")
}

func fmtNum(n, width int) string {
	s := strconv.Itoa(n)
	return strings.Repeat(" ", max(0, width-len(s))) + s
}

func padTo(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return ansi.Truncate(s, width, "")
}

// cutFromLeft keeps the end of s that fits in width columns, after "…".
func cutFromLeft(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && ansi.StringWidth("…"+string(runes)) > width {
		runes = runes[1:]
	}
	return "…" + string(runes)
}
