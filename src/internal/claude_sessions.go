package internal

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

const (
	claudeSessionsRefreshInterval = 3 * time.Second
	claudeFlashDuration           = 4 * time.Second
	claudeSendTextTimeout         = 3 * time.Second
	// read markers of sessions no longer listed are dropped after this long
	claudeSeenRetention = 30 * 24 * time.Hour
)

// Test seams.
var (
	claudeSessionRunner claudesession.Runner = claudesession.ExecRunner         //nolint:gochecknoglobals // test seam
	claudeProjectsDir                        = claudesession.DefaultProjectsDir //nolint:gochecknoglobals // test seam
	// claudeSessionsAutoRefresh turns the background refresh off in tests,
	// where its messages would race with tests that poke the model directly.
	claudeSessionsAutoRefresh = true //nolint:gochecknoglobals // test seam
	// stopProcess asks a session's process to exit.
	stopProcess = utils.TerminateProcess //nolint:gochecknoglobals // test seam
	// weztermSendText types text into a pane. It runs wezterm directly, not
	// through a shell, so paths with quotes or spaces arrive unchanged.
	weztermSendText = func(paneID int, text string) error { //nolint:gochecknoglobals // test seam
		ctx, cancel := context.WithTimeout(context.Background(), claudeSendTextTimeout)
		defer cancel()
		//nolint:gosec // fixed binary and numeric pane id; text is an argument, never run by a shell
		return exec.CommandContext(ctx, "wezterm", "cli", "send-text", "--pane-id",
			strconv.Itoa(paneID), "--", text).Run()
	}
)

type claudeSessionsState struct {
	snapshot claudesession.Snapshot
	panes    []weztermPane // WezTerm panes listed with the snapshot
	tracker  *claudesession.Tracker
	seen     *claudesession.SeenStore
	unread   map[string]bool
	inflight bool
	loaded   bool
	// folder is the folder the past sessions were listed for.
	folder string
	// pendingStop is the session the stop confirmation is about.
	pendingStop string
	// stopped holds the sessions superfile stopped. A refresh can still list
	// one as running while it exits; it is shown as finished all the same.
	stopped map[string]stoppedClaudeSession
	// layoutGen counts pane moves superfile made itself. A refresh that
	// started before a move carries an outdated pane list and is ignored.
	layoutGen int
	// tickGen names the refresh that is due; one asked for sooner makes
	// the earlier one's tick stale, so only one keeps the list fresh.
	tickGen int

	flash      string
	flashUntil time.Time

	// inbox is each running session's state at the last refresh, to see
	// what changed since (see updateClaudeInbox); nil before the first one.
	inbox map[string]claudeInboxPhase
	// waitingSince is when each session that needs you started to.
	waitingSince map[string]time.Time
	toast        *claudeToast
	// touched sums up the files running sessions edited, so git's letters
	// are read again when they change.
	touched string
	// order is the list's order for the day; loaded on first use.
	order       claudeSessionsOrder
	orderLoaded bool
}

// claudeSessionsOrder returns the day's order, read from disk the first time.
func (m *model) claudeSessionsOrder() *claudeSessionsOrder {
	if !m.claude.orderLoaded {
		m.claude.order = loadClaudeSessionsOrder()
		m.claude.orderLoaded = true
	}
	return &m.claude.order
}

// claudeSessionsDay is the day the list's order is for: today.
func (m *model) claudeSessionsDay() string {
	return today()
}

type claudeSessionsTickMsg struct{ gen int }

type claudeSessionsSnapshotMsg struct {
	snapshot claudesession.Snapshot
	folder   string
	panes    []weztermPane
	// panesListed is false when WezTerm couldn't be asked for its panes: the
	// panes known before stay.
	panesListed bool
	layoutGen   int
	// codex are Codex's sessions, merged in once the panes are known.
	codex []claudesession.Session
}

// claudeSessionsRefreshCmd lists sessions in the background. Only one refresh
// runs at a time; the next one is scheduled when its result arrives.
func (m *model) claudeSessionsRefreshCmd() tea.Cmd {
	if m.claude.inflight || !claudeSessionsAutoRefresh {
		return nil
	}
	m.claude.inflight = true
	if m.claude.tracker == nil {
		m.claude.tracker = claudesession.NewTracker()
	}
	if m.codex.tracker == nil {
		m.codex.tracker = codexsession.NewTracker()
	}
	codexTracker, codexDir := m.codex.tracker, codexHome()
	folder := ""
	if len(m.fileModel.filePanels) > 0 {
		folder = m.getFocusedFilePanel().location
	}
	tracker := m.claude.tracker
	layoutGen := m.claude.layoutGen
	run := claudeSessionRunner
	projects := claudeProjectsDir()
	order := m.claudeSessionsOrder()
	day := m.claudeSessionsDay()
	opts := claudesession.Options{
		Run:         run,
		ProjectsDir: projects,
		Tracker:     tracker,
		Folders:     append([]string(nil), order.Folders...),
		PerFolder:   claudeSessionsPerFolder,
		// Finished sessions are read for the day they were active.
		Since:   startOfDay(day),
		Include: order.include(),
	}
	if folder != "" && !slices.Contains(opts.Folders, folder) {
		opts.Folders = append(opts.Folders, folder)
	}
	if order.stale(day) {
		opts.RecentFolders = claudeSessionsRecentFolders
	}
	return func() tea.Msg {
		snap := claudesession.Collect(context.Background(), opts)
		msg := claudeSessionsSnapshotMsg{snapshot: snap, folder: folder, layoutGen: layoutGen}
		msg.codex = collectCodexSessions(codexsession.Options{Home: codexDir, Tracker: codexTracker,
			Since: opts.Since, Folders: snap.Folders, Include: opts.Include})
		if os.Getenv("WEZTERM_PANE") != "" {
			if listed, err := listWeztermPanes(folder); err == nil {
				msg.panes, msg.panesListed = listed, true
			}
		}
		return msg
	}
}

func (m *model) scheduleClaudeSessionsRefresh() tea.Cmd {
	m.claude.tickGen++
	gen := m.claude.tickGen
	return tea.Tick(claudeSessionsRefreshInterval, func(time.Time) tea.Msg {
		return claudeSessionsTickMsg{gen: gen}
	})
}

func (m *model) applyClaudeSessionsTick(msg claudeSessionsTickMsg) tea.Cmd {
	if msg.gen != m.claude.tickGen {
		return nil // a refresh came sooner and scheduled its own
	}
	return m.claudeSessionsRefreshCmd()
}

func (m *model) applyClaudeSessionsSnapshot(msg claudeSessionsSnapshotMsg) tea.Cmd {
	askBarTarget := m.askBarTarget()
	m.claude.inflight = false
	m.claude.loaded = true
	m.claude.snapshot = msg.snapshot
	m.claude.folder = msg.folder
	if msg.layoutGen == m.claude.layoutGen && msg.panesListed {
		m.claude.panes = msg.panes
	}
	m.mergeCodexSessions(msg.codex)
	askBarTarget, started := m.settleCodexStarts(askBarTarget)
	all := append(append([]claudesession.Session(nil), m.claude.snapshot.Live...), m.claude.snapshot.Past...)
	order := m.claudeSessionsOrder()
	if order.update(all, m.claudeSessionsDay()) {
		order.save()
	}
	m.settleStoppedClaudeSessions(true)
	m.titleNewClaudeSessions()
	if msg.snapshot.LiveErr != nil {
		slog.Debug("Claude sessions: live list unavailable", "error", msg.snapshot.LiveErr)
	}
	m.recomputeClaudeUnread()
	m.refreshSessionsList()
	m.syncSidebarSessions()
	m.keepAskBarTarget(askBarTarget)
	m.keepViewerPin()
	cmds := []tea.Cmd{m.scheduleClaudeSessionsRefresh(), m.updateClaudeInbox(), started}
	if m.sessionsShown() {
		m.loadSessionsConversation()
		cmds = append(cmds, m.watchClaudeQuestion())
	}
	if touched := m.claudeTouchedSummary(); touched != m.claude.touched {
		m.claude.touched = touched
		cmds = append(cmds, scheduleGitRefresh(m))
	}
	return tea.Batch(cmds...)
}

// claudeTouchedSummary sums up the files running sessions edited: it changes
// when a session edits a file it hadn't.
func (m *model) claudeTouchedSummary() string {
	var b strings.Builder
	for _, s := range m.claude.snapshot.Live {
		if n := len(s.EditedFiles); n > 0 {
			b.WriteString(s.ID + strconv.Itoa(n) + s.EditedFiles[n-1])
		}
	}
	return b.String()
}

func (m *model) claudeSeenStore() *claudesession.SeenStore {
	if m.claude.seen == nil {
		m.claude.seen = claudesession.LoadSeen(variable.ClaudeSessionsSeenFile)
	}
	return m.claude.seen
}

// recomputeClaudeUnread decides which sessions have a finished turn you
// haven't looked at. A session shown beside superfile counts as looked at.
func (m *model) recomputeClaudeUnread() {
	seen := m.claudeSeenStore()
	unread := make(map[string]bool)
	keep := make(map[string]bool)
	for _, s := range m.claude.snapshot.Live {
		keep[s.ID] = true
		mark := s.UpdateMark()
		if s.Live && m.isClaudeSessionOnScreen(s) {
			seen.MarkSeen(s.ID, mark)
		}
		if seen.Unread(s.ID, mark) {
			unread[s.ID] = true
		}
	}
	seen.Forget(keep, time.Now().Add(-claudeSeenRetention))
	if err := seen.Save(); err != nil {
		slog.Debug("Could not save Claude session read state", "error", err)
	}
	m.claude.unread = unread
}

// titleNewClaudeSessions names running sessions whose transcript gives no
// title yet, such as a fork before its first message, after the
// conversation title Claude Code shows on their pane.
func (m *model) titleNewClaudeSessions() {
	for i := range m.claude.snapshot.Live {
		s := &m.claude.snapshot.Live[i]
		if strings.TrimSpace(s.Title) != "" || s.IsCodex() {
			continue
		}
		if pane, ok := m.weztermPaneForSession(*s); ok {
			s.Title = claudePaneConversationTitle(pane.Title)
		}
	}
}

// claudePaneConversationTitle is the conversation title in a Claude Code
// pane's title, "✳ Fix the login bug", or "" when there is none yet.
func claudePaneConversationTitle(paneTitle string) string {
	glyph, title, ok := strings.Cut(strings.TrimSpace(paneTitle), " ")
	if !ok || utf8.RuneCountInString(glyph) != 1 {
		return ""
	}
	if r, _ := utf8.DecodeRuneInString(glyph); unicode.IsLetter(r) || unicode.IsDigit(r) {
		return ""
	}
	if title = strings.TrimSpace(title); title == "Claude Code" {
		return ""
	}
	return title
}

func (m *model) weztermPaneForSession(s claudesession.Session) (weztermPane, bool) {
	if s.TTY == "" {
		return weztermPane{}, false
	}
	self, selfErr := currentWeztermPaneID()
	for _, p := range m.claude.panes {
		if p.TTYName != s.TTY {
			continue
		}
		if selfErr == nil && p.PaneID == self {
			// A process on superfile's own terminal, such as an Ask run, is
			// no session pane: nothing may be typed into superfile for it.
			return weztermPane{}, false
		}
		return p, true
	}
	return weztermPane{}, false
}

// superfileTabID returns the WezTerm tab superfile runs in, or -1.
func (m *model) superfileTabID() int {
	paneID, err := currentWeztermPaneID()
	if err != nil {
		return -1
	}
	for _, p := range m.claude.panes {
		if p.PaneID == paneID {
			return p.TabID
		}
	}
	return -1
}

// isClaudeSessionVisible reports whether the session's pane is on screen next
// to superfile, in the same WezTerm tab.
func (m *model) isClaudeSessionVisible(s claudesession.Session) bool {
	pane, ok := m.weztermPaneForSession(s)
	if !ok {
		return false
	}
	tab := m.superfileTabID()
	return tab >= 0 && pane.TabID == tab
}

// isClaudeSessionOnScreen reports whether you see what the session says:
// its pane is beside superfile, or the preview shows it.
func (m *model) isClaudeSessionOnScreen(s claudesession.Session) bool {
	return m.isClaudeSessionVisible(s) || m.previewShowsSession(s.ID)
}

func (m *model) markClaudeSessionSeen(s claudesession.Session) {
	m.claudeSeenStore().MarkSeen(s.ID, time.Now())
	delete(m.claude.unread, s.ID)
	if err := m.claudeSeenStore().Save(); err != nil {
		slog.Debug("Could not save Claude session read state", "error", err)
	}
}

func (m *model) flashClaude(text string) {
	m.claude.flash = text
	m.claude.flashUntil = time.Now().Add(claudeFlashDuration)
}

// openClaudeSession shows or resumes a session:
//   - a session running in one of superfile's agent panes is shown beside
//     superfile; opening it again focuses it
//   - a session running in another WezTerm pane is switched to
//   - a background session is attached in a new agent pane
//   - a past session is resumed in a new agent pane, in bypass mode
func (m *model) openClaudeSession(id string) {
	s, ok := m.claude.snapshot.Find(id)
	if !ok {
		m.flashClaude("That session is no longer listed")
		return
	}
	m.markClaudeSessionSeen(s)
	message, err := m.jumpToClaudeSession(s, false)
	if err != nil {
		slog.Error("Could not open Claude session", "session", s.ID, "error", err)
		m.flashClaude(err.Error())
		return
	}
	m.flashClaude(message)
}

// jumpToClaudeSession shows a session beside superfile: its own pane when
// it runs in one, a new pane attached to it when it runs in the background,
// or a new pane resuming it, in bypass mode, when it has ended. With focus
// the keyboard moves into it as well.
func (m *model) jumpToClaudeSession(s claudesession.Session, focus bool) (string, error) {
	if s.IsCodex() {
		return m.jumpToCodexSession(s, focus)
	}
	title := s.DisplayTitle()
	if s.Live {
		if pane, ok := m.weztermPaneForSession(s); ok {
			return m.showWeztermPaneForSession(pane, title, focus)
		}
		if s.Background {
			target := s.Name
			if target == "" {
				target = s.ID
			}
			return m.launchVisibleSession(m.claudeSessionDir(s), "claude attach "+shellQuote(target),
				"Attached to "+title, focus)
		}
		return "", fmt.Errorf("%s isn't running in WezTerm (pid %d)", title, s.PID)
	}

	command := "claude --resume " + shellQuote(s.ID) + " --dangerously-skip-permissions"
	if s.RemoteControl {
		command += " --remote-control"
	}
	return m.launchVisibleSession(m.claudeSessionDir(s), command, "Resumed "+title, focus)
}

// claudeSessionDir is where to start a session's pane: its own folder when it
// still exists, else the focused panel's folder.
func (m *model) claudeSessionDir(s claudesession.Session) string {
	if s.Cwd != "" {
		if info, err := os.Stat(s.Cwd); err == nil && info.IsDir() {
			return s.Cwd
		}
	}
	return m.getFocusedFilePanel().location
}

// showWeztermPaneForSession brings a session's pane beside superfile, or
// switches WezTerm to it when it isn't one of superfile's agent panes. With
// focus, or when it is already beside superfile, the keyboard moves into it.
func (m *model) showWeztermPaneForSession(pane weztermPane, title string, focus bool) (string, error) {
	cwd := m.getFocusedFilePanel().location
	superfilePaneID, err := currentWeztermPaneID()
	if err != nil {
		return "", err
	}
	if pane.TabID == m.superfileTabID() {
		// Already on screen: opening it again means "let me type in it".
		if err := activateWeztermPane(cwd, pane.PaneID); err != nil {
			return "", err
		}
		return "Focused " + title, nil
	}

	m.refreshManagedWeztermState()
	if pane.Workspace == managedAgentHiddenWorkspace && !m.isManagedAgentPane(pane.PaneID) {
		// Parked by an earlier superfile, or lost track of: take it over.
		// Focusing it where it is would show nothing. Reconciling again
		// records where the panes really are before the swap.
		m.adoptParkedAgentPane(superfilePaneID, pane)
		m.refreshManagedWeztermState()
	}
	for _, managed := range m.managedWeztermState.Sessions {
		if managed.PaneID != pane.PaneID {
			continue
		}
		state, err := m.foregroundManagedAgentSessionByPaneID(superfilePaneID, m.managedWeztermState, pane.PaneID)
		if err != nil {
			return "", err
		}
		persistWeztermPaneLayoutState(superfilePaneID, state)
		state.loaded = true
		m.setManagedWeztermState(superfilePaneID, state)
		m.markPaneTabLocally(pane.PaneID)
		if !focus {
			// Moving a pane in makes it the active one; give the keyboard
			// back so the next key still reaches superfile.
			if err := activateWeztermPane(cwd, superfilePaneID); err != nil {
				slog.Warn("Could not give the keyboard back to Marmota", "error", err)
			}
			return "Showing " + title, nil
		}
		if err := activateWeztermPane(cwd, pane.PaneID); err != nil {
			return "", err
		}
		return "Switched to " + title, nil
	}

	// Not one of superfile's agent panes: switch WezTerm to it.
	if err := activateWeztermPane(cwd, pane.PaneID); err != nil {
		return "", err
	}
	return "Switched to " + title, nil
}

func (m *model) isManagedAgentPane(paneID int) bool {
	for _, managed := range m.managedWeztermState.Sessions {
		if managed.PaneID == paneID {
			return true
		}
	}
	return false
}

// adoptParkedAgentPane adds a pane waiting in the hidden workspace to the
// agent panes superfile manages, still parked.
func (m *model) adoptParkedAgentPane(superfilePaneID int, pane weztermPane) {
	state := m.managedWeztermState
	cwd := weztermPaneCwdPath(pane.Cwd)
	state.Sessions = append(state.Sessions, managedAgentSession{
		PaneID:      pane.PaneID,
		Provider:    managedAgentProviderClaude,
		Cwd:         cwd,
		DisplayName: managedAgentFolderName(cwd),
		VisibleSlot: managedAgentHiddenSlot,
	})
	state = normalizeWeztermPaneLayoutState(state)
	persistWeztermPaneLayoutState(superfilePaneID, state)
	state.loaded = true
	m.setManagedWeztermState(superfilePaneID, state)
}

// markPaneTabLocally records that a pane now sits beside superfile and that
// the agent pane shown there before has been parked, so ! and @ act on the
// real layout before the next refresh lists panes again.
func (m *model) markPaneTabLocally(paneID int) {
	m.claude.layoutGen++
	tab := m.superfileTabID()
	superfilePaneID, _ := currentWeztermPaneID()
	for i := range m.claude.panes {
		p := &m.claude.panes[i]
		switch {
		case p.PaneID == paneID:
			p.TabID = tab
			p.Workspace = ""
		case p.TabID == tab && p.PaneID != superfilePaneID:
			p.TabID = -1
			p.Workspace = managedAgentHiddenWorkspace
		}
	}
}

// visibleClaudeSessionPane returns the live session shown next to superfile.
func (m *model) visibleClaudeSessionPane() (claudesession.Session, weztermPane, bool) {
	tab := m.superfileTabID()
	if tab < 0 {
		return claudesession.Session{}, weztermPane{}, false
	}
	for _, s := range m.claude.snapshot.Live {
		if pane, ok := m.weztermPaneForSession(s); ok && pane.TabID == tab {
			return s, pane, true
		}
	}
	return claudesession.Session{}, weztermPane{}, false
}

// sendSelectionToAgent types the selected files and folders (or the one under
// the cursor) into the session shown next to superfile, Claude Code's or
// Codex's, as @mentions, then focuses that pane so you can say what to do
// with them.
func (m *model) sendSelectionToAgent() {
	if m.focusPanel != nonePanelFocus {
		m.flashClaude("Go to a file panel to send files")
		return
	}
	paths := m.selectionForAgent()
	if len(paths) == 0 {
		m.flashClaude("Nothing to send")
		return
	}
	session, pane, ok := m.visibleClaudeSessionPane()
	if !ok {
		m.flashClaude("No session next to Marmota. Pick one with " +
			firstKey(common.Hotkeys.JumpToUnreadSession) + " then enter, or start one with >claude or >codex")
		return
	}
	// A leading space keeps the first mention apart from what the prompt
	// already holds: Claude Code reads a mention only as a word of its own.
	text := " " + formatAgentReferences(paths, session.Cwd)
	if err := weztermSendText(pane.PaneID, text); err != nil {
		slog.Error("Could not send paths to Claude", "pane", pane.PaneID, "error", err)
		m.flashClaude("Couldn't type into its pane: " + err.Error())
		return
	}
	if err := activateWeztermPane(m.getFocusedFilePanel().location, pane.PaneID); err != nil {
		slog.Warn("Could not focus the Claude pane after sending paths", "error", err)
	}
	noun := "items"
	if len(paths) == 1 {
		noun = "item"
	}
	m.flashClaude(fmt.Sprintf("Sent %d %s to %s", len(paths), noun, session.DisplayTitle()))
}

// claudeMovedMsg reports a /cd typed into a session.
type claudeMovedMsg struct {
	title, folder string
	paneID        int
	// queued is set when the session was replying: Claude Code moves it
	// once the reply is done.
	queued bool
	// asks is set when the session asks first whether to work in a folder
	// it hasn't worked in.
	asks bool
	// codexID is the Codex session moved.
	codexID string
	err     error
}

// moveClaudeSessionHere moves the session beside superfile to the folder the
// file panel shows.
func (m *model) moveClaudeSessionHere() tea.Cmd {
	s, _, ok := m.visibleClaudeSessionPane()
	if !ok {
		m.flashClaude("No Claude session next to Marmota. Pick one with " +
			firstKey(common.Hotkeys.JumpToUnreadSession) + " then enter, or start one with >claude")
		return nil
	}
	return m.moveClaudeSessionTo(s)
}

// moveClaudeSessionTo moves a running session to the folder the file panel
// shows, by typing Claude Code's /cd into it. The conversation goes on
// there, and its transcript moves along. Codex has no /cd: its server moves
// the session, from its next turn on.
func (m *model) moveClaudeSessionTo(s claudesession.Session) tea.Cmd {
	title := s.DisplayTitle()
	if !s.Live {
		m.flashClaude(title + " has ended; resume it to move it")
		return nil
	}
	pane, ok := m.weztermPaneForSession(s)
	if !ok {
		m.flashClaude(title + " isn't in a WezTerm pane Marmota can type into")
		return nil
	}
	folder := m.getFocusedFilePanel().location
	if sameDir(s.Cwd, folder) {
		m.flashClaude(title + " already works in " + tildePath(folder))
		return nil
	}
	m.flashClaude("Moving " + title + " to " + tildePath(folder) + "…")
	msg := claudeMovedMsg{title: title, folder: folder, paneID: pane.PaneID,
		queued: s.Status == claudesession.StatusWorking}
	if s.IsCodex() {
		// Its pane's title names the folder it leaves until Codex sets it
		// again: the pane is known by the session from here on.
		m.recordCodexPane(s.ID, pane.PaneID)
		home := codexHome()
		msg.codexID = s.ID
		return func() tea.Msg {
			msg.err = codexMove(home, msg.codexID, folder)
			return msg
		}
	}
	return func() tea.Msg {
		// /cd takes the rest of the line as the path, spaces and all.
		box, err := sendToClaudePrompt(msg.paneID, "/cd "+folder)
		msg.asks = err == nil && !box.Shown
		msg.err = err
		return msg
	}
}

func (m *model) applyClaudeMoved(msg claudeMovedMsg) {
	where := tildePath(msg.folder)
	if msg.codexID != "" && msg.err == nil {
		// Codex's list says the folder it started in, and its rollout the
		// old one until its next turn: the list shows the new one now.
		if m.codex.moved == nil {
			m.codex.moved = make(map[string]string)
		}
		m.codex.moved[msg.codexID] = msg.folder
		for i := range m.claude.snapshot.Live {
			if s := &m.claude.snapshot.Live[i]; s.ID == msg.codexID {
				s.Cwd = msg.folder
			}
		}
		m.refreshSessionsList()
	}
	switch {
	case msg.err != nil:
		slog.Error("Could not move the Claude session", "session", msg.title, "error", msg.err)
		m.flashClaude("Couldn't move " + msg.title + ": " + msg.err.Error())
	case msg.asks:
		// Only you can say whether Claude may work there.
		if err := activateWeztermPane(m.getFocusedFilePanel().location, msg.paneID); err != nil {
			slog.Warn("Could not focus the Claude pane", "error", err)
		}
		m.flashClaude(msg.title + " asks whether to work in " + where + ": answer it there")
	case msg.queued:
		m.flashClaude(msg.title + " moves to " + where + " once it's done replying")
	default:
		m.flashClaude("Moved " + msg.title + " to " + where)
	}
}

// sameDir reports whether two paths name the same folder, through symlinks
// such as /tmp.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

type agentPath struct {
	path string
	dir  bool
}

// selectionForAgent returns the selected items in select mode, otherwise the
// item under the cursor.
func (m *model) selectionForAgent() []agentPath {
	panel := m.getFocusedFilePanel()
	isDir := make(map[string]bool, len(panel.element))
	for _, e := range panel.element {
		isDir[e.location] = e.directory
	}
	var paths []agentPath
	if panel.panelMode == selectMode && len(panel.selected) > 0 {
		for _, e := range panel.element {
			if _, ok := panel.selected[e.location]; ok {
				paths = append(paths, agentPath{path: e.location, dir: e.directory})
			}
		}
		return paths
	}
	if item := panel.getSelectedItemPtr(); item != nil {
		paths = append(paths, agentPath{path: item.location, dir: item.directory})
	}
	return paths
}

// formatAgentReferences turns paths into Claude Code @mentions: relative to
// the session's folder when inside it, folders ending in "/", and quoted when
// they contain spaces. They are typed as they are, with no space around them.
func formatAgentReferences(paths []agentPath, sessionCwd string) string {
	refs := make([]string, 0, len(paths))
	for _, p := range paths {
		ref := p.path
		if sessionCwd != "" {
			if rel, err := filepath.Rel(sessionCwd, p.path); err == nil && rel != ".." &&
				!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				ref = rel
			}
		}
		if p.dir && !strings.HasSuffix(ref, "/") {
			ref += "/"
		}
		if strings.ContainsAny(ref, " \t") {
			ref = `"` + strings.ReplaceAll(ref, `"`, `\"`) + `"`
		}
		refs = append(refs, "@"+ref)
	}
	return strings.Join(refs, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, isUnsafeShellRune) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func isUnsafeShellRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return false
	case r == '-', r == '_', r == '.', r == '/', r == ':':
		return false
	default:
		return true
	}
}

const (
	minFlashWidth = 10
	statusSepFG   = "#6c7086"
)

// claudeStatusSummary is the right side of the status line: a short message
// after an action, else counts of working and unread sessions.
func (m *model) claudeStatusSummary() string {
	style := lipgloss.NewStyle().Background(lipgloss.Color(common.Theme.FilePanelBG))
	if m.claude.flash != "" && time.Now().Before(m.claude.flashUntil) {
		width := max(minFlashWidth, m.fullWidth/2) //nolint:mnd // a message gets up to half the line
		return style.Foreground(lipgloss.Color(common.Theme.Hint)).Render(ansi.Truncate(m.claude.flash, width, "…"))
	}
	if !m.claude.loaded {
		return ""
	}
	working, needsYou := 0, 0
	for _, s := range m.claude.snapshot.Live {
		if s.Status == claudesession.StatusWorking {
			working++
		}
		if s.Status == claudesession.StatusWaiting {
			needsYou++
		}
	}
	unread := len(m.claude.unread)

	var parts []string
	if working > 0 {
		parts = append(parts, style.Foreground(lipgloss.Color(common.ClaudeAgentColor)).
			Render(fmt.Sprintf("✳ %d working", working)))
	}
	if needsYou > 0 {
		parts = append(parts, style.Foreground(lipgloss.Color(common.ClaudeNeedsYouColor)).Bold(true).
			Render(fmt.Sprintf("⚑ %d needs you", needsYou)))
	}
	if unread > 0 {
		parts = append(parts, style.Foreground(lipgloss.Color(common.Theme.Hint)).Bold(true).
			Render(fmt.Sprintf("● %d unread (!)", unread)))
	}
	return strings.Join(parts, style.Foreground(lipgloss.Color(statusSepFG)).Render(" · "))
}
