package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/ui/notify"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

const maxModalTitleRunes = 40

func (m *model) visibleSessionID() string {
	if s, _, ok := m.visibleClaudeSessionPane(); ok {
		return s.ID
	}
	return ""
}

func (m *model) sessionPlace(s claudesession.Session) string {
	pane, ok := m.weztermPaneForSession(s)
	switch {
	case ok && m.isClaudeSessionVisible(s):
		return "beside Marmota"
	case ok && pane.Workspace == managedAgentHiddenWorkspace:
		return "hidden"
	case ok:
		return "in another WezTerm tab"
	case s.Background:
		return "in the background"
	default:
		return "outside WezTerm"
	}
}

// newestFirstFiles lists changed files newest first, relative to the
// session's folder when they are inside it.
func newestFirstFiles(files []string, cwd string) []string {
	out := make([]string, 0, len(files))
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		if cwd != "" {
			if rel, err := filepath.Rel(cwd, f); err == nil && !strings.HasPrefix(rel, "..") {
				f = rel
			}
		}
		out = append(out, tildePath(f))
	}
	return out
}

// focusClaudeSession shows a session beside superfile and puts the keyboard
// in it; a finished session is resumed first.
func (m *model) focusClaudeSession(s claudesession.Session) {
	m.markClaudeSessionSeen(s)
	message, err := m.jumpToClaudeSession(s, true)
	if err != nil {
		slog.Error("Could not switch to Claude session", "session", s.ID, "error", err)
		m.flashClaude(err.Error())
		return
	}
	m.flashClaude(message)
}

// launchVisibleSession runs command in a new agent pane and shows it beside
// superfile; with focus the keyboard moves into it.
func (m *model) launchVisibleSession(cwd, command, message string, focus bool) (string, error) {
	return m.launchVisibleAgent(cwd, command, managedAgentProviderClaude, message, focus)
}

// launchVisibleAgent starts an agent beside superfile. When another agent
// is shown there, the new one takes its place and the other is parked.
func (m *model) launchVisibleAgent(cwd, command string, provider managedAgentProvider, message string,
	focus bool,
) (string, error) {
	message, err := m.launchManagedSessionInWeztermAt(cwd, command, provider, message)
	if err != nil {
		return "", err
	}
	sessions := m.managedWeztermState.Sessions
	if len(sessions) == 0 {
		return message, nil
	}
	launched := sessions[len(sessions)-1]
	superfilePaneID, err := currentWeztermPaneID()
	if err != nil {
		return message, nil //nolint:nilerr // launched; only the swap is skipped
	}
	if launched.VisibleSlot != managedAgentSlotOne {
		if err := m.foregroundManagedPane(superfilePaneID, launched.PaneID); err != nil {
			return message, err
		}
	}
	if focus {
		if err := activateWeztermPane(cwd, launched.PaneID); err != nil {
			slog.Warn("Could not focus the new session's pane", "error", err)
		}
		return message, nil
	}
	// A new pane takes the keyboard; give it back, so the next key still
	// reaches superfile.
	if err := activateWeztermPane(cwd, superfilePaneID); err != nil {
		slog.Warn("Could not give the keyboard back to Marmota", "error", err)
	}
	return message, nil
}

// foregroundManagedPane shows a managed agent pane beside superfile,
// parking the one shown there.
func (m *model) foregroundManagedPane(superfilePaneID, paneID int) error {
	state, err := m.foregroundManagedAgentSessionByPaneID(superfilePaneID, m.managedWeztermState, paneID)
	if err != nil {
		return err
	}
	persistWeztermPaneLayoutState(superfilePaneID, state)
	state.loaded = true
	m.setManagedWeztermState(superfilePaneID, state)
	m.markPaneTabLocally(paneID)
	return nil
}

// launchParkedAgent starts an agent in a new pane in the hidden workspace,
// where it works without taking the place of the agent beside superfile;
// the keyboard stays with superfile. It returns the new pane.
func (m *model) launchParkedAgent(cwd, command string, provider managedAgentProvider) (int, error) {
	superfilePaneID, err := currentWeztermPaneID()
	if err != nil {
		return 0, err
	}
	state, err := loadWeztermPaneLayoutState(superfilePaneID)
	if err != nil {
		slog.Warn("Ignoring unreadable WezTerm pane state", "paneID", superfilePaneID, "error", err)
		state = weztermPaneLayoutState{}
	}
	if allPanes, listErr := listWeztermPanes(cwd); listErr == nil {
		state = pruneDeadManagedPanes(state, allPanes)
	}
	paneID, err := spawnBackgroundManagedSessionInWezterm(cwd, command)
	if err != nil {
		return 0, fmt.Errorf("failed to spawn background agent session: %w", err)
	}
	state.Sessions = append(state.Sessions, managedAgentSession{
		PaneID:      paneID,
		Provider:    provider,
		Cwd:         cwd,
		DisplayName: managedAgentFolderName(cwd),
		VisibleSlot: managedAgentHiddenSlot,
	})
	// Normalizing puts a session in front when none is, which would claim
	// the new one is beside superfile: only a state with one there is.
	if len(state.ManagedPaneIDs) > 0 {
		state = normalizeWeztermPaneLayoutState(state)
	}
	persistWeztermPaneLayoutState(superfilePaneID, state)
	state.loaded = true
	m.setManagedWeztermState(superfilePaneID, state)
	// Should the new pane have taken the keyboard, superfile takes it back.
	if err := activateWeztermPane(cwd, superfilePaneID); err != nil {
		slog.Warn("Could not keep the keyboard in Marmota", "error", err)
	}
	return paneID, nil
}

// forkClaudeSession continues a copy of the session in a new git worktree.
func (m *model) forkClaudeSession(s claudesession.Session) {
	dir := m.claudeSessionDir(s)
	if !insideGitRepository(dir) {
		m.flashClaude("Forking makes a git worktree, and " + tildePath(dir) + " isn't in a git repository")
		return
	}
	command := "claude --resume " + shellQuote(s.ID) + " --fork-session --worktree --dangerously-skip-permissions"
	if s.RemoteControl {
		command += " --remote-control"
	}
	provider := managedAgentProviderClaude
	if s.IsCodex() {
		command, provider = forkCodexCommand(s), managedAgentProviderCodex
	}
	message, err := m.launchVisibleAgent(dir, command, provider, "Forked "+s.DisplayTitle()+" into a new worktree",
		true)
	if err != nil {
		slog.Error("Could not fork Claude session", "session", s.ID, "error", err)
		m.flashClaude(err.Error())
		return
	}
	if s.IsCodex() {
		m.codex.forks = append(m.codex.forks, codexFork{parent: s.ID, paneID: m.lastLaunchedPaneID(), began: time.Now()})
	}
	m.flashClaude(message)
}

// insideGitRepository reports whether dir is in a git repository or
// worktree.
func insideGitRepository(dir string) bool {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return true
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// renameClaudeSession renames a session the way Claude Code's own rename
// does, so `claude --resume`, the Claude app and the sheet all show the name.
// An empty name gives back Claude's own title.
func (m *model) renameClaudeSession(id, name string) tea.Cmd {
	s, ok := m.claude.snapshot.Find(id)
	if !ok {
		m.flashClaude("That session is no longer listed")
		return nil
	}
	if name == s.DisplayTitle() {
		return nil
	}
	if s.IsCodex() {
		// Codex's server names it, as Codex's own rename does.
		if name == "" {
			m.flashClaude("Give it a name: Codex can't clear one")
			return nil
		}
		m.setClaudeSessionTitle(id, name)
		home := codexHome()
		return func() tea.Msg {
			return claudeRenamedMsg{id: id, name: name, err: codexRename(home, id, name), codex: true}
		}
	}
	if name != "" && s.Live {
		if pane, inPane := m.weztermPaneForSession(s); inPane {
			// Renamed by its own /rename, so its pane shows the name at once.
			// The list shows it already.
			m.setClaudeSessionTitle(id, name)
			paneID := pane.PaneID
			return func() tea.Msg {
				_, err := sendToClaudePrompt(paneID, "/rename "+name)
				return claudeRenamedMsg{id: id, name: name, err: err}
			}
		}
	}
	m.nameClaudeSessionInTranscript(s, name, "")
	return nil
}

// claudeRenamedMsg reports a /rename typed into a running session, or a
// name Codex's server was given.
type claudeRenamedMsg struct {
	id, name string
	err      error
	codex    bool
}

func (m *model) applyClaudeRenamed(msg claudeRenamedMsg) {
	if msg.err == nil {
		m.flashClaude("Renamed to " + msg.name)
		return
	}
	if msg.codex {
		slog.Error("Could not rename the Codex session", "session", msg.id, "error", msg.err)
		m.flashClaude("Couldn't rename: " + msg.err.Error())
		return
	}
	slog.Info("Could not type /rename into the session; writing the name to its transcript",
		"session", msg.id, "error", msg.err)
	s, ok := m.claude.snapshot.Find(msg.id)
	if !ok {
		m.flashClaude("That session is no longer listed")
		return
	}
	m.nameClaudeSessionInTranscript(s, msg.name, msg.err.Error())
}

// nameClaudeSessionInTranscript renames a session by appending the record
// /rename writes to its transcript. The sheet, `claude --resume` and the
// Claude app show the name; a running session takes it when it is resumed.
// why says why a running one wasn't renamed with its own /rename.
func (m *model) nameClaudeSessionInTranscript(s claudesession.Session, name, why string) {
	if s.TranscriptPath == "" {
		m.flashClaude(s.DisplayTitle() + " has no conversation yet; rename it after its first message")
		return
	}
	if err := claudesession.SetCustomTitle(s.TranscriptPath, s.ID, name); err != nil {
		slog.Error("Could not rename Claude session", "session", s.ID, "error", err)
		m.flashClaude("Couldn't rename: " + err.Error())
		return
	}
	if name == "" {
		m.flashClaude("Name cleared; Claude's own title shows after the next refresh")
		return
	}
	m.setClaudeSessionTitle(s.ID, name)
	if s.Live && why != "" {
		m.flashClaude("Renamed to " + name + ". Its pane shows it once resumed: " + why)
		return
	}
	m.flashClaude("Renamed to " + name)
}

// setClaudeSessionTitle shows a new name for a session until the next
// refresh reads it back.
func (m *model) setClaudeSessionTitle(id, name string) {
	for _, list := range [][]claudesession.Session{m.claude.snapshot.Live, m.claude.snapshot.Past} {
		for i := range list {
			if list[i].ID == id {
				list[i].Title = name
			}
		}
	}
	m.refreshSessionsList()
}

// confirmStopClaudeSession asks before stopping a running session.
func (m *model) confirmStopClaudeSession(s claudesession.Session) {
	if !s.Live {
		m.flashClaude(s.DisplayTitle() + " has already ended")
		return
	}
	m.claude.pendingStop = s.ID
	m.notifyModel = notify.New(true,
		"Stop “"+ansi.Truncate(s.DisplayTitle(), maxModalTitleRunes, "…")+"”?",
		stopSessionText(s),
		notify.StopClaudeSessionAction)
}

// stopPendingClaudeSession stops the session the confirmation was about: it
// closes the session's WezTerm pane, stops a background session with
// `claude stop`, or asks the process to exit.
func (m *model) stopPendingClaudeSession() {
	id := m.claude.pendingStop
	m.claude.pendingStop = ""
	s, ok := m.claude.snapshot.Find(id)
	if !ok || !s.Live {
		return
	}
	title := s.DisplayTitle()
	var err error
	pane, inPane := m.weztermPaneForSession(s)
	if s.IsCodex() && s.Status != claudesession.StatusIdle {
		// Codex's server finishes a turn its window has left: stop it first.
		if err := codexInterrupt(codexHome(), s.ID); err != nil {
			slog.Error("Could not stop the Codex session's turn", "session", s.ID, "error", err)
			m.flashClaude("Couldn't stop " + title + ": " + err.Error())
			return
		}
	}
	switch {
	case s.IsCodex() && !inPane:
		// Its turn is stopped; with no window there is nothing else.
	case inPane:
		cwd := m.getFocusedFilePanel().location
		if err = killWeztermPane(cwd, pane.PaneID); err == nil {
			// Drops the closed pane and shows a parked session in its place.
			m.refreshManagedWeztermState()
			// WezTerm moves the keyboard to another pane when one closes, but
			// you are still in superfile.
			if superfilePaneID, idErr := currentWeztermPaneID(); idErr == nil {
				if actErr := activateWeztermPane(cwd, superfilePaneID); actErr != nil {
					slog.Debug("Could not give the keyboard back to Marmota", "error", actErr)
				}
			}
		}
	case s.Background:
		target := s.Name
		if target == "" {
			target = s.ID
		}
		ctx, cancel := context.WithTimeout(context.Background(), claudeSendTextTimeout)
		_, err = claudeSessionRunner(ctx, "claude", "stop", target)
		cancel()
	case s.PID > 0:
		err = stopProcess(s.PID)
	default:
		err = errors.New("no way to reach it")
	}
	if err != nil {
		slog.Error("Could not stop Claude session", "session", s.ID, "error", err)
		m.flashClaude("Couldn't stop " + title + ": " + err.Error())
		return
	}
	m.endLiveClaudeSession(s)
	m.flashClaude("Stopped " + title)
}

// stopSessionText says what stopping a session does.
func stopSessionText(s claudesession.Session) string {
	if s.IsCodex() {
		return "Codex stops and its pane closes. The conversation is kept: open it again later to resume it."
	}
	return "Claude stops and its pane closes. The conversation is kept: open it again later to resume it."
}

// claudeStopGrace is how long a stopped session's process is given to exit.
// Until then a refresh that still lists it as running is not believed.
const claudeStopGrace = 30 * time.Second

type stoppedClaudeSession struct {
	pid int
	at  time.Time
}

// endLiveClaudeSession shows a session superfile stopped as finished at
// once, instead of when a refresh no longer lists it as running.
func (m *model) endLiveClaudeSession(s claudesession.Session) {
	if m.claude.stopped == nil {
		m.claude.stopped = make(map[string]stoppedClaudeSession)
	}
	m.claude.stopped[s.ID] = stoppedClaudeSession{pid: s.PID, at: time.Now()}
	m.settleStoppedClaudeSessions(false)
	delete(m.claude.unread, s.ID)
	m.refreshSessionsList()
}

// settleStoppedClaudeSessions moves the sessions superfile stopped from the
// running list to the finished ones, which is where the next refresh lists
// them once they have exited. With fresh set the snapshot was
// just collected, and sessions it no longer lists as running are forgotten.
func (m *model) settleStoppedClaudeSessions(fresh bool) {
	snap := &m.claude.snapshot
	running := make(map[string]bool, len(snap.Live))
	live := snap.Live[:0]
	for _, s := range snap.Live {
		stop, stopped := m.claude.stopped[s.ID]
		if !stopped || s.PID != stop.pid || time.Since(stop.at) > claudeStopGrace {
			live = append(live, s)
			continue
		}
		running[s.ID] = true
		if s.TranscriptPath == "" ||
			slices.ContainsFunc(snap.Past, func(p claudesession.Session) bool { return p.ID == s.ID }) {
			continue
		}
		s.Live, s.PID, s.TTY, s.Status = false, 0, "", claudesession.StatusEnded
		snap.Past = append([]claudesession.Session{s}, snap.Past...)
	}
	snap.Live = live
	for id := range m.claude.stopped {
		if fresh && !running[id] {
			delete(m.claude.stopped, id)
		}
	}
}

func firstKey(keys []string) string {
	for _, k := range keys {
		if k != "" {
			return k
		}
	}
	return ""
}

func sessionAgeAnchor(s claudesession.Session) time.Time {
	if !s.LastActivity.IsZero() {
		return s.LastActivity
	}
	return s.StartedAt
}

// shortAge formats the time since t as "now", "4m", "3h", "2d" or "5w".
func shortAge(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	const (
		day  = 24 * time.Hour
		week = 7 * day
	)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < day:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < week:
		return fmt.Sprintf("%dd", int(d/day))
	default:
		return fmt.Sprintf("%dw", int(d/week))
	}
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
