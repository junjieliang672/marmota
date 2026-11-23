package jump

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
)

// slot is one row of the list: a section's title, an item, or a note
// such as "none".
type slot struct {
	header bool
	kind   Kind
	item   int // in visible(), for an item
	note   string
	more   int // items of the section that didn't fit
	total  int
}

// grid is the list's rows, and the items in them in the order the arrows
// go.
type grid struct {
	rows  []slot
	items []Item
}

func (m *Model) innerWidth() int { return max(20, m.width-common.BorderPadding) } //nolint:mnd // narrowest

// chromeRows is the panel's rows that aren't the list: its frame, the
// input, the sections' names on a row (more when they wrap), and the rule
// and path line at the foot, then the keys unless the status line shows
// them.
const chromeRows = 6

func (m *Model) bodyRows() int {
	rows := m.height - chromeRows - (len(m.scopeNames(m.innerWidth())) - 1)
	if !m.KeysElsewhere {
		rows--
	}
	return max(3, rows) //nolint:mnd // a row or two of items
}

func (m *Model) visible() []Item { return m.layout().items }

// layout places the sections shown one under the other, in Kinds' order,
// each as many of its items as fit, the rows shared out evenly among them.
func (m *Model) layout() grid {
	var g grid
	rows := m.bodyRows()
	typed := strings.TrimSpace(m.input.Value()) != ""
	if m.scope >= 0 {
		m.layoutOne(&g, rows)
		return g
	}
	var secs []Section
	for _, s := range m.shown {
		if len(s.Items) > 0 || !typed {
			secs = append(secs, s)
		}
	}
	// Each section's title, a blank line between sections, "none" for an
	// empty one: what is left goes to the items. A short panel drops the
	// blank lines first, then the empty sections, so each section with
	// items shows one at least.
	gap := 1
	free := freeRows(secs, rows, gap)
	if free < nonEmpty(secs) {
		gap = 0
		free = freeRows(secs, rows, gap)
	}
	if free < nonEmpty(secs) {
		secs = slices.DeleteFunc(secs, func(s Section) bool { return len(s.Items) == 0 })
		free = freeRows(secs, rows, gap)
	}
	counts := make([]int, len(secs))
	for i, s := range secs {
		counts[i] = len(s.Items)
	}
	shares := share(counts, free)
	for i, s := range secs {
		if i > 0 && gap > 0 {
			g.rows = append(g.rows, slot{note: " "})
		}
		g.rows = append(g.rows, slot{header: true, kind: s.Kind, more: len(s.Items) - shares[i], total: len(s.Items)})
		if len(s.Items) == 0 {
			g.rows = append(g.rows, slot{note: "none"})
		}
		for _, it := range s.Items[:shares[i]] {
			g.add(it)
		}
	}
	return g
}

// freeRows is what rows leave for items once secs have their titles, gap
// blank lines between them, and "none" when empty.
func freeRows(secs []Section, rows, gap int) int {
	free := rows - len(secs) - gap*max(0, len(secs)-1)
	for _, s := range secs {
		if len(s.Items) == 0 {
			free--
		}
	}
	return free
}

func nonEmpty(secs []Section) int {
	n := 0
	for _, s := range secs {
		if len(s.Items) > 0 {
			n++
		}
	}
	return n
}

// layoutOne fills the list with the one section shown.
func (m *Model) layoutOne(g *grid, rows int) {
	var s Section
	for _, sec := range m.shown {
		s = sec
	}
	s.Kind = Kinds[m.scope]
	fit := min(len(s.Items), rows-1)
	g.rows = append(g.rows, slot{header: true, kind: s.Kind, more: len(s.Items) - fit, total: len(s.Items)})
	if len(s.Items) == 0 {
		g.rows = append(g.rows, slot{note: "none"})
	}
	for _, it := range s.Items[:fit] {
		g.add(it)
	}
}

func (g *grid) add(it Item) {
	g.rows = append(g.rows, slot{item: len(g.items)})
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

// Render draws the list in a file panel's frame, focused or not, as the
// panel it takes the place of would be.
func (m *Model) Render(focused bool) string {
	width := m.innerWidth()
	st := common.PopupStyles(false)
	g := m.layout()
	lines := append([]string{m.topLine(width, st)}, m.scopeLines(width, st)...)
	head := len(lines)

	target, hasTarget := m.Picked()
	m.numWidth = len(strconv.Itoa(max(1, len(g.items))))
	for r := range m.bodyRows() {
		lines = append(lines, m.slotLine(g, r, width, target, hasTarget))
	}
	if len(g.items) == 0 && strings.TrimSpace(m.input.Value()) != "" {
		lines[head] = padTo(st.Muted.Render("  Nothing matches"), width)
	}
	lines = append(lines, st.Rule.Render(strings.Repeat("─", width)), m.pathLine(width, st, target, hasTarget))
	if !m.KeysElsewhere {
		lines = append(lines, m.keysLine(width, st))
	}

	r := ui.FilePanelRenderer(m.height, m.width, focused)
	r.SetBorderTitle("go to")
	if m.picking && len(g.items) > 0 {
		r.SetBorderTitleSuffix(strconv.Itoa(m.cursor+1) + "/" + strconv.Itoa(len(g.items)))
	}
	r.AddLines(lines...)
	return r.Render()
}

// topLine is what you type.
func (m *Model) topLine(width int, st common.PopupStyleSet) string {
	m.input.Width = max(1, width-4) //nolint:mnd // "› ", the cursor, a gap
	if m.picking {
		return ansi.Truncate(st.Muted.Render("› "+m.input.Value()), width, "…")
	}
	return st.Key.Render("› ") + m.input.View()
}

// scopeNames are the names of the sections tab goes through, row by row,
// width wide, with the index of the one shown: "All", then Kinds'. They
// sit on one row when they fit there, " · " between them; else on as few
// rows as hold them, the same number on each, so a narrow panel wraps them
// and cuts none. A list of one section names it alone.
func (m *Model) scopeNames(width int) [][]string {
	names := []string{"All"}
	for _, k := range Kinds {
		names = append(names, k.Title())
	}
	if m.only {
		names = []string{Kinds[m.scope].Title()}
	}
	fits := func(row []string) bool {
		return ansi.StringWidth(strings.Join(row, scopeSep)) <= width
	}
	for rows := 1; rows < len(names); rows++ {
		per := (len(names) + rows - 1) / rows
		var out [][]string
		ok := true
		for at := 0; at < len(names); at += per {
			row := names[at:min(at+per, len(names))]
			ok = ok && fits(row)
			out = append(out, row)
		}
		if ok {
			return out
		}
	}
	out := make([][]string, len(names))
	for i, n := range names {
		out[i] = []string{n}
	}
	return out
}

const scopeSep = " · "

// scopeLines draws scopeNames under what you type, the section shown lit:
// "All · Sessions · Pinned · Resume · Folders · Files".
func (m *Model) scopeLines(width int, st common.PopupStyleSet) []string {
	lit := m.scope + 1
	if m.only {
		lit = 0
	}
	var lines []string
	n := 0
	for _, row := range m.scopeNames(width) {
		parts := make([]string, len(row))
		for i, name := range row {
			if n == lit {
				parts[i] = st.Key.Render(name)
			} else {
				parts[i] = st.Faint.Render(name)
			}
			n++
		}
		lines = append(lines, padTo(ansi.Truncate(strings.Join(parts, st.Faint.Render(scopeSep)), width, "…"), width))
	}
	return lines
}

func (m *Model) slotLine(g grid, row, width int, target Item, hasTarget bool) string {
	st := common.PopupStyles(false)
	if row >= len(g.rows) {
		return strings.Repeat(" ", width)
	}
	s := g.rows[row]
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
	// The row enter goes to wears the file panel's cursor: the picked one,
	// or while searching the first match.
	onIt := m.picking && s.item == m.cursor ||
		!m.picking && hasTarget && it.Kind == target.Kind && it.Key == target.Key
	return m.itemLine(it, s.item+1, onIt, width)
}

// minWhereWidth is the columns where an item is needs to be cut to: under
// them it isn't shown.
const minWhereWidth = 10

// itemLine is one item: what it is (cc or co for a session, ▸ a folder,
// · a file), its name, where it is, and on the right its age or state.
// The row enter goes to (picked) is drawn as a file panel's cursor row: the
// cursor, and the theme's cursor bar under the row when it has one.
func (m *Model) itemLine(it Item, n int, picked bool, width int) string {
	st := common.PopupStyles(picked)
	lead := st.Text.Render("  ")
	if picked {
		lead = common.FilePanelCursorStyle.Background(common.PopupPickedBG()).Render(icon.Cursor + " ")
	}
	mark := st.Faint.Render("· ")
	switch {
	case it.Tag != "":
		mark = st.Session.Render(it.Tag) + st.Text.Render(" ")
		if it.Stated {
			mark = m.stateStyle(st, it.State).Render(sessionsui.Symbol(it.State)) + st.Text.Render(" ") + mark
		}
	case it.Dir:
		mark = st.Accent.Render("▸ ")
	}
	head := lead + st.Faint.Render(fmtNum(n, m.numWidth)+" ") + mark
	where := it.Where
	if m.ShortPath != nil && where != "" {
		where = m.ShortPath(where)
	}
	room := width - ansi.StringWidth(head) - len(it.Meta) - 2 //nolint:mnd // the gaps
	// The name first, whole when the row holds it: in the sidebar's 40
	// columns a row hasn't room for both, and where it is shows at the foot
	// for the row enter goes to.
	title := ansi.Truncate(it.Title, max(4, room), "…") //nolint:mnd // a few letters at least
	whereRoom := room - ansi.StringWidth(title) - 1
	if whereRoom < minWhereWidth && ansi.StringWidth(where) > whereRoom {
		where = "" // its last few letters would say nothing
	} else {
		where = cutFromLeft(where, whereRoom)
	}
	titleStyle := st.Text
	if picked {
		titleStyle = st.Picked
	}
	metaStyle := st.Faint
	if it.Stated {
		metaStyle = m.stateStyle(st, it.State)
	}
	gap := max(1, width-ansi.StringWidth(head)-ansi.StringWidth(title)-ansi.StringWidth(where)-len(it.Meta)-1)
	line := head + titleStyle.Render(title) + st.Text.Render(" ") + st.Muted.Render(where) +
		st.Text.Render(strings.Repeat(" ", gap)) + metaStyle.Render(it.Meta)
	return padTo(ansi.Truncate(line, width, "…"), width)
}

// stateStyle draws a session's state in its color, on the row's background:
// bold when it needs you or replied unread, as in the Sessions box.
func (m *Model) stateStyle(st common.PopupStyleSet, state sessionsui.State) lipgloss.Style {
	s := st.Text.Foreground(sessionsui.StateColor(state))
	if state == sessionsui.NeedsYou || state == sessionsui.Unread {
		s = s.Bold(true)
	}
	return s
}

// pathLine says where enter goes.
func (m *Model) pathLine(width int, st common.PopupStyleSet, target Item, ok bool) string {
	if m.renaming {
		m.rename.Width = max(10, width-12) //nolint:mnd // "rename › " and the cursor
		return st.Key.Render("rename › ") + m.rename.View()
	}
	text := ""
	switch {
	case ok && target.Goes != "":
		text = target.Goes
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

// FootKeys are the keys the list takes now, each key then what it does:
// the picked item's, or the search's.
func (m *Model) FootKeys() []string {
	target, ok := m.Picked()
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
	return keys
}

// keysLine is FootKeys at the list's foot, as many as fit.
func (m *Model) keysLine(width int, st common.PopupStyleSet) string {
	keys := m.FootKeys()
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
