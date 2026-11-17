package internal

// Sessions in tmux: every session Marmota starts runs in a window of its own
// tmux server (see package sessionhost), so it outlives WezTerm, Marmota and
// any pane that shows it. A pane beside Marmota only shows one: a tmux client
// of a view session of its own, which shows one window at a time.
//
// The rest of Marmota knows a session by the WezTerm pane it runs in. A tmux
// window gets a pane of its own kind instead: a negative pane id, which the
// pane seams (weztermGetText, weztermSendText, weztermSendKeys) and the
// layout calls (activate, kill, show beside Marmota) take to tmux. Its
// terminal is the tmux pane's, so a session is matched to it by tty as one
// in a WezTerm pane is.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// useTmuxSessions reports whether sessions are started in tmux: tmux is
// installed, and MARMOTA_NO_TMUX isn't set (the live test of sessions in
// WezTerm panes sets it). Tests start them in WezTerm panes unless they say
// otherwise.
var useTmuxSessions = func() bool { //nolint:gochecknoglobals // test seam
	if os.Getenv("MARMOTA_NO_TMUX") != "" {
		return false
	}
	_, err := exec.LookPath("tmux")
	return err == nil
}

// tmuxPaneWorkspace is the workspace a tmux window's pane reports: none of
// WezTerm's.
const tmuxPaneWorkspace = "tmux"

// tmuxDialTimeout is how long starting or reaching the tmux server may take.
const tmuxDialTimeout = 10 * time.Second

// tmuxRef is the tmux pane behind a negative pane id.
type tmuxRef struct {
	window, pane string
}

// tmuxPanes numbers Marmota's tmux windows for the rest of Marmota, and holds
// the connection to this computer's tmux. The pane seams run outside the
// model, in commands, so it is shared.
var tmuxPanes = &tmuxRegistry{ids: map[string]int{}, refs: map[int]tmuxRef{}} //nolint:gochecknoglobals // shared with commands

type tmuxRegistry struct {
	mu     sync.Mutex
	local  *sessionhost.Client
	ids    map[string]int // window id → pane id
	refs   map[int]tmuxRef
	lastID int
}

// localTmux is the connection to this computer's tmux server, which it
// starts when it isn't running.
func localTmux() (*sessionhost.Client, error) {
	r := tmuxPanes
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.local != nil {
		select {
		case <-r.local.Done():
			r.local = nil
		default:
			return r.local, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxDialTimeout)
	defer cancel()
	c, err := sessionhost.Dial(ctx, sessionhost.Host{Local: true})
	if err != nil {
		return nil, fmt.Errorf("couldn't start tmux: %w", err)
	}
	r.local = c
	return c, nil
}

// paneIDOf is the pane id of a window, the same each time it is asked.
func (r *tmuxRegistry) paneIDOf(w sessionhost.Window) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.ids[w.ID]; ok {
		r.refs[id] = tmuxRef{window: w.ID, pane: w.PaneID}
		return id
	}
	r.lastID--
	r.ids[w.ID] = r.lastID
	r.refs[r.lastID] = tmuxRef{window: w.ID, pane: w.PaneID}
	return r.lastID
}

func (r *tmuxRegistry) ref(id int) (tmuxRef, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref, ok := r.refs[id]
	return ref, ok
}

func isTmuxPane(id int) bool { return id < 0 }

// tmuxPaneOf is a window as the rest of Marmota sees a pane.
func tmuxPaneOf(w sessionhost.Window) weztermPane {
	return weztermPane{
		PaneID: tmuxPanes.paneIDOf(w), TabID: -1, WindowID: -1, Title: w.Title, Cwd: w.Cwd, TTYName: w.TTY,
		Workspace: tmuxPaneWorkspace, Size: weztermPaneSize{Rows: w.Height, Cols: w.Width},
	}
}

// listTmuxPanes lists the windows of this computer's tmux as panes. With no
// tmux server running there are none, and none is started for it.
func listTmuxPanes() ([]weztermPane, error) {
	if !useTmuxSessions() || !tmuxServerRunning() {
		return nil, nil
	}
	c, err := localTmux()
	if err != nil {
		return nil, err
	}
	windows, err := c.Windows()
	if err != nil {
		return nil, err
	}
	panes := make([]weztermPane, 0, len(windows))
	for _, w := range windows {
		panes = append(panes, tmuxPaneOf(w))
	}
	if ttys, err := c.ClientTTYs(); err == nil {
		viewerTTYs.Store(ttys)
	}
	return panes, nil
}

// tmuxServerRunning reports whether Marmota's tmux server runs, without
// starting it.
func tmuxServerRunning() bool {
	tmuxPanes.mu.Lock()
	connected := tmuxPanes.local != nil
	tmuxPanes.mu.Unlock()
	if connected {
		return true
	}
	return exec.Command("tmux", "-L", sessionhost.Socket, "has-session", "-t", //nolint:gosec // fixed words
		sessionhost.TmuxSession).Run() == nil
}

func tmuxTarget(id int) (*sessionhost.Client, tmuxRef, error) {
	ref, ok := tmuxPanes.ref(id)
	if !ok {
		return nil, tmuxRef{}, fmt.Errorf("no tmux pane %d", id)
	}
	c, err := localTmux()
	return c, ref, err
}

func tmuxGetText(id int) (string, error) {
	c, ref, err := tmuxTarget(id)
	if err != nil {
		return "", err
	}
	return c.Capture(ref.pane)
}

func tmuxSendText(id int, text string) error {
	c, ref, err := tmuxTarget(id)
	if err != nil {
		return err
	}
	return c.Paste(ref.pane, text)
}

func tmuxSendKeys(id int, keys string) error {
	c, ref, err := tmuxTarget(id)
	if err != nil {
		return err
	}
	return c.SendBytes(ref.pane, []byte(keys))
}

// tmuxCloseWindow ends a window and the session in it.
func tmuxCloseWindow(id int) error {
	c, ref, err := tmuxTarget(id)
	if err != nil {
		return err
	}
	return c.CloseWindow(ref.window)
}

// tmuxWindowName names the window of a session: by its id when it is known,
// so the window says whose it is.
func tmuxWindowName(provider managedAgentProvider, id string) string {
	prefix := sessionhost.ClaudePrefix
	if provider == managedAgentProviderCodex {
		prefix = sessionhost.CodexPrefix
	}
	if id == "" {
		id = "new-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return sessionhost.NameFor(prefix, id)
}

// tmuxSessionsOn reports whether new sessions start in tmux.
func (m *model) tmuxSessionsOn() bool {
	if !useTmuxSessions() {
		return false
	}
	_, err := currentWeztermPaneID()
	return err == nil
}

// startTmuxAgent starts command in a new tmux window in cwd and returns its
// pane id. Nothing on screen changes. id names the window when known.
func (m *model) startTmuxAgent(cwd, command string, provider managedAgentProvider, id string) (int, error) {
	c, err := localTmux()
	if err != nil {
		return 0, err
	}
	w, err := c.Start(tmuxWindowName(provider, id), cwd, command)
	if err != nil {
		return 0, fmt.Errorf("couldn't start the session in tmux: %w", err)
	}
	pane := tmuxPaneOf(w)
	// Listed at once, so the session is found in it before the next refresh.
	m.claude.panes = append(m.claude.panes, pane)
	m.tmux.lastLaunched = pane.PaneID
	return pane.PaneID, nil
}

// tmuxView names the view session of this Marmota's pane beside it.
func tmuxView(superfilePaneID int) string {
	return "view-" + strconv.Itoa(superfilePaneID)
}

// tmuxViewerPane is the WezTerm pane beside Marmota that shows a tmux window,
// when there is one in Marmota's tab.
func (m *model) tmuxViewerPane() (weztermPane, bool) {
	if m.tmux.viewer == 0 {
		return weztermPane{}, false
	}
	self, err := currentWeztermPaneID()
	if err != nil {
		return weztermPane{}, false
	}
	panes, err := listWeztermTabPanes(m.getFocusedFilePanel().location, self)
	if err != nil {
		return weztermPane{}, false
	}
	for _, p := range panes {
		if p.PaneID == m.tmux.viewer {
			return p, true
		}
	}
	m.tmux.viewer, m.tmux.viewWindow = 0, ""
	return weztermPane{}, false
}

// showTmuxPane shows a tmux window beside Marmota: in the pane already there
// that shows one, else in a new pane. With focus the keyboard goes there.
func (m *model) showTmuxPane(id int, title string, focus bool) (string, error) {
	c, ref, err := tmuxTarget(id)
	if err != nil {
		return "", err
	}
	// Scrolled back, it would show an old screen and take no keys.
	if err := c.LeaveCopyMode(ref.pane); err != nil {
		slog.Debug("Could not leave copy mode", "pane", ref.pane, "error", err)
	}
	self, err := currentWeztermPaneID()
	if err != nil {
		return "", err
	}
	cwd := m.getFocusedFilePanel().location
	view := tmuxView(self)
	viewer, shown := m.tmuxViewerPane()
	switch {
	case shown && m.tmux.viewWindow == ref.window:
		if err := activateWeztermPane(cwd, viewer.PaneID); err != nil {
			return "", err
		}
		return "Focused " + title, nil
	case shown:
		if err := c.Show(view, ref.window); err != nil {
			return "", err
		}
	default:
		m.parkLegacyAgentPane(cwd, self)
		paneID, err := m.splitTmuxViewer(cwd, self, sessionhost.AttachCommand(view, ref.window))
		if err != nil {
			return "", err
		}
		m.tmux.viewer = paneID
	}
	m.tmux.viewWindow = ref.window
	m.markTmuxShown(id)
	target := self
	if focus {
		target = m.tmux.viewer
	}
	// A new pane takes the keyboard; give it back unless asked not to.
	if err := activateWeztermPane(cwd, target); err != nil {
		slog.Warn("Could not move the keyboard", "pane", target, "error", err)
	}
	if focus {
		return "Switched to " + title, nil
	}
	return "Showing " + title, nil
}

// splitTmuxViewer opens the pane beside Marmota that shows tmux windows.
func (m *model) splitTmuxViewer(cwd string, self int, attach string) (int, error) {
	tabPanes, err := listWeztermTabPanes(cwd, self)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}
	pane, err := findWeztermPaneByID(tabPanes, self)
	if err != nil {
		return 0, err
	}
	_, agentCols, err := initialManagedWeztermSplitWidths(pane.Size.Cols)
	if err != nil {
		return 0, err
	}
	return splitWeztermPane(cwd, fmt.Sprintf("wezterm cli split-pane --pane-id %d --right --cells %d --cwd %q -- "+
		"/bin/sh -c %s", self, agentCols, cwd, shellQuote(attach)))
}

// parkLegacyAgentPane moves a session's WezTerm pane, from before sessions
// ran in tmux, out of the way of the pane that shows tmux windows.
func (m *model) parkLegacyAgentPane(cwd string, self int) {
	state := m.managedWeztermState
	for i, s := range state.Sessions {
		if s.VisibleSlot != managedAgentSlotOne {
			continue
		}
		if err := parkWeztermPane(cwd, s.PaneID); err != nil {
			slog.Debug("Could not park the old agent pane", "pane", s.PaneID, "error", err)
			continue
		}
		state.Sessions[i].VisibleSlot = managedAgentHiddenSlot
		state.ManagedPaneIDs, state.ForegroundPaneID = nil, 0
		persistWeztermPaneLayoutState(self, state)
		m.setManagedWeztermState(self, state)
	}
}

// markTmuxShown records which window is beside Marmota on the panes the
// rest of Marmota reads: it is in Marmota's tab, the others aren't.
func (m *model) markTmuxShown(id int) {
	tab := m.superfileTabID()
	for i := range m.claude.panes {
		if p := &m.claude.panes[i]; isTmuxPane(p.PaneID) {
			p.TabID = -1
			if p.PaneID == id {
				p.TabID = tab
			}
		}
	}
}

// settleTmuxShown reads which window the pane beside Marmota shows, after a
// refresh listed the panes anew.
func (m *model) settleTmuxShown() {
	if _, shown := m.tmuxViewerPane(); !shown {
		m.markTmuxShown(0)
		return
	}
	self, err := currentWeztermPaneID()
	if err != nil {
		return
	}
	c, err := localTmux()
	if err != nil {
		return
	}
	m.tmux.viewWindow = c.ViewWindow(tmuxView(self))
	for _, p := range m.claude.panes {
		if ref, ok := tmuxPanes.ref(p.PaneID); ok && isTmuxPane(p.PaneID) && ref.window == m.tmux.viewWindow {
			m.markTmuxShown(p.PaneID)
			return
		}
	}
	m.markTmuxShown(0)
}

// hideTmuxViewer closes the pane beside Marmota that shows a tmux window.
// The session goes on in tmux.
func (m *model) hideTmuxViewer() error {
	viewer, shown := m.tmuxViewerPane()
	if !shown {
		return errors.New("no session is beside Marmota")
	}
	if err := killWeztermPane(m.getFocusedFilePanel().location, viewer.PaneID); err != nil {
		return err
	}
	m.tmux.viewer, m.tmux.viewWindow = 0, ""
	m.markTmuxShown(0)
	return nil
}

// viewerTTYs are the terminals of the panes that show a tmux window, as last
// listed: their titles are the sessions', yet no session runs in them.
var viewerTTYs atomic.Value //nolint:gochecknoglobals // set by refreshes

// isViewerPane reports a WezTerm pane that only shows a tmux window.
func isViewerPane(p weztermPane) bool {
	ttys, _ := viewerTTYs.Load().([]string)
	return p.TTYName != "" && slices.Contains(ttys, p.TTYName)
}

// tmuxState is this Marmota's side of the sessions in tmux.
type tmuxState struct {
	// viewer is the WezTerm pane beside Marmota that shows a window, and
	// viewWindow the window it shows.
	viewer     int
	viewWindow string
	// lastLaunched is the pane of the session started last.
	lastLaunched int
	// migrated holds the old WezTerm panes whose sessions moved to tmux.
	migrated map[int]bool
}

// noTmuxNotice says why sessions don't start in tmux, once.
func noTmuxNotice() string {
	return "tmux isn't installed: sessions run in WezTerm panes and end with WezTerm. " +
		"Install it with `brew install tmux`."
}

// tmuxPaneShowingSession is the tmux window that shows a session whose own
// terminal is in no pane: one Claude Code runs in the background, such as
// one /resume brought into a window whose session it replaced.
// Claude Code puts the session's name in the window's title. A window
// whose own session still runs there isn't it.
func (m *model) tmuxPaneShowingSession(s claudesession.Session) (weztermPane, bool) {
	if !s.Live || s.Name == "" {
		return weztermPane{}, false
	}
	taken := map[string]bool{}
	for _, live := range m.claude.snapshot.Live {
		if live.TTY != "" {
			taken[live.TTY] = true
		}
	}
	for _, p := range m.claude.panes {
		if isTmuxPane(p.PaneID) && !taken[p.TTYName] && sessionNameInTitle(p.Title) == s.Name {
			return p, true
		}
	}
	return weztermPane{}, false
}

// sessionNameInTitle is the session name in a Claude Code window's title,
// "✳ marmota-nova" or "marmota-nova".
func sessionNameInTitle(title string) string {
	title = strings.TrimSpace(title)
	if glyph, rest, ok := strings.Cut(title, " "); ok && utf8.RuneCountInString(glyph) == 1 {
		if r, _ := utf8.DecodeRuneInString(glyph); !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return strings.TrimSpace(rest)
		}
	}
	return title
}

// focusAgentPane puts the keyboard in a session's pane, showing it beside
// Marmota first when it runs in tmux.
func (m *model) focusAgentPane(paneID int, title string) error {
	if isTmuxPane(paneID) {
		_, err := m.showTmuxPane(paneID, title, true)
		return err
	}
	return activateWeztermPane(m.getFocusedFilePanel().location, paneID)
}

// dropSessionsOutsideMarmota leaves out the running sessions Marmota didn't
// start: those in panes of your own, in other terminals, or in the
// background. Sessions that were running when sessions first ran in tmux
// all stay, whoever started them, so nothing listed before goes missing.
func (m *model) dropSessionsOutsideMarmota() {
	since := tmuxSince()
	if !m.managedWeztermState.loaded {
		m.refreshManagedWeztermState()
	}
	legacy := map[int]bool{}
	for _, s := range m.managedWeztermState.Sessions {
		legacy[s.PaneID] = true
	}
	snap := &m.claude.snapshot
	live := snap.Live[:0]
	for _, s := range snap.Live {
		pane, inPane := m.weztermPaneForSession(s)
		switch {
		case inPane && (isTmuxPane(pane.PaneID) || legacy[pane.PaneID]),
			!s.StartedAt.IsZero() && s.StartedAt.Before(since):
			live = append(live, s)
		}
	}
	snap.Live = live
}

// tmuxSince is when sessions first ran in tmux on this computer, noted the
// first time it is asked.
func tmuxSince() time.Time {
	if data, err := os.ReadFile(variable.TmuxSinceFile); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data))); err == nil {
			return t
		}
	}
	now := time.Now()
	if err := os.WriteFile(variable.TmuxSinceFile, []byte(now.Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		slog.Debug("Could not note when sessions started running in tmux", "error", err)
	}
	return now
}

// migrateToTmux moves the sessions that wait in WezTerm's hidden workspace,
// parked there by a Marmota from before sessions ran in tmux (this one or
// any other), into tmux: each, once it is idle with nothing typed, is closed
// and resumed in a window of its own. Busy ones wait for a later refresh.
// The one beside a Marmota stays; it moves once it is parked.
func (m *model) migrateToTmux() tea.Cmd {
	if !m.tmuxSessionsOn() {
		return nil
	}
	self, err := currentWeztermPaneID()
	if err != nil {
		return nil
	}
	if !m.managedWeztermState.loaded {
		m.refreshManagedWeztermState() // the panes an earlier Marmota in this pane started
	}
	cwd := m.getFocusedFilePanel().location
	var moved []string
	gone := map[int]bool{}
	for _, pane := range append([]weztermPane(nil), m.claude.panes...) {
		if isTmuxPane(pane.PaneID) || pane.Workspace != managedAgentHiddenWorkspace {
			continue
		}
		s, found := m.sessionInPane(pane.PaneID)
		// Only one that waits for you with nothing typed: closing and
		// resuming it then changes nothing you see or lose.
		if !found || s.Status != claudesession.StatusIdle || !promptEmpty(s, pane.PaneID) {
			continue
		}
		if err := killWeztermPane(cwd, pane.PaneID); err != nil {
			slog.Warn("Could not close a session's old pane", "pane", pane.PaneID, "error", err)
			continue
		}
		gone[pane.PaneID] = true
		provider := managedAgentProviderClaude
		if s.IsCodex() {
			provider = managedAgentProviderCodex
		}
		paneID, err := m.startTmuxAgent(m.claudeSessionDir(s), movedSessionCommand(s), provider, s.ID)
		if err != nil {
			slog.Error("Could not resume a session in tmux", "session", s.ID, "error", err)
			m.flashClaude("Couldn't move " + s.DisplayTitle() + " into tmux: " + err.Error())
			continue
		}
		if s.IsCodex() {
			m.recordCodexPane(s.ID, paneID)
		}
		m.movedToPane(s.ID, pane.PaneID, paneID)
		moved = append(moved, s.DisplayTitle())
	}
	if len(gone) == 0 {
		return nil
	}
	state := m.managedWeztermState
	kept := state.Sessions[:0]
	for _, managed := range state.Sessions {
		if !gone[managed.PaneID] {
			kept = append(kept, managed)
		}
	}
	state.Sessions = kept
	state = normalizeWeztermPaneLayoutState(state)
	persistWeztermPaneLayoutState(self, state)
	state.loaded = true
	m.setManagedWeztermState(self, state)
	if err := activateWeztermPane(cwd, self); err != nil {
		slog.Debug("Could not keep the keyboard in Marmota", "error", err)
	}
	if len(moved) > 0 {
		m.flashClaude("Moved into tmux, where they outlive WezTerm: " + strings.Join(moved, ", "))
	}
	return nil
}

// movedSessionCommand resumes a session that moves into tmux as it ran: in
// its permission mode, with its model and effort, and with Remote Control
// when it had it. A Codex session resumes with its own settings.
func movedSessionCommand(s claudesession.Session) string {
	if s.IsCodex() {
		return resumeCommand(s)
	}
	args := append(claudeask.PermissionArgs(s.PermissionMode), claudeask.ChoiceArgs(s.Model, s.Effort)...)
	if s.RemoteControl {
		args = append(args, "--remote-control")
	}
	parts := []string{"claude", "--resume", shellQuote(s.ID)}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

// promptEmpty reports a Claude Code session whose prompt shows and holds
// nothing typed, nor stashed. A Codex session's prompt isn't read: it moves
// once it has ended its turn.
func promptEmpty(s claudesession.Session, paneID int) bool {
	if s.IsCodex() {
		return true
	}
	box, err := readClaudePrompt(paneID)
	return err == nil && box.Shown && strings.TrimSpace(box.Text) == "" && !box.Stashed
}

// movedToPane has the list know at once that a session left its old pane
// for a tmux window: until the next refresh lists it there, enter on it
// would look for the pane that closed.
func (m *model) movedToPane(id string, oldPaneID, newPaneID int) {
	tty := ""
	panes := m.claude.panes[:0]
	for _, p := range m.claude.panes {
		if p.PaneID == oldPaneID {
			continue
		}
		if p.PaneID == newPaneID {
			tty = p.TTYName
		}
		panes = append(panes, p)
	}
	m.claude.panes = panes
	for i := range m.claude.snapshot.Live {
		if s := &m.claude.snapshot.Live[i]; s.ID == id {
			s.TTY, s.Background = tty, false
		}
	}
}

// sessionInPane is the running session in a WezTerm pane.
func (m *model) sessionInPane(paneID int) (claudesession.Session, bool) {
	for _, s := range m.claude.snapshot.Live {
		if pane, ok := m.weztermPaneForSession(s); ok && pane.PaneID == paneID {
			return s, true
		}
	}
	return claudesession.Session{}, false
}
