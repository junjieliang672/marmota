package internal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// codexServer stands in for Codex's server for `y session`: the sessions it
// has loaded, and for each a rollout that a message sent to it is answered
// in, as Codex records a turn.
type codexServer struct {
	mu       sync.Mutex
	dir      string
	sessions []claudesession.Session
	sent     []string // id: text
	turns    []codexsession.TurnOptions
	// reply is what a session answers a message with; "" leaves the turn
	// without its end, as while Codex still works on it.
	reply string
	// waiting has the sessions say they need you.
	waiting bool
}

func newCodexServer(t *testing.T, sessions ...claudesession.Session) *codexServer {
	t.Helper()
	f := &codexServer{dir: t.TempDir(), reply: "A reply."}
	for _, s := range sessions {
		s.Agent, s.Live = claudesession.AgentCodex, true
		if s.Status == "" {
			s.Status = claudesession.StatusIdle
		}
		f.sessions = append(f.sessions, s)
	}
	setForTest(t, &codexCollect, func(codexsession.Options) ([]claudesession.Session, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]claudesession.Session(nil), f.sessions...), nil
	})
	setForTest(t, &codexRead, func(_, id string, tracker *codexsession.Tracker) (claudesession.Session, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, s := range f.sessions {
			if s.ID != id {
				continue
			}
			if f.waiting {
				s.Status = claudesession.StatusWaiting
			}
			if path := f.rollout(id); fileExists(path) {
				s.TranscriptPath = path
				if sum, err := tracker.Summary(path); err == nil {
					s.LastTurnEnd = sum.LastTurnEnd
				}
			}
			return s, nil
		}
		return claudesession.Session{}, os.ErrNotExist
	})
	setForTest(t, &codexSend, func(_, id, text string, o codexsession.TurnOptions) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sent = append(f.sent, id+": "+text)
		f.turns = append(f.turns, o)
		f.record(id, "UserMessage", text)
		if f.reply != "" {
			f.record(id, "commentary", "I'll look.")
			f.record(id, "AgentMessage", f.reply)
			f.record(id, "task_complete", "")
		}
		return nil
	})
	return f
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (f *codexServer) rollout(id string) string { return filepath.Join(f.dir, "rollout-"+id+".jsonl") }

// record adds a line to a session's rollout, as Codex writes it.
func (f *codexServer) record(id, kind, text string) {
	item := map[string]any{"type": kind, "content": []map[string]any{{"type": "text", "text": text}}}
	switch kind {
	case "commentary": // what Codex says as it works
		item["type"], item["phase"] = "AgentMessage", "commentary"
	case "AgentMessage":
		item["phase"] = "final_answer"
	}
	payload := map[string]any{"type": "item_completed", "item": item}
	if kind == "task_complete" {
		payload = map[string]any{"type": "task_complete", "completed_at": time.Now().Unix()}
	}
	line, _ := json.Marshal(map[string]any{
		"timestamp": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "type": "event_msg", "payload": payload,
	})
	file, err := os.OpenFile(f.rollout(id), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(line, '\n'))
	time.Sleep(2 * time.Millisecond) // each record has its own moment
}

func codexWindow(id, name, title string) sessionhost.Window {
	return sessionhost.Window{ID: id, Name: name, PaneID: "%" + id[1:], Title: title}
}

// The Codex sessions `y session list` shows are those in Marmota's tmux
// windows: a window named for its session, or, opened before the session
// was known, titled for it by Codex.
func TestTheCodexSessionsListedAreThoseInMarmotasWindows(t *testing.T) {
	newCodexServer(t,
		claudesession.Session{ID: "0199aaaa-1", Title: "Fix the login", Cwd: "/w/repo"},
		claudesession.Session{ID: "0199bbbb-2", Title: "Write the docs", Cwd: "/w/docs"},
		claudesession.Session{ID: "0199cccc-3", Title: "In a tab of yours", Cwd: "/w/else"},
	)
	windows := []sessionhost.Window{
		codexWindow("@1", "cc-0199aaaa-1", "✳ a Claude Code session"),
		codexWindow("@2", "co-0199aaaa-1", "anything"),
		codexWindow("@3", "co-new-abc", "Write the docs | docs"),
		codexWindow("@4", "co-new-def", "Something else | else"),
	}

	var ids []string
	for _, s := range codexSessionsIn(windows) {
		ids = append(ids, s.ID)
	}

	assert.Equal(t, []string{"0199aaaa-1", "0199bbbb-2"}, ids)
	assert.Empty(t, codexSessionsIn(windows[:1]), "with no Codex window, none")
}

// A message to a Codex session goes through Codex's server, not into a
// pane, and the reply is the turn's, read off the session's rollout.
func TestAskingACodexSession(t *testing.T) {
	setForTest(t, &sessionAskPoll, time.Millisecond)
	codex := newCodexServer(t, claudesession.Session{ID: "0199aaaa-1", Title: "Fix the login", Cwd: "/w/repo"})
	codex.reply = "Line 3 sets the timeout."

	reply, err := SessionAsk("0199aaaa", "What does this do?\n\nThis is about the file /w/repo/a.go, line 3.",
		time.Minute, false, false)

	require.NoError(t, err)
	assert.Equal(t, "Line 3 sets the timeout.", reply, "its answer, without what it said as it worked")
	require.Len(t, codex.sent, 1)
	assert.True(t, strings.HasPrefix(codex.sent[0], "0199aaaa-1: What does this do?"), "sent to the whole id")

	// The same question again is answered by its own turn.
	codex.reply = "As I said: the timeout."
	reply, err = SessionAsk("0199aaaa-1", "What does this do?\n\nThis is about the file /w/repo/a.go, line 3.",
		time.Minute, false, false)
	require.NoError(t, err)
	assert.Equal(t, "As I said: the timeout.", reply)
}

// A Codex session that stops for an approval says so instead of a reply,
// unless it is to be waited for.
func TestACodexSessionThatNeedsYouSaysSo(t *testing.T) {
	setForTest(t, &sessionAskPoll, time.Millisecond)
	codex := newCodexServer(t, claudesession.Session{ID: "0199aaaa-1", Cwd: "/w/repo"})
	codex.reply, codex.waiting = "", true

	_, err := SessionAsk("0199aaaa-1", "Delete the build folder", time.Minute, false, false)

	needs, ok := IsNeedsYou(err)
	require.True(t, ok, "%v", err)
	assert.Contains(t, needs.Error(), "an approval or a question")
}

// send and read take a Codex session too, by the start of its id.
func TestSendingToAndReadingACodexSession(t *testing.T) {
	codex := newCodexServer(t, claudesession.Session{ID: "0199aaaa-1", Cwd: "/w/repo"})

	require.NoError(t, SessionSend("0199aa", "Run the tests"))
	assert.Equal(t, []string{"0199aaaa-1: Run the tests"}, codex.sent)

	// read is of the session as the server lists it: with its rollout
	codex.sessions[0].TranscriptPath = codex.rollout("0199aaaa-1")
	var out bytes.Buffer
	require.NoError(t, SessionRead(&out, "0199aaaa-1", 3))
	assert.Equal(t, "## you\nRun the tests\n\n## codex\nI'll look.\n\nA reply.\n\n", out.String(),
		"read shows all it wrote")

	err := SessionSend("ffff0000", "to no one")
	require.Error(t, err, "one that isn't Codex's is looked for among Claude Code's")
	assert.Empty(t, codex.sent[1:])
}

// The list says who runs each session.
func TestTheSessionListNamesTheAgent(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, WriteSessionList(&out, []ListedSession{
		{ID: "a1", Agent: AgentClaudeCode, Status: "idle", Title: "Fix the login", Cwd: "/w/repo"},
		{ID: "b2", Agent: AgentCodex, Status: "waiting", Waiting: "an approval or a question", Title: "Docs",
			Cwd: "/w/docs"},
	}, nil, false))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, []string{"ID", "AGENT", "HOST", "STATUS", "TITLE", "FOLDER"}, strings.Fields(lines[0]))
	assert.Regexp(t, `^a1\s+cc\s+here\s+idle\s+Fix the login\s+/w/repo$`, lines[1])
	assert.Regexp(t, `^b2\s+co\s+here\s+waiting: an approval or a question\s+Docs\s+/w/docs$`, lines[2])
}

// fakeCodexBinary is a codex that only keeps its window open.
func fakeCodexBinary(t *testing.T, script string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755))
	setForTest(t, &codexBinary, func() string { return path })
}

// `y session start --agent co` opens Codex in a window of Marmota's tmux,
// finds the session it makes in the folder, names the window for it and
// sends the message through Codex's server, at medium effort.
func TestStartingACodexSession(t *testing.T) {
	ownTmux(t)
	setForTest(t, &codexStartPoll, 20*time.Millisecond)
	fakeCodexBinary(t, "echo codex; sleep 60")
	folder := t.TempDir()
	codex := newCodexServer(t, claudesession.Session{ID: "0199old", Cwd: folder, StartedAt: time.Now().Add(-time.Hour)})
	// Codex's server lists the new session a moment after its window opens.
	go func() {
		time.Sleep(150 * time.Millisecond)
		codex.mu.Lock()
		defer codex.mu.Unlock()
		codex.sessions = append(codex.sessions, claudesession.Session{ID: "0199new", Agent: claudesession.AgentCodex,
			Live: true, Status: claudesession.StatusIdle, Cwd: folder, StartedAt: time.Now()})
	}()

	started, err := SessionStart(SessionStartOptions{Agent: "co", Cwd: folder, Prompt: "Summarize the plan"})

	require.NoError(t, err)
	assert.Equal(t, "0199new", started.ID, "the one that showed since, not the one there before")
	assert.Equal(t, AgentCodex, started.Agent)
	assert.Equal(t, []string{"0199new: Summarize the plan"}, codex.sent)
	assert.Equal(t, "medium", codex.turns[0].Effort)
	c, err := localTmux()
	require.NoError(t, err)
	windows, err := c.Windows()
	require.NoError(t, err)
	require.Len(t, windows, 1)
	assert.Equal(t, "co-0199new", windows[0].Name, "its window is named for it")

	listed := codexSessionsIn(windows)
	require.Len(t, listed, 1)
	assert.Equal(t, "0199new", listed[0].ID)

	_, err = SessionStart(SessionStartOptions{Agent: "co", On: "ssh me@host", Prompt: "hi"})
	assert.ErrorContains(t, err, "this computer only")
	_, err = SessionStart(SessionStartOptions{Agent: "gemini"})
	assert.ErrorContains(t, err, "--agent")
}

// A Codex that closes before it makes a session, as one not signed in
// does, is said so, and leaves no window behind.
func TestACodexThatClosesAtOnceIsSaidSo(t *testing.T) {
	ownTmux(t)
	setForTest(t, &codexStartPoll, 20*time.Millisecond)
	fakeCodexBinary(t, "exit 1")
	newCodexServer(t)

	_, err := SessionStart(SessionStartOptions{Agent: "co", Cwd: t.TempDir(), Prompt: "hi"})

	assert.ErrorContains(t, err, "Codex closed before it started a session")
}

// A Codex whose window says it can't use Codex's server is said so at once,
// with the reason it gives: its session would run out of Marmota's sight.
func TestACodexThatCantUseItsServerIsSaidSo(t *testing.T) {
	ownTmux(t)
	setForTest(t, &codexStartPoll, 20*time.Millisecond)
	fakeCodexBinary(t, `printf '  Cannot use the background server\n  Experimental feature request failed\n\n'
printf '  1. Run without daemon this time\n> 2. Cancel\n'; sleep 60`)
	newCodexServer(t)

	began := time.Now()
	_, err := SessionStart(SessionStartOptions{Agent: "co", Cwd: t.TempDir(), Prompt: "hi"})

	assert.ErrorContains(t, err, "can't use its background server (Experimental feature request failed)")
	assert.ErrorContains(t, err, "codex app-server daemon restart")
	assert.Less(t, time.Since(began), 10*time.Second, "not after the whole wait")
	c, tmuxErr := localTmux()
	require.NoError(t, tmuxErr)
	windows, tmuxErr := c.Windows()
	require.NoError(t, tmuxErr)
	assert.Empty(t, windows, "its window is closed")
}

// Marmota names a Codex session's window for it once it knows the session,
// and from then on finds the session's pane by the window's name, whatever
// title Codex gives it.
func TestACodexWindowIsNamedForItsSession(t *testing.T) {
	ownTmux(t)
	m := newClaudeTestModel(t)
	newFakeWezterm(t, marmotaTab())
	restoreSeams(t)
	paneID, err := m.launchParkedAgent(t.TempDir(), "echo codex; sleep 60", managedAgentProviderCodex, "")
	require.NoError(t, err)
	assert.Empty(t, codexWindowOf(paneID), "not known yet")

	m.recordCodexPane("0199aaaa-1", paneID)

	assert.Equal(t, "0199aaaa-1", codexWindowOf(paneID))
	c, err := localTmux()
	require.NoError(t, err)
	windows, err := c.Windows()
	require.NoError(t, err)
	require.Len(t, windows, 1)
	assert.Equal(t, "co-0199aaaa-1", windows[0].Name)

	// Another Marmota, which never recorded the pane, finds it by the name.
	resetTmuxRegistry()
	other := newClaudeTestModel(t)
	panes, err := listTmuxPanes()
	require.NoError(t, err)
	other.claude.panes = panes
	pane, ok := other.codexPane(claudesession.Session{ID: "0199aaaa-1", Agent: claudesession.AgentCodex, Live: true,
		Title: "A title its window doesn't have", Cwd: "/w/repo"}, map[int]bool{})
	require.True(t, ok)
	assert.Equal(t, panes[0].PaneID, pane.PaneID)
	_, ok = other.codexPane(claudesession.Session{ID: "0199bbbb-2", Agent: claudesession.AgentCodex, Live: true,
		Cwd: "/w/repo"}, map[int]bool{})
	assert.False(t, ok, "another session's window isn't taken for this one's")
}
