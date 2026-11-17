package internal

// `y session`: what a coding agent runs to start, list, write to and read
// the sessions Marmota runs in tmux, here or on another computer. It works
// without Marmota running, on the same tmux servers and remotes file, so
// what it starts shows in Marmota's Sessions box as any session does.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// SessionStartOptions are what `y session start` takes.
type SessionStartOptions struct {
	// On is how to reach the computer, "ssh me@host"; "" for this one.
	On     string
	Cwd    string
	Prompt string
	// Model, Effort and Mode as Claude Code takes them; "" for its
	// defaults, and for Mode bypassPermissions, as the Ask box starts one.
	Model, Effort, Mode string
}

// StartedSession is a session `y session start` started.
type StartedSession struct {
	ID     string `json:"id"`
	Host   string `json:"host"`
	Cwd    string `json:"cwd"`
	Window string `json:"window"`
}

// SessionStart starts a Claude Code session in tmux, on this computer or on
// the one On reaches, which Marmota remembers from then on. A session on
// another computer runs with Remote Control, so the Claude app has it too.
func SessionStart(o SessionStartOptions) (StartedSession, error) {
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
	cwd := o.Cwd
	switch {
	case cwd == "" && host == "":
		cwd, _ = os.Getwd()
	case cwd == "":
		cwd = "~"
	}
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
	w, err := c.Start(sessionhost.NameFor(sessionhost.ClaudePrefix, id), cwd, strings.Join(parts, " "))
	if err != nil {
		return StartedSession{}, err
	}
	return StartedSession{ID: id, Host: host, Cwd: cwd, Window: w.Name}, nil
}

// ListedSession is one line of `y session list`.
type ListedSession struct {
	ID     string `json:"id"`
	Host   string `json:"host"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Cwd    string `json:"cwd"`
	// Waiting is what it waits on when it needs you.
	Waiting string `json:"waiting,omitempty"`
}

// SessionList lists the sessions running in Marmota's tmux, here and on
// every computer it remembers. Computers it can't reach come back as errors
// beside what could be listed.
func SessionList() ([]ListedSession, map[string]error) {
	tracker := claudesession.NewTracker()
	var out []ListedSession
	add := func(s claudesession.Session) {
		waiting := ""
		if s.Status == claudesession.StatusWaiting {
			waiting = firstNonEmpty(s.PendingTool, s.WaitingFor)
		}
		out = append(out, ListedSession{ID: s.ID, Host: s.Host, Status: string(s.Status), Title: s.DisplayTitle(),
			Cwd: s.Cwd, Waiting: waiting})
	}
	for _, s := range localTmuxSessions(tracker) {
		add(s)
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

// findTmuxSession finds a session's window, here or on a computer Marmota
// remembers, by its id or the start of it.
func findTmuxSession(id string) (host string, pane int, err error) {
	if len(id) < 4 {
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
			if _, wid, _ := sessionhost.SessionOf(w.Name); strings.HasPrefix(wid, id) {
				return name, tmuxPaneOf(name, w).PaneID, nil
			}
		}
	}
	return "", 0, fmt.Errorf("no session %s runs in Marmota's tmux here or on a computer it remembers", id)
}

// SessionSend types text into a session's prompt and sends it, as the Ask
// box does: what was typed there is set aside and put back. Claude Code
// queues it while it is busy.
func SessionSend(id, text string) error {
	_, pane, err := findTmuxSession(id)
	if err != nil {
		return err
	}
	_, err = sendToClaudePrompt(pane, text)
	return err
}

// SessionRead writes the last turns of a session's conversation.
func SessionRead(w io.Writer, id string, turns int) error {
	host, pane, err := findTmuxSession(id)
	if err != nil {
		return err
	}
	ref, _ := tmuxPanes.ref(pane)
	c, err := hostClient(host)
	if err != nil {
		return err
	}
	windows, err := c.Windows()
	if err != nil {
		return err
	}
	full := ""
	for _, win := range windows {
		if win.ID == ref.window {
			_, full, _ = sessionhost.SessionOf(win.Name)
		}
	}
	var path string
	if host == "" {
		path = findLocalTranscript(full)
	} else {
		path, err = mirrorTranscript(c, host, full)
		if err != nil {
			return err
		}
	}
	if path == "" {
		return errors.New("it has no conversation yet")
	}
	conv, err := claudesession.Conversation(path, turns)
	if err != nil {
		return err
	}
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
			fmt.Fprintf(w, "## claude\n%s\n\n", strings.TrimSpace(t.Reply))
		}
	}
	return nil
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
	fmt.Fprintln(tw, "ID\tHOST\tSTATUS\tTITLE\tFOLDER")
	for _, s := range sessions {
		host := s.Host
		if host == "" {
			host = "here"
		}
		status := s.Status
		if s.Waiting != "" {
			status += ": " + s.Waiting
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, host, status, s.Title, s.Cwd)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for host, err := range failed {
		fmt.Fprintf(w, "%s: %v\n", host, err)
	}
	return nil
}
