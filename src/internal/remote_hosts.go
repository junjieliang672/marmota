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
// with why. listed are the computers it could.
type remoteListing struct {
	sessions []claudesession.Session
	panes    []weztermPane
	failed   map[string]error
	listed   map[string]bool
}

// listRemoteSessions lists the sessions in Marmota's windows on each
// computer it remembers, copying the new part of each one's transcript.
// Computers are asked all at once; one that can't be reached is left out,
// and tried again later.
func listRemoteSessions(tracker *claudesession.Tracker) remoteListing {
	hosts := loadRemoteHosts()
	each := make([]hostListing, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			each[i] = listHost(h.Name, tracker)
		}()
	}
	wg.Wait()
	listed := map[string]hostListing{}
	for i, h := range hosts {
		listed[h.Name] = each[i]
	}
	return joinListings(hosts, listed)
}

// hostListing is what one computer runs, as one listing found it: its
// sessions and the panes of its windows; or why it couldn't be asked.
type hostListing struct {
	sessions []claudesession.Session
	panes    []weztermPane
	err      error
}

// listHost lists one computer's sessions.
func listHost(name string, tracker *claudesession.Tracker) hostListing {
	var l hostListing
	l.sessions, l.panes, l.err = listHostSessions(name, tracker)
	return l
}

// joinListings puts the listings of the computers in hosts together. One
// not listed yet is left out.
func joinListings(hosts []sessionhost.Host, listed map[string]hostListing) remoteListing {
	out := remoteListing{failed: map[string]error{}, listed: map[string]bool{}}
	for _, h := range hosts {
		l, ok := listed[h.Name]
		switch {
		case !ok:
		case l.err != nil:
			out.failed[h.Name] = l.err
		default:
			out.listed[h.Name] = true
			out.sessions = append(out.sessions, l.sessions...)
			out.panes = append(out.panes, l.panes...)
		}
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
