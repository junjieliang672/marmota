package internal

import (
	"encoding/json"
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

// ownTemporaryRecord gives a test a record of temporary sessions of its own.
func ownTemporaryRecord(t *testing.T) {
	t.Helper()
	setForTest(t, &variable.TemporarySessionsFile, filepath.Join(t.TempDir(), "temporary_sessions.json"))
}

// someProcess is a process of the test's own, to stand for the program a
// temporary session was started for, and what ends it.
func someProcess(t *testing.T) (int, func()) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	end := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	t.Cleanup(end)
	return cmd.Process.Pid, end
}

func sessionIDs(sessions []claudesession.Session) []string {
	ids := []string{}
	for _, s := range sessions {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestTemporarySessionsAreOnARecord(t *testing.T) {
	ownTemporaryRecord(t)
	assert.False(t, isTemporarySession("a1"))

	require.NoError(t, rememberTemporarySession("a1"))
	require.NoError(t, rememberTemporarySession("b2"))

	assert.True(t, isTemporarySession("a1"))
	assert.True(t, isTemporarySession("b2"))
	assert.Equal(t, []string{"c3"}, sessionIDs(withoutTemporary([]claudesession.Session{{ID: "a1"}, {ID: "c3"}, {ID: "b2"}})))

	// One too old to matter is taken off when the next is put on.
	old := map[string]time.Time{"old": time.Now().Add(-temporaryKept - time.Hour), "a1": time.Now()}
	data, err := json.Marshal(old)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(variable.TemporarySessionsFile, data, 0o600))
	require.NoError(t, rememberTemporarySession("d4"))
	assert.False(t, isTemporarySession("old"))
	assert.True(t, isTemporarySession("a1"))
	assert.True(t, isTemporarySession("d4"))
}

// Marmota's Sessions box and its ended sessions leave temporary sessions
// out: running, starting or ended.
func TestMarmotaDoesntListTemporarySessions(t *testing.T) {
	ownTemporaryRecord(t)
	m := newClaudeTestModel(t)
	require.NoError(t, rememberTemporarySession("temp-running"))
	require.NoError(t, rememberTemporarySession("temp-ended"))
	require.NoError(t, rememberTemporarySession("temp-starting"))
	require.NoError(t, rememberTemporarySession("temp-codex"))

	m.applyClaudeSessionsSnapshot(claudeSessionsSnapshotMsg{
		snapshot: claudesession.Snapshot{
			Live: []claudesession.Session{{ID: "mine", Live: true}, {ID: "temp-running", Live: true}},
			Past: []claudesession.Session{{ID: "temp-ended"}, {ID: "done"}},
		},
		starting: []claudesession.Session{{ID: "temp-starting", Live: true}},
		codex: []claudesession.Session{
			{ID: "temp-codex", Agent: claudesession.AgentCodex, Live: true, Status: claudesession.StatusWorking},
			{ID: "codex", Agent: claudesession.AgentCodex, Live: true, Status: claudesession.StatusWorking},
		},
		layoutGen: m.claude.layoutGen,
	})

	assert.ElementsMatch(t, []string{"mine", "codex"}, sessionIDs(m.claude.snapshot.Live))
	assert.Equal(t, []string{"done"}, sessionIDs(m.claude.snapshot.Past))
}

// A temporary session closes when the program it was started for has gone,
// and is on the record before it runs.
func TestATemporarySessionClosesWithTheProgramItWasStartedFor(t *testing.T) {
	ownTemporaryRecord(t)
	// a claude of the test's own, first on the PATH its tmux starts with
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho a fake claude; sleep 60\n"), 0o755))
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	ownTmux(t)
	owner, end := someProcess(t)

	started, err := SessionStart(SessionStartOptions{Cwd: t.TempDir(), Prompt: "what do these lines do?", Temporary: true,
		Owner: owner})

	require.NoError(t, err)
	assert.True(t, isTemporarySession(started.ID))
	c, err := localTmux()
	require.NoError(t, err)
	names := func() []string {
		windows, err := c.Windows()
		require.NoError(t, err)
		out := []string{}
		for _, w := range windows {
			out = append(out, w.Name)
		}
		return out
	}
	time.Sleep(2500 * time.Millisecond) // past a look of its watcher
	assert.Equal(t, []string{"cc-" + started.ID}, names(), "it runs while its program does")

	end()
	deadline := time.Now().Add(10 * time.Second)
	for len(names()) > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	assert.Empty(t, names(), "its window closed with the program")

	_, err = SessionStart(SessionStartOptions{On: "ssh me@host", Temporary: true})
	assert.ErrorContains(t, err, "this computer only")
}

// A temporary Codex session's id isn't known when its window opens: until
// it is on the record, the window's name keeps the session that shows in
// its folder out of Marmota's list.
func TestATemporaryCodexSessionIsNeverListed(t *testing.T) {
	ownTemporaryRecord(t)
	ownTmux(t)
	setForTest(t, &codexStartPoll, 20*time.Millisecond)
	fakeCodexBinary(t, "echo codex; sleep 60")
	folder := t.TempDir()
	codex := newCodexServer(t, claudesession.Session{ID: "0199known", Cwd: folder, StartedAt: time.Now().Add(-time.Hour)})
	m := newClaudeTestModel(t)
	owner, _ := someProcess(t)
	// Marmota has listed the session that was there before.
	require.Len(t, m.withoutTemporaryCodex(append([]claudesession.Session(nil), codex.sessions...)), 1)

	// While it starts: its window is there, its session shows on Codex's
	// server, and its id isn't on the record yet.
	c, err := localTmux()
	require.NoError(t, err)
	w, err := c.Start(sessionhost.NameFor(sessionhost.CodexPrefix, temporaryWindow+"abc"), folder, "sleep 60")
	require.NoError(t, err)
	var panes []weztermPane
	require.Eventually(t, func() bool { // its program is in the folder a moment after the window opens
		panes, err = listTmuxPanes()
		return err == nil && len(panes) == 1 && sameDir(panes[0].Cwd, folder)
	}, 5*time.Second, 50*time.Millisecond)
	m.claude.panes = panes
	fresh := claudesession.Session{ID: "0199fresh", Agent: claudesession.AgentCodex, Live: true, Cwd: folder,
		Status: claudesession.StatusIdle, StartedAt: time.Now()}
	known := codex.sessions[0]
	elsewhere := claudesession.Session{ID: "0199else", Agent: claudesession.AgentCodex, Live: true, Cwd: t.TempDir()}

	kept := m.withoutTemporaryCodex([]claudesession.Session{known, fresh, elsewhere})
	assert.Equal(t, []string{"0199known", "0199else"}, sessionIDs(kept), "the one that shows in its folder isn't listed")
	require.NoError(t, c.CloseWindow(w.ID))

	// The start itself: the session is on the record once it is found.
	go func() {
		time.Sleep(150 * time.Millisecond)
		codex.mu.Lock()
		defer codex.mu.Unlock()
		codex.sessions = append(codex.sessions, claudesession.Session{ID: "0199temp", Agent: claudesession.AgentCodex,
			Live: true, Status: claudesession.StatusIdle, Cwd: folder, StartedAt: time.Now()})
	}()
	started, err := SessionStart(SessionStartOptions{Agent: "co", Cwd: folder, Prompt: "hi", Temporary: true, Owner: owner})
	require.NoError(t, err)
	assert.Equal(t, "0199temp", started.ID)
	assert.True(t, isTemporarySession("0199temp"))
	windows, err := c.Windows()
	require.NoError(t, err)
	require.Len(t, windows, 1)
	assert.Equal(t, "co-0199temp", windows[0].Name)

	panes, err = listTmuxPanes()
	require.NoError(t, err)
	m.claude.panes = panes
	codex.mu.Lock()
	now := append([]claudesession.Session(nil), codex.sessions...)
	codex.mu.Unlock()
	assert.Equal(t, []string{"0199known"}, sessionIDs(m.withoutTemporaryCodex(now)))
	assert.Empty(t, withoutTemporary(codexSessionsIn(windows)), "nor by y session list")
}

// A temporary session that stops to ask you something isn't in Marmota's
// Sessions box to answer: the error says how to open it.
func TestATemporarySessionThatNeedsYouSaysHowToOpenIt(t *testing.T) {
	ownTemporaryRecord(t)
	setForTest(t, &sessionAskPoll, time.Millisecond)
	codex := newCodexServer(t, claudesession.Session{ID: "0199aaaa-1", Cwd: "/w/repo"})
	codex.reply, codex.waiting = "", true
	require.NoError(t, rememberTemporarySession("0199aaaa-1"))

	_, err := SessionAsk("0199aaaa", "Delete the build folder", time.Minute, false, false)

	needs, ok := IsNeedsYou(err)
	require.True(t, ok, "%v", err)
	assert.Contains(t, needs.Error(), "y session show 0199aaaa-1")
	assert.NotContains(t, needs.Error(), "answer it from Marmota's Sessions box")
}

// `y session show` opens a pane under the one it runs in that shows the
// session's window, by a view of its own, so the pane beside Marmota stays
// on its session.
func TestShowOpensASessionsWindowUnderThePane(t *testing.T) {
	ownTmux(t)
	m := newClaudeTestModel(t) // WEZTERM_PANE is 10
	wez := newFakeWezterm(t, superfileTab())
	paneID, err := m.launchParkedAgent(t.TempDir(), "echo claude; sleep 60", managedAgentProviderClaude, "abcd1234-ef")
	require.NoError(t, err)
	ref, _ := tmuxPanes.ref(paneID)

	shown, err := SessionShow("abcd1234")

	require.NoError(t, err)
	assert.Equal(t, "abcd1234-ef", shown.ID)
	split := wez.commands[len(wez.commands)-1]
	assert.True(t, strings.HasPrefix(split, "wezterm cli split-pane --pane-id 10 --bottom --percent 50 "), split)
	assert.Contains(t, split, "show-abcd1234", "a view of its own")
	assert.Regexp(t, `select-window -t \S*`+ref.window+`\b`, split, "on the session's window")

	_, err = SessionShow("ffff0000")
	assert.Error(t, err)

	t.Setenv("WEZTERM_PANE", "")
	_, err = SessionShow("abcd1234")
	assert.ErrorContains(t, err, "this isn't a WezTerm pane")
}

// The editor's list of sessions is this computer's only, since asking the
// others takes a second and more: a computer Marmota remembers isn't asked.
func TestListingThisComputersSessionsAsksNoOther(t *testing.T) {
	ownTmux(t)
	asked := filepath.Join(t.TempDir(), "asked")
	require.NoError(t, saveRemoteHosts([]sessionhost.Host{{Name: "far",
		Command: []string{"/bin/sh", "-c", "touch " + asked + "; exit 1", "sh"}}}))
	t.Cleanup(func() { _ = os.Remove(variable.RemotesFile) })

	_, failed := SessionList(true)
	assert.Nil(t, failed)
	assert.NoFileExists(t, asked)

	_, failed = SessionList(false)
	assert.FileExists(t, asked, "listing every computer's asks it")
	assert.Contains(t, failed, "far")
}
