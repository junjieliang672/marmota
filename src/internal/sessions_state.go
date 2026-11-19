package internal

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// The Claude sessions the dock shows: the Sessions box's list, the Ask box's
// composer, and the selected session's conversation and question, which the
// preview draws while the box has the keyboard or V pinned it.

type sessionsFocus int

const (
	sessionsFocusList sessionsFocus = iota
	sessionsFocusAsk
	// sessionsFocusAnswer sends keys to the question the selected session
	// asks, in its pane.
	sessionsFocusAnswer
)

const (
	// sessionsConversationTurns is how many turns the preview reads.
	sessionsConversationTurns = 20
	// sessionsConversationTail is how much of a transcript's end the
	// preview reads first.
	sessionsConversationTail = 512 * 1024
	sessionsScrollStep       = 5
)

type sessionsState struct {
	focus sessionsFocus
	list  sessionsui.List
	// composer is the Ask box. What you write stays, whichever session you
	// pick, until you send or clear it.
	composer askui.Composer
	// scroll moves the conversation in the preview back by that many lines.
	scroll int
	// pendingSelect is a session the list selects once it is listed, such
	// as the one the Ask bar just started, if the list is still on New.
	pendingSelect string
	convo         sessionsConversation
	// kept is the conversations the preview left lately, by session.
	kept     map[string]sessionsConversation
	question claudeQuestion
	// selection is the text the mouse selected in the session preview.
	selection previewSelection
}

// sessionsConversation is the selected session's conversation, read from
// its transcript, and when the transcript last changed; branch is the git
// branch where it works.
type sessionsConversation struct {
	id      string
	path    string
	size    int64
	modTime time.Time
	turns   []claudesession.Turn
	// Of a Claude Code transcript, turns were read from its bytes from
	// from (0: its start) to read; it only grows, so the next read starts
	// at read. limit is how many turns are kept: more once older ones load,
	// loading while they do.
	from, read int64
	limit      int
	loading    bool
	// reading: the session's conversation is being read, in the background
	reading bool
	branch  string
}

// olderTurnsMsg is a session's conversation read further back.
type olderTurnsMsg struct {
	id, path   string
	turns      []claudesession.Turn
	from, read int64
	limit      int
	err        error
}

func newSessionsState() sessionsState {
	return sessionsState{list: sessionsui.New(), composer: askui.NewComposer()}
}

// isLeaveKey reports whether key is a cancel key that doesn't also copy
// (esc, not ctrl+c).
func isLeaveKey(key string) bool {
	return slices.Contains(common.Hotkeys.CancelTyping, key) && !slices.Contains(common.Hotkeys.CopyItems, key)
}

// previewShowsSession reports whether the preview shows the session: it is
// the Sessions box's, while the box or the Ask box has the keyboard or V
// pinned it.
func (m *model) previewShowsSession(id string) bool {
	return m.previewShowsASession() && !m.sessions.list.NewSelected() && m.sessions.list.SelectedID() == id
}

// previewShowsASession reports whether the preview shows the Sessions box's
// session rather than the cursor's file.
func (m *model) previewShowsASession() bool {
	return m.fileModel.filePreview.IsOpen() &&
		(m.focusPanel == dockSessionsFocus || m.focusPanel == dockAskFocus || m.viewerPinned())
}

// sessionsHere is the folder of the list's here group: the folder the file
// panel shows, or the nearest folder above it that a group is for.
func (m *model) sessionsHere() string {
	_, here := m.sessionsGroups()
	return here
}

// sessionsGroups lays out the sessions the list shows: those running in a
// WezTerm pane, the only ones it can bring beside superfile and type into,
// and those the Ask box started that aren't listed yet. Finished sessions,
// and those in another terminal or in the background, aren't listed.
func (m *model) sessionsGroups() ([]claudeSessionsGroup, string) {
	var shown []claudesession.Session
	for _, s := range m.claude.snapshot.Live {
		if _, ok := m.weztermPaneForSession(s); ok {
			shown = append(shown, s)
		}
	}
	shown = append(shown, m.startingSessions()...)
	groups := m.claudeSessionsOrder().groups(shown)
	return groups, hereFolder(m.getFocusedFilePanel().location, groups)
}

// refreshSessionsList gives the list the latest sessions.
func (m *model) refreshSessionsList() {
	if !m.toggleFooter {
		return // the dock that shows it is hidden
	}
	groups, _ := m.sessionsGroups()
	out := make([]sessionsui.Group, 0, len(groups))
	for _, g := range groups {
		group := sessionsui.Group{Folder: g.Folder}
		for _, s := range g.Sessions {
			group.Rows = append(group.Rows, sessionsui.Row{ID: s.ID, Name: s.Name, Summary: s.DisplayTitle()})
		}
		out = append(out, group)
	}
	if apps := m.appSessions(); len(apps) > 0 {
		group := sessionsui.Group{Folder: remoteSectionTitle}
		for _, s := range apps {
			group.Rows = append(group.Rows, sessionsui.Row{ID: s.ID, Summary: s.DisplayTitle()})
		}
		out = append(out, group)
	}
	m.sessions.list.SetGroups(out)
	// A row you chose since stays chosen.
	if id := m.sessions.pendingSelect; id != "" && (!m.sessions.list.NewSelected() || m.sessions.list.Select(id)) {
		m.sessions.pendingSelect = ""
	}
}

// askBarTarget is the listed session the Ask box writes to: "" when the box
// doesn't have the keyboard, or writes to a new session.
func (m *model) askBarTarget() string {
	if m.focusPanel != dockAskFocus || m.sessions.list.NewSelected() {
		return ""
	}
	return m.sessions.list.SelectedID()
}

// keepAskBarTarget has the Ask box write to a new session when the session
// it wrote to, id, has left the list, rather than to whichever row the
// cursor landed on.
func (m *model) keepAskBarTarget(id string) {
	if id == "" || m.sessions.list.SelectedID() == id {
		return
	}
	m.sessions.list.SelectNew()
	title := "Its session"
	if s, ok := m.claude.snapshot.Find(id); ok {
		title = s.DisplayTitle()
	}
	m.flashClaude(title + " left the list: your question now starts a new session")
}

// folderName is how a group names its folder: its base name, "~" for home.
func folderName(folder string) string {
	if folder == "" {
		return "no folder"
	}
	if home, _ := os.UserHomeDir(); folder == home {
		return "~"
	}
	return filepath.Base(folder)
}

func (m *model) sessionState(s claudesession.Session) sessionsui.State {
	switch {
	case s.App != nil && s.Status == claudesession.StatusWaiting:
		return sessionsui.NeedsYou
	case s.App != nil && s.Status == claudesession.StatusWorking:
		return sessionsui.Working
	case s.App != nil && s.App.Unread:
		return sessionsui.Unread
	case s.App != nil:
		return sessionsui.Idle
	case !s.Live:
		return sessionsui.Ended
	case s.Status == claudesession.StatusWaiting:
		return sessionsui.NeedsYou
	case s.Status == claudesession.StatusWorking:
		return sessionsui.Working
	case m.claude.unread[s.ID]:
		return sessionsui.Unread
	default:
		return sessionsui.Idle
	}
}

// selectedSession is the session the list's cursor is on.
func (m *model) selectedSession() (claudesession.Session, bool) {
	return m.findSession(m.sessions.list.SelectedID())
}

// findSession is a listed session, or the row of one the Ask box started.
func (m *model) findSession(id string) (claudesession.Session, bool) {
	if id == "" {
		return claudesession.Session{}, false
	}
	if s, ok := m.claude.snapshot.Find(id); ok {
		return s, true
	}
	if s, ok := m.appSessionByID(id); ok {
		return s, true
	}
	return m.startingSession(m.ask.starts[id])
}

// sessionsSelectionChanged catches up with the selected session: its
// conversation, and its question when it asks one.
func (m *model) sessionsSelectionChanged() tea.Cmd {
	id := m.sessions.list.SelectedID()
	if id != m.sessions.convo.id {
		m.sessions.scroll = 0
	}
	return tea.Batch(m.loadSessionsConversation(), m.watchClaudeQuestion())
}

// conversationsInBackground is whether a session's conversation is read
// off the screen's loop when the preview goes to it. Tests read it at once.
var conversationsInBackground = true //nolint:gochecknoglobals // test seam

// conversationsKept is how many sessions' conversations stay read after
// the preview leaves them, so going back to one shows it at once.
const conversationsKept = 8

// conversationReadMsg is a session's conversation, read and laid out in
// the background.
type conversationReadMsg struct{ convo sessionsConversation }

// loadSessionsConversation reads the selected session's conversation when
// its transcript changed since it was last read: what was written since, at
// once. A session the preview goes to is read in the background, its turns
// laid out there too, unless its conversation is kept from before; the
// preview shows the rest of it meanwhile.
func (m *model) loadSessionsConversation() tea.Cmd {
	s, ok := m.selectedSession()
	if !ok {
		m.keepConversation(m.sessions.convo)
		m.sessions.convo = sessionsConversation{id: m.sessions.list.SelectedID(), branch: gitBranch(s.Cwd)}
		return nil
	}
	c := m.sessions.convo
	if c.id != s.ID || c.path != s.TranscriptPath {
		m.keepConversation(c)
		c = m.sessions.kept[s.ID]
		delete(m.sessions.kept, s.ID)
		if c.id != s.ID || c.path != s.TranscriptPath {
			if s.TranscriptPath != "" && conversationsInBackground {
				m.sessions.convo = sessionsConversation{id: s.ID, path: s.TranscriptPath, reading: true}
				return m.readConversationCmd(s)
			}
			c = sessionsConversation{}
		}
	}
	if c.reading {
		m.sessions.convo = c
		return nil
	}
	m.sessions.convo = readConversation(c, s)
	return nil
}

// keepConversation keeps a conversation the preview leaves, to show at once
// when it comes back.
func (m *model) keepConversation(c sessionsConversation) {
	if c.id == "" || c.reading || c.loading {
		return
	}
	if m.sessions.kept == nil {
		m.sessions.kept = map[string]sessionsConversation{}
	}
	if len(m.sessions.kept) >= conversationsKept {
		clear(m.sessions.kept)
	}
	m.sessions.kept[c.id] = c
}

// readConversationCmd reads s's conversation, and lays out its turns as the
// preview will draw them, off the screen's loop.
func (m *model) readConversationCmd(s claudesession.Session) tea.Cmd {
	view, width := m.sessionsPaneView(), m.fileModel.filePreview.GetWidth()
	return func() tea.Msg {
		c := readConversation(sessionsConversation{}, s)
		view.Turns, view.Dir = c.turns, s.Cwd
		askui.PrepareTurns(view, width)
		return conversationReadMsg{convo: c}
	}
}

// conversationRead puts a conversation read in the background in the
// preview, if it is still on that session, and reads on to what was written
// meanwhile.
func (m *model) conversationRead(msg conversationReadMsg) tea.Cmd {
	c := m.sessions.convo
	if !c.reading || c.id != msg.convo.id || c.path != msg.convo.path {
		return nil
	}
	m.sessions.convo = msg.convo
	return m.loadSessionsConversation()
}

// readConversation is s's conversation: c, what was last read, when s is
// c's session and its transcript hasn't changed since, else read again.
func readConversation(c sessionsConversation, s claudesession.Session) sessionsConversation {
	branch := gitBranch(s.Cwd)
	if s.TranscriptPath == "" {
		return sessionsConversation{id: s.ID, branch: branch}
	}
	info, err := os.Stat(s.TranscriptPath)
	if err != nil {
		return sessionsConversation{id: s.ID, branch: branch}
	}
	if c.id == s.ID && c.path == s.TranscriptPath && c.size == info.Size() && c.modTime.Equal(info.ModTime()) {
		c.branch = branch
		return c
	}
	next := sessionsConversation{id: s.ID, path: s.TranscriptPath, size: info.Size(),
		modTime: info.ModTime(), limit: sessionsConversationTurns, branch: branch}
	switch {
	case s.IsCodex():
		next.turns, err = sessionConversation(s, sessionsConversationTurns)
	case c.id == s.ID && c.path == s.TranscriptPath && c.read > 0:
		// what was written since: a turn can be megabytes, read once
		next.from, next.limit, next.loading = c.from, c.limit, c.loading
		next.turns, next.read, err = claudesession.ConversationMore(s.TranscriptPath, c.turns, c.read, c.limit)
	default:
		// the transcript's tail; turns before it load as you scroll up to
		// them (loadOlderTurns), off the screen's loop
		next.turns, next.from, next.read, err = claudesession.ConversationTail(s.TranscriptPath,
			sessionsConversationTurns, sessionsConversationTail)
	}
	if err != nil {
		slog.Debug("Could not read the session's conversation", "session", s.ID, "error", err)
	}
	return next
}

// hasOlderTurns reports whether the conversation in the preview goes back
// further than it was read.
func (c sessionsConversation) hasOlderTurns() bool {
	return c.read > 0 && c.from > 0
}

// loadOlderTurns reads the conversation in the preview further back, four
// times as much of the transcript as before, for as many turns again, in
// the background: the preview has scrolled up to its first turn.
func (m *model) loadOlderTurns() tea.Cmd {
	c := &m.sessions.convo
	if c.loading || !c.hasOlderTurns() {
		return nil
	}
	c.loading = true
	id, path, limit := c.id, c.path, c.limit+sessionsConversationTurns
	tail := max(c.read-c.from, sessionsConversationTail) * 4 //nolint:mnd // four times as much
	view, width := m.sessionsPaneView(), m.fileModel.filePreview.GetWidth()
	return func() tea.Msg {
		turns, from, read, err := claudesession.ConversationTail(path, limit, tail)
		// laid out here, so they cost the screen nothing when they show
		view.Turns = turns
		askui.PrepareTurns(view, width)
		return olderTurnsMsg{id: id, path: path, turns: turns, from: from, read: read, limit: limit, err: err}
	}
}

// olderTurnsLoaded puts the turns read further back in the preview, which
// stays on what it showed: it is scrolled from the conversation's end.
func (m *model) olderTurnsLoaded(msg olderTurnsMsg) {
	c := &m.sessions.convo
	if msg.id != c.id || msg.path != c.path {
		return
	}
	c.loading = false
	if msg.err != nil {
		slog.Debug("Could not read the session's older turns", "session", msg.id, "error", msg.err)
		c.from = 0 // not again
		return
	}
	c.turns, c.from, c.read, c.limit = msg.turns, msg.from, msg.read, msg.limit
	// what was written meanwhile: read on from msg.read
	c.size = -1
	m.loadSessionsConversation()
}

// addPickToQuestion adds the selected files to the question as mentions.
// With nothing selected it adds the item under the cursor, or, with
// onlyWhenEmpty, only when the question is empty.
func (m *model) addPickToQuestion(onlyWhenEmpty bool) {
	panel := m.getFocusedFilePanel()
	selected := panel.panelMode == selectMode && len(panel.selected) > 0
	if !selected && onlyWhenEmpty && strings.TrimSpace(m.sessions.composer.Value()) != "" {
		return
	}
	cwd := panel.location
	if s, ok := m.selectedSession(); ok && s.Cwd != "" {
		cwd = s.Cwd
	}
	added := 0
	for _, p := range m.selectionForAgent() {
		if m.sessions.composer.AddMention(formatAgentReferences([]agentPath{p}, cwd), p.path) {
			added++
		}
	}
	if !onlyWhenEmpty {
		switch added {
		case 0:
			m.flashClaude("Already in your question")
		case 1:
			m.flashClaude("Added 1 item to your question")
		default:
			m.flashClaude("Added " + strconv.Itoa(added) + " items to your question")
		}
	}
}

// isDockQuitKey reports whether key quits superfile while the Sessions box
// has the keyboard: q does, unless you're typing a name or an answer.
func (m *model) isDockQuitKey(key string) bool {
	return m.sessions.focus != sessionsFocusAnswer && !m.sessions.list.IsRenaming() &&
		(slices.Contains(common.Hotkeys.Quit, key) || slices.Contains(common.Hotkeys.CdQuit, key))
}

// scrollSessionsPane moves the session in the preview back (1) or on (-1)
// by half the preview, no further than it goes.
func (m *model) scrollSessionsPane(direction int) tea.Cmd {
	page := max(sessionsScrollStep, m.fileModel.filePreview.GetHeight()/2) //nolint:mnd // half the pane
	return m.scrollSessionsPaneBy(direction * page)
}

// scrollSessionsPaneBy scrolls the session in the preview by lines: up, to
// older turns, when lines is positive.
func (m *model) scrollSessionsPaneBy(lines int) tea.Cmd {
	pv := &m.fileModel.filePreview
	limit := askui.ScrollLimit(m.sessionsPaneView(), "", pv.GetWidth(), pv.GetHeight())
	m.sessions.scroll = min(max(0, m.sessions.scroll+lines), limit)
	// older turns start loading two screens before the first, so they are
	// there by the time you reach it
	if lines > 0 && m.sessions.scroll >= limit-2*pv.GetHeight() {
		return m.loadOlderTurns()
	}
	return nil
}

// sessionsTypingKey handles a key while the list's search or rename box has
// the keyboard: cancel and confirm end it, other keys type.
func sessionsTypingKey(msg tea.KeyMsg, typeKey func(tea.Msg) tea.Cmd, cancel func(),
	confirm func() tea.Cmd,
) tea.Cmd {
	switch key := msg.String(); {
	case slices.Contains(common.Hotkeys.CancelTyping, key):
		cancel()
	case slices.Contains(common.Hotkeys.ConfirmTyping, key):
		return confirm()
	default:
		return typeKey(msg)
	}
	return nil
}

// withSelectedSession runs action on the session the list's cursor is on.
func (m *model) withSelectedSession(action func(claudesession.Session)) {
	if _, ok := m.sessions.list.Selected(); !ok {
		m.flashClaude("No session selected")
		return
	}
	s, ok := m.selectedSession()
	if !ok {
		m.flashClaude("That session is no longer listed")
		return
	}
	action(s)
}

// mentionClaudeSession adds the session's mention to the question and
// copies it, to paste into another session: Claude Code then messages it.
func (m *model) mentionClaudeSession(s claudesession.Session) {
	if s.IsCodex() {
		m.flashClaude("Codex sessions have no name other sessions can mention")
		return
	}
	if !s.Live || s.Name == "" {
		m.flashClaude("Only a running session can be mentioned; " + s.DisplayTitle() + " has ended")
		return
	}
	mention := sessionMention(s.Name)
	m.sessions.composer.AddMention(mention, "")
	if err := writeClipboard(mention); err != nil {
		slog.Debug("Could not copy the session's mention", "error", err)
		m.flashClaude("Added " + mention + " to your question")
		return
	}
	m.flashClaude("Added " + mention + " to your question, and copied it")
}

// copySessionFolder copies the folder the session works in.
func (m *model) copySessionFolder(s claudesession.Session) {
	if s.Cwd == "" {
		m.flashClaude("Marmota doesn't know " + s.DisplayTitle() + "'s folder")
		return
	}
	if err := writeClipboard(s.Cwd); err != nil {
		slog.Error("Could not copy the session's folder", "error", err)
		m.flashClaude("Couldn't copy the folder: " + err.Error())
		return
	}
	m.flashClaude("Copied " + shortPath(s.Cwd))
}

// sessionMention is how Claude Code mentions a session by name: @name, or
// @"name" when it has spaces.
func sessionMention(name string) string {
	if strings.ContainsAny(name, " \t") {
		return `@"` + name + `"`
	}
	return "@" + name
}

// moveSelectedSessionHere moves the selected running session to the file
// panel's folder (Claude Code's /cd).
func (m *model) moveSelectedSessionHere() tea.Cmd {
	s, ok := m.selectedSession()
	if !ok {
		m.flashClaude("No session selected")
		return nil
	}
	return m.moveClaudeSessionTo(s)
}
