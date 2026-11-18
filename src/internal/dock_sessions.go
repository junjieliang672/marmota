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
	// popupFrom is set while the s or z popup the box opened is open:
	// closed without going anywhere, it gives the keyboard back to the box.
	popupFrom bool
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

// dockSessions is the sessions the box lists, in the list's order: this
// computer's, then the Remote section's.
func (m *model) dockSessions() []claudesession.Session {
	groups, _ := m.sessionsGroups()
	var out []claudesession.Session
	for _, g := range groups {
		out = append(out, g.Sessions...)
	}
	return append(out, m.appSessions()...)
}

// dockPick is where ! lands: the first session below from that needs you
// or replied unread, round at the end, so ! again goes on to the next one;
// with from "" the first of those. With none, the first session.
func (m *model) dockPick(sessions []claudesession.Session, from string) string {
	if len(sessions) == 0 {
		return ""
	}
	at := slices.IndexFunc(sessions, func(s claudesession.Session) bool { return s.ID == from }) // -1: the top
	for i := 1; i <= len(sessions); i++ {
		s := sessions[(at+i)%len(sessions)]
		if state := m.sessionState(s); state == sessionsui.NeedsYou || state == sessionsui.Unread {
			return s.ID
		}
	}
	return sessions[0].ID
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
		from := ""
		if m.focusPanel == dockSessionsFocus {
			from = m.sessions.list.SelectedID()
		}
		id = m.dockPick(m.dockSessions(), from)
	}
	if id == "" {
		m.flashClaude("No live sessions. Start one with " + firstKey(common.Hotkeys.OpenAgentPrompt) + "claude or " +
			firstKey(common.Hotkeys.OpenAgentPrompt) + "codex")
		return tea.Batch(cmds...)
	}
	m.sessions.list.Select(id)
	if t := m.claude.toast; t != nil && t.id == id {
		m.claude.toast = nil // seen
	}
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

	if st := m.unlistedStart(list.SelectedID()); st != nil && m.startingSessionKey(st, key) {
		return nil
	}
	if s, ok := m.appSessionByID(list.SelectedID()); ok {
		if cmd, took := m.appKey(s, key); took {
			return cmd
		}
	}
	sessions := m.dockSessions()
	at := slices.IndexFunc(sessions, func(s claudesession.Session) bool { return s.ID == list.SelectedID() })
	prev := list.SelectedID()
	selectAt := func(i int) {
		if len(sessions) > 0 {
			list.Select(sessions[min(max(i, 0), len(sessions)-1)].ID)
		}
	}
	switch {
	case slices.Contains(common.Hotkeys.JumpToUnreadSession, key):
		return m.focusDockSessions()
	case isLeaveKey(key):
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
	case slices.Contains(common.Hotkeys.OpenHelpMenu, key):
		m.openHelpMenu()
	case slices.Contains(common.Hotkeys.OpenAgentPrompt, key):
		m.promptModal.Open(false)
	case slices.Contains(common.Hotkeys.OpenCommandLine, key):
		m.promptModal.Open(true)
	case slices.Contains(common.Hotkeys.CopyPath, key):
		m.withSelectedSession(m.copySessionFolder)
	case slices.Contains(common.Hotkeys.SendToShelf, key):
		return m.sendSessionFilesToShelf()
	case slices.Contains(common.Hotkeys.GoTo, key):
		// The s list goes to folders, which acts on the files. Closed with
		// esc, it gives the keyboard back here.
		m.leaveDockSessions()
		m.dock.popupFrom = true
		return m.openJump()
	}
	if list.SelectedID() != prev {
		m.sessions.scroll = 0
		return m.sessionsSelectionChanged()
	}
	return nil
}

// popupClosed gives the keyboard back to the Sessions box when the s or z
// popup it opened was closed without going to a folder.
func (m *model) popupClosed() tea.Cmd {
	if !m.dock.popupFrom {
		return nil
	}
	m.dock.popupFrom = false
	return m.focusDockSessionsOn(m.dock.lastID)
}

// startingSessionKey handles the keys that act on a session on the row of
// one that hasn't started: enter brings it beside Marmota; there is nothing
// yet to rename, fork, stop, mention or move. It reports whether it took
// the key.
func (m *model) startingSessionKey(st *askStart, key string) bool {
	switch {
	case slices.Contains(common.Hotkeys.Confirm, key):
		m.showStartingSession(st)
	case slices.Contains(common.Hotkeys.SessionMention, key), slices.Contains(common.Hotkeys.SessionRename, key),
		slices.Contains(common.Hotkeys.SessionFork, key), slices.Contains(common.Hotkeys.SessionStop, key),
		slices.Contains(common.Hotkeys.MoveSessionHere, key), slices.Contains(common.Hotkeys.ShowSessionFiles, key):
		m.flashClaude("It hasn't started yet: " + firstKey(common.Hotkeys.Confirm) + " brings it beside Marmota")
	default:
		return false
	}
	return true
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
		hint = "esc back"
	}
	r.SetBorderInfoItems(hint)

	inner := width - common.BorderPadding
	view := height - common.BorderPadding
	sessions := m.dockSessions()
	if len(sessions) == 0 {
		r.AddLines(dockDim(" No live sessions"), dockDim(" "+firstKey(common.Hotkeys.OpenAgentPrompt)+"claude or "+
			firstKey(common.Hotkeys.OpenAgentPrompt)+"codex starts one"))
		if m.app.err != nil {
			r.AddLines(m.remoteSectionLines(inner)...)
		}
		return r.Render()
	}
	now := time.Now()
	sel := m.sessions.list.SelectedID()
	var lines []string
	selLine := 0
	for i, s := range sessions {
		if s.App != nil && (i == 0 || sessions[i-1].App == nil) {
			lines = append(lines, m.remoteSectionLines(inner)...)
		}
		selected := focused && s.ID == sel
		if selected {
			selLine = len(lines)
		}
		lines = append(lines, m.dockRow(i+1, s, selected, inner, now)...)
	}
	if m.app.err != nil && !slices.ContainsFunc(sessions, func(s claudesession.Session) bool { return s.App != nil }) {
		lines = append(lines, m.remoteSectionLines(inner)...)
	}
	// Keep the selected session in view.
	top := 0
	if selLine+dockRowLines > view {
		top = selLine + dockRowLines - view
	}
	r.AddLines(lines[top:min(len(lines), top+view)]...)
	return r.Render()
}

// remoteSectionLines head the Remote section: nothing, its rows' cloud says
// where they run, but a line saying why claude.ai can't be asked when it can't.
func (m *model) remoteSectionLines(width int) []string {
	note := m.appSectionNote()
	if note == "" {
		return nil
	}
	base := lipgloss.NewStyle().Background(common.FilePanelBGColor).Foreground(common.FooterFGColor)
	dim := base.Foreground(lipgloss.Color(common.Theme.FooterBorder))
	return []string{dockFill(base.Render("  ")+common.HostMark(remoteSectionTitle, common.FilePanelBGColor)+
		dim.Render(ansi.Truncate(" "+note, max(0, width-4), "…")), width, base)}
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
	// Where it runs, then Remote Control, beside the age, so the titles stay
	// in line and keep the room.
	age := dim.Render(shortAge(now, sessionAgeAnchor(s)))
	if s.RemoteControl {
		age = common.RemoteControlMark(bg) + base.Render(" ") + age
	}
	host := s.Host
	if s.App != nil {
		host = remoteSectionTitle // another computer's
	}
	if host := common.HostMark(host, bg); host != "" {
		age = host + base.Render(" ") + age
	}

	titleStyle := base
	if selected {
		titleStyle = titleStyle.Bold(true)
	}
	if s.App != nil && (s.App.Offline || m.app.err != nil) { // its computer is off, or as last listed
		titleStyle = titleStyle.Foreground(lipgloss.Color(common.Theme.FooterBorder))
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
	if s.App != nil {
		return s.App.Doing
	}
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
		{Key: "esc", Description: "files"},
		{Key: firstKey(common.Hotkeys.Confirm), Description: "open beside"},
		{Key: firstKey(common.Hotkeys.SessionMention), Description: "mention"},
		{Key: firstKey(common.Hotkeys.SessionNew), Description: "new"},
		{Key: firstKey(common.Hotkeys.SessionRename), Description: "rename"},
		{Key: firstKey(common.Hotkeys.SessionFork), Description: "fork"},
		{Key: firstKey(common.Hotkeys.SessionStop), Description: "stop"},
		{Key: firstKey(common.Hotkeys.GoTo), Description: "go to"},
		{Key: firstKey(common.Hotkeys.OpenHelpMenu), Description: "help"},
	}
	if _, ok := m.appSessionByID(m.sessions.list.SelectedID()); ok {
		keys = []statusline.Hotkey{
			{Key: firstKey(common.Hotkeys.Confirm), Description: "open in the Claude app"},
			{Key: "esc", Description: "files"},
			{Key: firstKey(common.Hotkeys.GoTo), Description: "go to"},
			{Key: firstKey(common.Hotkeys.OpenHelpMenu), Description: "help"},
		}
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
