package zoxide

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
)

// Render draws the popup as the s popup is drawn, and as tall: what you
// type and the numbered entries (folder bright, where it is dim, score on
// the right) fill the list area of maxHeight rows, keys included; footer
// (the path line of where enter goes) goes under it.
func (m *Model) Render(footer ...string) string {
	width := m.width - common.BorderPadding
	st := common.PopupStyles(false)

	var lines []string
	if m.zClient == nil {
		lines = append(lines, st.Text.Render("Zoxide not available (check zoxide_support in config)"))
	} else {
		lines = append(lines, m.inputLine(width, st), st.Rule.Render(strings.Repeat("─", width)))
		if len(m.results) == 0 {
			lines = append(lines, st.Muted.Render("  No zoxide results found"))
		} else {
			lines = append(lines, m.resultLines(width, st)...)
		}
		for len(lines) < m.maxHeight-1 {
			lines = append(lines, "")
		}
		lines = append(lines, footer...)
		lines = append(lines, keysLine(st, width, "enter", "go", "↑↓", "pick", "ctrl+n", "new panel", "esc", ""))
	}

	key := ""
	if len(common.Hotkeys.OpenZoxide) > 0 {
		key = common.Hotkeys.OpenZoxide[0]
	}
	r := ui.PopupRenderer(len(lines)+common.BorderPadding, m.width, key, m.headline)
	r.AddLines(lines...)
	return r.Render()
}

// keysLine is "enter go · ↑↓ pick", keys in the key color, cut to width.
func keysLine(st common.PopupStyleSet, width int, keyThenWhat ...string) string {
	var parts []string
	for i := 0; i+1 < len(keyThenWhat); i += 2 {
		part := st.Key.Render(keyThenWhat[i])
		if what := keyThenWhat[i+1]; what != "" {
			part += st.Text.Render(" " + what)
		}
		parts = append(parts, part)
	}
	return ansi.Truncate(strings.Join(parts, st.Muted.Render(" · ")), width, "…")
}

// inputLine is what you type, and where enter goes on the right.
func (m *Model) inputLine(width int, st common.PopupStyleSet) string {
	hint := "enter: the picked one"
	if target, ok := m.Target(); ok && target == homeDir() && strings.TrimSpace(m.textInput.Value()) == "" {
		hint = "enter: ~"
	}
	m.textInput.Width = max(1, width-ansi.StringWidth(hint)-4) //nolint:mnd // "› ", the cursor, a gap
	input := st.Key.Render("› ") + m.textInput.View()
	gap := width - ansi.StringWidth(input) - ansi.StringWidth(hint)
	if gap < 1 {
		return input
	}
	return input + strings.Repeat(" ", gap) + st.Muted.Render(hint)
}

// resultLines are the visible entries, with a line for any above or below.
func (m *Model) resultLines(width int, st common.PopupStyleSet) []string {
	end := min(m.renderIndex+m.visibleResults(), len(m.results))
	numWidth := len(strconv.Itoa(end))
	var lines []string
	if m.renderIndex > 0 {
		lines = append(lines, st.Muted.Render("  ↑ More results above"))
	}
	// With nothing typed or picked, enter goes home, not to the first entry.
	shown := m.picked || strings.TrimSpace(m.textInput.Value()) != ""
	for i := m.renderIndex; i < end; i++ {
		lines = append(lines, m.resultLine(i, i == m.cursor && shown, numWidth, width))
	}
	if end < len(m.results) {
		lines = append(lines, st.Muted.Render("  ↓ More results below"))
	}
	return lines
}

// resultLine is one entry: its number, its folder's name, the folder it is
// in (cut from the left when there's no room: the path line has it whole),
// and its score; on the picked row's bar when picked.
func (m *Model) resultLine(i int, picked bool, numWidth, width int) string {
	st := common.PopupStyles(picked)
	res := m.results[i]
	lead := "  "
	if picked && !common.PopupHasBar() {
		lead = icon.Cursor + " " // a theme without a bar: the cursor marks it
	}
	head := lead + fmt.Sprintf("%*d ", numWidth, i+1)
	name := filepath.Base(res.Path)
	score := formatScore(res.Score)
	parent := m.shortPath(filepath.Dir(res.Path))
	room := width - ansi.StringWidth(head) - ansi.StringWidth(name) - len(score) - 3 //nolint:mnd // three gaps
	if room < 1 {
		parent = ""
	} else {
		parent = cutFromLeft(parent, room)
	}
	gap := max(1, width-ansi.StringWidth(head)-ansi.StringWidth(name)-ansi.StringWidth(parent)-len(score)-2)
	nameStyle := st.Text
	if picked {
		nameStyle = st.Picked
	}
	line := st.Text.Render(lead) + st.Faint.Render(head[len(lead):]) + nameStyle.Render(name) +
		st.Text.Render(strings.Repeat(" ", gap)) + st.Muted.Render(parent) + st.Text.Render("  ") + st.Faint.Render(score)
	return ansi.Truncate(line, width, "…")
}

// cutFromLeft keeps the end of s that fits in width columns, after "…".
// It counts columns, so a name in Chinese, two columns a character, fits.
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

// shortPath is path as the rest of superfile writes it (~ for home), when
// it was told how.
func (m *Model) shortPath(path string) string {
	if m.ShortPath != nil {
		return m.ShortPath(path)
	}
	return path
}

// formatScore is a score with one decimal, none when it is whole: 508,
// 23.5.
func formatScore(score float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(score, 'f', 1, 64), ".0")
}
