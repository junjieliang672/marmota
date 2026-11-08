package ask

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// textStyles are the styles answers are drawn with.
type textStyles struct {
	base, bold, code, dim lipgloss.Style
	// italic, link and head are emphasis, a link's text and headings;
	// codeBlock is fenced code, on a background when the theme has one;
	// border is a table's lines.
	italic, link, head, codeBlock, border lipgloss.Style
	// codeBG is set when codeBlock has a background.
	codeBG bool
}

type spanKind int

const (
	plainSpan spanKind = iota
	boldSpan
	codeSpan
	italicSpan
	linkSpan
)

type span struct {
	text string
	kind spanKind
}

var (
	headingLine  = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	bulletLine   = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	numberedLine = regexp.MustCompile(`^(\s*)(\d+[.)])\s+(.*)$`)
	ruleLine     = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	taskItem     = regexp.MustCompile(`^\[([ xX])\]\s+(.*)$`)
	tableDivider = regexp.MustCompile(`^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$`)
	linkText     = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

const (
	tabCells  = 4
	ruleCells = 24
	// Code blocks are indented this much.
	codeIndent = "  "
	// A list's own indent takes at most this share of the line.
	maxIndentShare = 2
)

// markdownLines lays out Markdown in lines at most width cells wide:
// headings, lists (task lists too), quotes, rules, fenced code and tables,
// with **bold**, *italic*, `code` and [links](url) inside. Blocks are kept
// apart by one empty line.
func markdownLines(text string, width int, st textStyles) []string {
	width = max(1, width)
	var out []string
	src := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	blank := false
	for i := 0; i < len(src); i++ {
		line := strings.ReplaceAll(src[i], "\t", strings.Repeat(" ", tabCells))
		trimmed := strings.TrimSpace(line)
		var block []string
		switch {
		case strings.HasPrefix(trimmed, "```"):
			var code []string
			for i++; i < len(src) && !strings.HasPrefix(strings.TrimSpace(src[i]), "```"); i++ {
				code = append(code, strings.ReplaceAll(src[i], "\t", strings.Repeat(" ", tabCells)))
			}
			block = codeLines(code, width, st)
		case isTableRow(trimmed) && i+1 < len(src) && tableDivider.MatchString(src[i+1]):
			rows := [][]string{splitRow(trimmed)}
			aligns := columnAligns(src[i+1])
			for i += 2; i < len(src) && isTableRow(strings.TrimSpace(src[i])); i++ {
				rows = append(rows, splitRow(strings.TrimSpace(src[i])))
			}
			i--
			block = tableLines(rows, aligns, width, st)
		case trimmed == "":
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		default:
			block = blockLines(line, trimmed, width, st)
		}
		blank = false
		out = append(out, block...)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func blockLines(line, trimmed string, width int, st textStyles) []string {
	switch {
	case ruleLine.MatchString(line):
		return []string{st.border.Render(strings.Repeat("─", min(width, ruleCells)))}
	case headingLine.MatchString(trimmed):
		text := headingLine.FindStringSubmatch(trimmed)[1]
		head := st
		head.base, head.bold, head.italic = st.head, st.head, st.head
		return wrapSpans(parseInline(text), width, "", "", head)
	case bulletLine.MatchString(line):
		m := bulletLine.FindStringSubmatch(line)
		indent := strings.Repeat(" ", min(len(m[1]), width/maxIndentShare))
		mark, text := "• ", m[2]
		if t := taskItem.FindStringSubmatch(text); t != nil {
			mark, text = "☐ ", t[2]
			if t[1] != " " {
				mark = "☑ "
			}
		}
		return wrapSpans(parseInline(text), width, indent+mark, indent+"  ", st)
	case numberedLine.MatchString(line):
		m := numberedLine.FindStringSubmatch(line)
		indent := strings.Repeat(" ", min(len(m[1]), width/maxIndentShare))
		return wrapSpans(parseInline(m[3]), width, indent+m[2]+" ",
			indent+strings.Repeat(" ", ansi.StringWidth(m[2])+1), st)
	case strings.HasPrefix(trimmed, ">"):
		quote := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		q := st
		q.base, q.bold = st.italic, st.italic.Bold(true)
		return wrapSpans(parseInline(quote), width, "▎ ", "▎ ", q)
	default:
		return wrapSpans(parseInline(trimmed), width, "", "", st)
	}
}

// codeLines draws fenced code: on a band of the theme's cursor color when
// it has one, else after a bar.
func codeLines(code []string, width int, st textStyles) []string {
	for len(code) > 0 && strings.TrimSpace(code[len(code)-1]) == "" {
		code = code[:len(code)-1]
	}
	if len(code) == 0 {
		return nil
	}
	room := max(1, width-len(codeIndent))
	var out []string
	for _, line := range code {
		for _, l := range hardWrap(strings.TrimRight(line, " "), room-1) {
			if st.codeBG {
				out = append(out, st.codeBlock.Render(" "+padRight(l, room-1)+" "))
			} else {
				out = append(out, st.border.Render("│ ")+st.code.Render(l))
			}
		}
	}
	return out
}

func padRight(s string, width int) string {
	if gap := width - ansi.StringWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// parseInline splits text into plain, **bold**, *italic*, `code` and link
// spans; a link shows its text.
func parseInline(text string) []span {
	text = linkText.ReplaceAllString(text, "\x01$1\x01")
	var spans []span
	var cur strings.Builder
	kind := plainSpan
	flush := func() {
		if cur.Len() > 0 {
			spans = append(spans, span{text: cur.String(), kind: kind})
			cur.Reset()
		}
	}
	toggle := func(k spanKind) {
		flush()
		if kind == k {
			kind = plainSpan
		} else {
			kind = k
		}
	}
	for i := 0; i < len(text); {
		switch {
		case kind != codeSpan && text[i] == '\x01':
			toggle(linkSpan)
			i++
		case (kind == plainSpan || kind == codeSpan) && text[i] == '`':
			toggle(codeSpan)
			i++
		case kind != codeSpan && strings.HasPrefix(text[i:], "**"):
			toggle(boldSpan)
			i += 2
		case kind != codeSpan && strings.HasPrefix(text[i:], "~~"):
			i += 2 // struck through: the text stays
		case kind != codeSpan && (text[i] == '*' || text[i] == '_') && isEmphasisMark(text, i, kind):
			toggle(italicSpan)
			i++
		default:
			cur.WriteByte(text[i])
			i++
		}
	}
	flush()
	return spans
}

// isEmphasisMark reports whether the * or _ at i opens or closes *italic*,
// and isn't part of a word like snake_case or 2*3.
func isEmphasisMark(text string, i int, kind spanKind) bool {
	before := i > 0 && isWordByte(text[i-1])
	after := i+1 < len(text) && isWordByte(text[i+1])
	if kind == italicSpan {
		return !after && i > 0 && text[i-1] != ' '
	}
	return !before && after && strings.IndexByte(text[i+1:], text[i]) >= 0
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

type styledRune struct {
	r    rune
	kind spanKind
}

// wrapSpans fills lines greedily, word by word. first begins the first line
// and rest the others; both are drawn dim.
func wrapSpans(spans []span, width int, first, rest string, st textStyles) []string {
	var runes []styledRune
	for _, s := range spans {
		for _, r := range s.text {
			runes = append(runes, styledRune{r: r, kind: s.kind})
		}
	}
	f := &lineFiller{width: width, first: first, rest: rest}
	for _, word := range splitWords(runes) {
		f.addWord(word)
	}
	if len(f.line) > 0 || len(f.lines) == 0 {
		f.newLine()
	}
	out := make([]string, len(f.lines))
	for i, l := range f.lines {
		prefix := rest
		if i == 0 {
			prefix = first
		}
		out[i] = st.dim.Render(prefix) + renderRunes(l, st)
	}
	return out
}

// noLineStart is punctuation that a line of CJK text doesn't begin with.
const noLineStart = "，。、；：？！）」』】》〉…,.;:?!)]}"

// lineFiller fills lines of width cells, after a prefix: first on the first
// line, rest on the others.
type lineFiller struct {
	width       int
	first, rest string
	lines       [][]styledRune
	line        []styledRune
	lineWidth   int
}

func (f *lineFiller) room() int {
	prefix := f.rest
	if len(f.lines) == 0 {
		prefix = f.first
	}
	return max(1, f.width-ansi.StringWidth(prefix))
}

func (f *lineFiller) newLine() {
	f.lines = append(f.lines, trimRight(f.line))
	f.line, f.lineWidth = nil, 0
}

func (f *lineFiller) add(runes []styledRune) {
	f.line = append(f.line, runes...)
	f.lineWidth += runesWidth(runes)
}

// addWord puts a word, or the spaces between words, on the lines. A word
// that doesn't fit moves to the next line, and one longer than a whole line
// is cut where the line ends. Each CJK character is a word of its own (see
// splitWords), so CJK text fills lines and breaks between characters.
func (f *lineFiller) addWord(word []styledRune) {
	if isSpace(word) {
		if f.lineWidth > 0 {
			f.add(word)
		}
		return
	}
	if f.lineWidth > 0 && f.lineWidth+runesWidth(word) > f.room() {
		f.breakBefore(word)
	}
	for len(word) > 0 {
		fit := max(1, fitRunes(word, f.room()-f.lineWidth)) // a rune wider than the line still shows
		f.add(word[:fit])
		if word = word[fit:]; len(word) > 0 {
			f.newLine()
		}
	}
}

// breakBefore starts a new line for word. Punctuation that doesn't begin a
// line takes the character before it along.
func (f *lineFiller) breakBefore(word []styledRune) {
	var carry []styledRune
	last := len(f.line) - 1
	if strings.ContainsRune(noLineStart, word[0].r) && last > 0 && !unicode.IsSpace(f.line[last].r) {
		carry = append(carry, f.line[last])
		f.line = f.line[:last]
		f.lineWidth -= runesWidth(carry)
	}
	f.newLine()
	f.add(carry)
}

// hardWrap breaks text into pieces at most width cells wide.
func hardWrap(text string, width int) []string {
	width = max(1, width)
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	var out []string
	for len(runes) > 0 {
		n, w := 0, 0
		for n < len(runes) {
			rw := ansi.StringWidth(string(runes[n]))
			if w+rw > width && n > 0 {
				break
			}
			w += rw
			n++
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

// splitWords cuts text into runs of spaces, runs of other narrow
// characters, and single wide (CJK) characters, which are words of their own.
func splitWords(runes []styledRune) [][]styledRune {
	var words [][]styledRune
	start := 0
	for i := 1; i <= len(runes); i++ {
		if i == len(runes) || isWide(runes[i]) || isWide(runes[i-1]) ||
			unicode.IsSpace(runes[i].r) != unicode.IsSpace(runes[start].r) {
			words = append(words, runes[start:i])
			start = i
		}
	}
	return words
}

func isWide(r styledRune) bool {
	return ansi.StringWidth(string(r.r)) > 1
}

func isSpace(word []styledRune) bool {
	return len(word) > 0 && unicode.IsSpace(word[0].r)
}

func runesWidth(runes []styledRune) int {
	w := 0
	for _, r := range runes {
		w += ansi.StringWidth(string(r.r))
	}
	return w
}

// fitRunes is how many of runes fit in width cells.
func fitRunes(runes []styledRune, width int) int {
	n, w := 0, 0
	for n < len(runes) {
		rw := ansi.StringWidth(string(runes[n].r))
		if w+rw > width {
			break
		}
		w += rw
		n++
	}
	return n
}

func trimRight(runes []styledRune) []styledRune {
	for len(runes) > 0 && unicode.IsSpace(runes[len(runes)-1].r) {
		runes = runes[:len(runes)-1]
	}
	return runes
}

func renderRunes(runes []styledRune, st textStyles) string {
	var b strings.Builder
	for i := 0; i < len(runes); {
		j := i
		for j < len(runes) && runes[j].kind == runes[i].kind {
			j++
		}
		text := make([]rune, 0, j-i)
		for _, r := range runes[i:j] {
			if unicode.IsSpace(r.r) {
				r.r = ' '
			}
			text = append(text, r.r)
		}
		style := st.base
		switch runes[i].kind {
		case boldSpan:
			style = st.bold
		case codeSpan:
			style = st.code
		case italicSpan:
			style = st.italic
		case linkSpan:
			style = st.link
		case plainSpan:
		}
		b.WriteString(style.Render(string(text)))
		i = j
	}
	return b.String()
}
