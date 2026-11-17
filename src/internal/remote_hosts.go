package internal

// Sessions on other computers: each runs in Marmota's tmux over there (see
// package sessionhost), started by `y session start --on <how to reach it>`,
// which remembers the computer. Marmota keeps one connection to each and
// lists the sessions in its windows there with this computer's, each with
// its computer's name. A session's transcript is copied here a piece at a
// time, so the preview, unread marks and titles come from the same code as
// for a session here.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// remoteHostsMu keeps two writers of the remotes file apart.
var remoteHostsMu sync.Mutex //nolint:gochecknoglobals // guards the remotes file

// loadRemoteHosts reads the computers Marmota remembers.
func loadRemoteHosts() []sessionhost.Host {
	data, err := os.ReadFile(variable.RemotesFile)
	if err != nil {
		return nil
	}
	var hosts []sessionhost.Host
	if err := json.Unmarshal(data, &hosts); err != nil {
		slog.Warn("Ignoring an unreadable remotes file", "file", variable.RemotesFile, "error", err)
		return nil
	}
	return slices.DeleteFunc(hosts, func(h sessionhost.Host) bool { return !h.Valid() || h.Local })
}

func saveRemoteHosts(hosts []sessionhost.Host) error {
	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(variable.RemotesFile), 0o755); err != nil {
		return err
	}
	tmp := variable.RemotesFile + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, variable.RemotesFile)
}

func remoteHostNamed(name string) (sessionhost.Host, bool) {
	for _, h := range loadRemoteHosts() {
		if h.Name == name {
			return h, true
		}
	}
	return sessionhost.Host{}, false
}

// sameRoute reports whether two hosts are reached the same way.
func sameRoute(a, b sessionhost.Host) bool {
	return a.SSH == b.SSH && slices.Equal(a.SSHArgs, b.SSHArgs) && slices.Equal(a.Command, b.Command)
}

// rememberRemoteHost finds the computer --on reaches among those Marmota
// remembers, or remembers it, named by what it calls itself. It connects to
// it either way, so a computer that can't be reached isn't remembered.
func rememberRemoteHost(on string) (sessionhost.Host, error) {
	h, err := sessionhost.ParseOn(on)
	if err != nil {
		return sessionhost.Host{}, err
	}
	remoteHostsMu.Lock()
	defer remoteHostsMu.Unlock()
	hosts := loadRemoteHosts()
	for _, known := range hosts {
		if sameRoute(known, h) {
			return known, nil
		}
	}
	// Reached under a name of its own until it says what it is called.
	h.Name = "new-" + h.Name
	hosts = append(hosts, h)
	if err := saveRemoteHosts(hosts); err != nil {
		return sessionhost.Host{}, err
	}
	c, err := hostClient(h.Name)
	if err != nil {
		_ = saveRemoteHosts(hosts[:len(hosts)-1])
		return sessionhost.Host{}, err
	}
	name, err := c.Hostname()
	if err != nil || name == "" {
		name = strings.TrimPrefix(h.Name, "new-")
	}
	name = uniqueHostName(name, hosts[:len(hosts)-1])
	tmuxPanes.mu.Lock()
	delete(tmuxPanes.clients, h.Name)
	tmuxPanes.clients[name] = c
	tmuxPanes.mu.Unlock()
	h.Name = name
	hosts[len(hosts)-1] = h
	return h, saveRemoteHosts(hosts)
}

func uniqueHostName(name string, hosts []sessionhost.Host) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(hosts, func(h sessionhost.Host) bool { return h.Name == n })
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s-%d", name, i); !taken(n) {
			return n
		}
	}
}

// remoteListing is what the computers Marmota remembers run: their
// sessions, the panes of their windows, and the computers it couldn't reach
// with why.
type remoteListing struct {
	sessions []claudesession.Session
	panes    []weztermPane
	failed   map[string]error
}

// listRemoteSessions lists the sessions in Marmota's windows on each
// computer it remembers, copying the new part of each one's transcript.
// Computers are asked one after another; one that can't be reached is left
// out, and tried again later.
func listRemoteSessions(tracker *claudesession.Tracker, skip map[string]bool) remoteListing {
	out := remoteListing{failed: map[string]error{}}
	for _, h := range loadRemoteHosts() {
		if skip[h.Name] {
			out.failed[h.Name] = errLoggingIn
			continue
		}
		sessions, panes, err := listHostSessions(h.Name, tracker)
		if err != nil {
			out.failed[h.Name] = err
			continue
		}
		out.sessions = append(out.sessions, sessions...)
		out.panes = append(out.panes, panes...)
	}
	return out
}

func listHostSessions(name string, tracker *claudesession.Tracker) ([]claudesession.Session, []weztermPane, error) {
	c, err := hostClient(name)
	if err != nil {
		return nil, nil, err
	}
	windows, err := c.Windows()
	if err != nil {
		return nil, nil, err
	}
	if len(windows) == 0 {
		return nil, nil, nil
	}
	agents, err := c.Agents()
	if err != nil {
		return nil, nil, err
	}
	live, err := claudesession.ParseLive(agents)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]claudesession.Session{}
	for _, s := range live {
		byID[s.ID] = s
	}
	var sessions []claudesession.Session
	panes := make([]weztermPane, 0, len(windows))
	for _, w := range windows {
		pane := tmuxPaneOf(name, w)
		panes = append(panes, pane)
		prefix, id, _ := sessionhost.SessionOf(w.Name)
		if prefix != sessionhost.ClaudePrefix {
			continue
		}
		s, listed := byID[id]
		if !listed {
			if starting, ok := startingInWindow(c, name, id, w, pane); ok {
				sessions = append(sessions, starting)
			}
			continue
		}
		s.Host, s.TTY = name, pane.TTYName
		if path, err := mirrorTranscript(c, name, s.ID); err == nil && path != "" {
			s.TranscriptPath = path
			if sum, err := tracker.Summary(path); err == nil {
				claudesession.ApplySummary(&s, sum)
			}
		} else if err != nil {
			slog.Debug("Could not copy a remote transcript", "host", name, "session", s.ID, "error", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, panes, nil
}

// remoteTranscripts remembers, for each copied transcript, where it is over
// there and how much of it was copied.
var remoteTranscripts = struct { //nolint:gochecknoglobals // shared by refreshes
	sync.Mutex
	paths  map[string]string
	copied map[string]int64
}{paths: map[string]string{}, copied: map[string]int64{}}

// mirrorTranscript brings the copy of a remote session's transcript up to
// date and returns its path here, or "" while it has none over there.
func mirrorTranscript(c *sessionhost.Client, host, id string) (string, error) {
	key := host + "/" + id
	local := filepath.Join(variable.RemoteTranscriptsDir, host, id+".jsonl")
	remoteTranscripts.Lock()
	remote, offset := remoteTranscripts.paths[key], remoteTranscripts.copied[key]
	remoteTranscripts.Unlock()
	if remote == "" {
		found, err := c.TranscriptPath(id)
		if err != nil || found == "" {
			return "", err
		}
		remote, offset = found, 0
	}
	if info, err := os.Stat(local); err != nil || info.Size() != offset {
		offset = 0 // the copy is gone, or isn't what was noted: copy it anew
	}
	for {
		data, size, err := c.ReadFrom(remote, offset)
		fresh := offset == 0 || errors.Is(err, sessionhost.ErrShrank)
		if err != nil && !errors.Is(err, sessionhost.ErrShrank) {
			return "", err
		}
		if err := writeMirror(local, data, fresh); err != nil {
			return "", err
		}
		if fresh {
			offset = 0
		}
		offset += int64(len(data))
		if len(data) == 0 || offset >= size {
			break
		}
	}
	remoteTranscripts.Lock()
	remoteTranscripts.paths[key], remoteTranscripts.copied[key] = remote, offset
	remoteTranscripts.Unlock()
	return local, nil
}

func writeMirror(path string, data []byte, fresh bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fresh {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// errLoggingIn is why a computer isn't asked while you log in to it.
var errLoggingIn = errors.New("logging in")

// mergeRemoteSessions lists the other computers' sessions with this one's.
// A computer that can't be reached keeps its sessions in the list as they
// were last seen, marked with why; that is said once, as it happens.
func (m *model) mergeRemoteSessions(r remoteListing) {
	if m.remote.last == nil {
		m.remote.last = loadLastRemoteSessions()
	}
	byHost := map[string][]claudesession.Session{}
	for _, s := range r.sessions {
		if !s.Starting {
			byHost[s.Host] = append(byHost[s.Host], s)
		}
	}
	changed := false
	for _, h := range loadRemoteHosts() {
		if _, failed := r.failed[h.Name]; !failed {
			if !sameSessions(m.remote.last[h.Name], byHost[h.Name]) {
				changed = true
			}
			m.remote.last[h.Name] = byHost[h.Name]
		}
	}
	if changed {
		saveLastRemoteSessions(m.remote.last)
	}
	m.claude.snapshot.Live = append(m.claude.snapshot.Live, r.sessions...)
	for name, err := range r.failed {
		for _, s := range m.remote.last[name] {
			s.Unreachable, s.NeedsLogin = err.Error(), sessionhost.NeedsLogin(err) || errors.Is(err, errLoggingIn)
			m.claude.snapshot.Live = append(m.claude.snapshot.Live, s)
		}
	}
	// The panes of the last listing go: these take their place.
	m.claude.panes = slices.DeleteFunc(m.claude.panes, func(p weztermPane) bool {
		return strings.HasPrefix(p.TTYName, "remote:")
	})
	m.claude.panes = append(m.claude.panes, r.panes...)
	if m.remote.failed == nil {
		m.remote.failed = map[string]string{}
	}
	for name, err := range r.failed {
		if m.remote.failed[name] != err.Error() && !errors.Is(err, errLoggingIn) {
			m.flashClaude(err.Error())
			slog.Warn("A remote computer can't be reached", "host", name, "error", err)
		}
		m.remote.failed[name] = err.Error()
	}
	for name := range m.remote.failed {
		if _, still := r.failed[name]; !still {
			delete(m.remote.failed, name)
		}
	}
}

// remoteState is this Marmota's side of the other computers.
type remoteState struct {
	// failed is why each computer that can't be reached can't, as last said.
	failed map[string]string
	// last is each computer's sessions as last listed, shown while it can't
	// be reached.
	last map[string][]claudesession.Session
	// loginPanes are the panes where you log in to a computer, by its name:
	// it isn't asked while one is open, and is asked at once after.
	loginPanes map[string]int
}

// reachHost is enter on a session whose computer can't be reached: a pane
// beside Marmota logs in to it, where ssh, or the company CLI, asks what it
// asks; or, when logging in wouldn't help, it is asked again now.
func (m *model) reachHost(s claudesession.Session) (string, error) {
	h, ok := remoteHostNamed(s.Host)
	if !ok {
		return "", errors.New("Marmota no longer knows " + s.Host)
	}
	argv := h.LoginArgv()
	if !s.NeedsLogin || len(argv) == 0 {
		forgetHostFailure(s.Host)
		return "Trying " + s.Host + " again", nil
	}
	if _, open := m.remote.loginPanes[s.Host]; open {
		return "", errors.New("log in to " + s.Host + " in the pane beside Marmota")
	}
	self, err := currentWeztermPaneID()
	if err != nil {
		return "", err
	}
	words := make([]string, len(argv))
	for i, a := range argv {
		words[i] = shellQuote(a)
	}
	// Beside Marmota, so its height stays: below the pane that shows a
	// session there, else on its right.
	cwd := m.getFocusedFilePanel().location
	anchor, where := self, "--right --percent 45"
	if viewer, shown := m.tmuxViewerPane(); shown {
		anchor, where = viewer.PaneID, "--bottom --percent 40"
	}
	pane, err := splitWeztermPane(cwd, fmt.Sprintf("wezterm cli split-pane --pane-id %d %s --cwd %q -- /bin/sh -c %s",
		anchor, where, cwd, shellQuote(strings.Join(words, " "))))
	if err != nil {
		return "", err
	}
	if err := activateWeztermPane(cwd, pane); err != nil {
		slog.Debug("Could not focus the login pane", "error", err)
	}
	if m.remote.loginPanes == nil {
		m.remote.loginPanes = map[string]int{}
	}
	m.remote.loginPanes[s.Host] = pane
	return "Log in to " + s.Host + " in the pane beside Marmota", nil
}

// settleLoginPanes forgets the login panes that have closed, and has their
// computers asked at once. It returns the computers still logging in.
func (m *model) settleLoginPanes() map[string]bool {
	open := map[string]bool{}
	if len(m.remote.loginPanes) == 0 {
		return open
	}
	panes, err := listWeztermPanes(m.getFocusedFilePanel().location)
	if err != nil {
		return open
	}
	for host, id := range m.remote.loginPanes {
		if slices.ContainsFunc(panes, func(p weztermPane) bool { return p.PaneID == id }) {
			open[host] = true
			continue
		}
		delete(m.remote.loginPanes, host)
		forgetHostFailure(host)
	}
	return open
}

// forgetHostFailure has a computer that couldn't be reached asked at the
// next chance, rather than after a while.
func forgetHostFailure(name string) {
	tmuxPanes.mu.Lock()
	delete(tmuxPanes.failed, name)
	tmuxPanes.mu.Unlock()
}

// startingInWindow is the row of a session in one of Marmota's windows
// that Claude Code doesn't list yet: one `y session start` started, still
// starting, or asking something first, such as whether to trust its
// folder. Its screen says which; the question is answered from Marmota as
// any session's. A window whose screen can't be read has no row.
func startingInWindow(c *sessionhost.Client, host, id string, w sessionhost.Window, pane weztermPane,
) (claudesession.Session, bool) {
	screen, err := c.Capture(w.PaneID)
	if err != nil {
		return claudesession.Session{}, false
	}
	s := claudesession.Session{ID: id, Host: host, Cwd: w.Cwd, Live: true, Starting: true, TTY: pane.TTYName,
		Status: claudesession.StatusWorking, PendingTool: "starting"}
	if _, asks := startDialog(screen); asks != "" {
		s.Status, s.PendingTool = claudesession.StatusWaiting, asks
	}
	return s, true
}

// loadLastRemoteSessions reads each computer's sessions as last seen.
func loadLastRemoteSessions() map[string][]claudesession.Session {
	last := map[string][]claudesession.Session{}
	data, err := os.ReadFile(variable.RemoteSessionsFile)
	if err != nil {
		return last
	}
	if err := json.Unmarshal(data, &last); err != nil {
		slog.Debug("Ignoring an unreadable file of remote sessions", "error", err)
		return map[string][]claudesession.Session{}
	}
	return last
}

func saveLastRemoteSessions(last map[string][]claudesession.Session) {
	data, err := json.Marshal(last)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(variable.RemoteSessionsFile), 0o755)
	}
	if err == nil {
		err = os.WriteFile(variable.RemoteSessionsFile, data, 0o600)
	}
	if err != nil {
		slog.Debug("Could not save the remote sessions as last seen", "error", err)
	}
}

// sameSessions reports two lists of the same sessions, in the same states.
func sameSessions(a, b []claudesession.Session) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Status != b[i].Status || a[i].Title != b[i].Title ||
			!a[i].LastActivity.Equal(b[i].LastActivity) {
			return false
		}
	}
	return true
}
