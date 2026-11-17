package internal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// ownTmux has sessions start in a tmux server of the test's own, and shuts
// it down after. Marmota's own server, and yours, stay untouched.
func ownTmux(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux isn't installed")
	}
	socket, use := sessionhost.Socket, useTmuxSessions
	sessionhost.Socket = fmt.Sprintf("marmota-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	useTmuxSessions = func() bool { return true }
	t.Setenv("HOME", t.TempDir())
	resetTmuxRegistry()
	own := sessionhost.Socket
	t.Cleanup(func() {
		resetTmuxRegistry()
		_ = exec.Command("tmux", "-L", own, "kill-server").Run()
		_ = os.Remove(tmuxSocketPath(own))
		sessionhost.Socket, useTmuxSessions = socket, use
	})
	return own
}

// resetTmuxRegistry forgets the connection and the panes, as a Marmota that
// starts afresh would have none.
func resetTmuxRegistry() {
	r := tmuxPanes
	r.mu.Lock()
	clients := r.clients
	r.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
	tmuxPanes = newTmuxRegistry()
}

// fakeAgent sets its title as Claude Code does, then answers what it reads.
const fakeAgent = `printf '\033]2;✳ eval sweep\007'; echo ready; while read l; do echo "got: $l"; done`

func waitForScreen(t *testing.T, paneID int, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		screen, err := weztermGetText(paneID)
		if err == nil && strings.Contains(screen, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %d never showed %q; it shows %q (%v)", paneID, want, screen, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestASessionStartsInTmuxAndIsReadAndTypedInto(t *testing.T) {
	ownTmux(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	restoreSeams(t)

	paneID, err := m.launchParkedAgent(t.TempDir(), fakeAgent, managedAgentProviderClaude, "abc")
	require.NoError(t, err)
	assert.Less(t, paneID, 0, "a tmux window's pane")
	for _, cmd := range wez.commands {
		assert.NotContains(t, cmd, "spawn", "no WezTerm pane opens for a session in the background")
	}

	waitForScreen(t, paneID, "ready")
	require.NoError(t, weztermSendText(paneID, "hello"))
	require.NoError(t, weztermSendKeys(paneID, "\r"))
	waitForScreen(t, paneID, "got: hello")

	panes, err := listTmuxPanes()
	require.NoError(t, err)
	require.Len(t, panes, 1)
	assert.Equal(t, paneID, panes[0].PaneID, "the same window keeps its pane id")
	assert.Equal(t, "✳ eval sweep", panes[0].Title)
	assert.True(t, strings.HasPrefix(panes[0].TTYName, "/dev/"), "a session is matched to it by tty")
}

// restoreSeams puts back the pane seams TestMain replaced, for a test that
// reads and types into real tmux windows.
func restoreSeams(t *testing.T) {
	t.Helper()
	getText, sendText, sendKeys := weztermGetText, weztermSendText, weztermSendKeys
	weztermGetText = func(id int) (string, error) {
		if isTmuxPane(id) {
			return tmuxGetText(id)
		}
		return getText(id)
	}
	weztermSendText = func(id int, text string) error {
		if isTmuxPane(id) {
			return tmuxSendText(id, text)
		}
		return sendText(id, text)
	}
	weztermSendKeys = func(id int, keys string) error {
		if isTmuxPane(id) {
			return tmuxSendKeys(id, keys)
		}
		return sendKeys(id, keys)
	}
	t.Cleanup(func() { weztermGetText, weztermSendText, weztermSendKeys = getText, sendText, sendKeys })
}

func TestShowingSessionsUsesOnePaneBesideMarmota(t *testing.T) {
	socket := ownTmux(t)
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())

	first, err := m.launchParkedAgent(t.TempDir(), "sleep 60", managedAgentProviderClaude, "one")
	require.NoError(t, err)
	second, err := m.launchParkedAgent(t.TempDir(), "sleep 60", managedAgentProviderClaude, "two")
	require.NoError(t, err)

	_, err = m.showTmuxPane(first, "one", false)
	require.NoError(t, err)
	splits := 0
	for _, cmd := range wez.commands {
		if strings.Contains(cmd, "split-pane") {
			splits++
			assert.Contains(t, cmd, "new-session -A -t marmota -s", "it attaches to a view session")
			assert.Contains(t, cmd, "view-10")
		}
	}
	require.Equal(t, 1, splits)
	assert.Equal(t, 10, wez.active, "the keyboard stays with Marmota")
	// The fake WezTerm runs nothing in its panes: make the view the
	// attach command would have made.
	ref, _ := tmuxPanes.ref(first)
	require.NoError(t, exec.Command("tmux", "-L", socket, "new-session", "-d", "-t", "marmota", "-s", "view-10").Run())
	require.NoError(t, exec.Command("tmux", "-L", socket, "select-window", "-t", "view-10:"+ref.window).Run())

	_, err = m.showTmuxPane(second, "two", true)
	require.NoError(t, err)
	splits = 0
	for _, cmd := range wez.commands {
		if strings.Contains(cmd, "split-pane") {
			splits++
		}
	}
	assert.Equal(t, 1, splits, "the pane beside Marmota switches window; no second pane opens")
	ref2, _ := tmuxPanes.ref(second)
	out, err := exec.Command("tmux", "-L", socket, "display-message", "-p", "-t", "view-10:", "#{window_id}").Output()
	require.NoError(t, err)
	assert.Equal(t, ref2.window, strings.TrimSpace(string(out)))
	assert.Equal(t, m.tmux.viewer, wez.active, "with focus the keyboard goes to it")
}

func TestARestartedMarmotaFindsTheSessionsInTmux(t *testing.T) {
	ownTmux(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, superfileTab())
	_, err := m.launchParkedAgent(t.TempDir(), fakeAgent, managedAgentProviderClaude, "kept")
	require.NoError(t, err)
	before, err := listTmuxPanes()
	require.NoError(t, err)
	require.Len(t, before, 1)

	// WezTerm, and Marmota with it, went away: a new Marmota knows nothing.
	resetTmuxRegistry()
	after, err := listTmuxPanes()
	require.NoError(t, err)
	require.Len(t, after, 1, "the session runs on in tmux")
	assert.Equal(t, before[0].TTYName, after[0].TTYName)
	assert.Equal(t, "✳ eval sweep", after[0].Title)
}

func TestSessionsThatRanBeforeTmuxStayListed(t *testing.T) {
	m := newClaudeTestModel(t)
	useTmuxSessions = func() bool { return true }
	t.Cleanup(func() { useTmuxSessions = func() bool { return false } })
	since := tmuxSince()
	m.claude.panes = []weztermPane{
		{PaneID: 20, TabID: 1, TTYName: "/dev/ttys020"},
		{PaneID: -1, TabID: -1, TTYName: "/dev/ttys040", Workspace: tmuxPaneWorkspace},
	}
	m.claude.snapshot.Live = []claudesession.Session{
		{ID: "old-own", TTY: "/dev/ttys020", Live: true, StartedAt: since.Add(-time.Hour)},
		{ID: "new-own", TTY: "/dev/ttys021", Live: true, StartedAt: since.Add(time.Minute)},
		{ID: "in-tmux", TTY: "/dev/ttys040", Live: true, StartedAt: since.Add(time.Minute)},
	}
	m.dropSessionsOutsideMarmota()
	var ids []string
	for _, s := range m.claude.snapshot.Live {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{"old-own", "in-tmux"}, ids,
		"one you started before keeps its place; a new one you started yourself isn't Marmota's")
	assert.True(t, since.Equal(tmuxSince()), "the moment is noted once")
}

func TestAMovedSessionResumesAsItRan(t *testing.T) {
	assert.Equal(t, "claude --resume a1 --permission-mode plan --model claude-opus-5-5 --effort high --remote-control", movedSessionCommand(claudesession.Session{ID: "a1", PermissionMode: "plan",
		Model: "claude-opus-5-5", Effort: "high", RemoteControl: true}))
	assert.Equal(t, "claude --resume b2 --dangerously-skip-permissions",
		movedSessionCommand(claudesession.Session{ID: "b2", PermissionMode: "bypassPermissions"}))
	assert.Equal(t, "claude --resume c3 --permission-mode default",
		movedSessionCommand(claudesession.Session{ID: "c3", PermissionMode: "default"}))
}

// tmuxSocketPath is where tmux keeps a server's socket, which a server
// killed from outside can leave behind.
func tmuxSocketPath(name string) string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return fmt.Sprintf("%s/tmux-%d/%s", dir, os.Getuid(), name)
}

func TestASessionResumedIntoAWindowIsFoundThereByItsName(t *testing.T) {
	m := newClaudeTestModel(t)
	m.claude.panes = []weztermPane{
		{PaneID: -1, TabID: -1, TTYName: "/dev/ttys040", Title: "marmota-nova", Workspace: tmuxPaneWorkspace},
		{PaneID: -2, TabID: -1, TTYName: "/dev/ttys041", Title: "✳ busy-one", Workspace: tmuxPaneWorkspace},
	}
	// Claude Code's background process has a terminal of its own, in no pane.
	resumed := claudesession.Session{ID: "09c7", Name: "marmota-nova", Live: true, Background: true,
		TTY: "/dev/ttys011"}
	m.claude.snapshot.Live = []claudesession.Session{
		resumed,
		{ID: "own", Name: "busy-one", TTY: "/dev/ttys041", Live: true},
	}
	pane, ok := m.weztermPaneForSession(resumed)
	require.True(t, ok, "a background session /resume brought into a window is found by the window's title")
	assert.Equal(t, -1, pane.PaneID)

	other := claudesession.Session{ID: "x", Name: "busy-one", Live: true, Background: true}
	_, ok = m.weztermPaneForSession(other)
	assert.False(t, ok, "a window whose own session runs there isn't another's")
}

func TestARestartedMarmotaTakesBackThePaneBesideIt(t *testing.T) {
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab(),
		weztermPane{PaneID: 31, TabID: 1, WindowID: 1, TTYName: "/dev/ttysA", Size: weztermPaneSize{Rows: 50, Cols: 99}},
		weztermPane{PaneID: 32, TabID: 1, WindowID: 1, TTYName: "/dev/ttysB", Size: weztermPaneSize{Rows: 50, Cols: 99}},
		weztermPane{PaneID: 33, TabID: 1, WindowID: 1, TTYName: "/dev/ttysC", Size: weztermPaneSize{Rows: 50, Cols: 99}})
	panes, err := listWeztermTabPanes(m.getFocusedFilePanel().location, 10)
	require.NoError(t, err)
	// ttysA and ttysB are the clients of its view: a restarted Marmota
	// opened a second pane beside it before this.
	m.keepOneViewer(10, panes, []string{"/dev/ttysA", "/dev/ttysB"})
	assert.Equal(t, 31, m.tmux.viewer, "the first pane attached to its view is taken back")
	assert.Nil(t, wez.panes[32], "a second pane attached to it closes")
	assert.NotNil(t, wez.panes[33], "a pane of the tab that shows no view stays")
}

func TestAComputerThatCantBeReachedKeepsItsSessionsAsLastSeen(t *testing.T) {
	m := newClaudeTestModel(t)
	file, last := variable.RemotesFile, variable.RemoteSessionsFile
	variable.RemotesFile = filepath.Join(t.TempDir(), "remotes.json")                //nolint:reassign // its own
	variable.RemoteSessionsFile = filepath.Join(t.TempDir(), "remote_sessions.json") //nolint:reassign // its own
	t.Cleanup(func() { variable.RemotesFile, variable.RemoteSessionsFile = file, last })
	require.NoError(t, saveRemoteHosts([]sessionhost.Host{{Name: "nova", SSH: "me@nova"}}))

	seen := claudesession.Session{ID: "r1", Host: "nova", Title: "Sweep", Live: true,
		Status: claudesession.StatusIdle, TTY: remoteTTY("nova", "%1")}
	m.mergeRemoteSessions(remoteListing{sessions: []claudesession.Session{seen}, failed: map[string]error{},
		listed: map[string]bool{"nova": true}})
	require.Len(t, m.claude.snapshot.Live, 1)

	m.claude.snapshot.Live = nil // the next refresh
	m.mergeRemoteSessions(remoteListing{failed: map[string]error{
		"nova": &sessionhost.DialError{Output: "me@nova: Permission denied (publickey)."}}})
	require.Len(t, m.claude.snapshot.Live, 1, "it stays, as last seen")
	s := m.claude.snapshot.Live[0]
	assert.Equal(t, "Sweep", s.Title)
	assert.True(t, s.NeedsLogin)
	assert.Contains(t, s.Unreachable, "Permission denied")
	assert.Equal(t, "needs login · enter logs in", m.dockDoing(s, m.sessionState(s)))

	m.dropSessionsOutsideMarmota()
	assert.Len(t, m.claude.snapshot.Live, 1, "a session on another computer isn't taken for one outside Marmota")

	_, err := m.sendAskTo(s, "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can't reach nova")

	m.claude.snapshot.Live = nil
	m.mergeRemoteSessions(remoteListing{failed: map[string]error{"nova": errors.New("network is unreachable")}})
	assert.Equal(t, "can't reach nova · enter tries again", m.dockDoing(m.claude.snapshot.Live[0],
		m.sessionState(m.claude.snapshot.Live[0])))

	// Marmota starts again, and nova still can't be reached.
	again := newClaudeTestModel(t)
	again.mergeRemoteSessions(remoteListing{failed: map[string]error{
		"nova": &sessionhost.DialError{Output: "Permission denied"}}})
	require.Len(t, again.claude.snapshot.Live, 1, "its sessions as last seen came from the file")
	assert.Equal(t, "Sweep", again.claude.snapshot.Live[0].Title)
	assert.True(t, again.claude.snapshot.Live[0].NeedsLogin)

	// nova is renamed in remotes.json: what was seen under its old name goes.
	require.NoError(t, os.WriteFile(variable.RemotesFile, []byte(`[{"name":"nova-2","ssh":"me@nova"}]`), 0o600))
	again.mergeRemoteSessions(remoteListing{})
	assert.NotContains(t, loadLastRemoteSessions(), "nova")
}

func TestAFolderUnderThisHomeIsTheOtherComputersHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, "~/proj", remoteFolder(home+"/proj", "nova"), "the shell expanded ~/proj here")
	assert.Equal(t, "~", remoteFolder(home, "nova"))
	assert.Equal(t, "~", remoteFolder("", "nova"))
	assert.Equal(t, "/srv/app", remoteFolder("/srv/app", "nova"))
	assert.Equal(t, home+"/proj", remoteFolder(home+"/proj", ""), "here a folder stays as it is")
}

// Each computer's sessions come in on their own: this computer's show
// before another computer has answered, and a computer's listing takes the
// place of its last one without waiting for the next of this computer's.
func TestEachComputersSessionsComeInOnTheirOwn(t *testing.T) {
	m := newClaudeTestModel(t)
	file, last := variable.RemotesFile, variable.RemoteSessionsFile
	variable.RemotesFile = filepath.Join(t.TempDir(), "remotes.json")                //nolint:reassign // its own
	variable.RemoteSessionsFile = filepath.Join(t.TempDir(), "remote_sessions.json") //nolint:reassign // its own
	t.Cleanup(func() { variable.RemotesFile, variable.RemoteSessionsFile = file, last })
	require.NoError(t, saveRemoteHosts([]sessionhost.Host{{Name: "nova", SSH: "me@nova"},
		{Name: "air", SSH: "me@air"}}))
	ids := func() []string {
		var out []string
		for _, s := range m.claude.snapshot.Live {
			out = append(out, s.ID)
		}
		return out
	}
	remote := func(host, id string) claudesession.Session {
		return claudesession.Session{ID: id, Host: host, Title: id, Live: true, Status: claudesession.StatusIdle,
			TTY: remoteTTY(host, "%1")}
	}
	here := claudesession.Snapshot{Live: []claudesession.Session{{ID: "here", Title: "here", Live: true}}}

	// air answers before this computer's sessions are listed: it waits for them.
	m.applyRemoteHostListed(remoteHostListedMsg{host: "air",
		listing: hostListing{sessions: []claudesession.Session{remote("air", "a1")}}})
	assert.Empty(t, ids())
	m.applyClaudeSessionsSnapshot(claudeSessionsSnapshotMsg{snapshot: here, layoutGen: m.claude.layoutGen})
	assert.ElementsMatch(t, []string{"here", "a1"}, ids(), "nova hasn't answered yet: the others show")

	m.applyRemoteHostListed(remoteHostListedMsg{host: "nova",
		listing: hostListing{sessions: []claudesession.Session{remote("nova", "n1")}}})
	assert.ElementsMatch(t, []string{"here", "a1", "n1"}, ids(), "nova's show as soon as it answers")

	m.applyRemoteHostListed(remoteHostListedMsg{host: "nova",
		listing: hostListing{sessions: []claudesession.Session{remote("nova", "n2")}}})
	assert.ElementsMatch(t, []string{"here", "a1", "n2"}, ids(), "its new listing takes the old one's place")

	m.applyClaudeSessionsSnapshot(claudeSessionsSnapshotMsg{snapshot: here, layoutGen: m.claude.layoutGen})
	assert.ElementsMatch(t, []string{"here", "a1", "n2"}, ids(), "this computer's next listing keeps theirs")

	m.applyRemoteHostListed(remoteHostListedMsg{host: "nova", listing: hostListing{err: errors.New("timed out")}})
	assert.ElementsMatch(t, []string{"here", "a1", "n2"}, ids())
	s, ok := m.claude.snapshot.Find("n2")
	require.True(t, ok)
	assert.Contains(t, s.Unreachable, "timed out", "kept as last seen")
}
