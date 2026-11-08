package ask

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
	"github.com/junjieliang672/marmota/src/internal/ui/rendering"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// Session is the selected session as the right side shows it.
type Session struct {
	// Glyph is the session's state, drawn in GlyphColor before Title, its
	// name and summary, in the border.
	Glyph      string
	GlyphColor lipgloss.TerminalColor
	Title      string
	// Pinned follows Title in the border ("pinned") while the preview stays
	// on the session as you browse files.
	Pinned string
	// Hint is the key at the foot of the border, as the dock's boxes have
	// theirs: "V pin".
	Hint string
	// State says what it is doing and since when ("finished 1h ago");
	// Place where it runs and Mode its permission mode, while it runs.
	State, Place, Mode string
	// Beside marks the session beside superfile: its place is drawn in the
	// color the list marks it with.
	Beside bool
	// Folder is where it works, Branch the git branch there.
	Folder, Branch string
	Error          string
	// Now is the tool it is running.
	Now string
	// Question is what it asks you, as its pane draws it; Answering says
	// the keys go to it. QuestionNote says how to answer, or why it can't
	// be answered here.
	Question     []string
	Answering    bool
	QuestionNote string
	// Turns is the conversation, from the session's transcript.
	Turns []claudesession.Turn
	Tasks []claudesession.Todo
	Files []string
	// Mark is search text: lines that hold it are marked.
	Mark string
	// About explains, under its own heading, what isn't a session yet, such
	// as what sending to a new one does.
	AboutTitle string
	About      []string
}

const (
	maxPromptLines  = 2
	maxTaskLines    = 6
	maxFileLines    = 6
	turnsShown      = 20
	replyIndent     = "  "
	replyMark       = "⏺ "
	promptMark      = "› "
	searchMark      = "▌"
	questionBar     = "▎ "
	maxRenderCached = 256
)

// fileIndent sets changed files and tasks off under their heading.
const fileIndent = "  "

// previewGap is the blank cells at least between a question's options and
// the preview Claude Code draws beside them.
const previewGap = 2

type paneStyles struct {
	answerStyles

	sec, secHint, now, ask, mark, kv, kvStrong, beside, prompt, errLine lipgloss.Style
}

func newPaneStyles() paneStyles {
	a := newAnswerStyles(common.FilePanelBGColor)
	return paneStyles{
		answerStyles: a,
		sec:          a.base.Foreground(common.FilePanelBorderActiveColor).Bold(true),
		secHint:      a.base.Foreground(common.FilePanelBorderColor),
		now:          a.base.Foreground(lipgloss.Color(common.ClaudeAgentColor)),
		ask:          a.base.Foreground(lipgloss.Color(common.ClaudeNeedsYouColor)),
		mark:         a.base.Foreground(lipgloss.Color(common.ClaudeNeedsYouColor)),
		kv:           a.base.Foreground(common.FilePanelBorderColor),
		kvStrong:     a.base,
		beside:       a.base.Foreground(lipgloss.Color(common.Theme.Correct)),
		prompt:       a.base.Bold(true),
		errLine:      a.bad,
	}
}

// RenderSession draws the right side in its box: the session, and the Ask
// bar (already drawn) at the foot. scroll moves the conversation back by
// that many lines.
func RenderSession(s Session, composer string, width, height, scroll int, focused bool) string {
	r := ui.FilePanelRenderer(height, width, focused)
	if s.Pinned != "" {
		r = ui.PinnedPanelRenderer(height, width)
	}
	if s.Glyph != "" {
		r.SetBorderTitleGlyph(s.Glyph, s.GlyphColor)
	}
	r.SetBorderTitle(s.Title)
	if s.Pinned != "" {
		r.SetBorderTitleSuffix(s.Pinned)
	}
	if s.Hint != "" {
		r.SetBorderInfoItems(s.Hint)
	}
	composerLines := strings.Split(composer, "\n")
	body, _ := layoutSession(s, len(composerLines), width, height, scroll)
	for _, l := range body {
		r.AddLineWithCustomTruncate(l, rendering.TailsTruncateRight)
	}
	for _, l := range composerLines {
		r.AddLineWithCustomTruncate(l, rendering.TailsTruncateRight)
	}
	return r.Render()
}

// ScrollLimit is how far back the conversation can be scrolled.
func ScrollLimit(s Session, composer string, width, height int) int {
	_, limit := layoutSession(s, len(strings.Split(composer, "\n")), width, height, 0)
	return limit
}

// layoutSession lays out what goes above the Ask bar, the conversation
// scrolled back by scroll lines at most, and returns how far it can go.
func layoutSession(s Session, composerLines, width, height, scroll int) ([]string, int) {
	inner := max(1, width-composerChrome)
	st := newPaneStyles()
	room := max(1, height-composerChrome-composerLines)

	head := factLines(s, inner, st)
	if s.Now != "" {
		head = append(gap(head), section("Now", "", inner, st), st.now.Render(ansi.Truncate("✳ "+s.Now, inner, "…")))
	}
	head = append(head, questionLines(s, inner, st)...)
	if s.AboutTitle != "" {
		head = append(gap(head), section(s.AboutTitle, "", inner, st))
		for _, text := range s.About {
			head = append(head, wrapSpans(parseInline(text), inner, "", "", st.textStyles)...)
		}
	}
	tail := tailLines(s, inner, st)
	convo := conversationLines(s, inner, st)
	if len(convo) > 0 && len(head) > 0 {
		convo = append([]string{""}, convo...)
	}
	// The conversation gets the room the rest leaves, newest last; scroll
	// moves back through it.
	convoRoom := room - len(head) - len(tail)
	if convoRoom < len(convo) && len(tail) > 0 {
		tail = nil // a long conversation comes before tasks and files
		convoRoom = room - len(head)
	}
	convoRoom = max(0, convoRoom)
	limit := max(0, len(convo)-convoRoom)
	scroll = min(max(0, scroll), limit)
	end := len(convo) - scroll
	start := max(0, end-convoRoom)
	body := append(append(append([]string{}, head...), convo[start:end]...), tail...)
	if len(body) > room {
		body = body[:room]
	}
	for len(body) < room {
		body = append(body, "")
	}
	return body, limit
}

// factLines are the two lines under the title: what the session is doing,
// where it runs and in which mode; then its folder and branch.
func factLines(s Session, width int, st paneStyles) []string {
	sep := st.kv.Render(" · ")
	var lines []string
	if s.State != "" {
		line := st.kv.Render(s.State)
		if s.Place != "" {
			place := st.kvStrong
			if s.Beside {
				place = st.beside
			}
			line += sep + place.Render(s.Place)
		}
		if s.Mode != "" {
			line += sep + st.kv.Render("⏵⏵ "+s.Mode)
		}
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	if s.Folder != "" {
		room := width
		if s.Branch != "" {
			room -= ansi.StringWidth(s.Branch) + ansi.StringWidth(" · ")
		}
		line := st.kv.Render(truncateLeft(s.Folder, room))
		if s.Branch != "" {
			line += sep + st.kvStrong.Render(s.Branch)
		}
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	if s.Error != "" {
		bad := textStyles{base: st.errLine, bold: st.errLine, code: st.errLine, dim: st.errLine, italic: st.errLine,
			link: st.errLine}
		lines = append(lines, wrapSpans([]span{{text: s.Error}}, width, "", "", bad)...)
	}
	return lines
}

// gap ends lines with an empty one, before the next section, unless there
// are none.
func gap(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	return append(lines, "")
}

func section(title, hint string, width int, st paneStyles) string {
	line := st.sec.Render(title)
	if hint != "" {
		if room := width - ansi.StringWidth(title) - ansi.StringWidth(hint); room >= 1 {
			line += st.base.Render(strings.Repeat(" ", room)) + st.secHint.Render(hint)
		}
	}
	return line
}

// questionLines draws what the session asks, as its pane shows it, behind a
// bar in the color of a session that needs you.
func questionLines(s Session, inner int, st paneStyles) []string {
	if len(s.Question) == 0 && s.QuestionNote == "" {
		return nil
	}
	hint := ""
	switch {
	case s.Answering:
		hint = "keys go to it · " + backKey() + " back"
	case len(s.Question) > 0:
		hint = firstKey(common.Hotkeys.OpenAskBar) + " to answer"
	}
	lines := []string{"", section("Question", hint, inner, st)}
	bar := st.ask.Render(questionBar)
	room := max(1, inner-ansi.StringWidth(questionBar))
	for _, q := range stackColumns(s.Question, room) {
		lines = append(lines, bar+questionLine(ansi.Truncate(q, room, ""), st))
	}
	if s.QuestionNote != "" {
		note := st.textStyles
		note.base = st.ask
		for _, l := range strings.Split(s.QuestionNote, "\n") {
			for _, w := range wrapSpans([]span{{text: l}}, room, "", "", note) {
				lines = append(lines, bar+w)
			}
		}
	}
	return lines
}

// questionLine colors a line of Claude Code's or Codex's dialog: the option
// the cursor is on and the options' numbers in the question color, the
// question itself bold, rules and key hints dim.
func questionLine(line string, st paneStyles) string {
	trimmed := strings.TrimSpace(line)
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	switch {
	case trimmed == "":
		return ""
	case strings.Trim(trimmed, "─━▔═") == "":
		return st.border.Render(line)
	case strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "› "): // Claude Code's, Codex's
		return st.base.Render(indent) + st.ask.Bold(true).Render(trimmed)
	case strings.HasSuffix(trimmed, "?"):
		return st.base.Render(indent) + st.prompt.Render(trimmed)
	case strings.Contains(trimmed, "Enter to ") || strings.Contains(trimmed, "Esc to ") ||
		strings.Contains(trimmed, "esc to cancel"):
		return st.dim.Render(line)
	}
	if n := optionNumber(trimmed); n != "" {
		return st.base.Render(indent) + st.ask.Render(n) + st.base.Render(strings.TrimPrefix(trimmed, n))
	}
	return st.base.Render(line)
}

// optionNumber is the "2." an option starts with, or "".
func optionNumber(text string) string {
	i := 0
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(text) || text[i] != '.' {
		return ""
	}
	return text[:i+1]
}

// conversationLines draws the turns, newest last: your prompt, the tools
// Claude used, and its reply after ⏺, one turn after another.
func conversationLines(s Session, width int, st paneStyles) []string {
	turns := s.Turns
	if len(turns) > turnsShown {
		turns = turns[len(turns)-turnsShown:]
	}
	hint := firstKey(common.Hotkeys.PageUp) + "/" + firstKey(common.Hotkeys.PageDown)
	lines := []string{section("Conversation", hint, width, st)}
	if len(turns) == 0 {
		if s.AboutTitle != "" {
			return nil // a new session: About says what happens
		}
		return append(lines, st.dim.Render("Nothing said yet"))
	}
	for i, t := range turns {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, turnBlock(t.Prompt, t.Tools, t.Reply, s.Mark, width, st)...)
	}
	return lines
}

// turnBlock draws one turn: "› prompt", the tools under it, and the reply.
func turnBlock(prompt string, tools []string, reply, mark string, width int, st paneStyles) []string {
	lines := promptLines(prompt, mark, width, st)
	if len(tools) > 0 {
		lines = append(lines, st.dim.Render(ansi.Truncate(replyIndent+"⎿ "+strings.Join(lastTools(tools), " · "),
			width, "…")))
	}
	return append(lines, replyLines(reply, mark, width, st)...)
}

// promptLines draws your prompt in bold, at most two lines of it.
func promptLines(prompt, mark string, width int, st paneStyles) []string {
	bold := st.textStyles
	bold.base, bold.code, bold.dim = st.prompt, st.prompt, st.base
	lines := wrapSpans([]span{{text: prompt}}, width, promptMark, replyIndent, bold)
	if len(lines) > maxPromptLines {
		lines = lines[:maxPromptLines]
		last := ansi.Truncate(lines[maxPromptLines-1], width-1, "")
		lines[maxPromptLines-1] = last + st.prompt.Render("…")
	}
	if hasMark(prompt, mark) {
		lines[0] = st.mark.Render(searchMark) + ansi.TruncateLeft(lines[0], 1, "")
	}
	return lines
}

// replyLines draws Claude's reply after ⏺, its lines indented under the
// first; those that hold the search text are marked.
func replyLines(reply, mark string, width int, st paneStyles) []string {
	if strings.TrimSpace(reply) == "" {
		return nil
	}
	body := cachedMarkdown(reply, width-len(replyIndent), st.textStyles)
	out := make([]string, len(body))
	for i, l := range body {
		prefix := st.base.Render(replyIndent)
		if i == 0 {
			prefix = st.base.Render(replyMark)
		}
		if hasMark(ansi.Strip(l), mark) {
			prefix = st.mark.Render(searchMark) + st.base.Render(" ")
		}
		out[i] = prefix + l
	}
	return out
}

func hasMark(text, mark string) bool {
	return mark != "" && strings.Contains(strings.ToLower(text), strings.ToLower(mark))
}

const toolsShown = 3

func lastTools(tools []string) []string {
	if len(tools) <= toolsShown {
		return tools
	}
	return append([]string{"…"}, tools[len(tools)-toolsShown:]...)
}

// tailLines lists the session's tasks and the files it changed.
func tailLines(s Session, width int, st paneStyles) []string {
	var lines []string
	if len(s.Tasks) > 0 {
		lines = append(lines, "", section("Tasks", "", width, st))
		for i, t := range s.Tasks {
			if i == maxTaskLines {
				lines = append(lines, st.dim.Render(fileIndent+"…"))
				break
			}
			box, style := "☐", st.base
			switch t.Status {
			case "completed":
				box, style = "☑", st.dim
			case "in_progress":
				box, style = "◐", st.now
			}
			task := fileIndent + box + " " + strings.TrimSpace(t.Content)
			lines = append(lines, style.Render(ansi.Truncate(task, width, "…")))
		}
	}
	if len(s.Files) > 0 {
		lines = append(lines, "", section("Changed files", "", width, st))
		for i, f := range s.Files {
			if i == maxFileLines {
				lines = append(lines, st.dim.Render(fileIndent+"…"))
				break
			}
			lines = append(lines, st.base.Render(fileIndent+truncateLeft(f, width-len(fileIndent))))
		}
	}
	return lines
}

// truncateLeft keeps the end of a path: "…/internal/model.go".
func truncateLeft(s string, width int) string {
	if width <= 1 || ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.TruncateLeft(s, ansi.StringWidth(s)-width+1, "…")
}

// A reply is laid out again only when it or the width changes: View draws
// the conversation many times over.
var (
	renderMu    sync.Mutex                 //nolint:gochecknoglobals // a cache of drawn replies
	renderCache = map[renderKey][]string{} //nolint:gochecknoglobals // a cache of drawn replies
)

type renderKey struct {
	text  string
	width int
}

func cachedMarkdown(text string, width int, st textStyles) []string {
	key := renderKey{text: text, width: width}
	renderMu.Lock()
	defer renderMu.Unlock()
	if lines, ok := renderCache[key]; ok {
		return lines
	}
	if len(renderCache) >= maxRenderCached {
		renderCache = map[renderKey][]string{}
	}
	lines := markdownLines(text, width, st)
	renderCache[key] = lines
	return lines
}

// stackColumns puts a preview Claude Code draws beside a question's options
// under them instead, when the two side by side are wider than width. The
// preview is a box (┌ … └) and whatever is drawn under it in its column.
func stackColumns(lines []string, width int) []string {
	widest := 0
	for _, l := range lines {
		widest = max(widest, ansi.StringWidth(l))
	}
	if widest <= width {
		return lines
	}
	start, col := previewColumn(lines)
	if start < 0 {
		return lines
	}
	end := start
	for ; end < len(lines); end++ {
		// The block ends where something crosses into the preview's column.
		if r := []rune(lines[end]); len(r) > col && (r[col-1] != ' ' || r[col-2] != ' ') {
			break
		}
	}
	left, right := splitAt(lines[start:end], col)
	out := append(append([]string{}, lines[:start]...), left...)
	return append(append(out, right...), lines[end:]...)
}

// previewColumn finds the line and the column where a preview's box starts
// beside the options, or -1.
func previewColumn(lines []string) (int, int) {
	for i, l := range lines {
		r := []rune(l)
		for j := previewGap; j < len(r); j++ {
			if r[j] == '┌' && r[j-1] == ' ' && r[j-2] == ' ' {
				return i, j
			}
		}
	}
	return -1, -1
}

// splitAt cuts lines into what is left of col and what is right of it.
// Blank lines stay with the preview, so it keeps its spacing.
func splitAt(lines []string, col int) ([]string, []string) {
	var left, right []string
	for _, l := range lines {
		r := []rune(l)
		if len(r) <= col {
			if strings.TrimSpace(l) == "" {
				right = append(right, "")
			} else {
				left = append(left, l)
			}
			continue
		}
		if lp := strings.TrimRight(string(r[:col]), " "); lp != "" {
			left = append(left, lp)
		}
		right = append(right, string(r[col:]))
	}
	return left, right
}

// backKey is the Ask bar key that types nothing, such as ctrl+k: the one
// that takes the keyboard back from a question, where K would be typed.
func backKey() string {
	for _, k := range common.Hotkeys.OpenAskBar {
		if len([]rune(k)) > 1 {
			return k
		}
	}
	return "esc"
}
