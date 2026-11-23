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

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/internal/ui/statusline"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// The Sessions box is under the preview, as wide as it: the live sessions,
// a row each, in two columns when the box is wide enough for both to say
// everything. It is as tall as its sessions, up to a share of the screen
// (layout.go). s gives it the keyboard, on the session that most recently
// started needing you, else the one you were last on, else the first; s
// again goes on to the next one, esc gives the keyboard back. While it has the keyboard, the preview shows
// the selected session, and its question when it asks one. It moves
// m.sessions.list.

// dockState is what the Sessions box keeps between visits.
type dockState struct {
	// lastID is the session the box was last on, for the next s.
	lastID string
	// prevFocus is what had the keyboard before s, to give it back to.
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
	// rows is the lines the box had when Marmota last ran, read once from
	// SessionsRowsFile (rowsRead) and kept there as it changes: the box is
	// that tall from the first frame, before the sessions are listed.
	rows     int
	rowsRead bool
}

const (
	// dockTitleMaxWidth is the widest the titles' column gets.
	dockTitleMaxWidth = 22
	// dockDoingMinWidth is the room what a session does needs to be worth
	// saying; with less, its title has the row.
	dockDoingMinWidth = 10
)

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

// dockPick is where s lands: the first session below from that needs you
// or replied unread, round at the end, so s again goes on to the next one;
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

// focusDockSessions gives the Sessions box the keyboard.
func (m *model) focusDockSessions() tea.Cmd {
	return m.focusDockSessionsOn("")
}

// focusDockSessionsOn gives the Sessions box the keyboard on session id, or
// with id "", on the one s picks.
func (m *model) focusDockSessionsOn(id string) tea.Cmd {
	var cmds []tea.Cmd
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

// leaveDockSessions gives the keyboard back to what had it before s.
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
		return m.scrollSessionsPane(1)
	case slices.Contains(common.Hotkeys.PageDown, key):
		return m.scrollSessionsPane(-1)
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
		// The go to list goes to folders, which acts on the files. Closed with
		// esc, it gives the keyboard back here.
		m.leaveDockSessions()
		m.dock.popupFrom = true
		return m.openJump()
	default:
		return m.dockScreenKey(key)
	}
	if list.SelectedID() != prev {
		m.sessions.scroll = 0
		return m.sessionsSelectionChanged()
	}
	return nil
}

// dockScreenKey handles, while the Sessions box has the keyboard, the keys
// of the files that act on no file: those that change what the screen shows
// do it and leave the keyboard here, those that go to another box take it
// there. A key that acts on the file under a panel's cursor (open, delete,
// paste) does nothing here: that file isn't what you are looking at.
func (m *model) dockScreenKey(key string) tea.Cmd {
	toFiles := func() {
		m.leaveDockSessions()
		m.focusPanel = nonePanelFocus
		m.getFocusedFilePanel().isFocused = true
	}
	switch {
	case slices.Contains(common.Hotkeys.ToggleFooter, key):
		return m.toggleFooterController()
	case slices.Contains(common.Hotkeys.ToggleFilePreviewPanel, key):
		m.toggleFilePreviewPanel()
	case slices.Contains(common.Hotkeys.ToggleDotFile, key):
		m.toggleDotFileController()
	case slices.Contains(common.Hotkeys.BackToEditor, key):
		return m.backToEditor()
	case slices.Contains(common.Hotkeys.FocusOnMetaData, key):
		toFiles()
		return m.focusOnMetadata()
	case slices.Contains(common.Hotkeys.NextFilePanel, key):
		toFiles()
		m.nextFilePanel()
	case slices.Contains(common.Hotkeys.PreviousFilePanel, key):
		toFiles()
		m.previousFilePanel()
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

// dockEntry is a line of the Sessions box: a session, or the Remote
// section's note.
type dockEntry struct {
	session int // its place in the sessions, -1 for the note
}

// dockEntries is the box's lines in order: this computer's sessions, the
// Remote section's note when claude.ai can't be asked, then its sessions.
func (m *model) dockEntries(sessions []claudesession.Session) []dockEntry {
	note := m.appSectionNote() != ""
	var out []dockEntry
	noted := false
	for i, s := range sessions {
		if note && !noted && s.App != nil {
			out = append(out, dockEntry{session: -1})
			noted = true
		}
		out = append(out, dockEntry{session: i})
	}
	if note && !noted && m.app.err != nil {
		out = append(out, dockEntry{session: -1})
	}
	return out
}

// dockNoSessionsLines is how many lines say there are no sessions and how
// to start one.
const dockNoSessionsLines = 2

// dockSessionLineCount is how many lines the box has to show, which is how
// tall it is. Until the sessions are listed it is what the box had when
// Marmota last ran, so the boxes are where they will be from the first
// frame: this computer's sessions and claude.ai's arrive one after the
// other, each a moment after the screen is first drawn.
func (m *model) dockSessionLineCount() int {
	sessions := m.dockSessions()
	n := len(m.dockEntries(sessions))
	if len(sessions) == 0 {
		n += dockNoSessionsLines
	}
	if !m.sessionsListed() {
		n = max(n, m.dockRowsBefore())
	}
	return n
}

// sessionsListed reports whether both lists have answered once: this
// computer's sessions, and claude.ai's (or that it can't be asked).
func (m *model) sessionsListed() bool {
	return m.claude.loaded && (m.app.answered || !appSessionsAutoRefresh)
}

// dockRowsBefore is the lines the Sessions box had when Marmota last ran, 0
// when that isn't known.
func (m *model) dockRowsBefore() int {
	if !m.dock.rowsRead {
		m.dock.rowsRead = true
		if data, err := os.ReadFile(variable.SessionsRowsFile); err == nil {
			m.dock.rows, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
	}
	return m.dock.rows
}

// rememberDockRows keeps the lines the Sessions box has, once the sessions
// are listed and when the number changes, for the next start.
func (m *model) rememberDockRows() tea.Cmd {
	if !m.sessionsListed() {
		return nil
	}
	rows := m.dockSessionLineCount()
	if rows == m.dockRowsBefore() {
		return nil
	}
	m.dock.rows = rows
	path := variable.SessionsRowsFile
	return func() tea.Msg {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err == nil {
			_ = os.WriteFile(path, []byte(strconv.Itoa(rows)+"\n"), 0o600)
		}
		return nil
	}
}

// dockSessionsRender draws the Sessions box.
func (m *model) dockSessionsRender() string {
	box := m.lay.sessions
	if !box.shown() {
		return ""
	}
	focused := m.focusPanel == dockSessionsFocus
	r := ui.DefaultFooterRenderer(box.h, box.w, focused)
	r.SetBorderTitleGlyph("✳", lipgloss.Color(claudeColor))
	r.SetBorderTitle("Sessions")
	hint := firstKey(common.Hotkeys.JumpToUnreadSession) + " go"
	if focused {
		hint = "esc back"
	}

	inner := box.w - common.BorderPadding
	view := box.h - common.BorderPadding
	sessions := m.dockSessions()
	entries := m.dockEntries(sessions)
	if len(sessions) == 0 && !m.sessionsListed() {
		// Not listed yet: the box has the height it had last time.
		r.SetBorderInfoItems(hint)
		r.AddLines(dockDim(" Listing the sessions…"))
		return r.Render()
	}
	if len(sessions) == 0 {
		r.SetBorderInfoItems(hint)
		r.AddLines(dockDim(" No live sessions"), dockDim(" "+firstKey(common.Hotkeys.OpenAgentPrompt)+"claude or "+
			firstKey(common.Hotkeys.OpenAgentPrompt)+"codex starts one"))
		if len(entries) > 0 {
			r.AddLines(m.remoteSectionLines(inner)...)
		}
		return r.Render()
	}

	cols := max(1, m.lay.sessionCols)
	colWidth := inner
	if cols > 1 {
		colWidth = (inner - sessionColumnGap*(cols-1)) / cols
	}
	// Keep the selected session in view.
	sel, selAt := m.sessions.list.SelectedID(), 0
	for i, e := range entries {
		if e.session >= 0 && sessions[e.session].ID == sel {
			selAt = i
		}
	}
	room := view * cols
	top := 0
	if focused && selAt >= room {
		top = selAt - room + 1
	}
	shown := entries[top:min(len(entries), top+room)]
	if seen := countSessions(shown); seen < len(sessions) {
		hint = strconv.Itoa(seen) + "/" + strconv.Itoa(len(sessions)) + " · " + hint
	}
	r.SetBorderInfoItems(hint)

	now := time.Now()
	titles := m.dockTitleWidth(sessions)
	base := lipgloss.NewStyle().Background(common.FooterBGColor)
	for row := 0; row < view && row < len(shown); row++ {
		var line strings.Builder
		for c := range cols {
			if c > 0 {
				line.WriteString(base.Render(strings.Repeat(" ", sessionColumnGap)))
			}
			if at := c*view + row; at < len(shown) {
				line.WriteString(m.dockEntryLine(shown[at], sessions, focused, colWidth, titles, now))
			} else {
				line.WriteString(base.Render(strings.Repeat(" ", colWidth)))
			}
		}
		r.AddLines(line.String())
	}
	return r.Render()
}

func countSessions(entries []dockEntry) int {
	n := 0
	for _, e := range entries {
		if e.session >= 0 {
			n++
		}
	}
	return n
}

// dockTitleWidth is the titles' column: as wide as the longest title, so
// what the sessions do starts in one column.
func (m *model) dockTitleWidth(sessions []claudesession.Session) int {
	w := 0
	for _, s := range sessions {
		w = max(w, ansi.StringWidth(s.DisplayTitle()))
	}
	return min(w, dockTitleMaxWidth)
}

func (m *model) dockEntryLine(e dockEntry, sessions []claudesession.Session, focused bool, width, titles int,
	now time.Time,
) string {
	if e.session < 0 {
		if lines := m.remoteSectionLines(width); len(lines) > 0 {
			return lines[0]
		}
		return strings.Repeat(" ", width)
	}
	s := sessions[e.session]
	selected := focused && s.ID == m.sessions.list.SelectedID()
	return m.dockRow(e.session+1, len(sessions), s, selected, width, titles, now)
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

// claudeColor is the ✳ orange Marmota uses for Claude.
const claudeColor = "#D97757"

// dockRow is a session's row: number, state, agent (cc, co), title, what it
// is doing or asking, then where it runs and how long ago it last did
// something. count is how many sessions there are: the numbers are as wide
// as the largest, so the titles start in one column, titles wide.
func (m *model) dockRow(n, count int, s claudesession.Session, selected bool, width, titles int,
	now time.Time,
) string {
	bg := common.FilePanelBGColor
	if selected && common.FilePanelCursorRowBGColor != "" {
		bg = common.FilePanelCursorRowBGColor
	}
	base := lipgloss.NewStyle().Background(bg).Foreground(common.FooterFGColor)
	dim := base.Foreground(lipgloss.Color(common.Theme.FooterBorder))
	state := m.sessionState(s)
	// The selected session has a bar at its left edge; the cursor glyph sat
	// on top of the number.
	mark := base.Render(" ")
	if selected {
		mark = base.Foreground(lipgloss.Color(common.Theme.FilePanelBorderActive)).Render("▌")
	}
	number := strconv.Itoa(n)
	number = strings.Repeat(" ", max(0, len(strconv.Itoa(count))-len(number))) + number
	prefix := mark + dim.Render(number+" ") + sessionsui.Glyph(state, bg) + base.Render(" ") +
		common.AgentTag(s.IsCodex(), bg) + base.Render(" ")
	// Where it runs, then Remote Control, beside the age, at the row's end.
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
	// The room between the prefix and the age, a space before the age.
	room := max(1, width-ansi.StringWidth(prefix)-ansi.StringWidth(age)-1)
	var middle string
	switch doingRoom := room - titles - 2; { //nolint:mnd // two spaces after the title
	case m.sessions.list.IsRenaming() && selected:
		middle = m.sessions.list.RenameView(room)
	case doingRoom >= dockDoingMinWidth:
		doingStyle := dim
		if state == sessionsui.NeedsYou {
			doingStyle = base.Foreground(sessionsui.StateColor(state))
		}
		title := ansi.Truncate(s.DisplayTitle(), titles, "…")
		doing := m.dockDoing(s, state)
		// What it does starts in the titles' column's line, two spaces after
		// the widest title. When it doesn't fit there, it starts as far left
		// as it needs, up to two spaces after this row's own title.
		pad := titles - ansi.StringWidth(title)
		if over := ansi.StringWidth(doing) - doingRoom; over > 0 {
			give := min(over, pad)
			pad -= give
			doingRoom += give
		}
		middle = titleStyle.Render(title) + base.Render(strings.Repeat(" ", pad+2)) + //nolint:mnd // two spaces
			doingStyle.Render(ansi.Truncate(doing, doingRoom, "…"))
	default:
		middle = titleStyle.Render(ansi.Truncate(s.DisplayTitle(), room, "…"))
	}
	line := prefix + middle
	if gap := width - ansi.StringWidth(line) - ansi.StringWidth(age); gap > 0 {
		line += base.Render(strings.Repeat(" ", gap))
	}
	return dockFill(line+age, width, base)
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
		// What you last asked it says which session this is; that it waits
		// for you, its mark says.
		if p := oneLine(s.LastPrompt); p != "" {
			return "you: " + p
		}
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
