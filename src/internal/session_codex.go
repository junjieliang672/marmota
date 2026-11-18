package internal

// `y session` for Codex sessions. They are found through Codex's server, as
// Marmota lists them, and written to through it rather than typed into a
// pane. A Codex session runs on this computer: Marmota reaches no Codex
// server on another.

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

const (
	// AgentClaudeCode and AgentCodex are what `y session start --agent`
	// takes, and what `y session list` says of each session, as Marmota's
	// Sessions box does.
	AgentClaudeCode = "cc"
	AgentCodex      = "co"

	// codexStartWait is how long `y session start` looks for the session a
	// Codex it started makes, and codexDialogAfter how long before it reads
	// the window for a question Codex asks first.
	codexStartWait   = 45 * time.Second
	codexDialogAfter = 3 * time.Second
	// codexStartEffort is a started Codex session's effort when none is
	// given: the Ask box's.
	codexStartEffort = "medium"
	// unnamedWindow starts the name of a window whose session wasn't known
	// when it opened (see tmuxWindowName).
	unnamedWindow = "new-"
	// codexNeedsYou is what a Codex session that stopped waits for: its
	// server says no more than that it does.
	codexNeedsYou = "an approval or a question"
)

//nolint:gochecknoglobals // test seams
var (
	codexRead      = codexsession.Read
	codexStartPoll = 400 * time.Millisecond
)

// agentOf is how `y session` names a session's agent.
func agentOf(s claudesession.Session) string {
	if s.IsCodex() {
		return AgentCodex
	}
	return AgentClaudeCode
}

// isCodexAgent reports what --agent takes for Codex.
func isCodexAgent(agent string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "", AgentClaudeCode, "claude", "claude-code":
		return false, nil
	case AgentCodex, "codex":
		return true, nil
	}
	return false, fmt.Errorf("--agent is %s (Claude Code) or %s (Codex), not %q", AgentClaudeCode, AgentCodex, agent)
}

// loadedCodexSessions are the sessions Codex's server runs now.
func loadedCodexSessions() []claudesession.Session {
	var out []claudesession.Session
	for _, s := range collectCodexSessions(codexsession.Options{Home: codexHome()}) {
		if s.Live {
			out = append(out, s)
		}
	}
	return out
}

// localCodexSessions are the Codex sessions in this computer's Marmota tmux
// windows.
func localCodexSessions() []claudesession.Session {
	if !useTmuxSessions() || !tmuxServerRunning() {
		return nil
	}
	c, err := localTmux()
	if err != nil {
		return nil
	}
	windows, err := c.Windows()
	if err != nil {
		return nil
	}
	return codexSessionsIn(windows)
}

// codexSessionsIn are the sessions Codex's server runs that the windows
// hold: a window is named for its session, "co-<id>", once that is known,
// and until then titled for it by Codex, "<name> | <folder>".
func codexSessionsIn(windows []sessionhost.Window) []claudesession.Session {
	named := map[string]bool{}
	var unnamed []sessionhost.Window
	for _, w := range windows {
		prefix, id, _ := sessionhost.SessionOf(w.Name)
		switch {
		case prefix != sessionhost.CodexPrefix:
		case strings.HasPrefix(id, unnamedWindow):
			unnamed = append(unnamed, w)
		default:
			named[id] = true
		}
	}
	if len(named) == 0 && len(unnamed) == 0 {
		return nil // no Codex here: its server isn't asked
	}
	var out []claudesession.Session
	taken := map[string]bool{}
	for _, s := range loadedCodexSessions() {
		if named[s.ID] {
			out = append(out, s)
			continue
		}
		for _, w := range unnamed {
			name, folder, titled := codexPaneTitle(w.Title)
			if taken[w.ID] || !titled || folder != filepath.Base(s.Cwd) || name != strings.TrimSpace(s.Title) {
				continue
			}
			taken[w.ID] = true
			out = append(out, s)
			break
		}
	}
	return out
}

// codexSessionByID finds a session Codex's server runs by its id, or the
// start of it, whichever window shows it.
func codexSessionByID(id string) (claudesession.Session, bool) {
	if len(id) < minSessionIDPrefix {
		return claudesession.Session{}, false
	}
	for _, s := range loadedCodexSessions() {
		if strings.HasPrefix(s.ID, id) {
			return s, true
		}
	}
	return claudesession.Session{}, false
}

// sessionStartCodex starts a Codex session in a window of Marmota's tmux.
// Codex can't be told an id to give a session: it is the first one its
// server loads in the folder from then on. The message goes through the
// server once the session is found, with the effort, and with Codex
// reviewing the approval requests in auto: as flags those would keep the
// session off that server (see codexsession.PermissionArgs).
func sessionStartCodex(o SessionStartOptions) (StartedSession, error) {
	if strings.TrimSpace(o.On) != "" {
		return StartedSession{}, errors.New("a Codex session starts on this computer only: leave --on out")
	}
	c, err := localTmux()
	if err != nil {
		return StartedSession{}, err
	}
	cwd := remoteFolder(o.Cwd, "")
	home := codexHome()
	known := map[string]bool{}
	for _, s := range loadedCodexSessions() {
		known[s.ID] = true
	}
	mode := o.Mode
	if mode == "default" { // Claude Code's name for asking each time
		mode = "manual"
	}
	args := append(codexsession.PermissionArgs(mode), codexsession.ModelArgs(o.Model)...)
	began := time.Now()
	w, err := c.Start(tmuxWindowName(managedAgentProviderCodex, ""), cwd, codexLaunch(args...))
	if err != nil {
		return StartedSession{}, err
	}
	id, err := awaitCodexStart(c, w, cwd, began, known)
	if err != nil {
		_ = c.CloseWindow(w.ID) // nothing lists it: it would run out of sight
		return StartedSession{}, err
	}
	name := sessionhost.NameFor(sessionhost.CodexPrefix, id)
	if err := c.Rename(w.ID, name); err != nil {
		slog.Warn("Could not name a Codex session's window", "session", id, "error", err)
		name = w.Name
	}
	started := StartedSession{ID: id, Cwd: cwd, Window: name}
	if o.Prompt == "" {
		return started, nil
	}
	turn := codexsession.TurnOptions{
		Effort: firstNonEmpty(o.Effort, codexStartEffort), Reviewer: codexsession.PermissionReviewer(mode),
	}
	if err := codexSend(home, id, o.Prompt, turn); err != nil {
		return started, fmt.Errorf("it started (%s), but the message couldn't be sent: %w", id, err)
	}
	return started, nil
}

// awaitCodexStart waits for the session the Codex in window w makes: the
// first one its server loads in cwd since began that wasn't known. A Codex
// that closes, or asks something first, such as whether to trust the
// folder, ends the wait with why.
func awaitCodexStart(c *sessionhost.Client, w sessionhost.Window, cwd string, began time.Time, known map[string]bool,
) (string, error) {
	for {
		time.Sleep(codexStartPoll)
		var found []claudesession.Session
		for _, s := range loadedCodexSessions() {
			if !known[s.ID] && sameDir(s.Cwd, cwd) && !s.StartedAt.Before(began.Add(-codexStartLookBack)) {
				found = append(found, s)
			}
		}
		if len(found) > 0 {
			slices.SortFunc(found, func(a, b claudesession.Session) int { return a.StartedAt.Compare(b.StartedAt) })
			return found[0].ID, nil
		}
		if windows, err := c.Windows(); err == nil &&
			!slices.ContainsFunc(windows, func(o sessionhost.Window) bool { return o.ID == w.ID }) {
			return "", errors.New("Codex closed before it started a session: is it installed and signed in?")
		}
		if screen, err := c.Capture(w.PaneID); err == nil {
			if why, refused := codexServerRefusal(screen); refused {
				return "", fmt.Errorf("Codex can't use its background server (%s): restart it with "+
					"`codex app-server daemon restart`, then ask again", why)
			}
			if lines, question := startDialog(screen); lines != nil && time.Since(began) > codexDialogAfter {
				return "", fmt.Errorf("Codex asks first: %q. Start a Codex session in %s from Marmota's Ask box "+
					"once, answer it there, then ask again", question, folderName(cwd))
			}
		}
		if time.Since(began) > codexStartWait {
			return "", fmt.Errorf("no Codex session showed in %s after %s", folderName(cwd), codexStartWait)
		}
	}
}

// codexNoServer heads what a Codex window shows when the server every
// Codex window shares won't have it: it offers to run by itself, out of
// Marmota's sight. Its next line says why.
const codexNoServer = "Cannot use the background server"

// codexServerRefusal reports a Codex window that says it can't use Codex's
// server, and why.
func codexServerRefusal(screen string) (string, bool) {
	_, after, found := strings.Cut(ansi.Strip(screen), codexNoServer)
	if !found {
		return "", false
	}
	for _, line := range strings.Split(after, "\n")[1:] {
		if line = strings.TrimSpace(line); line != "" {
			return line, true
		}
	}
	return "it doesn't say why", true
}

// sessionAskCodex is SessionAsk for a Codex session: the message goes
// through Codex's server, which runs it after the turn under way, and the
// reply is read off the session's rollout: what Codex marked as its answer,
// or all it wrote in the turn when it marked none.
func sessionAskCodex(s claudesession.Session, text string, timeout time.Duration, sent, patient bool) (string, error) {
	home := codexHome()
	tracker := codexsession.NewTracker()
	look := func() askedState {
		var st askedState
		now, err := codexRead(home, s.ID, tracker)
		if err != nil {
			return st
		}
		if now.TranscriptPath != "" {
			st.turns, _ = codexsession.Conversation(now.TranscriptPath, sessionAskTurns)
			for i := range st.turns {
				// its answer, without what it said as it worked
				if answer := st.turns[i].Answer; answer != "" {
					st.turns[i].Reply = answer
				}
			}
			st.lastTurnEnd = now.LastTurnEnd
		}
		if now.Status == claudesession.StatusWaiting {
			st.needsYou = codexNeedsYou
		}
		return st
	}
	key := askKey(text)
	if sent {
		return awaitAskedTurn(look, key, time.Time{}, timeout, patient)
	}
	before := look()
	if err := codexSend(home, s.ID, text, codexsession.TurnOptions{}); err != nil {
		return "", err
	}
	return awaitAskedTurn(look, key, askedAt(before.turns, key), timeout, patient)
}
