package internal

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/internal/ui/statusline"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// The Sessions box is the left of the dock at the foot of the screen: the
// live sessions, two lines each. ! gives it the keyboard, on the session
// that most recently started needing you, else the one you were last on,
// else the first; ! or esc gives the keyboard back. While it has the
// keyboard, the preview shows the selected session, and its question when
// it asks one. It moves m.sessions.list.

// dockState is what the Sessions box keeps between visits.
type dockState struct {
	// lastID is the session the box was last on, for the next !.
	lastID string
	// prevFocus is what had the keyboard before !, to give it back to.
	prevFocus focusPanelType
	// jumpCount is a count typed before g or G.
	jumpCount string
	// askFrom is what had the keyboard when the Ask box took it: esc goes
	// back there, the Sessions box included.
	askFrom focusPanelType
	// turns is the Ask box's session's recent turns, read when its
	// transcript changes.
	turns dockTurns
	// pinned is V: the preview shows the session the box is on while the
	// keyboard is in the files.
	pinned bool
}

const (
	dockSessionsMinWidth = 30
	dockSessionsMaxWidth = 56
	dockRowLines         = 2
)

// dockSessionsWidth is the Sessions box's width: a third of the screen,
// within limits.
func (m *model) dockSessionsWidth() int {
	return min(max(m.fullWidth/3, dockSessionsMinWidth), dockSessionsMaxWidth) //nolint:mnd // a third
}

// sessionsShown reports whether a session is on screen to watch: sessions
// mode, or the Sessions box with the keyboard.
func (m *model) sessionsShown() bool {
	return m.focusPanel == dockSessionsFocus || m.focusPanel == dockAskFocus || m.viewerPinned()
}

// dockSessions is the sessions the box lists, in the list's order.
func (m *model) dockSessions() []claudesession.Session {
	groups, _ := m.sessionsGroups()
	var out []claudesession.Session
	for _, g := range groups {
		out = append(out, g.Sessions...)
	}
	return out
}

// dockPick is where ! lands: the session a toast is about, the newest news;
// else the session that most recently started needing you, else the one the
// box was last on, else the first.
func (m *model) dockPick(sessions []claudesession.Session) string {
	if t := m.claude.toast; t != nil && m.claudeToastDue(*t) &&
		slices.ContainsFunc(sessions, func(s claudesession.Session) bool { return s.ID == t.id }) {
		return t.id
	}
	var newest string
	var newestAt time.Time
	for _, s := range sessions {
		if m.sessionState(s) != sessionsui.NeedsYou {
			continue
		}
		if at := m.claude.waitingSince[s.ID]; newest == "" || at.After(newestAt) {
			newest, newestAt = s.ID, at
		}
	}
	if newest != "" {
		return newest
	}
	for _, s := range sessions {
		if s.ID == m.dock.lastID {
			return s.ID
		}
	}
	if len(sessions) > 0 {
		return sessions[0].ID
	}
	return ""
}

// focusDockSessions gives the Sessions box the keyboard, showing the dock
// first when it is hidden.
func (m *model) focusDockSessions() tea.Cmd {
	return m.focusDockSessionsOn("")
}

// focusDockSessionsOn gives the Sessions box the keyboard on session id, or
// with id "", on the one ! picks.
func (m *model) focusDockSessionsOn(id string) tea.Cmd {
	var cmds []tea.Cmd
	if !m.toggleFooter {
		cmds = append(cmds, m.toggleFooterController())
	}
	m.refreshSessionsList()
	if id == "" || !slices.ContainsFunc(m.dockSessions(), func(s claudesession.Session) bool { return s.ID == id }) {
		id = m.dockPick(m.dockSessions())
	}
	if id == "" {
		m.flashClaude("No live sessions. Start one with " + firstKey(common.Hotkeys.OpenAgentPrompt) + "claude or " +
			firstKey(common.Hotkeys.OpenAgentPrompt) + "codex")
		return tea.Batch(cmds...)
	}
	m.sessions.list.Select(id)
	m.claude.toast = nil // ! answers it
	if m.focusPanel != dockSessionsFocus {
		m.dock.prevFocus = m.focusPanel
	}
	m.focusPanel = dockSessionsFocus
	m.getFocusedFilePanel().isFocused = false
	m.sessions.scroll = 0
	return tea.Batch(append(cmds, m.sessionsSelectionChanged())...)
}

// stepDockSession gives the Sessions box the keyboard on the next session
// (step 1) or the previous one (-1): from the files, next to the session the
// box was last on, else the first or the last. It goes round at the ends.
func (m *model) stepDockSession(step int) tea.Cmd {
	m.refreshSessionsList()
	sessions := m.dockSessions()
	if len(sessions) == 0 {
		return m.focusDockSessions() // says how to start one
	}
	from := m.dock.lastID
	if m.focusPanel == dockSessionsFocus {
		from = m.sessions.list.SelectedID()
	}
	at := slices.IndexFunc(sessions, func(s claudesession.Session) bool { return s.ID == from })
	switch {
	case at < 0 && step > 0:
		at = 0
	case at < 0:
		at = len(sessions) - 1
	default:
		at = ((at+step)%len(sessions) + len(sessions)) % len(sessions)
	}
	return m.focusDockSessionsOn(sessions[at].ID)
}

// leaveDockSessions gives the keyboard back to what had it before !.
func (m *model) leaveDockSessions() {
	if m.sessions.list.IsRenaming() {
		m.sessions.list.CancelRename()
	}
	m.stopAnswering()
	m.dock.lastID = m.sessions.list.SelectedID()
	m.focusPanel = m.dock.prevFocus
	if m.focusPanel == dockSessionsFocus {
		m.focusPanel = nonePanelFocus
	}
	m.getFocusedFilePanel().isFocused = m.focusPanel == nonePanelFocus
	if !m.viewerPinned() {
		m.forgetClaudeQuestion() // a pinned session's question stays in the preview
	}
}

// dockSessionsKey handles a key while the Sessions box has the keyboard.
//
//nolint:cyclop,funlen // one case per key of the box
func (m *model) dockSessionsKey(msg tea.KeyMsg) tea.Cmd {
	if m.sessions.focus == sessionsFocusAnswer {
		return m.answerKey(msg)
	}
	list := &m.sessions.list
	if list.IsRenaming() {
		return sessionsTypingKey(msg, list.UpdateRename, list.CancelRename, func() tea.Cmd {
			return m.renameClaudeSession(list.EndRename())
		})
	}
	key := msg.String()
	if msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] >= '0' && msg.Runes[0] <= '9' &&
		(m.dock.jumpCount != "" || msg.Runes[0] != '0') {
		m.dock.jumpCount += string(msg.Runes)
		return nil
	}
	count, _ := strconv.Atoi(m.dock.jumpCount)
	m.dock.jumpCount = ""

	sessions := m.dockSessions()
	at := slices.IndexFunc(sessions, func(s claudesession.Session) bool { return s.ID == list.SelectedID() })
	prev := list.SelectedID()
	selectAt := func(i int) {
		if len(sessions) > 0 {
			list.Select(sessions[min(max(i, 0), len(sessions)-1)].ID)
		}
	}
	switch {
	case slices.Contains(common.Hotkeys.JumpToUnreadSession, key), isLeaveKey(key):
		m.leaveDockSessions()
		return nil
	case slices.Contains(common.Hotkeys.NextSession, key):
		return m.stepDockSession(1)
	case slices.Contains(common.Hotkeys.PreviousSession, key):
		return m.stepDockSession(-1)
	case slices.Contains(common.Hotkeys.ListUp, key):
		selectAt(at - 1)
	case slices.Contains(common.Hotkeys.ListDown, key):
		selectAt(at + 1)
	case slices.Contains(common.Hotkeys.ListTop, key):
		selectAt(max(count, 1) - 1)
	case slices.Contains(common.Hotkeys.ListBottom, key):
		if count > 0 {
			selectAt(count - 1)
		} else {
			selectAt(len(sessions) - 1)
		}
	case slices.Contains(common.Hotkeys.PageUp, key):
		m.scrollSessionsPane(1)
	case slices.Contains(common.Hotkeys.PageDown, key):
		m.scrollSessionsPane(-1)
	case slices.Contains(common.Hotkeys.OpenAskBar, key):
		if s, ok := m.selectedSession(); ok && m.canAnswerClaudeQuestion(s) {
			m.startAnswering()
			return nil
		}
		return m.openDockAsk(prev)
	case slices.Contains(common.Hotkeys.SessionNew, key):
		return m.openDockAsk("")
	case slices.Contains(common.Hotkeys.Confirm, key):
		m.withSelectedSession(m.focusClaudeSession)
	case slices.Contains(common.Hotkeys.SessionMention, key):
		m.withSelectedSession(m.mentionClaudeSession)
	case slices.Contains(common.Hotkeys.SessionRename, key):
		if _, ok := list.Selected(); ok {
			return list.StartRename()
		}
	case slices.Contains(common.Hotkeys.SessionFork, key):
		m.withSelectedSession(m.forkClaudeSession)
	case slices.Contains(common.Hotkeys.SessionStop, key):
		m.withSelectedSession(m.confirmStopClaudeSession)
	case slices.Contains(common.Hotkeys.MoveSessionHere, key):
		return m.moveSelectedSessionHere()
	case slices.Contains(common.Hotkeys.ShowSessionFiles, key):
		return m.toggleSessionFiles()
	case slices.Contains(common.Hotkeys.PinViewer, key):
		return m.togglePinViewer()
	case slices.Contains(common.Hotkeys.FocusOnSidebar, key):
		// The s and z popups go to folders: they act on the files.
		m.leaveDockSessions()
		m.focusOnSideBar()
		return nil
	case slices.Contains(common.Hotkeys.OpenZoxide, key):
		m.leaveDockSessions()
		return m.zoxideModal.Open()
	}
	if list.SelectedID() != prev {
		m.sessions.scroll = 0
		return m.sessionsSelectionChanged()
	}
	return nil
}

// dockRender is the dock: the Sessions box, Details and the Ask box; the
// Ask box takes Details' room while it has the keyboard.
func (m *model) dockRender() string {
	if m.dockAskOpen() {
		return lipgloss.JoinHorizontal(lipgloss.Top, m.dockSessionsRender(),
			m.dockAskRender(m.fullWidth-m.dockSessionsWidth()))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.dockSessionsRender(), m.detailsRender(),
		m.dockAskRender(m.dockAskIdleWidth()))
}

// dockSessionsRender draws the Sessions box.
func (m *model) dockSessionsRender() string {
	width, height := m.dockSessionsWidth(), m.footerHeight+common.BorderPadding
	focused := m.focusPanel == dockSessionsFocus
	r := ui.DefaultFooterRenderer(height, width, focused)
	r.SetBorderTitleGlyph("✳", lipgloss.Color(claudeColor))
	r.SetBorderTitle("Sessions")
	hint := firstKey(common.Hotkeys.JumpToUnreadSession) + " go"
	if focused {
		hint = firstKey(common.Hotkeys.JumpToUnreadSession) + " back"
	}
	r.SetBorderInfoItems(hint)

	inner := width - common.BorderPadding
	view := height - common.BorderPadding
	sessions := m.dockSessions()
	if len(sessions) == 0 {
		r.AddLines(dockDim(" No live sessions"), dockDim(" "+firstKey(common.Hotkeys.OpenAgentPrompt)+"claude or "+
			firstKey(common.Hotkeys.OpenAgentPrompt)+"codex starts one"))
		return r.Render()
	}
	now := time.Now()
	sel := m.sessions.list.SelectedID()
	var lines []string
	selLine := 0
	for i, s := range sessions {
		selected := focused && s.ID == sel
		if selected {
			selLine = len(lines)
		}
		lines = append(lines, m.dockRow(i+1, s, selected, inner, now)...)
	}
	// Keep the selected session in view.
	top := 0
	if selLine+dockRowLines > view {
		top = selLine + dockRowLines - view
	}
	r.AddLines(lines[top:min(len(lines), top+view)]...)
	return r.Render()
}

// claudeColor is the ✳ orange superfile uses for Claude.
const claudeColor = "#D97757"

// dockRow is a session's two lines: number, state, agent (cc, co), title
// and age; then what it is doing or asking.
func (m *model) dockRow(n int, s claudesession.Session, selected bool, width int, now time.Time) []string {
	bg := common.FilePanelBGColor
	if selected && common.FilePanelCursorRowBGColor != "" {
		bg = common.FilePanelCursorRowBGColor
	}
	base := lipgloss.NewStyle().Background(bg).Foreground(common.FooterFGColor)
	dim := base.Foreground(lipgloss.Color(common.Theme.FooterBorder))
	state := m.sessionState(s)
	// The selected session has a bar down its left edge, on both its lines;
	// the cursor glyph sat on top of the number.
	mark := base.Render("  ")
	if selected {
		mark = base.Foreground(lipgloss.Color(common.Theme.FilePanelBorderActive)).Render("▌") + base.Render(" ")
	}
	prefix := mark + dim.Render(strconv.Itoa(n)+" ") + sessionsui.Glyph(state, bg) + base.Render(" ") +
		common.AgentTag(s.IsCodex(), bg) + base.Render(" ")
	age := dim.Render(shortAge(now, sessionAgeAnchor(s)))

	titleStyle := base
	if selected {
		titleStyle = titleStyle.Bold(true)
	}
	if state == sessionsui.NeedsYou {
		titleStyle = titleStyle.Foreground(sessionsui.StateColor(state))
	}
	var title string
	if m.sessions.list.IsRenaming() && selected {
		title = m.sessions.list.RenameView(max(1, width-ansi.StringWidth(prefix)-1))
	} else {
		room := width - ansi.StringWidth(prefix) - ansi.StringWidth(age) - 1
		title = titleStyle.Render(ansi.Truncate(s.DisplayTitle(), max(1, room), "…"))
	}
	first := prefix + title
	if gap := width - ansi.StringWidth(first) - ansi.StringWidth(age); gap > 0 {
		first += base.Render(strings.Repeat(" ", gap))
	}
	first += age

	doingStyle := dim
	if state == sessionsui.NeedsYou {
		doingStyle = base.Foreground(sessionsui.StateColor(state))
	}
	indent := mark + base.Render(strings.Repeat(" ", ansi.StringWidth(prefix)-ansi.StringWidth(mark)))
	second := indent + doingStyle.Render(ansi.Truncate(m.dockDoing(s, state), max(1, width-ansi.StringWidth(indent)), "…"))
	return []string{dockFill(first, width, base), dockFill(second, width, base)}
}

// dockDoing is what a session is doing, in a few words.
func (m *model) dockDoing(s claudesession.Session, state sessionsui.State) string {
	switch state {
	case sessionsui.NeedsYou:
		return "asks: " + firstNonEmpty(s.PendingTool, s.WaitingFor, "a question")
	case sessionsui.Working:
		return firstNonEmpty(s.PendingTool, "working")
	case sessionsui.Unread:
		return "replied, unread"
	case sessionsui.Idle:
		return "waiting for your next prompt"
	case sessionsui.Ended:
	}
	return "finished"
}

func dockFill(s string, width int, st lipgloss.Style) string {
	if gap := width - ansi.StringWidth(s); gap > 0 {
		return s + st.Render(strings.Repeat(" ", gap))
	}
	return ansi.Truncate(s, width, "")
}

func dockDim(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(common.Theme.FooterBorder)).
		Background(common.FooterBGColor).Render(s)
}

// dockSessionPreview is the preview while the Sessions box has the
// keyboard: the selected session.
func (m *model) dockSessionPreview(width, height int) string {
	v := m.sessionsPaneView()
	if key := firstKey(common.Hotkeys.PinViewer); key != "" {
		v.Hint = key + " pin"
		if m.viewerPinned() {
			v.Pinned, v.Hint = "pinned", key+" unpin"
		}
	}
	return askui.RenderSession(v, "", width, height, m.sessions.scroll,
		m.sessions.focus == sessionsFocusAnswer)
}

// dockStatusLine is the status line while the Sessions box has the
// keyboard.
func (m *model) dockStatusLine() string {
	ask := firstKey(common.Hotkeys.OpenAskBar)
	keys := []statusline.Hotkey{
		{Key: ask, Description: "answer/write"},
		{Key: firstKey(common.Hotkeys.JumpToUnreadSession) + "/esc", Description: "files"},
		{Key: firstKey(common.Hotkeys.Confirm), Description: "open beside"},
		{Key: firstKey(common.Hotkeys.SessionMention), Description: "mention"},
		{Key: firstKey(common.Hotkeys.SessionNew), Description: "new"},
		{Key: firstKey(common.Hotkeys.SessionRename), Description: "rename"},
		{Key: firstKey(common.Hotkeys.SessionFork), Description: "fork"},
		{Key: firstKey(common.Hotkeys.SessionStop), Description: "stop"},
		{Key: firstKey(common.Hotkeys.FocusOnSidebar) + "/" + firstKey(common.Hotkeys.OpenZoxide), Description: "go"},
	}
	switch {
	case m.sessions.focus == sessionsFocusAnswer:
		keys = []statusline.Hotkey{
			{Key: "1-9", Description: "choose"}, {Key: "↑↓", Description: "move"},
			{Key: "enter", Description: "confirm"}, {Key: "esc", Description: "cancel the question"},
			{Key: askBackKey(), Description: "stop answering"},
		}
	case m.sessions.list.IsRenaming():
		keys = []statusline.Hotkey{{Key: "enter", Description: "rename"}, {Key: "esc", Description: "cancel"}}
	}
	return m.statusLineModel.RenderHotkeys(keys)
}
