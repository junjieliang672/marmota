// Package details draws the Details box at the foot of the screen: the
// cursor's file (FILE), the machine (MAC) and file operations (OPS), each
// section under a coloured label, scrolled as one document.
package details

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
	"github.com/junjieliang672/marmota/src/pkg/sysstat"
)

// File is what FILE says about the item under the cursor.
type File struct {
	Path        string // absolute; a folder's ends in "/"
	IsDir       bool
	Items       int    // entries in a folder, -1 when unknown
	Size        string // "" when unknown, such as a folder's before it is counted
	Modified    time.Time
	Permissions string
	Resolution  string
	Page        string
	Git         string // git's letter when the item has changes, else ""
	Info        string // instead of the facts: "Loading metadata...", an error
}

// OpState is where a file operation is.
type OpState int

const (
	OpRunning OpState = iota
	OpDone
	OpFailed
	OpCancelled
)

// Op is one file operation: a copy, a move, a delete, an extract.
type Op struct {
	Name     string
	State    OpState
	Done     int
	Total    int
	DoneTime time.Time
}

// Input is everything the box shows. A nil File or Mac, or no Ops, leaves
// that section out.
type Input struct {
	File *File
	Ops  []Op
	Mac  *sysstat.Stats
	Now  time.Time
}

// Model is the box: its size and how far it is scrolled.
type Model struct {
	width  int // including borders
	height int
	offset int
	lines  []string // the last document drawn, for scrolling
}

func New() Model {
	return Model{}
}

func (m *Model) SetDimensions(width, height int) {
	m.width, m.height = width, height
	m.clamp()
}

func (m *Model) viewHeight() int {
	return max(0, m.height-2) //nolint:mnd // top and bottom border
}

func (m *Model) innerWidth() int {
	return max(0, m.width-2) //nolint:mnd // left and right border
}

// ScrollBy moves the view n lines, down when positive.
func (m *Model) ScrollBy(n int) {
	m.offset += n
	m.clamp()
}

func (m *Model) ScrollTop() {
	m.offset = 0
}

func (m *Model) ScrollBottom() {
	m.offset = len(m.lines)
	m.clamp()
}

func (m *Model) clamp() {
	m.offset = min(m.offset, len(m.lines)-m.viewHeight())
	m.offset = max(m.offset, 0)
}

// Render draws the box for in. focusKey is the key that focuses it, shown
// on its bottom edge until it has the keyboard.
func (m *Model) Render(in Input, focused bool, focusKey string) string {
	m.lines = m.document(in)
	m.clamp()
	r := ui.DefaultFooterRenderer(m.height, m.width, focused)
	r.SetBorderTitle("Details")
	hint := focusKey + " focus"
	if focused {
		hint = "↑↓ scroll"
	}
	if len(m.lines) > 0 {
		last := min(len(m.lines), m.offset+m.viewHeight())
		hint = fmt.Sprintf("%d/%d · %s", last, len(m.lines), hint)
	}
	r.SetBorderInfoItems(hint)
	end := min(len(m.lines), m.offset+m.viewHeight())
	r.AddLines(m.lines[m.offset:end]...)
	return r.Render()
}

// document is every line of the box, sections separated by a blank line.
func (m *Model) document(in Input) []string {
	w := m.innerWidth()
	var sections [][]string
	if in.File != nil {
		sections = append(sections, section(kindFile, fileLines(*in.File, w-gutter), w))
	}
	if in.Mac != nil {
		if lines := macLines(*in.Mac); len(lines) > 0 {
			sections = append(sections, section(kindMac, lines, w))
		}
	}
	// OPS last: it grows with each operation, and FILE and MAC stay put.
	if len(in.Ops) > 0 {
		sections = append(sections, section(kindOps, opsLines(in.Ops, in.Now), w))
	}
	var out []string
	for i, s := range sections {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, s...)
	}
	return out
}

type kind int

const (
	kindFile kind = iota
	kindOps
	kindMac
)

// The section colours come from the theme: FILE the path colour, OPS the
// cancel colour, MAC the end of the gradient.
func (k kind) color() lipgloss.Color {
	switch k {
	case kindOps:
		return lipgloss.Color(common.Theme.Cancel)
	case kindMac:
		if g := common.Theme.GradientColor; len(g) > 0 {
			return lipgloss.Color(g[len(g)-1])
		}
		return lipgloss.Color(common.Theme.Hint)
	case kindFile:
	}
	return lipgloss.Color(common.Theme.FilePanelTopPath)
}

func (k kind) label() string {
	switch k {
	case kindOps:
		return "OPS"
	case kindMac:
		return "MAC"
	case kindFile:
	}
	return "FILE"
}

// gutter is the width of the label column: " FILE " and a space.
const gutter = 7

// section puts a coloured label chip on the first line and a bar of the
// same colour down the left of the others, and cuts each line to w.
func section(k kind, lines []string, w int) []string {
	bg := lipgloss.Color(common.Theme.FooterBG)
	chip := lipgloss.NewStyle().Background(k.color()).
		Foreground(lipgloss.Color(common.Theme.FullScreenBG)).Bold(true).
		Render(fmt.Sprintf(" %-4s ", k.label())) + lipgloss.NewStyle().Background(bg).Render(" ")
	bar := lipgloss.NewStyle().Foreground(k.color()).Background(bg).Render("  ▎    ")
	var out []string
	for i, l := range lines {
		// A line wider than the box goes on under the bar: nothing is cut.
		for j, part := range strings.Split(ansi.Wrap(l, max(1, w-gutter), ""), "\n") {
			head := bar
			if i == 0 && j == 0 {
				head = chip
			}
			out = append(out, head+part)
		}
	}
	return out
}

func styled(fg lipgloss.Color, bold bool) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(lipgloss.Color(common.Theme.FooterBG)).Bold(bold)
}

// lab is a dim label, val a bright value.
func lab(s string) string { return styled(lipgloss.Color(common.Theme.FooterBorder), false).Render(s) }
func val(s string) string { return styled(lipgloss.Color(common.Theme.FooterFG), true).Render(s) }
func ok(s string) string  { return styled(lipgloss.Color(common.Theme.Correct), false).Render(s) }
func bad(s string) string { return styled(lipgloss.Color(common.Theme.Error), false).Render(s) }

const meterWidth = 10

// meter is a bar meterWidth wide, frac of it filled in fg.
func meter(frac float64, fg lipgloss.Color) string {
	frac = min(max(frac, 0), 1)
	n := int(frac*meterWidth + 0.5) //nolint:mnd // round to the nearest cell
	return styled(fg, false).Render(strings.Repeat("▓", n)) +
		lab(strings.Repeat("░", meterWidth-n))
}

// fileLines is the full path, then its facts, each wrapped to w.
func fileLines(f File, w int) []string {
	if f.Info != "" {
		return []string{lab(f.Info)}
	}
	var facts []string
	if f.IsDir {
		facts = append(facts, "folder")
		if f.Items >= 0 {
			facts = append(facts, fmt.Sprintf("%d %s", f.Items, plural(f.Items, "item", "items")))
		}
	}
	for _, s := range []string{f.Resolution, f.Page, f.Size} {
		if s != "" {
			facts = append(facts, s)
		}
	}
	second := []string{}
	if len(facts) > 0 {
		second = append(second, val(strings.Join(facts, " · ")), lab("·"))
	}
	second = append(second, lab(icon.Calendar), val(f.Modified.Format("Jan 2, 2006 15:04")))
	if f.Permissions != "" && f.Permissions != "drwxr-xr-x" && f.Permissions != "-rw-r--r--" {
		second = append(second, lab("· "+f.Permissions)) // only when they aren't the usual
	}
	if f.Git != "" {
		second = append(second, lab("· git"), val(f.Git))
	}
	var out []string
	for _, l := range wrapPath(f.Path, w) {
		out = append(out, val(l))
	}
	if w > 0 {
		return append(out, strings.Split(ansi.Wrap(joinSpaced(second), w, ""), "\n")...)
	}
	return append(out, joinSpaced(second))
}

// wrapPath splits path into lines at most w wide, breaking before a "/" so
// each line ends at a folder; a name wider than w is split where it must.
func wrapPath(path string, w int) []string {
	if w <= 0 || len([]rune(path)) <= w {
		return []string{path}
	}
	var lines []string
	line := ""
	for _, part := range strings.SplitAfter(path, "/") {
		if part == "" {
			continue
		}
		// A name that fits on a line starts a new one; a longer one is
		// split anyway, so it carries on from where the line is.
		if line != "" && len([]rune(part)) <= w && len([]rune(line))+len([]rune(part)) > w {
			lines = append(lines, line)
			line = ""
		}
		line += part
		for r := []rune(line); len(r) > w; r = []rune(line) {
			lines = append(lines, string(r[:w]))
			line = string(r[w:])
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func opsLines(ops []Op, now time.Time) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op.State {
		case OpRunning:
			frac := 1.0
			if op.Total > 0 {
				frac = min(float64(op.Done)/float64(op.Total), 1)
			}
			out = append(out, joinSpaced([]string{
				val(op.Name), meter(frac, lipgloss.Color(common.Theme.Correct)),
				val(fmt.Sprintf("%.0f%%", frac*100)), //nolint:mnd // percent
				lab(fmt.Sprintf("· %d of %d", op.Done, op.Total)),
			}))
		case OpDone:
			out = append(out, joinSpaced([]string{ok("✓ " + op.Name), lab(ago(now, op.DoneTime))}))
		case OpFailed:
			out = append(out, joinSpaced([]string{bad("✗ " + op.Name), lab(ago(now, op.DoneTime))}))
		case OpCancelled:
			out = append(out, joinSpaced([]string{lab("✗ " + op.Name + " · cancelled"), lab(ago(now, op.DoneTime))}))
		}
	}
	return out
}

func macLines(s sysstat.Stats) []string {
	c := kindMac.color()
	var out []string
	if s.CPUOK {
		out = append(out, joinSpaced([]string{lab("cpu   "), meter(s.CPUPercent/100, c), //nolint:mnd // percent
			val(fmt.Sprintf("%.0f%%", s.CPUPercent))}))
	}
	if s.MemOK && s.MemTotal > 0 {
		out = append(out, joinSpaced([]string{lab("memory"), meter(float64(s.MemUsed)/float64(s.MemTotal), c),
			val(gib(s.MemUsed)), lab("/ " + gib(s.MemTotal) + " GB")}))
	}
	if s.DiskOK && s.DiskTotal > 0 {
		out = append(out, joinSpaced([]string{lab("disk  "), meter(float64(s.DiskUsed)/float64(s.DiskTotal), c),
			val(gb(s.DiskUsed)), lab("/ " + gb(s.DiskTotal) + " GB")}))
	}
	return out
}

// gib is bytes in GiB, as macOS counts memory; gb is bytes in GB, as it
// counts disks.
func gib(b uint64) string { return trimNum(float64(b) / (1 << 30)) } //nolint:mnd // GiB
func gb(b uint64) string  { return trimNum(float64(b) / 1e9) }       //nolint:mnd // GB

func trimNum(v float64) string {
	if v >= 10 { //nolint:mnd // whole numbers from 10 up
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func ago(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// joinSpaced joins styled parts with a space on the footer's background.
func joinSpaced(parts []string) string {
	sp := lipgloss.NewStyle().Background(lipgloss.Color(common.Theme.FooterBG)).Render(" ")
	var kept []string
	for _, p := range parts {
		if ansi.Strip(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sp)
}
