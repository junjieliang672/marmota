package internal

import (
	"cmp"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	filepreview "github.com/junjieliang672/marmota/src/pkg/file_preview"
)

// The preview never shows the session that is beside Marmota, where you
// read it already (the user, 2026-11-27).
//
// Enter on a session brings it beside Marmota and takes the keyboard there;
// the Sessions box then goes on to the session most worth seeing of the
// others on this computer: one that needs you, else one that replied
// unread, else one working, else an idle one, the latest to do something
// first. With no other, it stays.
//
// While the keyboard is away from Marmota (the terminal says so: it lost
// the focus) the preview follows: it goes to a session of a higher tier
// when there is one. When several need you, replied unread or work, the
// preview is cut into cells, one under another, a session each, in the
// Sessions box's order, each in a color of its own and scrolled on its own
// by the wheel over it. Back in Marmota the preview is one session again:
// the cell you clicked, else the one of the highest tier.

const (
	// cellMinRows is the fewest rows a cell has, its frame included: the
	// preview is cut into as many cells as have that many.
	cellMinRows = 8
	// cellClickGrace is how long after the cells went a click still picks
	// the cell that was under it: the click that gives Marmota the
	// keyboard can arrive after the terminal said it has the focus.
	cellClickGrace = 500 * time.Millisecond
)

// cellPalette is the cells' colors: none the Sessions box draws a state
// in, nor an agent's.
var cellPalette = []lipgloss.Color{ //nolint:gochecknoglobals // a fixed palette
	"#89dceb", "#cba6f7", "#a6e3a1", "#f5c2e7", "#94e2d5", "#b4befe",
}

// A session's tier while the preview follows: the lower, the more it is
// worth seeing.
const (
	tierNeedsYou = iota + 1
	tierUnread
	tierWorking
	tierIdle
	tierNone
)

type previewFollow struct {
	// colors is each session's color, as an index of cellPalette, from its
	// first cell until it ends.
	colors map[string]int
	// scroll is how far back each cell's conversation is scrolled, lines is
	// how many lines it had when last drawn: a scrolled cell stays on what
	// it shows when its session writes on.
	scroll, lines map[string]int
	// convos is the conversations of the cells' sessions, but the selected
	// one's, which is the preview's own.
	convos map[string]sessionsConversation
	// touched is the cell clicked while the keyboard was away.
	touched string
	// marks is the cells' colors by session while the Sessions box is
	// drawn: a session in a cell has its color at its row's start.
	marks map[string]lipgloss.Color
	// gone is the cells as they were when the keyboard came back, at
	// goneAt.
	gone   []goneCell
	goneAt time.Time
}

// goneCell is a cell that was on rows from..to (to excluded) of the preview.
type goneCell struct {
	id               string
	from, to, scroll int
}

// cellConversationMsg is a cell's conversation, read in the background.
type cellConversationMsg struct {
	convo sessionsConversation
}

// followTier is how much s is worth seeing while the preview follows.
func (m *model) followTier(s claudesession.Session) int {
	switch m.sessionState(s) {
	case sessionsui.NeedsYou:
		return tierNeedsYou
	case sessionsui.Unread:
		return tierUnread
	case sessionsui.Working:
		return tierWorking
	case sessionsui.Idle:
		return tierIdle
	case sessionsui.Ended:
	}
	return tierNone
}

// followCandidates is the sessions the preview may go to on its own, in the
// Sessions box's order: this computer's, running and listed, without the
// one beside Marmota and without except.
func (m *model) followCandidates(except string) []claudesession.Session {
	beside := m.visibleSessionID()
	var out []claudesession.Session
	for _, s := range m.dockSessions() {
		if s.App != nil || s.Host != "" || !s.Live || s.ID == beside || s.ID == except ||
			m.unlistedStart(s.ID) != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

// worthSeeingFirst orders sessions by tier, then by which did something
// last.
func (m *model) worthSeeingFirst(a, b claudesession.Session) int {
	if c := cmp.Compare(m.followTier(a), m.followTier(b)); c != 0 {
		return c
	}
	return sessionAgeAnchor(b).Compare(sessionAgeAnchor(a))
}

// mostWorthSeeing is the one of sessions the preview goes to.
func (m *model) mostWorthSeeing(sessions []claudesession.Session) (claudesession.Session, bool) {
	if len(sessions) == 0 {
		return claudesession.Session{}, false
	}
	return slices.MinFunc(sessions, m.worthSeeingFirst), true
}

// previewGoesTo puts the Sessions box, and so the preview, on session id.
func (m *model) previewGoesTo(id string) tea.Cmd {
	if m.sessions.list.SelectedID() == id || !m.sessions.list.Select(id) {
		return nil
	}
	m.sessions.scroll = 0
	return m.sessionsSelectionChanged()
}

// previewLeaves has the preview go on from session id once enter brought it
// beside Marmota: to the session most worth seeing of the others, if there
// is one.
func (m *model) previewLeaves(id string) tea.Cmd {
	if id == "" || m.dock.pinned || m.visibleSessionID() != id {
		return nil
	}
	next, ok := m.mostWorthSeeing(m.followCandidates(id))
	if !ok {
		return nil
	}
	return m.previewGoesTo(next.ID)
}

// following reports whether the preview follows the sessions on its own:
// the keyboard is away from Marmota, and the preview shows the session the
// Sessions box is on, one of this computer's, unpinned.
func (m *model) following() bool {
	if m.terminalFocused || m.focusPanel != dockSessionsFocus || m.sessions.focus != sessionsFocusList ||
		m.sessions.list.IsRenaming() || m.dock.pinned || !m.fileModel.filePreview.IsOpen() || m.overlayOpen() {
		return false
	}
	s, ok := m.selectedSession()
	return ok && s.App == nil && !m.sessions.list.NewSelected() && m.unlistedStart(s.ID) == nil
}

// followPreview has the preview follow, after the sessions were listed anew
// and when the keyboard leaves: it goes to a session of a higher tier than
// the one it shows, or to the one most worth seeing when what it shows is
// beside Marmota now or gone; and the cells' conversations are read.
func (m *model) followPreview() tea.Cmd {
	m.forgetCellColors()
	if !m.following() {
		return nil
	}
	var cmds []tea.Cmd
	cands := m.followCandidates("")
	if best, ok := m.mostWorthSeeing(cands); ok {
		sel, _ := m.selectedSession()
		shown := slices.ContainsFunc(cands, func(s claudesession.Session) bool { return s.ID == sel.ID })
		if !shown || m.followTier(best) < m.followTier(sel) {
			cmds = append(cmds, m.previewGoesTo(best.ID))
		}
	}
	cells, _ := m.previewCells()
	return tea.Batch(append(cmds, m.loadCellConversations(cells)...)...)
}

// previewCells is the sessions the preview shows in cells, in the Sessions
// box's order, and how many more had no room: those that need you, replied
// unread or work, when there are two at least while the preview follows.
// With fewer the preview is one session.
func (m *model) previewCells() ([]claudesession.Session, int) {
	if !m.following() {
		return nil, 0
	}
	room := m.fileModel.filePreview.GetHeight() / cellMinRows
	var cells []claudesession.Session
	for _, s := range m.followCandidates("") {
		if m.followTier(s) <= tierWorking {
			cells = append(cells, s)
		}
	}
	if len(cells) < 2 || room < 2 { //nolint:mnd // one cell is the preview as it is
		return nil, 0
	}
	more := len(cells) - room
	if more <= 0 {
		return cells, 0
	}
	first := slices.Clone(cells)
	slices.SortStableFunc(first, m.worthSeeingFirst)
	first = first[:room]
	cells = slices.DeleteFunc(cells, func(s claudesession.Session) bool {
		return !slices.ContainsFunc(first, func(f claudesession.Session) bool { return f.ID == s.ID })
	})
	return cells, more
}

// cellRows is the preview's rows each of n cells takes, to excluded: the
// same number each, the last down to the preview's foot.
func cellRows(n, height int) [][2]int {
	rows := make([][2]int, n)
	each := height / n
	for i := range rows {
		rows[i] = [2]int{i * each, (i + 1) * each}
	}
	rows[n-1][1] = height
	return rows
}

// cellConvo is the conversation of the session in a cell.
func (m *model) cellConvo(id string) sessionsConversation {
	if m.sessions.convo.id == id {
		return m.sessions.convo
	}
	return m.follow.convos[id]
}

// loadCellConversations brings the cells' conversations up to date: what
// was written since, at once; a session whose conversation wasn't read yet
// is read in the background.
func (m *model) loadCellConversations(cells []claudesession.Session) []tea.Cmd {
	var cmds []tea.Cmd
	if m.follow.convos == nil {
		m.follow.convos = map[string]sessionsConversation{}
	}
	for _, s := range cells {
		if s.ID == m.sessions.list.SelectedID() {
			continue // the preview's own
		}
		c := m.follow.convos[s.ID]
		if c.id != s.ID || c.path != s.TranscriptPath {
			c = m.sessions.kept[s.ID]
			if c.id != s.ID || c.path != s.TranscriptPath {
				c = sessionsConversation{}
				if s.TranscriptPath != "" && conversationsInBackground {
					m.follow.convos[s.ID] = sessionsConversation{id: s.ID, path: s.TranscriptPath, reading: true}
					cmds = append(cmds, m.readCellConversationCmd(s))
					continue
				}
			}
		}
		if c.reading {
			continue
		}
		m.follow.convos[s.ID] = readConversation(c, s)
	}
	return cmds
}

// readCellConversationCmd reads s's conversation for its cell, and lays out
// its turns, off the screen's loop.
func (m *model) readCellConversationCmd(s claudesession.Session) tea.Cmd {
	view, width := m.sessionPaneViewOf(s, sessionsConversation{id: s.ID}), m.fileModel.filePreview.GetWidth()
	return func() tea.Msg {
		c := readConversation(sessionsConversation{}, s)
		view.Turns = c.turns
		askui.PrepareTurns(view, width)
		return cellConversationMsg{convo: c}
	}
}

// cellConversationRead puts a conversation read in the background in its
// cell, if the cell still waits for it.
func (m *model) cellConversationRead(msg cellConversationMsg) {
	if cur, ok := m.follow.convos[msg.convo.id]; ok && cur.reading && cur.path == msg.convo.path {
		m.follow.convos[msg.convo.id] = msg.convo
	}
}

// cellColor is the color of session id's cell: the first no other session
// has, from its first cell on.
func (m *model) cellColor(id string) lipgloss.Color {
	f := &m.follow
	if f.colors == nil {
		f.colors = map[string]int{}
	}
	at, ok := f.colors[id]
	if !ok {
		for at = 0; ; at++ {
			taken := false
			for _, other := range f.colors {
				taken = taken || other == at
			}
			if !taken {
				break
			}
		}
		f.colors[id] = at
	}
	return cellPalette[at%len(cellPalette)]
}

// forgetCellColors gives up the colors of sessions that have ended.
func (m *model) forgetCellColors() {
	for id := range m.follow.colors {
		if s, ok := m.claude.snapshot.Find(id); !ok || !s.Live {
			delete(m.follow.colors, id)
		}
	}
}

// cellMarks is the cells' colors by session, for the Sessions box's rows.
func (m *model) cellMarks() map[string]lipgloss.Color {
	cells, _ := m.previewCells()
	if len(cells) == 0 {
		return nil
	}
	marks := make(map[string]lipgloss.Color, len(cells))
	for _, s := range cells {
		marks[s.ID] = m.cellColor(s.ID)
	}
	return marks
}

// previewCellsRender draws the preview cut into cells, more sessions having
// no room for one.
func (m *model) previewCellsRender(cells []claudesession.Session, more, width, height int) string {
	f := &m.follow
	if f.scroll == nil {
		f.scroll, f.lines = map[string]int{}, map[string]int{}
	}
	listed := m.dockSessions()
	out := make([]string, 0, len(cells))
	for i, rows := range cellRows(len(cells), height) {
		s := cells[i]
		view := m.sessionPaneViewOf(s, m.cellConvo(s.ID))
		// A scrolled cell stays on what it shows: what its session wrote
		// since is under it.
		lines := askui.CellLines(view, width)
		if was, ok := f.lines[s.ID]; ok && f.scroll[s.ID] > 0 && lines > was {
			f.scroll[s.ID] += lines - was
		}
		f.lines[s.ID] = lines
		cell := askui.Cell{
			Number: 1 + slices.IndexFunc(listed, func(l claudesession.Session) bool { return l.ID == s.ID }),
			Color:  m.cellColor(s.ID),
			Asks:   m.followTier(s) == tierNeedsYou,
			Scroll: f.scroll[s.ID],
		}
		if i == len(cells)-1 {
			cell.More = more
		}
		out = append(out, askui.RenderCell(view, cell, width, rows[1]-rows[0]))
	}
	return strings.Join(out, "\n") + filepreview.ClearKittyImages()
}

// terminalFocusChanged follows the keyboard coming to Marmota or leaving
// it: away, the preview follows the sessions; back, its cells become one
// session again.
func (m *model) terminalFocusChanged(focused bool) tea.Cmd {
	if !focused {
		m.terminalFocused = false
		m.follow.touched = ""
		return m.followPreview()
	}
	cells, _ := m.previewCells()
	m.terminalFocused = true
	return m.cellsBecomeOne(cells)
}

// cellsBecomeOne ends the cells: the preview goes to the one clicked, else
// to the one most worth seeing, scrolled as its cell was.
func (m *model) cellsBecomeOne(cells []claudesession.Session) tea.Cmd {
	f := &m.follow
	defer func() { f.scroll, f.lines, f.convos, f.touched = nil, nil, nil, "" }()
	f.gone = nil
	if len(cells) == 0 {
		return nil
	}
	for i, rows := range cellRows(len(cells), m.fileModel.filePreview.GetHeight()) {
		f.gone = append(f.gone, goneCell{id: cells[i].ID, from: rows[0], to: rows[1], scroll: f.scroll[cells[i].ID]})
	}
	f.goneAt = time.Now()
	to, _ := m.mostWorthSeeing(cells)
	if slices.ContainsFunc(cells, func(s claudesession.Session) bool { return s.ID == f.touched }) {
		to.ID = f.touched
	}
	return m.cellBecomesPreview(to.ID, f.scroll[to.ID])
}

// cellBecomesPreview puts the whole preview on the session of a cell,
// scrolled as the cell was.
func (m *model) cellBecomesPreview(id string, scroll int) tea.Cmd {
	if c, ok := m.follow.convos[id]; ok {
		m.keepConversation(c) // read already: it shows at once
	}
	cmd := m.previewGoesTo(id)
	m.sessions.scroll = scroll
	return cmd
}

// cellsMouse takes the mouse over the cells: the wheel scrolls the cell
// under the pointer, and no other; a click picks the session the preview
// goes to when the keyboard is back. It reports whether it took the event.
func (m *model) cellsMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	press := msg.Action == tea.MouseActionPress
	inPreview := m.previewUnder(msg.X, msg.Y) == sessionPreviewArea
	if press && msg.Button == tea.MouseButtonLeft && inPreview && time.Since(m.follow.goneAt) < cellClickGrace {
		// The click that brought the keyboard back, after the cells went.
		for _, c := range m.follow.gone {
			if msg.Y >= c.from && msg.Y < c.to {
				m.follow.gone = nil
				return m.cellBecomesPreview(c.id, c.scroll), true
			}
		}
	}
	cells, _ := m.previewCells()
	if len(cells) == 0 || !inPreview {
		return nil, false
	}
	pv := &m.fileModel.filePreview
	for i, rows := range cellRows(len(cells), pv.GetHeight()) {
		if msg.Y < rows[0] || msg.Y >= rows[1] {
			continue
		}
		id := cells[i].ID
		if press && msg.Button == tea.MouseButtonLeft {
			m.follow.touched = id
		} else if lines, _, ok := wheelScroll(msg); press && ok && lines != 0 {
			if m.follow.scroll == nil {
				m.follow.scroll, m.follow.lines = map[string]int{}, map[string]int{}
			}
			view := m.sessionPaneViewOf(cells[i], m.cellConvo(id))
			limit := askui.CellScrollLimit(view, pv.GetWidth(), rows[1]-rows[0])
			m.follow.scroll[id] = min(max(0, m.follow.scroll[id]-lines), limit)
		}
	}
	return nil, true
}
