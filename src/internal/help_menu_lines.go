package internal

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
)

// The help menu is drawn as lines: an entry's description wraps under
// itself rather than being cut, so an entry can take several lines, and
// helpMenu.renderIndex is the first line shown.

// helpMenuMinDescWidth keeps descriptions readable when the keys are wide.
const helpMenuMinDescWidth = 20

// helpMenuViewLines is how many lines of entries fit: the height less the
// border, the search bar and the line under it.
func (m *model) helpMenuViewLines() int {
	return max(1, m.helpMenu.height-common.InnerPadding)
}

func helpMenuHotkeyText(keys []string) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "" { // an unset second key
			parts = append(parts, formatHotkeyForDisplay(k))
		}
	}
	return strings.Join(parts, " | ")
}

// helpMenuLines is every line of the filtered entries, with where each
// entry starts and how many lines it takes.
func (m *model) helpMenuLines() ([]string, []int, []int) {
	data := m.helpMenu.filteredData
	keyWidth := 0
	for _, d := range data {
		if d.subTitle == "" {
			keyWidth = max(keyWidth, ansi.StringWidth(helpMenuHotkeyText(d.hotkey))+1)
		}
	}
	const cursorWidth = 2
	inner := m.helpMenu.width - common.BorderPadding
	descWidth := max(helpMenuMinDescWidth, inner-cursorWidth-keyWidth)
	indent := strings.Repeat(" ", cursorWidth+keyWidth)

	var lines []string
	starts := make([]int, len(data))
	counts := make([]int, len(data))
	for i, d := range data {
		starts[i] = len(lines)
		if d.subTitle != "" {
			lines = append(lines, common.HelpMenuTitleStyle.Render(" "+d.subTitle))
			counts[i] = 1
			continue
		}
		cursor := "  "
		if m.helpMenu.cursor == i {
			cursor = common.FilePanelCursorStyle.Render(icon.Cursor + " ")
		}
		keys := helpMenuHotkeyText(d.hotkey)
		keys = strings.Repeat(" ", max(0, keyWidth-1-ansi.StringWidth(keys))) + keys + " "
		wrapped := strings.Split(ansi.Wrap(d.description, descWidth, ""), "\n")
		lines = append(lines, cursor+common.HelpMenuHotkeyStyle.Render(keys)+common.ModalStyle.Render(wrapped[0]))
		for _, more := range wrapped[1:] {
			lines = append(lines, common.ModalStyle.Render(indent+more))
		}
		counts[i] = len(wrapped)
	}
	return lines, starts, counts
}

// helpMenuClampTop keeps the first line shown within the lines there are.
func (m *model) helpMenuClampTop(total int) {
	m.helpMenu.renderIndex = min(m.helpMenu.renderIndex, max(0, total-m.helpMenuViewLines()))
	m.helpMenu.renderIndex = max(0, m.helpMenu.renderIndex)
}

// helpMenuScrollToCursor scrolls just enough to show all of the selected
// entry, and the section title above it when it is a section's first.
func (m *model) helpMenuScrollToCursor() {
	lines, starts, counts := m.helpMenuLines()
	c := m.helpMenu.cursor
	if c < 0 || c >= len(starts) {
		m.helpMenuClampTop(len(lines))
		return
	}
	start, end := starts[c], starts[c]+counts[c]
	if c > 0 && m.helpMenu.filteredData[c-1].subTitle != "" {
		start = starts[c-1]
	}
	view := m.helpMenuViewLines()
	if start < m.helpMenu.renderIndex {
		m.helpMenu.renderIndex = start
	}
	if end > m.helpMenu.renderIndex+view {
		m.helpMenu.renderIndex = end - view
	}
	m.helpMenuClampTop(len(lines))
}

// helpMenuScrollBy scrolls the help menu n lines, down when positive, as the
// mouse wheel does. The selection moves onto the screen if it scrolled off.
func (m *model) helpMenuScrollBy(n int) {
	lines, starts, counts := m.helpMenuLines()
	m.helpMenu.renderIndex += n
	m.helpMenuClampTop(len(lines))
	top, view := m.helpMenu.renderIndex, m.helpMenuViewLines()
	visible := func(i int) bool { return starts[i] >= top && starts[i]+counts[i] <= top+view }
	c := m.helpMenu.cursor
	if c >= 0 && c < len(starts) && visible(c) {
		return
	}
	pick := -1
	for i, d := range m.helpMenu.filteredData {
		if d.subTitle != "" || !visible(i) {
			continue
		}
		pick = i
		if n > 0 {
			break // scrolling down: the first entry in view
		}
	}
	if pick >= 0 {
		m.helpMenu.cursor = pick
	}
}
