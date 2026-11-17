package internal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	if r.local != nil {
		r.local.Close()
	}
	r.local, r.ids, r.refs, r.lastID = nil, map[string]int{}, map[int]tmuxRef{}, 0
	r.mu.Unlock()
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
