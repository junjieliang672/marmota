package internal

// `y session`: what a coding agent runs to start, list, write to and read
// the sessions Marmota runs in tmux, here or on another computer. It works
// without Marmota running, on the same tmux servers and remotes file, so
// what it starts shows in Marmota's Sessions box as any session does. The
// sessions are Claude Code's, and on this computer Codex's too (see
// session_codex.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// SessionStartOptions are what `y session start` takes.
type SessionStartOptions struct {
	// Agent is who runs it: "" or "cc" Claude Code, "co" Codex.
	Agent string
	// On is how to reach the computer, "ssh me@host"; "" for this one.
	On     string
	Cwd    string
	Prompt string
	// Model, Effort and Mode as Claude Code takes them; "" for its
	// defaults, and for Mode bypassPermissions, as the Ask box starts one.
	// Codex takes its own models, its nearest to each mode, and medium
	// effort when none is given.
	Model, Effort, Mode string
	// Temporary starts a session Marmota doesn't list (see
	// temporary_sessions.go), which closes when the process Owner has gone:
	// the program that asked for it. It starts on this computer.
	Temporary bool
	Owner     int
}

// StartedSession is a session `y session start` started.
type StartedSession struct {
	ID     string `json:"id"`
	Agent  string `json:"agent"`
	Host   string `json:"host"`
	Cwd    string `json:"cwd"`
	Window string `json:"window"`
}

// SessionStart starts a Claude Code session in tmux, on this computer or on
// the one On reaches, which Marmota remembers from then on. A session on
// another computer runs with Remote Control, so the Claude app has it too.
// With Agent "co" it starts a Codex session, on this computer.
func SessionStart(o SessionStartOptions) (StartedSession, error) {
	codex, err := isCodexAgent(o.Agent)
	if err != nil {
		return StartedSession{}, err
	}
	if o.Temporary && strings.TrimSpace(o.On) != "" {
		return StartedSession{}, errors.New("a temporary session starts on this computer only: leave --on out")
	}
	if codex {
		started, err := sessionStartCodex(o)
		started.Agent = AgentCodex
		return started, err
	}
	host := ""
	if strings.TrimSpace(o.On) != "" {
		h, err := rememberRemoteHost(o.On)
		if err != nil {
			return StartedSession{}, err
		}
		host = h.Name
	}
	c, err := hostClient(host)
	if err != nil {
		return StartedSession{}, err
	}
	cwd := remoteFolder(o.Cwd, host)
	id := newAskSessionID()
	args := append(claudeask.PermissionArgs(o.Mode), claudeask.ChoiceArgs(o.Model, o.Effort)...)
	if host != "" {
		args = append(args, "--remote-control")
	}
	parts := []string{"claude", "--session-id", shellQuote(id)}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	if o.Prompt != "" {
		parts = append(parts, "--", shellQuote(o.Prompt))
	}
	command := strings.Join(parts, " ")
	if o.Temporary {
		// On the record before it runs: Marmota never lists it.
		if err := rememberTemporarySession(id); err != nil {
			return StartedSession{}, fmt.Errorf("couldn't record the temporary session: %w", err)
		}
		command = closesWith(o.Owner) + command
	}
	w, err := c.Start(sessionhost.NameFor(sessionhost.ClaudePrefix, id), cwd, command)
	if err != nil {
		return StartedSession{}, err
	}
	return StartedSession{ID: id, Agent: AgentClaudeCode, Host: host, Cwd: cwd, Window: w.Name}, nil
}

// remoteFolder is where a session starts: here the current folder when none
// is given; on another computer its home, and a folder under this
// computer's home is taken as one under that computer's: a shell expanded
// --cwd ~/proj into /Users/me/proj before it got here.
func remoteFolder(cwd, host string) string {
	if host == "" {
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		return cwd
	}
	if cwd == "" {
		return "~"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if cwd == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(cwd, home+"/"); ok {
			return "~/" + rest
		}
	}
	return cwd
}

// ListedSession is one line of `y session list`.
type ListedSession struct {
	ID string `json:"id"`
	// Agent is "cc" for Claude Code, "co" for Codex.
	Agent  string `json:"agent"`
	Host   string `json:"host"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Cwd    string `json:"cwd"`
	// Waiting is what it waits on when it needs you.
	Waiting string `json:"waiting,omitempty"`
}

// SessionList lists the sessions running in Marmota's tmux, here and on
// every computer it remembers: Claude Code's, then Codex's, which run here.
// Computers it can't reach come back as errors beside what could be listed.
// With local, only this computer's are listed, which is the quick part: the
// editor's list of sessions to ask is that.
func SessionList(local bool) ([]ListedSession, map[string]error) {
	tracker := claudesession.NewTracker()
	var out []ListedSession
	add := func(s claudesession.Session) {
		waiting := ""
		if s.Status == claudesession.StatusWaiting {
			waiting = firstNonEmpty(s.PendingTool, s.WaitingFor)
		}
		if waiting == "" && s.IsCodex() && s.Status == claudesession.StatusWaiting {
			waiting = codexNeedsYou
		}
		out = append(out, ListedSession{ID: s.ID, Agent: agentOf(s), Host: s.Host, Status: string(s.Status),
			Title: s.DisplayTitle(), Cwd: s.Cwd, Waiting: waiting})
	}
	for _, s := range withoutTemporary(localTmuxSessions(tracker)) {
		add(s)
	}
	for _, s := range withoutTemporary(localCodexSessions()) {
		add(s)
	}
	if local {
		return out, nil
	}
	remote := listRemoteSessions(tracker)
	for _, s := range remote.sessions {
		add(s)
	}
	return out, remote.failed
}

// localTmuxSessions are the Claude Code sessions in this computer's
// Marmota tmux windows.
func localTmuxSessions(tracker *claudesession.Tracker) []claudesession.Session {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := claudeSessionRunner(ctx, "claude", "agents", "--json")
	if err != nil {
		return nil
	}
	live, _ := claudesession.ParseLive(data)
	byID := map[string]claudesession.Session{}
	for _, s := range live {
		byID[s.ID] = s
	}
	var out []claudesession.Session
	for _, w := range windows {
		prefix, id, _ := sessionhost.SessionOf(w.Name)
		s, ok := byID[id]
		if prefix != sessionhost.ClaudePrefix || !ok {
			continue
		}
		if path := claudesession.FindTranscript(claudeProjectsDir(), s.Cwd, s.ID); path != "" {
			s.TranscriptPath = path
			if sum, err := tracker.Summary(path); err == nil {
				claudesession.ApplySummary(&s, sum)
			}
		}
		out = append(out, s)
	}
	return out
}

// minSessionIDPrefix is how much of a session's id names it.
const minSessionIDPrefix = 4

// findTmuxSession finds a Claude Code session's window, here or on a
// computer Marmota remembers, by its id or the start of it.
func findTmuxSession(id string) (host string, pane int, err error) {
	if len(id) < minSessionIDPrefix {
		return "", 0, errors.New("give at least 4 characters of the session's id")
	}
	hosts := []string{""}
	for _, h := range loadRemoteHosts() {
		hosts = append(hosts, h.Name)
	}
	for _, name := range hosts {
		if name == "" && (!useTmuxSessions() || !tmuxServerRunning()) {
			continue
		}
		c, err := hostClient(name)
		if err != nil {
			continue
		}
		windows, err := c.Windows()
		if err != nil {
			continue
		}
		for _, w := range windows {
			// A Codex session's prompt isn't typed into: its server takes
			// what is sent (see codexSessionByID).
			if prefix, wid, _ := sessionhost.SessionOf(w.Name); prefix == sessionhost.ClaudePrefix &&
				strings.HasPrefix(wid, id) {
				return name, tmuxPaneOf(name, w).PaneID, nil
			}
		}
	}
	return "", 0, fmt.Errorf("no session %s runs in Marmota's tmux here or on a computer it remembers", id)
}

// SessionSend types text into a session's prompt and sends it, as the Ask
// box does: what was typed there is set aside and put back. Claude Code
// queues it while it is busy. A Codex session gets it through Codex's
// server, which runs it after the turn under way.
func SessionSend(id, text string) error {
	if s, ok := codexSessionByID(id); ok {
		return codexSend(codexHome(), s.ID, text, codexsession.TurnOptions{})
	}
	_, pane, err := findTmuxSession(id)
	if err != nil {
		return err
	}
	_, err = sendToClaudePrompt(pane, text)
	return err
}

// sessionByID finds a session by its id, or the start of it: where
// it runs, its pane, its whole id, and how to read its transcript, which for
// another computer brings the copy here up to date first.
func sessionByID(id string) (host string, pane int, full string, transcript func() (string, error), err error) {
	host, pane, err = findTmuxSession(id)
	if err != nil {
		return "", 0, "", nil, err
	}
	ref, _ := tmuxPanes.ref(pane)
	c, err := hostClient(host)
	if err != nil {
		return "", 0, "", nil, err
	}
	windows, err := c.Windows()
	if err != nil {
		return "", 0, "", nil, err
	}
	for _, win := range windows {
		if win.ID == ref.window {
			_, full, _ = sessionhost.SessionOf(win.Name)
		}
	}
	transcript = func() (string, error) {
		if host == "" {
			return findLocalTranscript(full), nil
		}
		return mirrorTranscript(c, host, full)
	}
	return host, pane, full, transcript, nil
}

// SessionRead writes the last turns of a session's conversation.
func SessionRead(w io.Writer, id string, turns int) error {
	if s, ok := codexSessionByID(id); ok {
		if s.TranscriptPath == "" {
			return errors.New("it has no conversation yet")
		}
		conv, err := codexsession.Conversation(s.TranscriptPath, turns)
		if err != nil {
			return err
		}
		writeTurns(w, conv, "codex")
		return nil
	}
	_, _, _, transcript, err := sessionByID(id)
	if err != nil {
		return err
	}
	path, err := transcript()
	if err != nil {
		return err
	}
	if path == "" {
		return errors.New("it has no conversation yet")
	}
	conv, _, err := claudesession.ConversationBack(path, turns)
	if err != nil {
		return err
	}
	writeTurns(w, conv, "claude")
	return nil
}

// writeTurns writes turns as `y session read` shows them, who being the
// agent that replied.
func writeTurns(w io.Writer, conv []claudesession.Turn, who string) {
	for _, t := range conv {
		if t.Prompt != "" {
			fmt.Fprintf(w, "## you\n%s\n\n", strings.TrimSpace(t.Prompt))
		}
		for _, tool := range t.Tools {
			fmt.Fprintf(w, "- %s\n", tool)
		}
		if len(t.Tools) > 0 {
			fmt.Fprintln(w)
		}
		if t.Reply != "" {
			fmt.Fprintf(w, "## %s\n%s\n\n", who, strings.TrimSpace(t.Reply))
		}
	}
}

// findLocalTranscript finds a session's transcript here by its id alone.
func findLocalTranscript(id string) string {
	dirs, _ := os.ReadDir(claudeProjectsDir())
	for _, d := range dirs {
		path := claudeProjectsDir() + "/" + d.Name() + "/" + id + ".jsonl"
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// WriteSessionList writes `y session list`, as a table or as JSON.
func WriteSessionList(w io.Writer, sessions []ListedSession, failed map[string]error, asJSON bool) error {
	if asJSON {
		type listing struct {
			Sessions    []ListedSession   `json:"sessions"`
			Unreachable map[string]string `json:"unreachable,omitempty"`
		}
		l := listing{Sessions: sessions, Unreachable: map[string]string{}}
		for host, err := range failed {
			l.Unreachable[host] = err.Error()
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(l)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tAGENT\tHOST\tSTATUS\tTITLE\tFOLDER")
	for _, s := range sessions {
		host := s.Host
		if host == "" {
			host = "here"
		}
		status := s.Status
		if s.Waiting != "" {
			status += ": " + s.Waiting
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Agent, host, status, s.Title, s.Cwd)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for host, err := range failed {
		fmt.Fprintf(w, "%s: %v\n", host, err)
	}
	return nil
}

// AdoptedSession is a session `y session adopt` found, or moved.
type AdoptedSession struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Cwd    string `json:"cwd"`
	// Moved is set once it runs in Marmota's tmux.
	Moved bool `json:"moved"`
	// Error is why it couldn't be moved.
	Error string `json:"error,omitempty"`
}

// adoptStopWait is how long a session's old process is given to exit.
const adoptStopWait = 10 * time.Second

// SessionAdopt moves the Claude Code sessions running on the computer on
// reaches, outside Marmota's tmux, into it: each one's process is stopped,
// leaving its terminal at the shell, and it is resumed with the same id in
// a window of Marmota's tmux there, in its own permission mode, model,
// effort and Remote Control. It is listed in Marmota from then on. names
// picks some by name or id, or the start of an id; none picks all. With
// listOnly nothing is moved.
func SessionAdopt(on string, names []string, listOnly bool) ([]AdoptedSession, error) {
	if strings.TrimSpace(on) == "" {
		return nil, errors.New("give --on: the sessions on this computer aren't moved")
	}
	h, err := rememberRemoteHost(on)
	if err != nil {
		return nil, err
	}
	c, err := hostClient(h.Name)
	if err != nil {
		return nil, err
	}
	candidates, err := sessionsOutsideMarmota(c, h.Name)
	if err != nil {
		return nil, err
	}
	picked := candidates
	if len(names) > 0 {
		picked = nil
		for _, name := range names {
			found := false
			for _, s := range candidates {
				if s.Name == name || s.ID == name || (len(name) >= 4 && strings.HasPrefix(s.ID, name)) {
					picked, found = append(picked, s), true
				}
			}
			if !found {
				return nil, fmt.Errorf("no session %q runs on %s outside Marmota", name, h.Name)
			}
		}
	}
	out := make([]AdoptedSession, 0, len(picked))
	for _, s := range picked {
		a := AdoptedSession{ID: s.ID, Name: s.Name, Title: s.DisplayTitle(), Status: string(s.Status), Cwd: s.Cwd}
		if !listOnly {
			if err := adoptSession(c, s); err != nil {
				a.Error = err.Error()
			} else {
				a.Moved = true
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// sessionsOutsideMarmota are the Claude Code sessions a computer runs that
// aren't in a window of Marmota's tmux there, with what their transcripts
// and command lines say of how they run.
func sessionsOutsideMarmota(c *sessionhost.Client, host string) ([]claudesession.Session, error) {
	agents, err := c.Agents()
	if err != nil {
		return nil, err
	}
	live, err := claudesession.ParseLive(agents)
	if err != nil {
		return nil, err
	}
	windows, err := c.Windows()
	if err != nil {
		return nil, err
	}
	inMarmota := map[string]bool{}
	for _, w := range windows {
		if _, id, ok := sessionhost.SessionOf(w.Name); ok {
			inMarmota[id] = true
		}
	}
	tracker := claudesession.NewTracker()
	var out []claudesession.Session
	for _, s := range live {
		if inMarmota[s.ID] {
			continue
		}
		s.Host = host
		if path, err := mirrorTranscript(c, host, s.ID); err == nil && path != "" {
			if sum, err := tracker.Summary(path); err == nil {
				claudesession.ApplySummary(&s, sum)
			}
		}
		if s.PID > 0 {
			if command, err := c.Shell("ps -o command= -p "+strconv.Itoa(s.PID), sessionhost.ShellTimeout); err == nil {
				s.RemoteControl = s.RemoteControl || strings.Contains(command, "--remote-control") ||
					strings.Contains(command, "--rc")
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// adoptSession stops a session where it runs and resumes it in Marmota's
// tmux. A session Claude Code runs in the background is stopped with
// `claude stop`; one in a terminal by its process, which leaves the
// terminal at its shell.
func adoptSession(c *sessionhost.Client, s claudesession.Session) error {
	switch {
	case s.Background:
		// `claude stop` takes the job's id, not its name.
		target := s.JobID
		if target == "" {
			target = s.ID
		}
		if _, err := c.Shell("claude stop "+shellQuote(target), sessionhost.ShellTimeout); err != nil {
			return fmt.Errorf("couldn't stop it: %w", err)
		}
	case s.PID > 0:
		pid := strconv.Itoa(s.PID)
		wait := strconv.Itoa(int(adoptStopWait / (100 * time.Millisecond)))
		if _, err := c.Shell(`kill -TERM `+pid+` 2>/dev/null; i=0; while kill -0 `+pid+` 2>/dev/null && [ $i -lt `+
			wait+` ]; do sleep 0.1; i=$((i + 1)); done; kill -KILL `+pid+` 2>/dev/null; true`,
			adoptStopWait+sessionhost.ShellTimeout); err != nil {
			return fmt.Errorf("couldn't stop it: %w", err)
		}
	default:
		return errors.New("it has no process to stop")
	}
	cwd := s.Cwd
	if cwd == "" {
		cwd = "~"
	}
	if _, err := c.Start(sessionhost.NameFor(sessionhost.ClaudePrefix, s.ID), cwd, movedSessionCommand(s)); err != nil {
		return fmt.Errorf("stopped, but couldn't resume it in Marmota's tmux: %w", err)
	}
	return nil
}
