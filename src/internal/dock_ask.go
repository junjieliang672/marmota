package internal

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/internal/ui/statusline"
	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// The Ask box is under the preview, as wide as it, above the Sessions box:
// three rows that say where your next message goes (its top edge), what you
// last asked and how that stands (its line), and what it runs with (its
// bottom edge). K gives it the keyboard and you write in that same line; the
// box grows a row with each line more of what is written, on rows of the
// preview's (layout.go). It writes to the session the Sessions box is on,
// else the one you were last on, else the first; with none live, it starts
// one in this folder. Its draft is m.sessions.composer.

// dockAskOpen reports whether the Ask box has the keyboard: it is written in.
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
	m.ask.resume = nil
	m.sessions.composer.Blur()
	m.saveAskHistory() // the draft, for the next Marmota too
	m.sessions.composer.SetEscText("")
	m.sessions.focus = sessionsFocusList
	if id := m.sessions.list.SelectedID(); id != "" && !m.sessions.list.NewSelected() {
		m.dock.lastID = id
	}
	if m.dock.askFrom == dockSessionsFocus && len(m.dockSessions()) > 0 {
		// Back to the Sessions box, on the session it was on.
		if m.sessions.list.NewSelected() || !m.sessions.list.Select(m.sessions.list.SelectedID()) {
			m.sessions.list.Select(m.dockPick(m.dockSessions(), ""))
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
	if m.ask.resume != nil && (slices.Contains(common.Hotkeys.AskChooseAgent, key) ||
		slices.Contains(common.Hotkeys.AskClear, key)) {
		m.ask.resume = nil // the other agent, or a clean start: a new session again
	}
	switch {
	case slices.Contains(common.Hotkeys.AskResume, key):
		if m.askSending().New {
			return m.openJumpForResume()
		}
		return nil
	case slices.Contains(common.Hotkeys.CancelTyping, key):
		m.closeDockAsk()
		return nil
	case slices.Contains(common.Hotkeys.AskEdit, key):
		return m.editAskDraft()
	case slices.Contains(common.Hotkeys.ConfirmTyping, key):
		cmd := m.sendAsk()
		if id := m.sessions.pendingSelect; id != "" {
			m.dock.lastID = id // the session it started, once listed
		}
		return cmd
	case slices.Contains(common.Hotkeys.PageUp, key):
		return m.scrollSessionsPane(1)
	case slices.Contains(common.Hotkeys.PageDown, key):
		return m.scrollSessionsPane(-1)
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
// the session it writes to, typed there or sent from Marmota, oldest
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

// askSidePadding is the space each side of the Ask box's lines, inside its
// frame. askEdgeChrome is what the bottom edge puts around its words: a
// space each side and a piece of the edge after them.
const (
	askSidePadding = 1
	askEdgeChrome  = 3
)

// askInner is the width of the Ask box's lines in a box width wide.
func askInner(width int) int {
	return max(1, width-common.BorderPadding-2*askSidePadding)
}

// dockAskLineCount is how many lines the Ask box has to show, width wide:
// one, or while it is written in those of what is written.
func (m *model) dockAskLineCount(width int) int {
	if !m.dockAskOpen() {
		return 1 // without drawing it: this is asked after every message
	}
	return len(m.dockAskLines(askInner(width), askMaxLines))
}

// dockAskShown is the session the Ask box says it writes to: the one being
// written to while it has the keyboard, else the one K would write to; one
// without an ID for a new session.
func (m *model) dockAskShown() claudesession.Session {
	if !m.dockAskOpen() {
		s, _ := m.findSession(m.dockAskTarget())
		return s
	}
	if s, ok := m.selectedSession(); ok && !m.sessions.list.NewSelected() {
		return s
	}
	return claudesession.Session{}
}

// dockAskRender draws the Ask box: who your next message goes to on its top
// edge, what it runs with on its bottom edge, and between them what you
// write, else what you last asked and how that stands.
func (m *model) dockAskRender() string {
	box := m.lay.ask
	if !box.shown() {
		return ""
	}
	r := ui.DefaultFooterRenderer(box.h, box.w, m.dockAskOpen())
	target := m.dockAskShown()
	m.loadAskHistory() // a draft left last time shows
	name := m.newSessionName()
	if target.ID != "" {
		name = target.DisplayTitle()
	}
	r.SetBorderTitle("Ask → " + name)
	r.SetBorderInfoItems(m.dockAskEdge(target, box.w-common.BorderPadding-askEdgeChrome))
	for _, l := range m.dockAskLines(askInner(box.w), box.h-common.BorderPadding) {
		r.AddLines(strings.Repeat(" ", askSidePadding) + l)
	}
	return r.Render()
}

// dockAskLines is the Ask box's lines, width wide, on rows rows at most:
// while it is written in, what is written and, when sending does something
// other than usual, what; else the one line dockAskIdleLine draws.
func (m *model) dockAskLines(width, rows int) []string {
	rows = max(1, rows)
	if !m.dockAskOpen() {
		return []string{m.dockAskIdleLine(m.dockAskShown(), width)}
	}
	var note []string
	if sending := m.askSending(); !sending.Usual && sending.Text != "" && rows > 1 {
		note = []string{dockStyles().dim.Render(ansi.Truncate("→ "+sending.Text, width, "…"))}
	}
	return append(m.sessions.composer.DraftLines(width, rows-len(note)), note...)
}

// dockAskEdge is the Ask box's bottom edge, within width: the program,
// model and effort a message runs with, the computer, and the key that
// writes; for a new session its permission mode in the computer's place.
// Short of room the computer goes first, then what it runs with.
func (m *model) dockAskEdge(s claudesession.Session, width int) string {
	where := ansi.Strip(dockHostLine(s.Host, width))
	if s.ID == "" {
		where = "⏵⏵ " + claudeask.PermissionModeLabel(m.sessions.composer.PermissionMode())
	}
	parts := []string{ansi.Strip(m.dockAgentLine(s, width)), where}
	switch key := firstKey(common.Hotkeys.OpenAskBar); {
	case s.App != nil:
		// Written to in the Claude app: which computer and model aren't known here.
		parts = []string{ansi.Strip(common.HostMark(remoteSectionTitle, common.FooterBGColor)) + " on another computer",
			"enter: open in Claude app"}
	case m.dockAskOpen():
		if m.askSending().Changed {
			parts = append(parts, "from your next message")
		}
	case s.ID != "" && m.sessionState(s) == sessionsui.NeedsYou:
		parts = append(parts, key+" answer")
	default:
		parts = append(parts, key+" write")
	}
	for len(parts) > 1 && ansi.StringWidth(strings.Join(parts, " · ")) > width {
		drop := min(1, len(parts)-2) //nolint:mnd // the last says the key, and stays
		parts = slices.Delete(parts, drop, drop+1)
	}
	return ansi.Truncate(strings.Join(parts, " · "), width, "…")
}

// dockAskIdleLine is the Ask box's line while it isn't written in, width
// wide: a draft that waits, else your last message to the session and, at
// its right, how that stands: working, what it asks you, when it replied.
// Its replies stay in the preview.
func (m *model) dockAskIdleLine(s claudesession.Session, width int) string {
	st := dockStyles()
	draft := ""
	if text := strings.TrimSpace(m.sessions.composer.Value()); text != "" {
		draft = st.dim.Render("draft: " + oneLine(text))
	}
	left, right := draft, ""
	switch {
	case s.ID == "":
		left = firstNonEmpty(draft, st.dim.Render("starts in the background"))
	case s.App != nil && draft == "":
		left = st.dim.Render(firstNonEmpty(s.App.Doing, "its conversation is in the Claude app"))
		if m.sessionState(s) == sessionsui.NeedsYou {
			left = lipgloss.NewStyle().Foreground(sessionsui.StateColor(sessionsui.NeedsYou)).
				Background(common.FooterBGColor).Render(ansi.Strip(left))
		}
	case s.App == nil:
		left, right = m.dockAskLastTurn(s, width, draft)
	}
	room := width - ansi.StringWidth(right)
	if right != "" {
		room -= 2 // the gap before it
	}
	left = ansi.Truncate(left, max(1, room), st.dim.Render("…"))
	gap := max(0, width-ansi.StringWidth(left)-ansi.StringWidth(right))
	return left + st.val.Render(strings.Repeat(" ", gap)) + right
}

// dockAskLastTurn is your last message to a running session, or the draft
// that waits when there is one, and how the message stands, for a line
// width wide.
func (m *model) dockAskLastTurn(s claudesession.Session, width int, draft string) (string, string) {
	st := dockStyles()
	now := time.Now()
	left, right := st.dim.Render("nothing sent yet"), ""
	turn, ok := lastOf(m.askTurns(s))
	switch {
	case draft != "":
		left = draft
	case ok:
		left = st.dim.Render("you, "+agoWords(now, turn.At)+": ") + st.val.Render("“"+oneLine(turn.Prompt)+"”")
	case s.LastPrompt != "":
		// A turn longer than the transcript's tail Conversation reads has
		// its prompt out of reach; the session still knows it.
		left = st.dim.Render("you: ") + st.val.Render("“"+oneLine(s.LastPrompt)+"”")
	}
	switch state := m.sessionState(s); state {
	case sessionsui.Working:
		right = lipgloss.NewStyle().Foreground(lipgloss.Color(claudeColor)).
			Background(common.FooterBGColor).Render("✳ working")
	case sessionsui.NeedsYou:
		asks := "◉ asks you"
		if what := firstNonEmpty(s.PendingTool, s.WaitingFor); what != "" {
			asks += ": " + oneLine(what)
		}
		// Half the line, and what your message leaves of the other half.
		room := max(width/2, width-ansi.StringWidth(left)-2) //nolint:mnd // half; the gap between them
		right = lipgloss.NewStyle().Foreground(sessionsui.StateColor(state)).
			Background(common.FooterBGColor).Render(ansi.Truncate(asks, max(1, room), "…"))
	case sessionsui.Unread, sessionsui.Idle, sessionsui.Ended:
		if ok && !turn.RepliedAt.IsZero() {
			right = st.dim.Render("replied " + agoWords(now, turn.RepliedAt))
		}
	}
	return left, right
}

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

type dockStyleSet struct{ val, dim, ok lipgloss.Style }

func dockStyles() dockStyleSet {
	base := lipgloss.NewStyle().Background(common.FooterBGColor)
	return dockStyleSet{
		val: base.Foreground(common.FooterFGColor),
		dim: base.Foreground(lipgloss.Color(common.Theme.FooterBorder)),
		ok:  base.Foreground(lipgloss.Color(common.Theme.Correct)),
	}
}

// askRecentTurns is how many of a session's last turns the Ask box reads.
const askRecentTurns = 8

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

// askTurns is the last turns of the session the Ask box writes to: the
// preview's, when it shows that session, which it reads off the screen's
// loop (none while it does), else read here.
func (m *model) askTurns(s claudesession.Session) []claudesession.Turn {
	if c := m.sessions.convo; m.sessionsShown() && c.id == s.ID && c.path == s.TranscriptPath {
		if c.reading {
			return nil
		}
		return c.turns[max(0, len(c.turns)-askRecentTurns):]
	}
	return m.dock.recentTurns(s)
}

// lastOf is the last of turns, when there is one.
func lastOf(turns []claudesession.Turn) (claudesession.Turn, bool) {
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

// dockAskStatusLine is the status line while the Ask box has the keyboard:
// its keys, which the box itself has no room for. As many as fit show.
func (m *model) dockAskStatusLine() string {
	return m.statusLineModel.RenderModeHotkeys("Ask", m.dockAskKeys())
}

// dockAskKeys are the keys the Ask box takes while it is written in, the
// ones that are its own first: what a new session starts with, then those
// every text field has.
func (m *model) dockAskKeys() []statusline.Hotkey {
	sending := m.askSending()
	keys := []statusline.Hotkey{{Key: "enter", Description: "send"}} //nolint:goconst // the key's name
	if sending.New {
		other := "Codex"
		if m.askStartsCodex() {
			other = "Claude Code"
		}
		keys = append(keys, statusline.Hotkey{Key: firstKey(common.Hotkeys.AskChooseAgent), Description: other})
	}
	if !sending.Fixed || sending.Change {
		// What a new session starts with, or a running Codex session's next message.
		keys = append(keys,
			statusline.Hotkey{Key: firstKey(common.Hotkeys.AskChooseModel), Description: "model"},
			statusline.Hotkey{Key: firstKey(common.Hotkeys.AskChooseEffort), Description: "effort"})
	}
	if !sending.Fixed {
		keys = append(keys,
			statusline.Hotkey{Key: firstKey(common.Hotkeys.AskChoosePermissionMode), Description: "permissions"})
	}
	if sending.New {
		resume := "resume one"
		if sending.Resume != "" {
			resume = "another"
		}
		keys = append(keys, statusline.Hotkey{Key: firstKey(common.Hotkeys.AskResume), Description: resume})
	}
	return append(keys,
		statusline.Hotkey{Key: "esc", Description: "close"},
		statusline.Hotkey{Key: "shift+enter", Description: "new line"},
		statusline.Hotkey{Key: "↑↓", Description: "history"},
		statusline.Hotkey{Key: firstKey(common.Hotkeys.AskEdit), Description: "editor"},
		statusline.Hotkey{Key: firstKey(common.Hotkeys.AskClear), Description: "clear"})
}

// dockHostLine is the Ask box's line for the computer the session runs on
// (host, or "" for this one): "󰍹 kingjj", "󰅣 mac-mini".
func dockHostLine(host string, width int) string {
	st := dockStyles()
	name := host
	if name == "" {
		name = thisComputerName()
	}
	mark := common.HostMark(host, common.FooterBGColor)
	if mark != "" {
		mark += st.dim.Render(" ")
	}
	return ansi.Truncate(mark+st.val.Render(name), width, "…")
}

var (
	thisComputerOnce sync.Once //nolint:gochecknoglobals // read once
	thisComputer     string    //nolint:gochecknoglobals // read once
)

// thisComputerName is this computer's name: computer_name, or its hostname
// without the domain, "kingjj" for kingjj.local.
func thisComputerName() string {
	if name := strings.TrimSpace(common.Config.ComputerName); name != "" {
		return name
	}
	thisComputerOnce.Do(func() {
		name, err := os.Hostname()
		if err != nil {
			return
		}
		thisComputer, _, _ = strings.Cut(name, ".")
	})
	return thisComputer
}
