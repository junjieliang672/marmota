package claudesession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLive(t *testing.T) {
	out := []byte(`[
	 {"pid":"74816","cwd":"/repo","kind":"interactive","startedAt":"1793837260788",
	  "sessionId":"3c2a1f4b","name":"repo-d8","status":"busy"},
	 {"pid":501,"cwd":"/other","kind":"background","startedAt":1793837000000,
	  "sessionId":"bg1","name":"nightly","status":"idle"},
	 {"pid":"1","sessionId":"","status":"busy"}
	]`)
	got, err := ParseLive(out)
	require.NoError(t, err)
	require.Len(t, got, 2, "entries without a session id are skipped")

	assert.Equal(t, "3c2a1f4b", got[0].ID)
	assert.Equal(t, 74816, got[0].PID)
	assert.Equal(t, StatusWorking, got[0].Status)
	assert.False(t, got[0].Background)
	assert.Equal(t, int64(1793837260788), got[0].StartedAt.UnixMilli())

	assert.Equal(t, StatusIdle, got[1].Status)
	assert.True(t, got[1].Background)
	assert.Equal(t, 501, got[1].PID)

	empty, err := ParseLive([]byte("  "))
	require.NoError(t, err)
	assert.Empty(t, empty)

	_, err = ParseLive([]byte("not json"))
	require.Error(t, err)
}

func TestParsePS(t *testing.T) {
	out := []byte(" 74816 ttys000  claude --dangerously-skip-permissions --remote-control\n" +
		"  501 ??       claude --bg\n" +
		"garbage line\n")
	got := ParsePS(out)
	require.Len(t, got, 2)
	assert.Equal(t, "/dev/ttys000", got[74816].TTY)
	assert.True(t, commandHasRemoteControl(got[74816].Command))
	assert.Empty(t, got[501].TTY, "processes without a terminal have no tty")
	assert.False(t, commandHasRemoteControl(got[501].Command))
	assert.True(t, commandHasRemoteControl("claude --remote-control=work"))
	assert.False(t, commandHasRemoteControl("claude --remote-control-session-name-prefix x"))
}

func TestProjectSlug(t *testing.T) {
	assert.Equal(t, "-Users-kingjj-Developer-superfile-custom", ProjectSlug("/Users/kingjj/Developer/superfile-custom"))
	assert.Equal(t, "-a-b-c-d-e", ProjectSlug("/a.b_c d/e"))
}

const transcriptFixture = `{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"s1"}
{"type":"user","message":{"role":"user","content":"rename the logos"},"timestamp":"2026-11-04T10:00:00Z","cwd":"/repo","sessionId":"s1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"tool_use","name":"Edit","input":{"file_path":"/repo/a.go"}}]},"timestamp":"2026-11-04T10:00:05Z"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]},"timestamp":"2026-11-04T10:00:06Z"}
{"type":"assistant","isSidechain":true,"message":{"role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"subagent noise"}]},"timestamp":"2026-11-04T10:00:07Z"}
{"type":"assistant","effort":"high","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Renamed four files."},{"type":"tool_use","name":"Write","input":{"file_path":"/repo/b.go"}}]},"timestamp":"2026-11-04T10:00:08Z"}
{"type":"system","subtype":"turn_duration","durationMs":8000,"timestamp":"2026-11-04T10:00:09Z"}
{"type":"ai-title","aiTitle":"Organize logos","sessionId":"s1"}
{"type":"bridge-session","sessionId":"s1","bridgeSessionId":"cse_1"}
{"type":"system","subtype":"bridge_status","content":"/remote-control is active","url":"https://claude.ai/code/session_1","timestamp":"2026-11-04T10:00:10Z"}
{"type":"system","subtype":"away_summary","content":"You asked for logo cleanup; done.","timestamp":"2026-11-04T10:00:11Z"}
{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"},"timestamp":"2026-11-04T10:00:12Z"}
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestTrackerSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	writeFile(t, path, transcriptFixture)

	tr := NewTracker()
	sum, err := tr.Summary(path)
	require.NoError(t, err)

	assert.Equal(t, "s1", sum.SessionID)
	assert.Equal(t, "Organize logos", sum.Title)
	assert.Equal(t, "/repo", sum.Cwd)
	assert.Equal(t, "rename the logos", sum.LastPrompt, "command plumbing is not a prompt")
	assert.Equal(t, "You asked for logo cleanup; done.", sum.AwaySummary)
	assert.True(t, sum.RemoteControl)
	assert.Equal(t, "https://claude.ai/code/session_1", sum.RemoteURL)
	assert.Equal(t, "bypassPermissions", sum.PermissionMode)
	assert.Equal(t, "claude-opus-5-5", sum.Model, "what it last answered with, not a subagent's")
	assert.Equal(t, "high", sum.Effort)
	assert.Equal(t, []string{"/repo/a.go", "/repo/b.go"}, sum.EditedFiles)
	assert.Equal(t, "2026-11-04T10:00:09Z", sum.LastTurnEnd.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-11-04T10:00:12Z", sum.LastActivity.UTC().Format(time.RFC3339))
}

func TestTrackerReadsOnlyAppendedLinesAndWaitsForPartialLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s2.jsonl")
	writeFile(t, path, `{"type":"ai-title","aiTitle":"First"}`+"\n")
	tr := NewTracker()

	sum, err := tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "First", sum.Title)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"ai-title","aiTitle":"Second"}` + "\n" + `{"type":"ai-title","aiTi`)
	require.NoError(t, err)
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Second", sum.Title, "the half-written line is not parsed yet")

	_, err = f.WriteString(`tle":"Third"}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Third", sum.Title, "the completed line is parsed on the next call")

	// A rewritten (shorter) file is parsed from the start.
	writeFile(t, path, `{"type":"ai-title","aiTitle":"Fresh"}`+"\n")
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Fresh", sum.Title)

	require.NoError(t, os.Remove(path))
	_, err = tr.Summary(path)
	require.Error(t, err)
}

func TestFindAndPastTranscripts(t *testing.T) {
	projects := t.TempDir()
	home := "/Users/me"
	repo := "/Users/me/dev/repo"
	sub := repo + "/src/internal"

	old := filepath.Join(projects, ProjectSlug(repo), "old.jsonl")
	newer := filepath.Join(projects, ProjectSlug(sub), "newer.jsonl")
	atHome := filepath.Join(projects, ProjectSlug(home), "home.jsonl")
	empty := filepath.Join(projects, ProjectSlug(repo), "empty.jsonl")
	writeFile(t, old, `{"type":"ai-title","aiTitle":"old"}`+"\n")
	writeFile(t, newer, `{"type":"ai-title","aiTitle":"newer"}`+"\n")
	writeFile(t, atHome, `{"type":"ai-title","aiTitle":"home"}`+"\n")
	writeFile(t, empty, "")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))

	got := PastTranscripts(projects, sub, home, 10)
	assert.Equal(t, []string{newer, old}, got, "ancestors are included, home and empty files are not")
	assert.Equal(t, []string{newer}, PastTranscripts(projects, sub, home, 1))
	assert.Empty(t, PastTranscripts(projects, "/elsewhere", home, 10))

	assert.Equal(t, old, FindTranscript(projects, repo, "old"))
	assert.Equal(t, newer, FindTranscript(projects, "/wrong/cwd", "newer"), "falls back to searching every project")
	assert.Empty(t, FindTranscript(projects, repo, "missing"))
	assert.Equal(t, "old", SessionIDFromPath(old))
}

func TestListsFolderMatchesPastTranscripts(t *testing.T) {
	home := "/Users/me"
	assert.True(t, ListsFolder("/w/repo/src", home, "/w/repo/src"))
	assert.True(t, ListsFolder("/w/repo/src", home, "/w/repo/"), "a parent's sessions are listed too")
	assert.False(t, ListsFolder("/w/repo", home, "/w/repo/src"), "a subfolder's are not")
	assert.False(t, ListsFolder("/Users/me/repo", home, home), "nor the home folder's")
	assert.False(t, ListsFolder("", home, "/w"))
	assert.False(t, ListsFolder("/w", home, ""))
}

func TestCollect(t *testing.T) {
	projects := t.TempDir()
	repo := "/Users/me/dev/repo"
	writeFile(t, filepath.Join(projects, ProjectSlug(repo), "live1.jsonl"),
		`{"type":"ai-title","aiTitle":"Provider registry"}`+"\n")
	writeFile(t, filepath.Join(projects, ProjectSlug(repo), "rc1.jsonl"),
		`{"type":"last-prompt","lastPrompt":"check the phone build"}`+"\n")
	writeFile(
		t,
		filepath.Join(projects, ProjectSlug(repo), "past1.jsonl"),
		`{"type":"ai-title","aiTitle":"Old work"}`+"\n"+`{"type":"system","subtype":"turn_duration","timestamp":"2026-11-01T00:00:00Z"}`+"\n",
	)

	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch name {
		case "claude":
			return []byte(
				`[{"pid":"10","cwd":"` + repo + `","kind":"interactive","startedAt":"2000","sessionId":"live1","name":"repo-a","status":"busy"},
			{"pid":"11","cwd":"` + repo + `","kind":"interactive","startedAt":"3000","sessionId":"rc1","name":"repo-b","status":"idle"}]`,
			), nil
		case "ps":
			return []byte(
				"10 ttys001 claude --dangerously-skip-permissions\n11 ttys002 claude --dangerously-skip-permissions --remote-control\n",
			), nil
		}
		return nil, errors.New("unexpected command")
	}

	snap := Collect(context.Background(), Options{
		Run: run, ProjectsDir: projects, Folders: []string{repo}, Home: "/Users/me", PerFolder: 5,
	})
	require.NoError(t, snap.LiveErr)
	require.Len(t, snap.Live, 2)
	assert.Equal(t, "rc1", snap.Live[0].ID, "newest live session first")
	assert.Equal(t, "check the phone build", snap.Live[0].Title, "falls back to the last prompt")
	assert.True(t, snap.Live[0].RemoteControl)
	assert.Equal(t, "/dev/ttys002", snap.Live[0].TTY)
	assert.Equal(t, "Provider registry", snap.Live[1].Title)
	assert.False(t, snap.Live[1].RemoteControl)

	require.Len(t, snap.Past, 1, "live sessions are not repeated as past ones")
	assert.Equal(t, "past1", snap.Past[0].ID)
	assert.Equal(t, StatusEnded, snap.Past[0].Status)

	found, ok := snap.Find("past1")
	assert.True(t, ok)
	assert.Equal(t, "Old work", found.Title)
	assert.Contains(t, calls, "claude agents --json")
}

func TestCollectWithoutClaude(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("executable file not found")
	}
	snap := Collect(context.Background(),
		Options{Run: run, ProjectsDir: t.TempDir(), Folders: []string{"/x"}, PerFolder: 3})
	require.Error(t, snap.LiveErr)
	assert.Empty(t, snap.Live)
	assert.Empty(t, snap.Past)
}

func TestSeenStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "seen.json")
	s := LoadSeen(path)
	t0 := time.UnixMilli(1_000_000)

	assert.False(t, s.Unread("a", t0), "first sight is not unread")
	assert.False(t, s.Unread("a", t0))
	assert.True(t, s.Unread("a", t0.Add(time.Second)), "a newer turn is unread")
	s.MarkSeen("a", t0.Add(2*time.Second))
	assert.False(t, s.Unread("a", t0.Add(time.Second)))
	assert.False(t, s.Unread("", t0))
	assert.False(t, s.Unread("b", time.Time{}))

	require.NoError(t, s.Save())
	reloaded := LoadSeen(path)
	assert.True(t, reloaded.Unread("a", t0.Add(3*time.Second)), "state survives a restart")

	reloaded.Forget(map[string]bool{}, t0.Add(time.Hour))
	assert.False(t, reloaded.Unread("a", t0.Add(4*time.Second)), "forgotten sessions start fresh")

	writeFile(t, path, "{corrupt")
	assert.NotNil(t, LoadSeen(path))
}

func TestSessionHelpers(t *testing.T) {
	s := Session{ID: "0123456789", Cwd: "/x/repo"}
	assert.Equal(t, "repo", s.DisplayTitle())
	s.Name = "repo-a"
	assert.Equal(t, "repo-a", s.DisplayTitle())
	s.Title = "  Work  "
	assert.Equal(t, "Work", s.DisplayTitle())
	assert.Equal(t, "01234567", Session{ID: "0123456789"}.DisplayTitle())

	t1 := time.Unix(10, 0)
	t2 := time.Unix(20, 0)
	assert.Equal(t, t1, Session{LastActivity: t1}.UpdateMark())
	assert.Equal(t, t2, Session{LastActivity: t1, LastTurnEnd: t2}.UpdateMark())
	assert.Equal(t, "ab…", truncateRunes("abcdef", 3))
}

func TestPastSessionsComeOnlyFromTheCurrentFolder(t *testing.T) {
	projects := t.TempDir()
	writeFile(t, filepath.Join(projects, ProjectSlug("/other/app"), "rc-old.jsonl"),
		`{"type":"ai-title","aiTitle":"Phone review"}`+"\n"+`{"type":"bridge-session","bridgeSessionId":"cse_1"}`+"\n")
	writeFile(t, filepath.Join(projects, ProjectSlug("/repo"), "plain.jsonl"),
		`{"type":"ai-title","aiTitle":"Plain"}`+"\n")
	writeFile(t, filepath.Join(projects, ProjectSlug("/repo"), "rc-here.jsonl"),
		`{"type":"ai-title","aiTitle":"Remote here"}`+"\n"+`{"type":"bridge-session","bridgeSessionId":"cse_2"}`+"\n")
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte("[]"), nil }

	snap := Collect(context.Background(), Options{
		Run: run, ProjectsDir: projects, Folders: []string{"/repo"}, Home: "/home", PerFolder: 5,
	})

	var past []string
	for _, s := range snap.Past {
		past = append(past, s.Title)
	}
	assert.ElementsMatch(t, []string{"Plain", "Remote here"}, past,
		"finished Remote Control sessions of this folder are listed with the others")
	_, ok := snap.Find("rc-old")
	assert.False(t, ok, "finished sessions of other folders aren't listed")
	found, _ := snap.Find("rc-here")
	assert.True(t, found.RemoteControl)
}

func TestCustomTitleFromClaudeCodesRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeFile(t, path, `{"type":"ai-title","aiTitle":"Pull remote"}`+"\n"+
		`{"type":"custom-title","customTitle":"Sessions work","sessionId":"s"}`+"\n")
	tr := NewTracker()

	sum, err := tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Sessions work", sum.CustomTitle)
	assert.Equal(t, "Pull remote", sum.Title)

	s := Session{}
	applySummary(&s, sum)
	assert.Equal(t, "Sessions work", s.Title, "a given name wins over Claude's title")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"custom-title","customTitle":"","sessionId":"s"}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Empty(t, sum.CustomTitle, "an empty name clears it")
}

func TestTrackerKeepsTheLatestTodoList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	todo := func(items string) string {
		return `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"TodoWrite",` +
			`"input":{"todos":[` + items + `]}}]}}` + "\n"
	}
	writeFile(t, path, todo(`{"content":"Read the loader","status":"in_progress","activeForm":"Reading"}`)+
		todo(`{"content":"Read the loader","status":"completed","activeForm":"Reading"},`+
			`{"content":"Run the tests","status":"pending","activeForm":"Running the tests"}`))

	sum, err := NewTracker().Summary(path)
	require.NoError(t, err)
	assert.Equal(t, []Todo{
		{Content: "Read the loader", Status: "completed", ActiveForm: "Reading"},
		{Content: "Run the tests", Status: "pending", ActiveForm: "Running the tests"},
	}, sum.Todos, "each TodoWrite replaces the list")

	s := Session{}
	applySummary(&s, sum)
	assert.Len(t, s.Todos, 2)
}

// superfile sends requests with a context block after them; the prompt
// superfile shows, and a title made from it, are what you typed.
func TestPromptsLeaveOutSuperfilesContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	prompt := "group the logos into logos/\n\n" + ContextOpenTag + "\ncwd:      ~/repo\ncursor:   a.png\n" +
		ContextCloseTag
	line, err := json.Marshal(map[string]any{
		"type": "user", "sessionId": "s", "timestamp": "2026-11-05T10:00:00Z",
		"message": map[string]any{"role": "user", "content": prompt},
	})
	require.NoError(t, err)
	writeFile(t, path, string(line)+"\n")

	sum, err := NewTracker().Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "group the logos into logos/", sum.LastPrompt)

	assert.Equal(t, "a  b", StripContext("a "+ContextOpenTag+"x"+ContextCloseTag+" b"))
	assert.Equal(t, "cut ", StripContext("cut "+ContextOpenTag+"never closed"))
	assert.Equal(t, "plain", StripContext("plain"))
	assert.Equal(t, "older ", StripContext("older <superfile-context>\ncwd: ~/r\n</superfile-context>"),
		"blocks from before the rename are left out too")
}

// Claude Code lists a `claude -p` run, such as superfile's Ask bar starts, as
// an interactive session while it works. There is nothing to switch to.
func TestCollectLeavesOutPrintRuns(t *testing.T) {
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		switch name {
		case "claude":
			return []byte(
				`[{"pid":"10","cwd":"/r","kind":"interactive","startedAt":"1","sessionId":"ask","status":"busy"},
			{"pid":"11","cwd":"/r","kind":"interactive","startedAt":"2","sessionId":"real","status":"idle"}]`,
			), nil
		case "ps":
			return []byte("10 ttys001 claude -p --output-format stream-json -- what is this\n" +
				"11 ttys002 claude --dangerously-skip-permissions -- explain -p flags\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	snap := Collect(context.Background(), Options{Run: run, ProjectsDir: t.TempDir(), Folders: []string{"/r"}})
	require.Len(t, snap.Live, 1)
	assert.Equal(t, "real", snap.Live[0].ID, "a -p in the prompt doesn't make a print run")

	assert.True(t, commandIsPrintRun("claude --print hi"))
	assert.False(t, commandIsPrintRun("claude --resume x"))
}

func TestSetCustomTitleRenamesTheWayClaudeDoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeFile(t, path, `{"type":"ai-title","aiTitle":"Pull remote","sessionId":"s"}`+"\n")
	tr := NewTracker()

	require.NoError(t, SetCustomTitle(path, "s", "Sessions work"))
	sum, err := tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Sessions work", sum.CustomTitle)
	s := Session{}
	applySummary(&s, sum)
	assert.Equal(t, "Sessions work", s.Title, "a given name wins over Claude's title")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `{"type":"custom-title","customTitle":"Sessions work","sessionId":"s"}`+"\n")

	require.NoError(t, SetCustomTitle(path, "s", ""))
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Empty(t, sum.CustomTitle, "an empty name clears it")

	// A half-written line from Claude is never appended to.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"assi`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.ErrorIs(t, SetCustomTitle(path, "s", "x"), ErrTranscriptBusy)

	assert.Error(t, SetCustomTitle(filepath.Join(t.TempDir(), "missing.jsonl"), "s", "x"))
}

func TestWaitingSessionsNeedYou(t *testing.T) {
	got, err := ParseLive([]byte(`[{"pid":7,"sessionId":"s1","status":"waiting","waitingFor":"input needed"},
		{"pid":8,"sessionId":"s2","status":"idle"}]`))
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, StatusWaiting, got[0].Status)
	assert.Equal(t, "input needed", got[0].WaitingFor)
	assert.Equal(t, StatusIdle, got[1].Status)
}

func TestTrackerKnowsTheToolCallClaudeWaitsOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	question := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1",` +
		`"name":"AskUserQuestion","input":{"questions":[{"question":"Red or blue?"}]}}]}}`
	writeFile(t, path, question+"\n")
	tr := NewTracker()
	sum, err := tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Red or blue?", sum.PendingTool)

	answer := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1",` +
		`"content":"Blue"}]}}`
	bash := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2",` +
		`"name":"Bash","input":{"command":"go test ./src/pkg/ime/..."}}]}}`
	writeFile(t, path, question+"\n"+answer+"\n"+bash+"\n")
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Bash(go test ./src/pkg/ime/...)", sum.PendingTool)

	done := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2",` +
		`"content":"ok"}]}}`
	writeFile(t, path, question+"\n"+answer+"\n"+bash+"\n"+done+"\n")
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Empty(t, sum.PendingTool, "its result came back")
}

func TestTrackerKnowsALastReplyThatFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	failed := `{"type":"assistant","isApiErrorMessage":true,"error":"authentication_failed",` +
		`"message":{"role":"assistant","content":[{"type":"text","text":"Login expired · Please run /login"}]}}`
	writeFile(t, path, failed+"\n")
	tr := NewTracker()
	sum, err := tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "Login expired · Please run /login", sum.LastError)

	ok := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}`
	writeFile(t, path, failed+"\n"+ok+"\n")
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Empty(t, sum.LastError, "it replied again")
}

func TestConversationReadsTheLastTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"move the launchers"},"timestamp":"2026-11-05T10:00:00Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/r/providers.go"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Moved them."}]}}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"<system-reminder>x</system-reminder>"}}`,
		`{"type":"user","message":{"role":"user","content":"add a test\n\n<marmota-context>\ncwd: ~/r\n</marmota-context>"}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"a subagent"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Added one."}]}}`,
	}
	writeFile(t, path, strings.Join(lines, "\n")+"\n")

	turns, err := Conversation(path, 10)
	require.NoError(t, err)
	require.Len(t, turns, 2, "tool results and meta records are no turns")
	assert.Equal(t, "move the launchers", turns[0].Prompt)
	assert.Equal(t, "Moved them.", turns[0].Reply)
	assert.Equal(t, []string{"Edit(providers.go)"}, turns[0].Tools)
	assert.Equal(t, "add a test", turns[1].Prompt, "superfile's context block is left out")
	assert.Equal(t, "Added one.", turns[1].Reply, "subagents' text is not the reply")

	last, err := Conversation(path, 1)
	require.NoError(t, err)
	assert.Equal(t, "add a test", last[0].Prompt)
}

// A screenshot a tool took can be a megabyte: the turn's prompt is then
// before the tail Conversation reads, and the reply after it still shows,
// under that prompt.
func TestATurnCutByTheTailKeepsItsPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	image := strings.Repeat("A", conversationTail+cutPromptChunk)
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"first"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"One."}]}}`,
		`{"type":"user","message":{"role":"user","content":"look at the page"},"timestamp":"2026-11-08T21:59:50Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Screenshot","input":{}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"data":"` + image + `"}}]}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"It shows the ledger."}]}}`,
	}
	writeFile(t, path, strings.Join(lines, "\n")+"\n")

	turns, err := Conversation(path, 10)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Equal(t, "look at the page", turns[0].Prompt)
	assert.Equal(t, "It shows the ledger.", turns[0].Reply)
	assert.Equal(t, "2026-11-08T21:59:50Z", turns[0].At.UTC().Format(time.RFC3339))

	// Nothing said between the cut and the next prompt: no cut turn.
	writeFile(t, path, strings.Join(append(lines[:5:5],
		`{"type":"user","message":{"role":"user","content":"and the totals"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"40."}]}}`,
	), "\n")+"\n")
	turns, err = Conversation(path, 10)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Equal(t, "and the totals", turns[0].Prompt)
}

// Claude Code records text pasted into its prompt, as superfile types it,
// between pasted_content tags.
func TestAPastedPromptIsAPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	pasted := `\n\n<pasted_content id=\"7bde\">\nadd a test\n\n<marmota-context>\ncwd: ~/r\n` +
		`</marmota-context>\n</pasted_content id=\"7bde\">\n`
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"first"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"One."}]}}`,
		`{"type":"user","message":{"role":"user","content":"` + pasted + `"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Two."}]}}`,
	}
	writeFile(t, path, strings.Join(lines, "\n")+"\n")

	turns, err := Conversation(path, 10)
	require.NoError(t, err)
	require.Len(t, turns, 2)
	assert.Equal(t, "add a test", turns[1].Prompt)
	assert.Equal(t, "Two.", turns[1].Reply)

	sum, err := NewTracker().Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "add a test", sum.LastPrompt)
}

func TestCollectListsFoldersAndKeepsTodaysSessions(t *testing.T) {
	projects := t.TempDir()
	a, b := "/w/a", "/w/b"
	session := func(folder, id, title string, extra ...string) {
		lines := append([]string{`{"type":"ai-title","aiTitle":"` + title + `","cwd":"` + folder + `"}`}, extra...)
		writeFile(t, filepath.Join(projects, ProjectSlug(folder), id+".jsonl"), strings.Join(lines, "\n")+"\n")
	}
	session(a, "a1", "first")
	session(a, "a2", "second")
	session(a, "ask1", "one question",
		`{"type":"user","entrypoint":"sdk-cli","message":{"role":"user","content":"what is this?"}}`)
	session(b, "b1", "elsewhere")
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte("[]"), nil }

	snap := Collect(context.Background(), Options{Run: run, ProjectsDir: projects, Folders: []string{a}, PerFolder: 1,
		Include: map[string]string{"a2": a, "ask1": a}})
	var ids []string
	for _, s := range snap.Past {
		ids = append(ids, s.ID)
	}
	assert.ElementsMatch(t, []string{"a1", "a2", "ask1"}, ids,
		"one newest of the folder, plus the sessions asked for, a print-only one included")
	assert.Equal(t, []string{a}, snap.Folders)

	snap = Collect(context.Background(), Options{Run: run, ProjectsDir: projects, PerFolder: 5, RecentFolders: 10})
	assert.ElementsMatch(t, []string{a, b}, snap.Folders, "the folders sessions were active in")
	for _, s := range snap.Past {
		assert.NotEqual(t, "ask1", s.ID, "a session only claude -p ran is left out")
	}
}

// With Since, only sessions active since are listed, but for those asked
// for always, such as a folder's Ask session.
func TestCollectListsOnlyRecentFinishedSessions(t *testing.T) {
	projects := t.TempDir()
	folder := "/w/a"
	yesterday := time.Now().Add(-30 * time.Hour)
	session := func(id string, at time.Time, extra ...string) {
		path := filepath.Join(projects, ProjectSlug(folder), id+".jsonl")
		lines := append([]string{`{"type":"ai-title","aiTitle":"` + id + `","cwd":"` + folder + `"}`}, extra...)
		writeFile(t, path, strings.Join(lines, "\n")+"\n")
		require.NoError(t, os.Chtimes(path, at, at))
	}
	session("today", time.Now())
	session("old", yesterday)
	session("old-listed", yesterday)
	session("old-ask", yesterday,
		`{"type":"user","entrypoint":"sdk-cli","message":{"role":"user","content":"what is this?"}}`)
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte("[]"), nil }

	snap := Collect(context.Background(), Options{
		Run: run, ProjectsDir: projects, Folders: []string{folder}, PerFolder: 5,
		Since:   time.Now().Add(-time.Hour),
		Include: map[string]string{"old-listed": folder},
		Always:  map[string]string{"old-ask": folder},
	})
	var ids []string
	for _, s := range snap.Past {
		ids = append(ids, s.ID)
	}
	assert.ElementsMatch(t, []string{"today", "old-ask"}, ids)
}
