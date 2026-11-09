package codexsession

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// A rollout is a session's log: one JSON record a line, each
// {timestamp, type, payload}. The records superfile reads:
//
//	session_meta                  the folder; a fork's history_base
//	turn_context                  the model and effort of the turn
//	event_msg/item_completed      UserMessage, AgentMessage, CommandExecution,
//	                              FileChange (each file's unified_diff)
//	event_msg/task_complete       a turn ended (completed_at, unix seconds)
//	event_msg/turn_aborted        a turn was stopped

// Summary is what superfile needs from one rollout.
type Summary struct {
	Cwd          string
	LastPrompt   string
	Model        string
	Effort       string
	LastActivity time.Time
	LastTurnEnd  time.Time
	// EditedFiles are the files Codex changed with its patch tool, oldest
	// first; files it changed through shell commands aren't recorded.
	EditedFiles []string
}

const (
	maxEditedFiles = 500
	maxPromptRunes = 600
	maxToolRunes   = 120
)

type rolloutRecord struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rolloutPayload struct {
	Type        string       `json:"type"`
	Cwd         string       `json:"cwd"`
	Model       string       `json:"model"`
	Effort      string       `json:"effort"`
	CompletedAt int64        `json:"completed_at"`
	Item        *rolloutItem `json:"item"`
	HistoryBase *historyBase `json:"history_base"`
}

// historyBase is where a fork's history is: a fork's rollout keeps no copy
// of the conversation it continues, only the session it was forked from
// and how much of that session's rollout was said before.
type historyBase struct {
	ThreadID      string `json:"thread_id"`
	EndByteOffset int64  `json:"end_byte_offset"`
}

type rolloutItem struct {
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Command []string                 `json:"command"`
	Changes map[string]rolloutChange `json:"changes"`
}

type rolloutChange struct {
	Type        string  `json:"type"` // add, update or delete
	UnifiedDiff string  `json:"unified_diff"`
	Content     string  `json:"content"` // an added or deleted file's text
	MovePath    *string `json:"move_path"`
}

// text is a message's text, its parts joined.
func (it *rolloutItem) text() string {
	parts := make([]string, 0, len(it.Content))
	for _, c := range it.Content {
		if strings.EqualFold(c.Type, "text") && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, strings.TrimSpace(c.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

func parseRecord(line []byte) (rolloutRecord, rolloutPayload, bool) {
	var r rolloutRecord
	var p rolloutPayload
	if json.Unmarshal(line, &r) != nil || json.Unmarshal(r.Payload, &p) != nil {
		return r, p, false
	}
	return r, p, true
}

func (s *Summary) apply(line []byte) {
	r, p, ok := parseRecord(line)
	if !ok {
		return
	}
	ts := parseTime(r.Timestamp)
	if ts.After(s.LastActivity) {
		s.LastActivity = ts
	}
	switch r.Type {
	case "session_meta":
		if s.Cwd == "" {
			s.Cwd = p.Cwd
		}
	case "turn_context":
		if p.Model != "" {
			s.Model = p.Model
		}
		if p.Effort != "" {
			s.Effort = p.Effort
		}
	case "event_msg":
		s.applyEvent(p, ts)
	}
}

func (s *Summary) applyEvent(p rolloutPayload, ts time.Time) {
	switch p.Type {
	case "task_complete":
		end := ts
		if p.CompletedAt > 0 {
			end = time.Unix(p.CompletedAt, 0)
		}
		if end.After(s.LastTurnEnd) {
			s.LastTurnEnd = end
		}
	case "turn_aborted":
		if ts.After(s.LastTurnEnd) {
			s.LastTurnEnd = ts
		}
	case "item_completed":
		if p.Item == nil {
			return
		}
		switch p.Item.Type {
		case "UserMessage":
			if text := singleLine(claudesession.StripContext(p.Item.text())); text != "" {
				s.LastPrompt = truncateRunes(text, maxPromptRunes)
			}
		case "FileChange":
			for path, ch := range p.Item.Changes {
				s.addEditedFile(path)
				if ch.MovePath != nil && *ch.MovePath != "" {
					s.addEditedFile(*ch.MovePath)
				}
			}
		}
	}
}

func (s *Summary) addEditedFile(path string) {
	for i, p := range s.EditedFiles {
		if p == path {
			s.EditedFiles = append(s.EditedFiles[:i], s.EditedFiles[i+1:]...)
			break
		}
	}
	s.EditedFiles = append(s.EditedFiles, path)
	if len(s.EditedFiles) > maxEditedFiles {
		s.EditedFiles = s.EditedFiles[len(s.EditedFiles)-maxEditedFiles:]
	}
}

// Tracker summarises rollouts incrementally: a file is read once, then only
// what was appended since.
type Tracker struct {
	mu    sync.Mutex
	files map[string]*trackedRollout
}

type trackedRollout struct {
	size    int64
	modTime time.Time
	offset  int64
	summary Summary
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{files: make(map[string]*trackedRollout)}
}

// Summary returns the up-to-date summary of the rollout at path.
func (t *Tracker) Summary(path string) (Summary, error) {
	info, err := os.Stat(path)
	if err != nil {
		t.mu.Lock()
		delete(t.files, path)
		t.mu.Unlock()
		return Summary{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	tf := t.files[path]
	if tf != nil && tf.size == info.Size() && tf.modTime.Equal(info.ModTime()) {
		return copySummary(tf.summary), nil
	}
	if tf == nil || info.Size() < tf.offset {
		tf = &trackedRollout{}
	}
	if err := tf.readFrom(path); err != nil {
		return Summary{}, err
	}
	tf.size, tf.modTime = info.Size(), info.ModTime()
	t.files[path] = tf
	return copySummary(tf.summary), nil
}

func copySummary(s Summary) Summary {
	s.EditedFiles = append([]string(nil), s.EditedFiles...)
	return s
}

func (tf *trackedRollout) readFrom(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(tf.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(f, 64*1024) //nolint:mnd // read buffer
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			tf.offset += int64(len(line))
			if line = bytes.TrimSpace(line); len(line) > 0 && line[0] == '{' {
				tf.summary.apply(line)
			}
		}
		// A last line without '\n' is still being written: read it next time.
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// conversationTail is how much of a rollout's end Conversation reads.
const conversationTail = 512 * 1024

// maxForks is how many forks back Conversation follows a fork's history.
const maxForks = 8

// Conversation returns the last limit turns of a rollout. Only its tail is
// read, so a long session costs no more than a short one. A fork's turns
// follow those of the session it was forked from, said before the fork.
func Conversation(path string, limit int) ([]claudesession.Turn, error) {
	return conversation(path, -1, limit, maxForks)
}

// conversation reads the turns of a rollout's first end bytes (-1: all of
// it), and of the history a fork continues, up to forks forks back.
func conversation(path string, end int64, limit, forks int) ([]claudesession.Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if end < 0 || end > info.Size() {
		end = info.Size()
	}
	start := max(0, end-conversationTail)
	reader := bufio.NewReaderSize(io.NewSectionReader(f, start, end-start), 64*1024) //nolint:mnd // read buffer
	var turns []claudesession.Turn
	first := start
	if start > 0 {
		cut, _ := reader.ReadBytes('\n') // the cut first line
		first += int64(len(cut))
		turns = []claudesession.Turn{{}} // see claudesession.SettleCutTurn
	}
	for {
		line, err := reader.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			turns = applyTurnLine(turns, line)
		}
		if err != nil {
			break
		}
	}
	if start > 0 {
		turns = claudesession.SettleCutTurn(f, first, turns, limit, `"UserMessage"`,
			func(line []byte) (claudesession.Turn, bool) {
				if t := applyTurnLine(nil, line); len(t) == 1 {
					return t[0], true
				}
				return claudesession.Turn{}, false
			})
	}
	if forks > 0 && (limit <= 0 || len(turns) < limit) {
		if base := forkedHistory(f); base != nil {
			if parent := rolloutOf(path, base.ThreadID); parent != "" {
				earlier, err := conversation(parent, base.EndByteOffset, limit-len(turns), forks-1)
				if err == nil {
					turns = append(earlier, turns...)
				}
			}
		}
	}
	if limit > 0 && len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return turns, nil
}

// forkedHistory is a fork's history_base, from its rollout's first record;
// nil for a session that isn't a fork.
func forkedHistory(f *os.File) *historyBase {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	line, _ := bufio.NewReaderSize(f, 64*1024).ReadBytes('\n') //nolint:mnd // read buffer
	r, p, ok := parseRecord(bytes.TrimSpace(line))
	if !ok || r.Type != "session_meta" || p.HistoryBase == nil || p.HistoryBase.ThreadID == "" {
		return nil
	}
	return p.HistoryBase
}

// rolloutOf finds the rollout of session id from another one's path: Codex
// names it rollout-<time>-<id>.jsonl, in sessions/<year>/<month>/<day>.
func rolloutOf(path, id string) string {
	if strings.ContainsAny(id, `*?[]\/`) {
		return ""
	}
	name := "rollout-*-" + id + ".jsonl"
	day := filepath.Dir(path)
	sessions := filepath.Dir(filepath.Dir(filepath.Dir(day)))
	for _, pattern := range []string{filepath.Join(day, name), filepath.Join(sessions, "*", "*", "*", name)} {
		if found, _ := filepath.Glob(pattern); len(found) > 0 {
			return found[0]
		}
	}
	return ""
}

// applyTurnLine adds one record to the turns: a prompt starts a turn;
// Codex's messages, commands and edits fill the latest one.
func applyTurnLine(turns []claudesession.Turn, line []byte) []claudesession.Turn {
	r, p, ok := parseRecord(line)
	if !ok || r.Type != "event_msg" || p.Type != "item_completed" || p.Item == nil {
		return turns
	}
	at := parseTime(r.Timestamp)
	it := p.Item
	if it.Type == "UserMessage" {
		if prompt := strings.TrimSpace(claudesession.StripContext(it.text())); prompt != "" {
			turns = append(turns, claudesession.Turn{Prompt: prompt, At: at})
		}
		return turns
	}
	if len(turns) == 0 {
		return turns
	}
	t := &turns[len(turns)-1]
	switch it.Type {
	case "AgentMessage":
		if text := it.text(); text != "" {
			if t.Reply != "" {
				t.Reply += "\n\n"
			}
			t.Reply += text
			t.RepliedAt = at
		}
	case "CommandExecution":
		if len(it.Command) > 0 {
			t.Tools = append(t.Tools, truncateRunes("Ran("+singleLine(it.Command[len(it.Command)-1])+")",
				maxToolRunes))
		}
	case "FileChange":
		for path := range it.Changes {
			t.Tools = append(t.Tools, "Edit("+filepath.Base(path)+")")
		}
	}
	return turns
}

// hunkHeader is a unified diff's hunk line: @@ -3,7 +3,8 @@, counts optional.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Changes maps each file the session changed with its patch tool to the
// changes, in the order it made them: Codex records each edit's diff, and
// keeps no copy of a file from before.
func Changes(path string) (map[string][]claudesession.Hunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string][]claudesession.Hunk)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20) //nolint:mnd // rollout lines can be long
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"FileChange"`)) {
			continue
		}
		r, p, ok := parseRecord(line)
		if !ok || p.Item == nil || p.Item.Type != "FileChange" {
			continue
		}
		at := parseTime(r.Timestamp)
		for file, ch := range p.Item.Changes {
			out[file] = append(out[file], changeHunks(ch, at)...)
		}
	}
	return out, sc.Err()
}

// changeHunks turns one recorded change into hunks: an update's diff, or a
// whole file added or deleted.
func changeHunks(ch rolloutChange, at time.Time) []claudesession.Hunk {
	switch ch.Type {
	case "add", "delete":
		text := ch.Content
		if text == "" {
			return parseUnifiedDiff(ch.UnifiedDiff, at)
		}
		lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
		mark := "+"
		h := claudesession.Hunk{NewStart: 1, NewLines: len(lines), At: at}
		if ch.Type == "delete" {
			mark = "-"
			h = claudesession.Hunk{OldStart: 1, OldLines: len(lines), At: at}
		}
		for _, l := range lines {
			h.Lines = append(h.Lines, mark+l)
		}
		return []claudesession.Hunk{h}
	default:
		return parseUnifiedDiff(ch.UnifiedDiff, at)
	}
}

func parseUnifiedDiff(diff string, at time.Time) []claudesession.Hunk {
	var hunks []claudesession.Hunk
	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			hunks = append(hunks, claudesession.Hunk{
				OldStart: atoi(m[1]), OldLines: count(m[2]),
				NewStart: atoi(m[3]), NewLines: count(m[4]), At: at,
			})
			continue
		}
		if len(hunks) == 0 || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		h := &hunks[len(hunks)-1]
		h.Lines = append(h.Lines, line)
	}
	return hunks
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// count is a hunk header's line count, 1 when it is left out.
func count(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
