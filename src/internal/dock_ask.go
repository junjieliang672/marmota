package internal

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/internal/ui/statusline"
	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// The Ask box is the right end of the dock, under the preview. Idle, it
// says where your next message goes and how your last one went; K gives it
// the keyboard, and it grows over Details while you write. It writes to the
// session the Sessions box is on, else the one you were last on, else the
// first; with none live, it starts one in this folder. It is sessions
// mode's Ask bar (m.sessions.composer), so the draft, its history and how a
// new session runs are the same in both.

const (
	dockAskMinWidth = 28
	dockAskMaxWidth = 40
)

// dockAskIdleWidth is the Ask box's width while it doesn't have the
// keyboard.
func (m *model) dockAskIdleWidth() int {
	return min(max(m.fullWidth/4, dockAskMinWidth), dockAskMaxWidth) //nolint:mnd // a quarter
}

// dockAskOpen reports whether the Ask box has the keyboard, and so is grown.
func (m *model) dockAskOpen() bool {
	return m.focusPanel == dockAskFocus
}

// dockAskTarget is the session the Ask box writes to: the Sessions box's
// while it has the keyboard, else the one you were last on, else the first;
// "" for a new session.
func (m *model) dockAskTarget() string {
	sessions := m.dockSessions()
	has := func(id string) bool {
		return id != "" && slices.ContainsFunc(sessions, func(s claudesession.Session) bool { return s.ID == id })
	}
	if m.focusPanel == dockSessionsFocus || m.focusPanel == dockAskFocus {
		if id := m.sessions.list.SelectedID(); has(id) && !m.sessions.list.NewSelected() {
			return id
		}
		if m.focusPanel == dockAskFocus {
			return "" // writing to a new session
		}
	}
	if has(m.dock.lastID) {
		return m.dock.lastID
	}
	if len(sessions) > 0 {
		return sessions[0].ID
	}
	return ""
}

// openDockAsk gives the Ask box the keyboard, writing to target ("" for a
// new session in this folder).
func (m *model) openDockAsk(target string) tea.Cmd {
	var cmds []tea.Cmd
	if !m.toggleFooter {
		cmds = append(cmds, m.toggleFooterController())
	}
	m.refreshSessionsList()
	if target == "" || !m.sessions.list.Select(target) {
		m.sessions.list.SelectNew()
	}
	if m.focusPanel != dockAskFocus {
		m.dock.askFrom = m.focusPanel
	}
	if m.focusPanel != dockSessionsFocus && m.focusPanel != dockAskFocus {
		m.dock.prevFocus = m.focusPanel
	}
	m.focusPanel = dockAskFocus
	m.getFocusedFilePanel().isFocused = false
	m.sessions.focus = sessionsFocusAsk
	m.sessions.scroll = 0
	m.sessions.composer.SetEscText("close")
	m.loadAskHistory()
	// What a new session gets when the box keeps Claude Code's choices.
	m.ask.defaultModel, m.ask.defaultEffort = claudeask.Defaults(filepath.Dir(claudeProjectsDir()))
	m.ask.codexModel, _ = codexsession.Defaults(codexHome())
	m.sessions.composer.SetCodexModels(codexsession.Models(codexHome()), m.ask.codexModel)
	// The context sent with a question says what git shows: read it again.
	cmds = append(cmds, m.sessions.composer.Focus(), m.sessionsSelectionChanged(), m.freshGitStatusCmd())
	return tea.Batch(cmds...)
}

// closeDockAsk folds the Ask box back, giving the keyboard to what had it
// before: a panel, or the Sessions box. The draft stays.
func (m *model) closeDockAsk() {
	m.sessions.composer.Blur()
	m.saveAskHistory() // the draft, for the next superfile too
	m.sessions.composer.SetEscText("")
	m.sessions.focus = sessionsFocusList
	if id := m.sessions.list.SelectedID(); id != "" && !m.sessions.list.NewSelected() {
		m.dock.lastID = id
	}
	if m.dock.askFrom == dockSessionsFocus && len(m.dockSessions()) > 0 {
		// Back to the Sessions box, on the session it was on.
		if m.sessions.list.NewSelected() || !m.sessions.list.Select(m.sessions.list.SelectedID()) {
			m.sessions.list.Select(m.dockPick(m.dockSessions()))
		}
		m.focusPanel = dockSessionsFocus
		return
	}
	m.focusPanel = m.dock.prevFocus
	if m.focusPanel == dockAskFocus || m.focusPanel == dockSessionsFocus {
		m.focusPanel = nonePanelFocus
	}
	m.getFocusedFilePanel().isFocused = m.focusPanel == nonePanelFocus
	m.forgetClaudeQuestion()
}

// isAskToggle reports whether key gives back the keyboard you handed to a
// session's question: only an Ask bar key that types nothing, such as
// ctrl+k, since K is a letter there. The Ask bar itself closes with esc:
// ctrl+k deletes to the end of the line in it.
func isAskToggle(key string) bool {
	return slices.Contains(common.Hotkeys.OpenAskBar, key) && len([]rune(key)) > 1
}

// askBackKey is the key that gives back the keyboard handed to a question.
func askBackKey() string {
	for _, k := range common.Hotkeys.OpenAskBar {
		if isAskToggle(k) {
			return k
		}
	}
	return "esc"
}

// dockAskKey handles a key while the Ask box has the keyboard.
func (m *model) dockAskKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, key):
		m.closeDockAsk()
		return nil
	case slices.Contains(common.Hotkeys.ConfirmTyping, key):
		cmd := m.sendAsk()
		if id := m.sessions.pendingSelect; id != "" {
			m.dock.lastID = id // the session it started, once listed
		}
		return cmd
	case slices.Contains(common.Hotkeys.PageUp, key):
		m.scrollSessionsPane(1)
		return nil
	case slices.Contains(common.Hotkeys.PageDown, key):
		m.scrollSessionsPane(-1)
		return nil
	case slices.Contains(common.Hotkeys.AskChooseAgent, key) && !m.askSending().New:
		return nil // a running session's agent is its own
	case (slices.Contains(common.Hotkeys.AskChooseModel, key) || slices.Contains(common.Hotkeys.AskChooseEffort, key)) &&
		!m.askSending().New:
		m.changeSessionChoice(slices.Contains(common.Hotkeys.AskChooseModel, key))
		return nil
	}
	m.sessions.composer.SetRecall(m.askRecall())
	return m.sessions.composer.HandleKey(msg)
}

// askRecall is what up and down step through in the Ask box: the prompts of
// the session it writes to, typed there or sent from superfile, oldest
// first; nil for a new session, which steps through what the box sent.
func (m *model) askRecall() []string {
	s, ok := m.selectedSession()
	if !ok || m.sessions.list.NewSelected() {
		return nil
	}
	var prompts []string
	if m.sessions.convo.id == s.ID {
		for _, t := range m.sessions.convo.turns {
			if p := oneLine(t.Prompt); p != "" {
				prompts = append(prompts, p)
			}
		}
	}
	if len(prompts) == 0 && s.LastPrompt != "" {
		prompts = []string{s.LastPrompt}
	}
	if prompts == nil {
		prompts = []string{} // its own, though it has none yet
	}
	return prompts
}

// mentionInAskDraft adds the cursor's item, or the selection, to the Ask
// draft as mentions (m in a file panel).
func (m *model) mentionInAskDraft() {
	m.loadAskHistory()
	before := len(m.sessions.composer.Mentions())
	m.addPickToQuestion(false)
	if n := len(m.sessions.composer.Mentions()) - before; n > 0 {
		m.flashClaude("Added to the Ask draft; " + firstKey(common.Hotkeys.OpenAskBar) + " to write")
	}
}

// newSessionName says where the Ask box writes when it starts a session:
// "new Codex session in web".
func (m *model) newSessionName() string {
	agent := "Claude Code"
	if m.askStartsCodex() {
		agent = "Codex"
	}
	return "new " + agent + " session in " + folderName(m.sessionsHere())
}

// dockAskRender draws the Ask box, width wide: grown while it has the
// keyboard, else the idle summary.
func (m *model) dockAskRender(width int) string {
	height := m.footerHeight + common.BorderPadding
	open := m.dockAskOpen()
	r := ui.DefaultFooterRenderer(height, width, open)
	target, _ := m.claude.snapshot.Find(m.dockAskTarget())
	title := "Ask"
	if open {
		name := m.newSessionName()
		if s, ok := m.selectedSession(); ok && !m.sessions.list.NewSelected() {
			name = s.DisplayTitle()
		}
		title = "Ask → " + name
		r.SetBorderTitle(title)
		inner := width - common.BorderPadding - 2 //nolint:mnd // 1 space each side
		lines := m.sessions.composer.RenderLines(inner, m.footerHeight, m.askSending())
		lines = append(lines, m.dockAskMore(inner, m.footerHeight-len(lines))...)
		for _, l := range lines {
			r.AddLines(" " + l)
		}
		r.SetBorderInfoItems("Details folded")
		return r.Render()
	}
	m.loadAskHistory() // a draft left last time shows
	r.SetBorderTitle(title)
	r.SetBorderInfoItems(firstKey(common.Hotkeys.OpenAskBar))
	r.AddLines(m.dockAskIdleLines(target, width-common.BorderPadding, m.footerHeight)...)
	return r.Render()
}

// askRecentTurns is how many of your questions the open Ask box can list.
const askRecentTurns = 8

// dockAskMore fills the room under the bar while you write, rows tall: the
// git branch and how many files are changed, then the questions you asked
// the session, newest first, each with when it was replied to. The
// answers themselves stay in the preview.
func (m *model) dockAskMore(width, rows int) []string {
	if rows < 2 { //nolint:mnd // a gap and a line
		return nil
	}
	st := dockStyles()
	out := []string{""}
	if c := m.askContext(); c.InGit {
		out = append(out, st.dim.Render(ansi.Truncate(icon.Git+" "+firstNonEmpty(c.GitBranch, "detached")+
			" · "+strconv.Itoa(c.GitChanged)+" changed", width, "…")))
	}
	s, ok := m.selectedSession()
	if !ok || m.sessions.list.NewSelected() {
		out = append(out, st.dim.Render(ansi.Truncate(
			"Sending starts a session in "+folderName(m.sessionsHere())+" in the background.", width, "…")))
		return out[:min(len(out), rows)]
	}
	turns := m.dock.recentTurns(s)
	if len(turns) == 0 && s.LastPrompt != "" {
		// A turn longer than the transcript tail that is read has its
		// prompt out of reach; the session still knows it.
		turns = []claudesession.Turn{{Prompt: s.LastPrompt}}
	}
	if len(turns) == 0 {
		return append(out, st.dim.Render("Nothing asked yet."))[:min(len(out)+1, rows)]
	}
	now := time.Now()
	state := m.sessionState(s)
	for i := len(turns) - 1; i >= 0 && len(out) < rows; i-- {
		t := turns[i]
		var when string
		switch {
		case i == len(turns)-1 && state == sessionsui.Working:
			when = "working"
		case i == len(turns)-1 && state == sessionsui.NeedsYou:
			when = "asks you"
		case !t.RepliedAt.IsZero():
			when = "replied " + agoWords(now, t.RepliedAt)
		case t.Reply != "":
			when = "replied"
		default:
			when = "no reply"
		}
		right := st.dim.Render(" " + when)
		if when == "working" {
			right = lipgloss.NewStyle().Foreground(lipgloss.Color(claudeColor)).
				Background(common.FooterBGColor).Render(" ✳ working")
		} else if when == "asks you" {
			right = lipgloss.NewStyle().Foreground(sessionsui.StateColor(state)).
				Background(common.FooterBGColor).Render(" ◉ asks you")
		}
		room := width - ansi.StringWidth(right) - 2 //nolint:mnd // "› "
		q := st.val.Render("› " + ansi.Truncate(oneLine(t.Prompt), max(1, room), "…"))
		if gap := width - ansi.StringWidth(q) - ansi.StringWidth(right); gap > 0 {
			q += st.val.Render(strings.Repeat(" ", gap))
		}
		out = append(out, q+right)
	}
	return out
}

// dockAskIdleLines is the folded box, rows tall: where your next message
// goes, which program and model it runs with, then your last message to it
// and how it stands; in the rows left, what it is doing or asking, else
// your messages before. Its replies stay in the preview.
func (m *model) dockAskIdleLines(s claudesession.Session, width, rows int) []string {
	st := dockStyles()
	key := firstKey(common.Hotkeys.OpenAskBar)
	if s.ID == "" {
		lines := []string{st.val.Render(ansi.Truncate("→ "+m.newSessionName(), width, "…")),
			m.dockAgentLine(s, width), st.dim.Render(key + " write")}
		lines = append(lines, m.dockDraftLine(width)...)
		mode := "⏵⏵ " + claudeask.PermissionModeLabel(m.sessions.composer.PermissionMode())
		return fillRows(lines, dimLines(wrapLines("starts in the background, "+mode, width, rows)), rows)
	}
	state := m.sessionState(s)
	verb := " write"
	if state == sessionsui.NeedsYou {
		verb = " answer its question"
	}
	head := []string{st.val.Render(ansi.Truncate("→ "+s.DisplayTitle(), width, "…")),
		m.dockAgentLine(s, width), st.dim.Render(key + verb)}
	head = append(head, m.dockDraftLine(width)...)
	turn, ok := m.dock.lastTurn(s)
	now := time.Now()
	var body []string
	switch {
	case ok:
		body = append(body, st.dim.Render("you, "+agoWords(now, turn.At)+":"))
	case s.LastPrompt != "":
		// A turn longer than the transcript's tail Conversation reads has
		// its prompt out of reach; the session still knows it.
		turn.Prompt = s.LastPrompt
		body = append(body, st.dim.Render("you:"))
	default:
		return fillRows(head, []string{st.dim.Render("nothing sent yet")}, rows)
	}
	// The message gets a second row when how it stands still fits under it.
	promptRows := min(maxIdlePromptRows, max(1, rows-len(head)-len(body)-1))
	for _, l := range wrapLines("“"+oneLine(turn.Prompt)+"”", width, promptRows) {
		body = append(body, st.val.Render(l))
	}
	var more string
	switch state {
	case sessionsui.Working:
		body = append(body, lipgloss.NewStyle().Foreground(lipgloss.Color(claudeColor)).
			Background(common.FooterBGColor).Render("✳ working"))
		more = m.dockDoing(s, state)
	case sessionsui.NeedsYou:
		body = append(body, lipgloss.NewStyle().Foreground(sessionsui.StateColor(state)).
			Background(common.FooterBGColor).Render("◉ asks you"))
		more = firstNonEmpty(s.PendingTool, s.WaitingFor)
	case sessionsui.Unread, sessionsui.Idle, sessionsui.Ended:
		replied := sessionAgeAnchor(s)
		if !turn.RepliedAt.IsZero() {
			replied = turn.RepliedAt
		}
		body = append(body, st.ok.Render("✓ replied "+agoWords(now, replied)))
	}
	if more == "working" {
		more = ""
	}
	lines := fillRows(head, body, rows)
	room := rows - len(lines)
	if room > 0 && more != "" {
		return append(lines, dimLines(wrapLines(more, width, room))...)
	}
	turns := m.dock.recentTurns(s)
	for i := len(turns) - 2; i >= 0 && room > 0; i-- { // the last is above
		if p := oneLine(turns[i].Prompt); p != "" {
			lines = append(lines, st.dim.Render(ansi.Truncate("› "+p, width, "…")))
			room--
		}
	}
	return lines
}

// maxIdlePromptRows is how many rows your last message takes in the folded
// box.
const maxIdlePromptRows = 2

// dockAgentLine is "cc opus 5.5 · high": the program a session runs in and
// the model and effort it answers with; for a new session, the box's
// choices.
func (m *model) dockAgentLine(s claudesession.Session, width int) string {
	st := dockStyles()
	codex, model, effort := s.IsCodex(), s.Model, s.Effort
	if s.ID == "" {
		c := &m.sessions.composer
		codex = c.StartsCodex()
		model, effort = c.Choices()
		switch {
		case codex:
			model, effort = firstNonEmpty(model, m.ask.codexModel), c.CodexStartEffort()
		default:
			model, effort = firstNonEmpty(model, m.ask.defaultModel), firstNonEmpty(effort, m.ask.defaultEffort)
		}
		model, effort = firstNonEmpty(model, "default"), firstNonEmpty(effort, "default")
	} else if ch := m.ask.change; ch.id == s.ID {
		model, effort = firstNonEmpty(ch.model, model), firstNonEmpty(ch.effort, effort)
	}
	var parts []string
	if model != "" {
		parts = append(parts, claudeask.ModelLabel(model))
	}
	if effort != "" {
		parts = append(parts, effort)
	}
	if len(parts) == 0 {
		parts = []string{"Claude Code"}
		if codex {
			parts = []string{"Codex"}
		}
	}
	tag := common.AgentTag(codex, common.FooterBGColor)
	return tag + st.val.Render(ansi.Truncate(" "+strings.Join(parts, " · "), max(1, width-ansi.StringWidth(tag)), "…"))
}

// fillRows is head, a blank row when there is room for it, then body, within
// rows.
func fillRows(head, body []string, rows int) []string {
	lines := append([]string{}, head...)
	if len(head)+1+len(body) <= rows {
		lines = append(lines, "")
	}
	lines = append(lines, body...)
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return lines
}

// dimLines draws lines dim.
func dimLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = dockStyles().dim.Render(l)
	}
	return out
}

// wrapLines wraps text to width over at most rows rows, the last ending in
// "…" when there is more.
func wrapLines(text string, width, rows int) []string {
	if rows <= 0 || width <= 0 {
		return nil
	}
	lines := strings.Split(ansi.Wrap(text, width, ""), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
		lines[rows-1] = ansi.Truncate(strings.TrimRight(lines[rows-1], " ")+" …", width, "…")
	}
	return lines
}

// dockDraftLine says a draft waits, when one does.
func (m *model) dockDraftLine(width int) []string {
	draft := strings.TrimSpace(m.sessions.composer.Value())
	if draft == "" {
		return nil
	}
	return []string{dockStyles().dim.Render(ansi.Truncate("draft: "+oneLine(draft), width, "…"))}
}

type dockStyleSet struct{ val, dim, ok lipgloss.Style }

func dockStyles() dockStyleSet {
	base := lipgloss.NewStyle().Background(common.FooterBGColor)
	return dockStyleSet{
		val: base.Foreground(common.FooterFGColor),
		dim: base.Foreground(lipgloss.Color(common.Theme.FooterBorder)),
		ok:  base.Foreground(lipgloss.Color(common.Theme.Correct)),
	}
}

// dockTurns is a session's recent turns, read again only when its
// transcript changes.
type dockTurns struct {
	path    string
	size    int64
	modTime time.Time
	turns   []claudesession.Turn
}

// recentTurns is the session's last turns, oldest first.
func (d *dockState) recentTurns(s claudesession.Session) []claudesession.Turn {
	if s.TranscriptPath == "" {
		return nil
	}
	info, err := os.Stat(s.TranscriptPath)
	if err != nil {
		return nil
	}
	c := d.turns
	if c.path == s.TranscriptPath && c.size == info.Size() && c.modTime.Equal(info.ModTime()) {
		return c.turns
	}
	turns, _ := sessionConversation(s, askRecentTurns)
	d.turns = dockTurns{path: s.TranscriptPath, size: info.Size(), modTime: info.ModTime(), turns: turns}
	return turns
}

// lastTurn is the session's last turn, when there is one to read.
func (d *dockState) lastTurn(s claudesession.Session) (claudesession.Turn, bool) {
	turns := d.recentTurns(s)
	if len(turns) == 0 {
		return claudesession.Turn{}, false
	}
	return turns[len(turns)-1], true
}

// agoWords is how long ago t was: "just now", "4m ago", "2h ago".
func agoWords(now, t time.Time) string {
	if age := shortAge(now, t); age != "" && age != "now" {
		return age + " ago"
	}
	return "just now"
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// dockAskStatusLine is the status line while the Ask box has the keyboard.
func (m *model) dockAskStatusLine() string {
	keys := []statusline.Hotkey{
		{Key: "enter", Description: "send"},
		{Key: "↑↓", Description: "history"},
		{Key: firstKey(common.Hotkeys.AskClear), Description: "clear"},
	}
	if !m.askSending().Fixed {
		// What a new session starts with; a running one keeps its own.
		keys = append(keys,
			statusline.Hotkey{Key: firstKey(common.Hotkeys.AskChooseModel), Description: "model"},
			statusline.Hotkey{Key: firstKey(common.Hotkeys.AskChooseEffort), Description: "effort"},
			statusline.Hotkey{Key: firstKey(common.Hotkeys.AskChoosePermissionMode), Description: "permissions"})
	}
	keys = append(keys, statusline.Hotkey{Key: "esc", Description: "close, keeps the draft"})
	return m.statusLineModel.RenderModeHotkeys("Ask", keys)
}
