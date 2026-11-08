// Package ask draws the session the preview shows, and the Ask box's
// Composer: it holds your question, which stays until you send or clear it,
// whichever session you pick.
package ask

import (
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

const (
	historyLimit   = 100
	inputCharLimit = 4000
	maxInputRows   = 3
	composerChrome = 2 // its border
)

// Composer is the Ask bar.
type Composer struct {
	input textinput.Model
	// model and effort index claudeask.Models and claudeask.Efforts, and
	// permission claudeask.PermissionModes: how a session the bar starts or
	// resumes runs.
	model, effort, permission int
	// codex is set when a session the bar starts runs in Codex rather than
	// Claude Code. codexModel and codexEffort are the choices for it, ""
	// keeping Codex's, among codexModels; codexDefault is Codex's model.
	codex                   bool
	codexModel, codexEffort string
	codexModels             []codexsession.Model
	codexDefault            string
	history                 []string
	// recallFrom, when set, is what up and down step through instead of
	// history: what was said to the session the bar writes to.
	recallFrom []string
	// histPos is the recalled history entry, -1 when none; saved is what
	// was written before recalling.
	histPos int
	saved   string
	// textWidth is how wide the question last wrapped, for up and down to
	// move a row.
	textWidth int
	// mentions maps each mention added to the question to the path it
	// names, so it can be written for whichever session it goes to;
	// sessions holds the mentions of sessions.
	mentions map[string]string
	sessions map[string]bool
	notice   string
	failed   bool
	// escText is what esc does, for the keys line: "" says "back to the list".
	escText string
}

// NewComposer returns an empty Ask bar.
func NewComposer() Composer {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "Write to this session, or describe a task"
	in.CharLimit = inputCharLimit
	in.Cursor.SetMode(cursor.CursorHide) // the bar draws its own cursor
	c := Composer{input: in, histPos: -1, mentions: map[string]string{}, sessions: map[string]bool{}}
	c.ResetChoices()
	return c
}

// defaultEffort is the effort a new session gets unless another is chosen.
const defaultEffort = "medium"

// ResetChoices goes back to what a new session gets unless you choose
// otherwise: Claude Code, its default model, medium effort.
func (c *Composer) ResetChoices() {
	c.codex, c.model, c.codexModel, c.codexEffort = false, 0, "", ""
	c.effort = max(0, slices.Index(claudeask.Efforts, defaultEffort))
}

// Focus gives the bar the keyboard.
func (c *Composer) Focus() tea.Cmd { return c.input.Focus() }

// Blur takes the keyboard away; what you wrote stays.
func (c *Composer) Blur() { c.input.Blur() }

// Focused reports whether the bar has the keyboard.
func (c *Composer) Focused() bool { return c.input.Focused() }

// Value is the question.
func (c *Composer) Value() string { return c.input.Value() }

// SetValue replaces the question, such as one kept from before.
func (c *Composer) SetValue(text string) {
	c.input.SetValue(text)
	c.input.CursorEnd()
}

// Clear empties the question.
func (c *Composer) Clear() {
	c.input.SetValue("")
	c.mentions = map[string]string{}
	c.sessions = map[string]bool{}
	c.histPos = -1
	c.notice = ""
}

// AddMention adds a mention, such as "@src/render.go", to the end of the
// question unless it is there already. path is the file it names, "" for a
// session's mention.
func (c *Composer) AddMention(text, path string) bool {
	value := c.input.Value()
	if hasMention(value, text) {
		return false
	}
	if value != "" && !strings.HasSuffix(value, " ") {
		value += " "
	}
	c.input.SetValue(value + text + " ")
	c.input.CursorEnd()
	if path != "" {
		c.mentions[text] = path
	} else {
		c.sessions[text] = true
	}
	return true
}

// Mentions maps each file mention still in the question to its path.
func (c *Composer) Mentions() map[string]string {
	value := c.input.Value()
	out := make(map[string]string, len(c.mentions))
	for text, path := range c.mentions {
		if hasMention(value, text) {
			out[text] = path
		}
	}
	return out
}

// hasMention reports whether the question holds the mention as a word of
// its own; a quoted one has spaces in it.
func hasMention(value, mention string) bool {
	for i := 0; ; {
		at := strings.Index(value[i:], mention)
		if at < 0 {
			return false
		}
		start, end := i+at, i+at+len(mention)
		if (start == 0 || unicode.IsSpace(rune(value[start-1]))) &&
			(end == len(value) || unicode.IsSpace(rune(value[end]))) {
			return true
		}
		i = start + 1
	}
}

// SetMentions restores the paths of mentions kept from before.
func (c *Composer) SetMentions(mentions map[string]string) {
	c.mentions = map[string]string{}
	for text, path := range mentions {
		c.mentions[text] = path
	}
}

// Choices are the model and effort a session the bar starts gets, Claude
// Code's or Codex's; "" keeps its own.
func (c *Composer) Choices() (string, string) {
	if c.codex {
		return c.CodexChoices()
	}
	return c.ClaudeChoices()
}

// ClaudeChoices are the model and effort for a Claude Code session.
func (c *Composer) ClaudeChoices() (string, string) {
	return claudeask.Models[c.model], claudeask.Efforts[c.effort]
}

// SetChoices restores the model and effort chosen earlier for a Claude Code
// session.
func (c *Composer) SetChoices(model, effort string) {
	c.model = max(0, slices.Index(claudeask.Models, model))
	c.effort = max(0, slices.Index(claudeask.Efforts, effort))
}

// StartsCodex reports that a session the bar starts runs in Codex.
func (c *Composer) StartsCodex() bool { return c.codex }

// SetStartsCodex chooses Codex, or Claude Code, for the sessions the bar
// starts.
func (c *Composer) SetStartsCodex(codex bool) { c.codex = codex }

// CodexChoices are the model and effort for a Codex session.
func (c *Composer) CodexChoices() (string, string) { return c.codexModel, c.codexEffort }

// SetCodexChoices restores the model and effort chosen earlier for a Codex
// session.
func (c *Composer) SetCodexChoices(model, effort string) {
	c.codexModel, c.codexEffort = model, effort
}

// SetCodexModels gives the models Codex offers, and its own, which a Codex
// session gets when the bar keeps Codex's choice. A choice kept from
// before that Codex no longer offers goes back to Codex's.
func (c *Composer) SetCodexModels(models []codexsession.Model, defaultModel string) {
	c.codexModels, c.codexDefault = models, defaultModel
	if len(models) == 0 {
		return
	}
	if !slices.Contains(c.codexModelChoices(), c.codexModel) {
		c.codexModel = ""
	}
	if !slices.Contains(c.codexEffortChoices(), c.codexEffort) {
		c.codexEffort = ""
	}
}

// CodexStartEffort is the effort a Codex session the bar starts gets: the
// one chosen, else medium, or the model's own when it doesn't take medium.
func (c *Composer) CodexStartEffort() string {
	if c.codexEffort != "" {
		return c.codexEffort
	}
	id := c.codexModel
	if id == "" {
		id = c.codexDefault
	}
	for _, m := range c.codexModels {
		if m.ID == id && m.DefaultEffort != "" && !slices.Contains(m.Efforts, defaultEffort) {
			return m.DefaultEffort
		}
	}
	return defaultEffort
}

// codexEffortsFallback are the efforts offered when Codex's list of models
// can't be read.
var codexEffortsFallback = []string{"low", "medium", "high", "xhigh"} //nolint:gochecknoglobals // fixed list

// CodexModelChoices are the Codex models to step through: "" (keep the
// one there is), then those Codex's picker offers.
func (c *Composer) CodexModelChoices() []string {
	out := []string{""}
	for _, m := range c.codexModels {
		out = append(out, m.ID)
	}
	return out
}

// CodexEffortChoices are the efforts a Codex model takes, "" (keep the one
// there is) first; model "" is Codex's own.
func (c *Composer) CodexEffortChoices(model string) []string {
	if model == "" {
		model = c.codexDefault
	}
	efforts := codexEffortsFallback
	for _, m := range c.codexModels {
		if m.ID == model && len(m.Efforts) > 0 {
			efforts = m.Efforts
		}
	}
	return append([]string{""}, efforts...)
}

func (c *Composer) codexModelChoices() []string  { return c.CodexModelChoices() }
func (c *Composer) codexEffortChoices() []string { return c.CodexEffortChoices(c.codexModel) }

// NextChoice is the choice after current in choices, round to the first.
func NextChoice(choices []string, current string) string {
	return choices[(slices.Index(choices, current)+1)%len(choices)]
}

// PermissionMode is the mode a session the bar starts or resumes runs in.
func (c *Composer) PermissionMode() string {
	return claudeask.PermissionModes[c.permission]
}

// SetPermissionMode restores a permission mode chosen earlier.
func (c *Composer) SetPermissionMode(mode string) {
	c.permission = max(0, slices.Index(claudeask.PermissionModes, mode))
}

// History is what was sent, oldest first.
func (c *Composer) History() []string { return c.history }

// SetHistory restores what was sent before.
func (c *Composer) SetHistory(history []string) {
	c.history = append([]string(nil), history...)
}

// SetRecall has up and down step through prompts, oldest first, rather than
// what the bar sent: those of the session it writes to. nil goes back to
// what the bar sent.
func (c *Composer) SetRecall(prompts []string) {
	if c.histPos >= 0 && !slices.Equal(prompts, c.recallFrom) {
		c.histPos = -1 // another session: start again from its latest
	}
	c.recallFrom = prompts
}

// recallList is what up and down step through.
func (c *Composer) recallList() []string {
	if c.recallFrom != nil {
		return c.recallFrom
	}
	return c.history
}

// Sent records a question that went, and empties the bar.
func (c *Composer) Sent(text string) {
	text = strings.TrimSpace(text)
	if text != "" && (len(c.history) == 0 || c.history[len(c.history)-1] != text) {
		c.history = append(c.history, text)
		if len(c.history) > historyLimit {
			c.history = c.history[len(c.history)-historyLimit:]
		}
	}
	c.Clear()
}

// SetNotice shows a line under the question, red when it says something
// failed; "" removes it.
func (c *Composer) SetNotice(text string, failed bool) {
	c.notice, c.failed = text, failed
}

// HandleKey takes a key while the bar has the keyboard: typing, the
// choices, clearing and recalling questions. Sending and leaving are the
// caller's.
func (c *Composer) HandleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	switch {
	case slices.Contains(common.Hotkeys.AskChooseAgent, key):
		c.codex = !c.codex
	case slices.Contains(common.Hotkeys.AskChooseModel, key) && c.codex:
		c.codexModel = NextChoice(c.codexModelChoices(), c.codexModel)
		if !slices.Contains(c.codexEffortChoices(), c.codexEffort) {
			c.codexEffort = "" // the model doesn't take it
		}
	case slices.Contains(common.Hotkeys.AskChooseModel, key):
		c.model = (c.model + 1) % len(claudeask.Models)
	case slices.Contains(common.Hotkeys.AskChooseEffort, key) && c.codex:
		c.codexEffort = NextChoice(c.codexEffortChoices(), c.codexEffort)
	case slices.Contains(common.Hotkeys.AskChooseEffort, key):
		c.effort = (c.effort + 1) % len(claudeask.Efforts)
	case slices.Contains(common.Hotkeys.AskChoosePermissionMode, key):
		c.permission = (c.permission + 1) % len(claudeask.PermissionModes)
	case slices.Contains(common.Hotkeys.AskClear, key):
		c.Clear()
	case slices.Contains(common.Hotkeys.AskPrevious, key):
		c.recall(1)
	case slices.Contains(common.Hotkeys.AskNext, key):
		c.recall(-1)
	case key == "up":
		c.up()
	case key == "down":
		c.down()
	default:
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		c.histPos = -1
		c.notice = ""
		return cmd
	}
	return nil
}

// up moves the cursor a row up in a question that wraps over several rows.
// From the first row, and while you step through earlier questions, it
// recalls the question before, as up does in Claude Code.
func (c *Composer) up() {
	if c.histPos < 0 && c.moveRow(-1) {
		return
	}
	c.recall(1)
}

// down moves the cursor a row down, or from the last row to the end. While
// you step through earlier questions it goes to the question after, and
// past the last back to what you were writing.
func (c *Composer) down() {
	switch {
	case c.histPos >= 0:
		c.recall(-1)
	case !c.moveRow(1):
		c.input.CursorEnd()
	}
}

// moveRow moves the cursor step rows, keeping its column where the row is
// long enough. It reports whether there was a row to move to.
func (c *Composer) moveRow(step int) bool {
	value := []rune(c.input.Value())
	if len(value) == 0 || c.textWidth <= 0 {
		return false
	}
	rows, row := c.rows(value, c.textWidth)
	to := row + step
	if to < 0 || to >= len(rows) {
		return false
	}
	col := ansi.StringWidth(string(value[rows[row][0]:min(c.input.Position(), len(value))]))
	target := rows[to]
	pos := target[0]
	for pos < target[1] && ansi.StringWidth(string(value[target[0]:pos+1])) <= col {
		pos++
	}
	if to < len(rows)-1 && pos == target[1] {
		pos-- // the row's end is the next row's start
	}
	c.input.SetCursor(pos)
	return true
}

// recall steps through earlier questions: 1 back, -1 forward to what you
// were writing.
func (c *Composer) recall(step int) {
	list := c.recallList()
	if len(list) == 0 {
		return
	}
	if c.histPos < 0 {
		c.saved = c.input.Value()
	}
	pos := c.histPos + step
	switch {
	case pos < 0:
		c.histPos = -1
		c.input.SetValue(c.saved)
	case pos < len(list):
		c.histPos = pos
		c.input.SetValue(list[len(list)-1-pos])
	default:
		return
	}
	c.input.CursorEnd()
}

type composerStyles struct {
	base, dim, input, cursor, key, mention, session, bad lipgloss.Style
}

func newComposerStyles() composerStyles {
	bg := common.FilePanelBGColor
	base := lipgloss.NewStyle().Foreground(common.FilePanelFGColor).Background(bg)
	session := base.Foreground(lipgloss.Color(common.Theme.Hint))
	if len(common.Theme.GradientColor) > 1 {
		session = base.Foreground(lipgloss.Color(common.Theme.GradientColor[1]))
	}
	return composerStyles{
		base:    base,
		dim:     base.Foreground(common.FilePanelBorderColor),
		input:   base.Bold(true),
		cursor:  base.Reverse(true),
		key:     base.Foreground(lipgloss.Color(common.Theme.Hint)),
		mention: base.Foreground(common.FilePanelDirectoryColor),
		session: session,
		bad:     base.Foreground(lipgloss.Color(common.Theme.Error)),
	}
}

// Sending says what sending does now, for the line under the question.
type Sending struct {
	// Text is what happens, e.g. "typed into its pane (hidden)".
	Text string
	// Fixed is set when the bar's permission mode doesn't apply, as for a
	// running session, which gets the question typed into its pane.
	Fixed bool
	// Model and Effort are what a running session answers with, which the
	// bar's choices don't change; "" when unknown.
	Model, Effort string
	// DefaultModel and DefaultEffort are what a new session gets when the
	// bar keeps its choice: Claude Code's, or Codex's when Codex is chosen;
	// "" when it sets none.
	DefaultModel, DefaultEffort string
	// New is set when sending starts a session: the bar's choice of Claude
	// Code or Codex applies.
	New bool
	// Change is set when the model and effort keys change a running
	// session's, from the next message; Changed when they have.
	Change, Changed bool
}

const (
	// minInputRows is the room the question takes at least, as in the
	// mockup.
	minInputRows = 2
	inputMark    = "› "
	inputIndent  = "  "
)

// Render draws the bar in a box width cells wide: the question, what
// sending does with the model and effort, and the keys.
func (c *Composer) Render(width int, sending Sending) string {
	lines := c.RenderLines(max(1, width-composerChrome-composerChrome), 0, sending)
	border := common.FilePanelBorderColor
	if c.Focused() {
		border = common.FilePanelBorderActiveColor
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		BorderBackground(common.FilePanelBGColor).Background(common.FilePanelBGColor).Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

// SetEscText says what esc does, for the keys line.
func (c *Composer) SetEscText(text string) { c.escText = text }

// RenderLines is the bar's content, inner wide, for a frame drawn elsewhere:
// the question, a notice, what sending does, the model and effort, and the
// keys, wrapped rather than cut. Within rows (0: any number) the question
// gives up rows first, keeping one.
func (c *Composer) RenderLines(inner, rows int, sending Sending) []string {
	st := newComposerStyles()
	inner = max(1, inner)
	var tail []string
	if c.notice != "" {
		style := st.dim
		if c.failed {
			style = st.bad
		}
		for _, l := range strings.Split(ansi.Wrap(c.notice, inner, ""), "\n") {
			tail = append(tail, style.Render(l))
		}
	}
	tail = append(tail, c.modeLine(inner, sending, st))
	tail = append(tail, c.choicesLines(inner, sending, st)...)
	tail = append(tail, c.keysLines(inner, sending, st)...)
	input := c.inputLines(inner, st)
	if over := len(input) + len(tail) - rows; rows > 0 && over > 0 {
		input = input[min(over, len(input)-1):] // the rows nearest the cursor stay
	}
	for len(input) < minInputRows && (rows <= 0 || len(input)+len(tail) < rows) {
		input = append(input, "")
	}
	lines := append(input, tail...)
	if rows > 0 && len(lines) > rows {
		lines = lines[:rows]
	}
	for i := range lines {
		if gap := inner - ansi.StringWidth(lines[i]); gap > 0 {
			lines[i] += st.base.Render(strings.Repeat(" ", gap))
		}
	}
	return lines
}

// wrapItems lays items out in rows width wide, joined by " · ", starting a
// row where the next item doesn't fit; an item wider than a row is cut.
func wrapItems(items []string, width int, st composerStyles) []string {
	sep := st.dim.Render(" · ")
	var rows []string
	row := ""
	for _, it := range items {
		it = ansi.Truncate(it, width, st.dim.Render("…"))
		switch {
		case row == "":
			row = it
		case ansi.StringWidth(row)+ansi.StringWidth(sep)+ansi.StringWidth(it) <= width:
			row += sep + it
		default:
			rows = append(rows, row)
			row = it
		}
	}
	if row != "" {
		rows = append(rows, row)
	}
	return rows
}

// modeLine is "→ what sending does".
func (c *Composer) modeLine(width int, sending Sending, st composerStyles) string {
	return st.base.Render(ansi.Truncate("→ "+sending.Text, width, "…"))
}

// choicesLines are the model and effort a request goes with, on lines of
// their own: for a new session the bar's choices, or the agent's defaults,
// with the keys that change them; for a running one, its own, with the
// keys when they change it; none when they aren't known.
func (c *Composer) choicesLines(width int, sending Sending, st composerStyles) []string {
	model, effort := sending.Model, sending.Effort
	if !sending.Fixed {
		model, effort = c.Choices()
		if model == "" {
			model = sending.DefaultModel // Claude Code's, or Codex's
		}
		if effort == "" {
			effort = sending.DefaultEffort
		}
		model, effort = firstSet(model, "default"), firstSet(effort, "default")
	}
	keys := (!sending.Fixed || sending.Change) && c.Focused()
	var items []string
	add := func(key []string, label, value string) {
		if value == "" && !keys {
			return
		}
		item := st.dim.Render(label+" ") + st.base.Render(firstSet(value, "unknown"))
		if keys {
			item = st.key.Render(firstKey(key)) + st.dim.Render(" ") + item
		}
		items = append(items, item)
	}
	add(common.Hotkeys.AskChooseModel, "model", claudeask.ModelLabel(model))
	add(common.Hotkeys.AskChooseEffort, "effort", effort)
	if sending.Changed {
		items = append(items, st.dim.Render("from your next message"))
	}
	return wrapItems(items, width, st)
}

func firstSet(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// keysLines name the bar's keys while it has the keyboard, else how to
// give it the keyboard, wrapped rather than cut.
func (c *Composer) keysLines(width int, sending Sending, st composerStyles) []string {
	keys := [][2]string{{firstKey(common.Hotkeys.OpenAskBar), "write"}}
	if c.Focused() {
		keys = [][2]string{{"enter", "send"}}
		if sending.New {
			other := "Codex"
			if c.codex {
				other = "Claude Code"
			}
			keys = append(keys, [2]string{firstKey(common.Hotkeys.AskChooseAgent), other})
		}
		keys = append(keys, [2]string{"↑↓", "history"}, [2]string{firstKey(common.Hotkeys.AskClear), "clear"})
		if !sending.Fixed {
			keys = append(keys, [2]string{firstKey(common.Hotkeys.AskChoosePermissionMode), "permissions"})
		}
		esc := c.escText
		if esc == "" {
			esc = "back to the list"
		}
		keys = append(keys, [2]string{"esc", esc})
	}
	items := make([]string, 0, len(keys))
	for _, k := range keys {
		items = append(items, st.key.Render(k[0])+st.dim.Render(" "+k[1]))
	}
	return wrapItems(items, width, st)
}

// inputLines draws the question over at most three rows after "› ", with
// the cursor when the bar has the keyboard. File mentions read in the
// folder color, session mentions in their own.
func (c *Composer) inputLines(width int, st composerStyles) []string {
	value := []rune(c.input.Value())
	focused := c.Focused()
	if len(value) == 0 {
		return []string{c.placeholderLine(width, st)}
	}
	textWidth := max(1, width-len(inputMark))
	c.textWidth = textWidth
	pos := min(c.input.Position(), len(value))
	rows, cursorRow := c.rows(value, textWidth)
	kinds := c.mentionKinds(value)
	first := max(0, cursorRow-maxInputRows+1)
	lines := make([]string, 0, maxInputRows)
	for i := first; i < len(rows) && len(lines) < maxInputRows; i++ {
		cursor := -1
		if focused && i == cursorRow {
			cursor = pos
		}
		prefix := st.base.Render(inputIndent)
		if i == 0 {
			prefix = st.input.Render(inputMark)
		}
		lines = append(lines, prefix+inputRow(value, rows[i], kinds, cursor, st))
	}
	return lines
}

// rows wraps the question to width, and says which row the cursor is on.
func (c *Composer) rows(value []rune, width int) ([][2]int, int) {
	pos := min(c.input.Position(), len(value))
	rows := wrapRanges(value, width)
	if last := rows[len(rows)-1]; c.Focused() && pos == len(value) &&
		ansi.StringWidth(string(value[last[0]:last[1]])) >= width {
		rows = append(rows, [2]int{len(value), len(value)}) // the cursor starts a new row
	}
	cursorRow := len(rows) - 1
	for i, r := range rows {
		if pos < r[1] {
			cursorRow = i
			break
		}
	}
	return rows, cursorRow
}

// placeholderLine says what to write, or how to start writing.
func (c *Composer) placeholderLine(width int, st composerStyles) string {
	if !c.Focused() {
		return st.dim.Render(ansi.Truncate(
			firstKey(common.Hotkeys.OpenAskBar)+" to write to the selected session", width, "…"))
	}
	return st.input.Render(inputMark) + st.cursor.Render(" ") +
		st.dim.Render(ansi.Truncate(c.input.Placeholder, width-len(inputMark)-1, "…"))
}

type mentionKind int

const (
	notMention mentionKind = iota
	fileMention
	sessionMention
)

// inputRow draws the runes r spans; cursor is where the cursor is, -1 when
// it is on another row.
func inputRow(value []rune, r [2]int, kinds []mentionKind, cursor int, st composerStyles) string {
	var line strings.Builder
	for j := r[0]; j < r[1]; j++ {
		style := st.input
		switch kinds[j] {
		case fileMention:
			style = st.mention
		case sessionMention:
			style = st.session
		case notMention:
		}
		if j == cursor {
			style = st.cursor
		}
		line.WriteString(style.Render(string(value[j])))
	}
	if cursor >= r[1] {
		line.WriteString(st.cursor.Render(" "))
	}
	return line.String()
}

// mentionKinds marks the runes that belong to an @mention, and whether it
// names a session (added with p, or quoted) or a file.
func (c *Composer) mentionKinds(value []rune) []mentionKind {
	kinds := make([]mentionKind, len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '@' || (i > 0 && !unicode.IsSpace(value[i-1])) {
			continue
		}
		end := i + 1
		quoted := end < len(value) && value[end] == '"'
		for end < len(value) && (!unicode.IsSpace(value[end]) || quoted) {
			if quoted && end > i+1 && value[end] == '"' {
				end++
				break
			}
			end++
		}
		kind := fileMention
		if _, isFile := c.mentions[string(value[i:end])]; !isFile && (quoted || c.sessions[string(value[i:end])]) {
			kind = sessionMention
		}
		for j := i; j < end; j++ {
			kinds[j] = kind
		}
		i = end - 1
	}
	return kinds
}

// wrapRanges cuts text into rows of at most width cells, breaking after a
// space when a word doesn't fit, and anywhere in CJK text.
func wrapRanges(runes []rune, width int) [][2]int {
	var rows [][2]int
	for start := 0; start < len(runes); {
		end, cells, lastSpace := start, 0, -1
		for end < len(runes) {
			w := ansi.StringWidth(string(runes[end]))
			if cells+w > width {
				break
			}
			if unicode.IsSpace(runes[end]) {
				lastSpace = end
			}
			cells += w
			end++
		}
		if end < len(runes) && lastSpace > start && ansi.StringWidth(string(runes[end])) == 1 {
			end = lastSpace + 1
		}
		if end == start {
			end++ // a rune wider than the row
		}
		rows = append(rows, [2]int{start, end})
		start = end
	}
	if len(rows) == 0 {
		rows = append(rows, [2]int{0, 0})
	}
	return rows
}

func firstKey(keys []string) string {
	for _, k := range keys {
		if k != "" {
			return k
		}
	}
	return ""
}
