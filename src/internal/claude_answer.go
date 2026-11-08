package internal

import (
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/pkg/claudeprompt"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// Answering a session's question from the Sessions box. While the selected
// session asks something, the right side shows the question as its pane
// draws it: Claude Code's own dialog, with its tabs, check boxes and
// previews. K (open_ask_bar) hands the keyboard to it: keys go to the pane as typed,
// so whatever the dialog takes works, and esc cancels it as it would there.
// ctrl+k (its key that types nothing) gives the keyboard back.

const (
	// claudeQuestionPoll is how often the question is read off its pane.
	claudeQuestionPoll = 400 * time.Millisecond
	// claudeQuestionMaxLines caps how much of the pane the question takes.
	claudeQuestionMaxLines = 40
	// claudeQuestionLookBack is how far up the pane the question is looked
	// for.
	claudeQuestionLookBack = 80
	minRuleCells           = 10
)

// claudeQuestion is what the selected session asks, as its pane shows it.
type claudeQuestion struct {
	// gen changes whenever the question is for another session; reads and
	// sends for an older one are dropped.
	gen    int
	id     string
	paneID int
	// codex is set for a Codex session, which draws its questions its way.
	codex bool
	lines []string
	// reading and ticking say a read is under way, or one is due, so only
	// one loop reads the pane.
	reading, ticking bool
	// keys wait to be sent while a send is under way.
	keys    []string
	sending bool
}

type claudeQuestionMsg struct {
	gen   int
	lines []string
	// over is set when the pane shows its prompt again: there is no
	// question any more.
	over bool
	err  error
}

type claudeQuestionTickMsg struct{ gen int }

type claudeKeysSentMsg struct {
	gen int
	err error
}

// watchClaudeQuestion starts reading the selected session's question off
// its pane when it asks one, and stops when it doesn't.
func (m *model) watchClaudeQuestion() tea.Cmd {
	q := &m.sessions.question
	s, ok := m.selectedSession()
	var pane weztermPane
	asking := ok && m.sessionsShown() && !m.sessions.list.NewSelected() && s.Live &&
		s.Status == claudesession.StatusWaiting
	if asking {
		pane, asking = m.weztermPaneForSession(s)
	}
	if !asking {
		if q.id != "" {
			m.forgetClaudeQuestion()
		}
		return nil
	}
	if q.id == s.ID && q.paneID == pane.PaneID {
		return m.readClaudeQuestion()
	}
	m.forgetClaudeQuestion()
	q.id, q.paneID, q.codex = s.ID, pane.PaneID, s.IsCodex()
	return m.readClaudeQuestion()
}

// forgetClaudeQuestion drops the question shown, and the keyboard with it.
func (m *model) forgetClaudeQuestion() {
	q := &m.sessions.question
	*q = claudeQuestion{gen: q.gen + 1}
	if m.sessions.focus == sessionsFocusAnswer {
		m.sessions.focus = sessionsFocusList
	}
}

func (m *model) readClaudeQuestion() tea.Cmd {
	q := &m.sessions.question
	if q.reading || q.id == "" {
		return nil
	}
	q.reading = true
	gen, paneID, codex := q.gen, q.paneID, q.codex
	return func() tea.Msg {
		screen, err := weztermGetText(paneID)
		if err != nil {
			return claudeQuestionMsg{gen: gen, err: err}
		}
		lines, over := claudeQuestionLines(screen)
		if codex {
			lines, over = codexQuestionLines(screen)
		}
		return claudeQuestionMsg{gen: gen, lines: lines, over: over}
	}
}

func (m *model) applyClaudeQuestion(msg claudeQuestionMsg) tea.Cmd {
	q := &m.sessions.question
	if msg.gen != q.gen {
		return nil
	}
	q.reading = false
	switch {
	case msg.err != nil:
		slog.Debug("Could not read the question off the session's pane", "error", msg.err)
	case msg.over:
		q.lines = nil
		if m.sessions.focus == sessionsFocusAnswer && len(q.keys) == 0 && !q.sending {
			m.sessions.focus = sessionsFocusList
			m.flashClaude("Answered")
		}
	default:
		q.lines = msg.lines
	}
	if q.ticking || !m.sessionsShown() {
		return nil
	}
	q.ticking = true
	gen := q.gen
	return tea.Tick(claudeQuestionPoll, func(time.Time) tea.Msg { return claudeQuestionTickMsg{gen: gen} })
}

func (m *model) applyClaudeQuestionTick(msg claudeQuestionTickMsg) tea.Cmd {
	q := &m.sessions.question
	if msg.gen != q.gen {
		return nil
	}
	q.ticking = false
	if !m.sessionsShown() {
		return nil
	}
	return m.watchClaudeQuestion()
}

// canAnswerClaudeQuestion reports whether the right side shows the
// session's question, so the keyboard can go to it.
func (m *model) canAnswerClaudeQuestion(s claudesession.Session) bool {
	q := m.sessions.question
	return s.Live && s.Status == claudesession.StatusWaiting && q.id == s.ID && len(q.lines) > 0
}

func (m *model) startAnswering() {
	m.sessions.focus = sessionsFocusAnswer
	m.sessions.composer.Blur()
}

// stopAnswering gives the keyboard back to the list.
func (m *model) stopAnswering() {
	if m.sessions.focus == sessionsFocusAnswer {
		m.sessions.focus = sessionsFocusList
	}
}

// answerKey sends a key to the question, or with ctrl+k gives the keyboard
// back to the list.
func (m *model) answerKey(msg tea.KeyMsg) tea.Cmd {
	if isAskToggle(msg.String()) { // K is a letter here: ctrl+k
		m.stopAnswering()
		return nil
	}
	keys := claudeKeyBytes(msg)
	if keys == "" {
		return nil
	}
	q := &m.sessions.question
	q.keys = append(q.keys, keys)
	return m.sendClaudeKeys()
}

func (m *model) sendClaudeKeys() tea.Cmd {
	q := &m.sessions.question
	if q.sending || len(q.keys) == 0 || q.id == "" {
		return nil
	}
	q.sending = true
	keys := strings.Join(q.keys, "")
	q.keys = nil
	gen, paneID := q.gen, q.paneID
	return func() tea.Msg {
		return claudeKeysSentMsg{gen: gen, err: weztermSendKeys(paneID, keys)}
	}
}

func (m *model) applyClaudeKeysSent(msg claudeKeysSentMsg) tea.Cmd {
	q := &m.sessions.question
	if msg.gen != q.gen {
		return nil
	}
	q.sending = false
	if msg.err != nil {
		slog.Error("Could not send keys to the session", "error", msg.err)
		m.flashClaude("Couldn't reach its pane: " + msg.err.Error())
		q.keys = nil
		m.stopAnswering()
		return nil
	}
	return tea.Batch(m.sendClaudeKeys(), m.readClaudeQuestion())
}

// claudeKeyBytes is what a terminal sends for a key, for the keys a
// question takes: typing, digits, space, enter, esc, tab, the arrows and
// backspace.
func claudeKeyBytes(msg tea.KeyMsg) string {
	switch msg.Type { //nolint:exhaustive // the keys a question takes; others aren't sent
	case tea.KeyRunes:
		if msg.Alt {
			return ""
		}
		return string(msg.Runes)
	case tea.KeySpace:
		return " "
	case tea.KeyEnter:
		return claudeKeyEnter
	case tea.KeyEsc:
		return "\x1b"
	case tea.KeyTab:
		return "\t"
	case tea.KeyShiftTab:
		return "\x1b[Z"
	case tea.KeyUp:
		return "\x1b[A"
	case tea.KeyDown:
		return "\x1b[B"
	case tea.KeyRight:
		return "\x1b[C"
	case tea.KeyLeft:
		return "\x1b[D"
	case tea.KeyBackspace:
		return "\x7f"
	default:
		return ""
	}
}

// claudeQuestionLines cuts the question out of a Claude Code screen: from
// the rule over its dialog, after the last thing said, to the bottom. over
// reports that the screen shows the prompt box instead.
func claudeQuestionLines(screen string) ([]string, bool) {
	if claudeprompt.Read(screen).Shown {
		return nil, true
	}
	lines := strings.Split(ansi.Strip(strings.ReplaceAll(screen, "\r", "")), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	// The rule under the dialog carries the session's name.
	if n := len(lines); n > 0 && isNamedRule(lines[n-1]) {
		lines = lines[:n-1]
	}
	if len(lines) == 0 {
		return nil, false
	}
	floor := max(0, len(lines)-claudeQuestionLookBack)
	said := floor - 1
	for i := len(lines) - 1; i >= floor; i-- {
		if isSaidLine(lines[i]) {
			said = i
			break
		}
	}
	start := -1
	for i := said + 1; i < len(lines); i++ {
		if isRuleLine(lines[i]) {
			start = i
			break
		}
	}
	if start < 0 {
		start = max(said+1, len(lines)-claudeQuestionMaxLines)
	}
	out := lines[start:]
	for len(out) > 0 && (strings.TrimSpace(out[0]) == "" || isRuleLine(out[0])) {
		out = out[1:]
	}
	if len(out) > claudeQuestionMaxLines {
		out = out[len(out)-claudeQuestionMaxLines:]
	}
	return out, false
}

// isSaidLine reports a line of the conversation: Claude's replies and tool
// calls start with ⏺, their results with ⎿, a finished turn with ✻.
func isSaidLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "⏺") || strings.HasPrefix(t, "⎿") || strings.HasPrefix(t, "✻")
}

func isRuleLine(line string) bool {
	t := strings.TrimSpace(line)
	if len([]rune(t)) < minRuleCells {
		return false
	}
	return strings.Trim(t, "─━▔═") == ""
}

// isNamedRule reports a rule with a name in it: "──── Fix login ─".
func isNamedRule(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "──") && strings.HasSuffix(t, "─") && !isRuleLine(t)
}

// codexQuestionLines cuts the question out of a Codex screen: from the last
// thing Codex did ("• Running …", "• Edited …") to the line that says how to
// answer ("Press enter to confirm or esc to cancel", which a narrow pane
// wraps). The blank lines Codex spaces its dialog with are left out, so its
// options fit. over reports that the screen shows Codex's prompt instead.
func codexQuestionLines(screen string) ([]string, bool) {
	lines := strings.Split(ansi.Strip(strings.ReplaceAll(screen, "\r", "")), "\n")
	end := -1
	for i := len(lines) - 1; i >= 0; i-- {
		lines[i] = strings.TrimRight(lines[i], " ")
		if end < 0 && (strings.Contains(lines[i], "esc to cancel") || strings.Contains(lines[i], "Press enter to")) {
			end = i
		}
	}
	if end < 0 {
		over := false
		for _, l := range lines {
			over = over || strings.Contains(l, "? for shortcuts")
		}
		return nil, over
	}
	for end+1 < len(lines) && !strings.Contains(lines[end], "cancel") && strings.TrimSpace(lines[end+1]) != "" {
		end++
	}
	start := max(0, end-claudeQuestionLookBack+1)
	for i := end; i >= start; i-- {
		if strings.HasPrefix(lines[i], "• ") {
			start = i
			break
		}
	}
	out := make([]string, 0, end+1-start)
	for _, l := range lines[start : end+1] {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	if len(out) > claudeQuestionMaxLines {
		out = out[len(out)-claudeQuestionMaxLines:]
	}
	return out, false
}
