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
	sessionsScrollStep        = 5
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
	question      claudeQuestion
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
	branch  string
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
	return m.startingSession(m.ask.starts[id])
}

// sessionsSelectionChanged catches up with the selected session: its
// conversation, and its question when it asks one.
func (m *model) sessionsSelectionChanged() tea.Cmd {
	id := m.sessions.list.SelectedID()
	if id != m.sessions.convo.id {
		m.sessions.scroll = 0
	}
	m.loadSessionsConversation()
	return m.watchClaudeQuestion()
}

// loadSessionsConversation reads the selected session's conversation when
// its transcript changed since it was last read.
func (m *model) loadSessionsConversation() {
	s, ok := m.selectedSession()
	if !ok || s.TranscriptPath == "" {
		m.sessions.convo = sessionsConversation{id: m.sessions.list.SelectedID(), branch: gitBranch(s.Cwd)}
		return
	}
	branch := gitBranch(s.Cwd)
	info, err := os.Stat(s.TranscriptPath)
	if err != nil {
		m.sessions.convo = sessionsConversation{id: s.ID, branch: branch}
		return
	}
	c := m.sessions.convo
	if c.id == s.ID && c.path == s.TranscriptPath && c.size == info.Size() && c.modTime.Equal(info.ModTime()) {
		m.sessions.convo.branch = branch
		return
	}
	turns, err := sessionConversation(s, sessionsConversationTurns)
	if err != nil {
		slog.Debug("Could not read the session's conversation", "session", s.ID, "error", err)
	}
	m.sessions.convo = sessionsConversation{id: s.ID, path: s.TranscriptPath, size: info.Size(),
		modTime: info.ModTime(), turns: turns, branch: branch}
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
func (m *model) scrollSessionsPane(direction int) {
	page := max(sessionsScrollStep, m.fileModel.filePreview.GetHeight()/2) //nolint:mnd // half the pane
	m.scrollSessionsPaneBy(direction * page)
}

// scrollSessionsPaneBy scrolls the session in the preview by lines: up, to
// older turns, when lines is positive.
func (m *model) scrollSessionsPaneBy(lines int) {
	pv := &m.fileModel.filePreview
	limit := askui.ScrollLimit(m.sessionsPaneView(), "", pv.GetWidth(), pv.GetHeight())
	m.sessions.scroll = min(max(0, m.sessions.scroll+lines), limit)
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
