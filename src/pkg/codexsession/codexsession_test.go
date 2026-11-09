package codexsession

import (
	"bufio"
	"crypto/sha1" //nolint:gosec // the WebSocket handshake
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// fakeServer answers like Codex's server: a WebSocket on a Unix socket,
// with a notification and a ping before each answer, as the real one may
// send them at any time.
type fakeServer struct {
	mu      sync.Mutex
	answers map[string]any
	calls   []call
}

type call struct {
	Method string
	Params map[string]any
}

func startFakeServer(t *testing.T, answers map[string]any) (string, *fakeServer) {
	t.Helper()
	// Unix socket paths are short: t.TempDir can be too long on macOS.
	home, err := os.MkdirTemp("/tmp", "cx")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := SocketPath(home)
	require.NoError(t, os.MkdirAll(filepath.Dir(sock), 0o755))
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	srv := &fakeServer{answers: answers}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn)
		}
	}()
	return home, srv
}

func (f *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	req, err := http.ReadRequest(r)
	if err != nil {
		return
	}
	sum := sha1.Sum([]byte(req.Header.Get("Sec-Websocket-Key") + wsGUID)) //nolint:gosec // the protocol's
	_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: "+base64.StdEncoding.EncodeToString(sum[:])+"\r\n\r\n")
	for {
		data, err := readClientFrame(r)
		if err != nil {
			return
		}
		var msg struct {
			ID     *int           `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(data, &msg) != nil || msg.ID == nil {
			continue
		}
		f.mu.Lock()
		f.calls = append(f.calls, call{msg.Method, msg.Params})
		answer, ok := f.answers[msg.Method]
		f.mu.Unlock()
		writeServerFrame(conn, wsOpPing, []byte("hi"))
		writeServerFrame(conn, wsOpText, []byte(`{"method":"account/updated","params":{}}`))
		reply := map[string]any{"id": *msg.ID, "result": answer}
		if msg.Method == "initialize" {
			reply["result"] = map[string]any{}
		} else if !ok {
			reply = map[string]any{"id": *msg.ID, "error": map[string]any{"code": -1, "message": "unknown " + msg.Method}}
		}
		data, _ = json.Marshal(reply)
		writeServerFrame(conn, wsOpText, data)
	}
}

func (f *fakeServer) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Method)
	}
	return out
}

func readClientFrame(r *bufio.Reader) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := uint64(hdr[1] & 0x7F)
	switch n {
	case 126:
		var l uint16
		_ = binary.Read(r, binary.BigEndian, &l)
		n = uint64(l)
	case 127:
		_ = binary.Read(r, binary.BigEndian, &n)
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(r, mask); err != nil {
		return nil, err
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	if hdr[0]&0x0F == wsOpPong {
		return readClientFrame(r)
	}
	return payload, nil
}

// writeServerFrame writes an unmasked frame, in two parts when it is long
// enough, to test messages split over frames.
func writeServerFrame(w io.Writer, op byte, data []byte) {
	if op == wsOpText && len(data) > 200 {
		half := len(data) / 2
		writeFrameRaw(w, op, false, data[:half])
		writeFrameRaw(w, 0, true, data[half:])
		return
	}
	writeFrameRaw(w, op, true, data)
}

func writeFrameRaw(w io.Writer, op byte, fin bool, data []byte) {
	first := op
	if fin {
		first |= wsFinBit
	}
	hdr := []byte{first}
	switch n := len(data); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	_, _ = w.Write(append(hdr, data...))
}

const testRollout = `{"timestamp":"2026-11-08T09:13:55.042Z","type":"session_meta","payload":{"id":"t1","cwd":"/w/repo"}}
{"timestamp":"2026-11-08T09:13:55.900Z","type":"turn_context","payload":{"model":"gpt-6.1-sol","effort":"low"}}
{"timestamp":"2026-11-08T09:13:55.980Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"fix the login <marmota-context>folder: /w/repo</marmota-context>"}]}}}
{"timestamp":"2026-11-08T09:13:58.042Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"Text","text":"Looking at it."}],"phase":"commentary"}}}
{"timestamp":"2026-11-08T09:13:59.395Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["/bin/zsh","-lc","go test ./..."]}}}
{"timestamp":"2026-11-08T09:14:00.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","changes":{"/w/repo/login.go":{"type":"update","unified_diff":"@@ -3,2 +3,2 @@\n ctx\n-old\n+new\n","move_path":null}}}}}
{"timestamp":"2026-11-08T09:14:01.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","changes":{"/w/repo/new.go":{"type":"add","content":"package repo\n\nfunc New() {}\n"}}}}}
{"timestamp":"2026-11-08T09:14:02.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"Text","text":"Fixed."}]}}}
{"timestamp":"2026-11-08T09:14:02.100Z","type":"event_msg","payload":{"type":"task_complete","completed_at":1794129242}}
`

func writeRollout(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout-2026-11-08T01-13-54-t1.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
	return path
}

func TestSummaryReadsTheRollout(t *testing.T) {
	path := writeRollout(t, testRollout)
	tr := NewTracker()
	sum, err := tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "/w/repo", sum.Cwd)
	assert.Equal(t, "gpt-6.1-sol", sum.Model)
	assert.Equal(t, "low", sum.Effort)
	assert.Equal(t, "fix the login", sum.LastPrompt, "superfile's context block is left out")
	assert.Equal(t, time.Unix(1794129242, 0), sum.LastTurnEnd)
	assert.Equal(t, []string{"/w/repo/login.go", "/w/repo/new.go"}, sum.EditedFiles)

	// Appended lines are read on the next call, and only they.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"timestamp":"2026-11-08T09:20:00Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"and the logout"}]}}}` + "\n" + `{"timestamp":"2026-11-08T09:21:00Z","type":"event_msg","payload":{"type":"turn_aborted"}}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	sum, err = tr.Summary(path)
	require.NoError(t, err)
	assert.Equal(t, "and the logout", sum.LastPrompt)
	assert.Equal(t, "2026-11-08T09:21:00Z", sum.LastTurnEnd.UTC().Format(time.RFC3339), "an interrupted turn ends it too")
}

func TestConversationMakesTurns(t *testing.T) {
	turns, err := Conversation(writeRollout(t, testRollout), 0)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Equal(t, "fix the login", turns[0].Prompt)
	assert.Equal(t, "Looking at it.\n\nFixed.", turns[0].Reply)
	assert.Equal(t, []string{"Ran(go test ./...)", "Edit(login.go)", "Edit(new.go)"}, turns[0].Tools)
	assert.False(t, turns[0].RepliedAt.IsZero())
}

// A fork's rollout keeps no copy of the conversation it continues: its
// turns follow those of the session it was forked from, said before the
// fork, from that session's rollout of another day.
func TestAForksConversationStartsWithTheForkedOnes(t *testing.T) {
	sessions := t.TempDir()
	write := func(day, id, text string) string {
		dir := filepath.Join(sessions, "2026", "11", day)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		path := filepath.Join(dir, "rollout-2026-11-"+day+"T01-13-54-"+id+".jsonl")
		require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
		return path
	}
	later := `{"timestamp":"2026-11-08T09:20:00Z","type":"event_msg","payload":{"type":"item_completed",` +
		`"item":{"type":"UserMessage","content":[{"type":"text","text":"after the fork"}]}}}` + "\n"
	write("07", "t1", testRollout+later)
	fork := write("08", "t2", `{"timestamp":"2026-11-08T09:30:00Z","type":"session_meta","payload":{"id":"t2",`+
		`"cwd":"/w/wt/repo","history_base":{"thread_id":"t1","end_byte_offset":`+
		strconv.Itoa(len(testRollout))+`}}}`+"\n"+
		`{"timestamp":"2026-11-08T09:31:00Z","type":"event_msg","payload":{"type":"item_completed",`+
		`"item":{"type":"UserMessage","content":[{"type":"text","text":"in the fork"}]}}}`+"\n")

	turns, err := Conversation(fork, 0)
	require.NoError(t, err)
	var prompts []string
	for _, turn := range turns {
		prompts = append(prompts, turn.Prompt)
	}
	assert.Equal(t, []string{"fix the login", "in the fork"}, prompts, "not what the other said after")
	assert.Equal(t, "Looking at it.\n\nFixed.", turns[0].Reply)

	turns, err = Conversation(fork, 1)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Equal(t, "in the fork", turns[0].Prompt)
}

func TestChangesAreTheRecordedDiffs(t *testing.T) {
	changes, err := Changes(writeRollout(t, testRollout))
	require.NoError(t, err)
	require.Len(t, changes["/w/repo/login.go"], 1)
	h := changes["/w/repo/login.go"][0]
	assert.Equal(t, claudesession.Hunk{OldStart: 3, OldLines: 2, NewStart: 3, NewLines: 2,
		Lines: []string{" ctx", "-old", "+new"}, At: h.At}, h)
	require.Len(t, changes["/w/repo/new.go"], 1)
	assert.Equal(t, []string{"+package repo", "+", "+func New() {}"}, changes["/w/repo/new.go"][0].Lines,
		"an added file is all added lines")
}

func TestCollectListsLoadedAndRecentSessions(t *testing.T) {
	rollout := writeRollout(t, testRollout)
	now := time.Now().Unix()
	name := "Fix the login"
	home, srv := startFakeServer(t, map[string]any{
		"thread/loaded/list": map[string]any{"data": []string{"t1", "sub"}},
		"thread/read": map[string]any{"thread": map[string]any{
			"id": "t1", "cwd": "/w/repo", "name": name, "createdAt": now - 60, "updatedAt": now,
			"status": map[string]any{"type": "active", "activeFlags": []string{"waitingOnApproval"}},
			"path":   rollout,
		}},
		"thread/list": map[string]any{"data": []map[string]any{
			{"id": "t1", "cwd": "/w/repo", "updatedAt": now, "status": map[string]any{"type": "active"}},
			{"id": "old", "cwd": "/w/repo", "preview": "yesterday's", "updatedAt": now - 3*86400,
				"status": map[string]any{"type": "notLoaded"}},
			{"id": "else", "cwd": "/w/other", "preview": "elsewhere", "updatedAt": now,
				"status": map[string]any{"type": "notLoaded"}},
			{"id": "done", "cwd": "/w/repo", "preview": "finished today", "updatedAt": now,
				"status": map[string]any{"type": "notLoaded"}},
		}},
	})
	sessions, err := Collect(Options{Home: home, Since: time.Now().Add(-time.Hour), Folders: []string{"/w/repo"}})
	require.NoError(t, err)
	// thread/read answers every id with t1 in this fake: "sub" comes back as
	// t1 and is listed once.
	require.Len(t, sessions, 2)
	s := sessions[0]
	assert.Equal(t, "t1", s.ID)
	assert.True(t, s.IsCodex())
	assert.True(t, s.Live)
	assert.Equal(t, claudesession.StatusWaiting, s.Status)
	assert.Equal(t, name, s.Title)
	assert.Equal(t, rollout, s.TranscriptPath)
	assert.Equal(t, "gpt-6.1-sol", s.Model, "from the rollout")
	assert.Equal(t, []string{"/w/repo/login.go", "/w/repo/new.go"}, s.EditedFiles)

	done := sessions[1]
	assert.Equal(t, "done", done.ID)
	assert.False(t, done.Live)
	assert.Equal(t, claudesession.StatusEnded, done.Status)
	assert.Equal(t, "finished today", done.Title, "no name: its first prompt")
	assert.Contains(t, srv.methods(), "thread/list")
}

func TestCollectWithoutAServer(t *testing.T) {
	_, err := Collect(Options{Home: t.TempDir()})
	assert.ErrorIs(t, err, ErrNoServer)
}

func TestSendRenameAndInterrupt(t *testing.T) {
	home, srv := startFakeServer(t, map[string]any{
		"turn/start":      map[string]any{"turn": map[string]any{"id": "turn9"}},
		"thread/name/set": map[string]any{},
		"thread/turns/list": map[string]any{"data": []map[string]any{
			{"id": "turn9", "status": "inProgress"}, {"id": "turn8", "status": "completed"},
		}},
		"turn/interrupt": map[string]any{},
	})
	require.NoError(t, Send(home, "t1", strings.Repeat("long prompt ", 30),
		TurnOptions{Effort: "max", Reviewer: "auto_review"}))
	require.NoError(t, Rename(home, "t1", "Login fix"))
	require.NoError(t, Interrupt(home, "t1"))
	srv.mu.Lock()
	defer srv.mu.Unlock()
	var got []string
	for _, c := range srv.calls {
		if c.Method == "initialize" {
			continue
		}
		got = append(got, c.Method)
		switch c.Method {
		case "turn/start":
			input := c.Params["input"].([]any)[0].(map[string]any)
			assert.Equal(t, strings.Repeat("long prompt ", 30), input["text"])
			assert.Equal(t, "max", c.Params["effort"])
			assert.Equal(t, "auto_review", c.Params["approvalsReviewer"])
		case "thread/name/set":
			assert.Equal(t, "Login fix", c.Params["name"])
		case "turn/interrupt":
			assert.Equal(t, "turn9", c.Params["turnId"], "the turn under way")
		}
	}
	assert.Equal(t, []string{"turn/start", "thread/name/set", "thread/turns/list", "turn/interrupt"}, got)
}

func TestAnErrorAnswerIsAnError(t *testing.T) {
	home, _ := startFakeServer(t, map[string]any{})
	err := Rename(home, "t1", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown thread/name/set")
}

func TestDefaultsComeFromConfigToml(t *testing.T) {
	home := t.TempDir()
	model, effort := Defaults(home)
	assert.Empty(t, model+effort, "no config")
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"),
		[]byte("model = \"gpt-6.1-sol\"\nmodel_reasoning_effort = \"low\"\n\n[projects.\"/w\"]\ntrust_level = \"trusted\"\n"), 0o600))
	model, effort = Defaults(home)
	assert.Equal(t, "gpt-6.1-sol", model)
	assert.Equal(t, "low", effort)
}

// The models are those Codex's picker lists, in its order, each with the
// efforts it takes.
func TestModelsAreThoseCodexsPickerOffers(t *testing.T) {
	home := t.TempDir()
	assert.Nil(t, Models(home), "no list")
	require.NoError(t, os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(`{"models":[
		{"slug":"luna","visibility":"list","priority":6,"default_reasoning_level":"medium",
		 "supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"}]},
		{"slug":"reserve","visibility":"hide","priority":5},
		{"slug":"sol","visibility":"list","priority":1,"default_reasoning_level":"low",
		 "supported_reasoning_levels":[{"effort":"low"},{"effort":"ultra"}]}]}`), 0o644))
	assert.Equal(t, []Model{
		{ID: "sol", Efforts: []string{"low", "ultra"}, DefaultEffort: "low"},
		{ID: "luna", Efforts: []string{"low", "medium"}, DefaultEffort: "medium"},
	}, Models(home))
}

func TestFlags(t *testing.T) {
	assert.Equal(t, []string{"--dangerously-bypass-approvals-and-sandbox"}, PermissionArgs("bypassPermissions"))
	assert.Equal(t, []string{"--sandbox", "read-only", "--ask-for-approval", "on-request"}, PermissionArgs("plan"))
	assert.Equal(t, []string{"--sandbox", "workspace-write", "--ask-for-approval", "on-request"}, PermissionArgs("auto"),
		"not --approve-for-me, which keeps the session off the shared server")
	assert.Equal(t, "auto_review", PermissionReviewer("auto"))
	assert.Empty(t, PermissionReviewer("acceptEdits"))
	assert.Equal(t, []string{"--model", "gpt-6-luna"}, ModelArgs("gpt-6-luna"))
	assert.Empty(t, ModelArgs(""))
	for _, mode := range []string{"bypassPermissions", "acceptEdits", "auto", "plan", "manual"} {
		for _, arg := range PermissionArgs(mode) {
			assert.NotContains(t, []string{"--config", "-c", "--approve-for-me"}, arg, mode)
		}
	}
}

func TestTheNewestCodexThatRunsIsChosen(t *testing.T) {
	versions := map[string][]int{
		"/nvm/codex":  nil, // broken install
		"/brew/codex": {0, 36, 0},
		"/app/codex":  {0, 160, 0},
		"/old/codex":  {0, 99, 9},
	}
	got := newestBinary([]string{"/nvm/codex", "/brew/codex", "/app/codex", "/old/codex"},
		func(p string) []int { return versions[p] })
	assert.Equal(t, "/app/codex", got)
	assert.Equal(t, "codex", newestBinary([]string{"/nvm/codex"}, func(string) []int { return nil }))
	assert.Equal(t, []int{0, 160, 0}, parseVersion("codex-cli 0.160.0\n"))
}
