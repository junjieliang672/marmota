package claudesession

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// Turn is one exchange of a conversation: what was asked, Claude's reply,
// and the tools it used on the way.
type Turn struct {
	Prompt string
	Reply  string
	Tools  []string
	At     time.Time
	// RepliedAt is when Claude last wrote text in the turn; zero until it
	// has.
	RepliedAt time.Time
	// Answer is the part of Reply an agent marked as its answer, apart
	// from what it said as it worked: Codex does, Claude Code doesn't.
	Answer string
}

// conversationTail is how much of a transcript's end Conversation reads,
// and ConversationBack first; conversationTailMax the most ConversationBack
// reads.
const (
	conversationTail    = 512 * 1024
	conversationTailMax = 32 << 20
)

const (
	// cutPromptLimit is how far before a tail its turn's prompt is looked
	// for, and cutPromptChunk how much is read at a time going back.
	cutPromptLimit = 16 << 20
	cutPromptChunk = 256 * 1024
)

// Conversation returns the last limit turns of a transcript. Only the file's
// last conversationTail bytes are read, so a long session costs no more than
// a short one; turns before them are left out.
func Conversation(path string, limit int) ([]Turn, error) {
	turns, _, err := conversation(path, limit, conversationTail)
	return turns, err
}

// ConversationBack is Conversation reading further back, four times as much
// each time, until it has limit turns (up to conversationTailMax bytes):
// the turn going on can be megabytes by itself, a screenshot a tool took,
// a long file read. It also returns how much of the file it read, which
// ConversationMore goes on from.
func ConversationBack(path string, limit int) ([]Turn, int64, error) {
	return conversation(path, limit, conversationTailMax)
}

// ConversationMore is turns, a transcript's last limit turns as read up to
// offset, with what was written after it. A transcript is only written at
// its end; one shorter than offset is read again.
func ConversationMore(path string, turns []Turn, offset int64, limit int) ([]Turn, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if info.Size() < offset {
		return ConversationBack(path, limit)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, err
	}
	turns = append([]Turn(nil), turns...)
	if n := len(turns); n > 0 {
		turns[n-1].Tools = append([]string(nil), turns[n-1].Tools...) // filled in place
	}
	turns, read := applyLines(turns, bufio.NewReaderSize(f, 64*1024)) //nolint:mnd // read buffer
	if limit > 0 && len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return turns, offset + read, nil
}

// ConversationTail is the last limit turns in a transcript's last tail
// bytes, where those bytes start (0: the whole file was read), and where the
// last whole line read ends, which ConversationMore goes on from. Turns
// further back are read with a larger tail.
func ConversationTail(path string, limit int, tail int64) ([]Turn, int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, 0, err
	}
	return conversationTailOf(f, info.Size(), max(tail, 1), limit)
}

func conversation(path string, limit int, most int64) ([]Turn, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	tail := int64(conversationTail)
	for {
		turns, _, end, err := conversationTailOf(f, info.Size(), tail, limit)
		if err != nil || limit <= 0 || len(turns) >= limit || tail >= info.Size() || tail >= most {
			return turns, end, err
		}
		tail *= 4
	}
}

// conversationTailOf is the last limit turns in the last tail bytes of f, a
// transcript size bytes long, where those bytes start, and where the last
// whole line read ends.
func conversationTailOf(f *os.File, size, tail int64, limit int) ([]Turn, int64, int64, error) {
	start := max(0, size-tail)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, 0, 0, err
	}
	reader := bufio.NewReaderSize(f, 64*1024) //nolint:mnd // read buffer
	var turns []Turn
	first := start
	if start > 0 {
		cut, _ := reader.ReadBytes('\n') // the cut first line
		first += int64(len(cut))
		turns = []Turn{{}} // see SettleCutTurn
	}
	turns, read := applyLines(turns, reader)
	if start > 0 {
		turns = SettleCutTurn(f, first, turns, limit, `"type":"user"`, func(line []byte) (Turn, bool) {
			if t := applyTurnLine(nil, line); len(t) == 1 {
				return t[0], true
			}
			return Turn{}, false
		})
	}
	if limit > 0 && len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return turns, start, first + read, nil
}

// applyLines adds the whole lines reader holds to turns, and says how many
// bytes they are. A last line not ended yet is still being written: it is
// left for the next read.
func applyLines(turns []Turn, reader *bufio.Reader) ([]Turn, int64) {
	var read int64
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return turns, read
		}
		read += int64(len(line))
		if len(bytes.TrimSpace(line)) > 0 {
			turns = applyTurnLine(turns, line)
		}
	}
}

// SettleCutTurn finishes the turn a tail read began with. The tail of a
// transcript can start in the middle of a turn, its prompt further back: one
// screenshot a tool took can be a megabyte. So a tail read starts with an
// empty turns[0], which gathers what is said before the tail's first prompt.
// Left empty, it is dropped; otherwise its prompt is the last line before
// first, the tail's first whole line, that has hint in it and that prompt
// makes a turn of. It isn't looked for when the last limit turns (all: 0)
// leave it out.
func SettleCutTurn(r io.ReaderAt, first int64, turns []Turn, limit int, hint string,
	prompt func(line []byte) (Turn, bool),
) []Turn {
	if len(turns) == 0 {
		return turns
	}
	if t := turns[0]; (t.Reply == "" && len(t.Tools) == 0) || (limit > 0 && len(turns) > limit) {
		return turns[1:]
	}
	turns[0].Prompt = "…" // when it isn't found
	lastLineBefore(r, first, cutPromptLimit, func(line []byte) bool {
		if !bytes.Contains(line, []byte(hint)) {
			return false
		}
		t, ok := prompt(line)
		if ok {
			turns[0].Prompt, turns[0].At = t.Prompt, t.At
		}
		return ok
	})
	return turns
}

// lastLineBefore reads r's lines back from end, the start of a line, until
// match accepts one, at most limit bytes back. It reports whether one was
// accepted.
func lastLineBefore(r io.ReaderAt, end, limit int64, match func(line []byte) bool) bool {
	floor := max(0, end-limit)
	pos := end
	var rest []byte // from pos to the start of the last line looked at
	for pos > floor {
		n := min(cutPromptChunk, pos-floor)
		data := make([]byte, n, n+int64(len(rest)))
		if _, err := r.ReadAt(data, pos-n); err != nil && !errors.Is(err, io.EOF) {
			return false
		}
		pos -= n
		data = append(data, rest...)
		lineEnd := len(data)
		for {
			k := bytes.LastIndexByte(data[:max(lineEnd-1, 0)], '\n')
			if k < 0 && pos > 0 {
				break // the line starts further back
			}
			if line := bytes.TrimSpace(data[k+1 : lineEnd]); len(line) > 0 && match(line) {
				return true
			}
			if k < 0 {
				return false // that was the first line
			}
			lineEnd = k + 1
		}
		rest = data[:lineEnd]
	}
	return false
}

// applyTurnLine adds one transcript record to the turns: a prompt starts a
// turn, Claude's text and tool calls fill the latest one.
func applyTurnLine(turns []Turn, line []byte) []Turn {
	var r record
	if json.Unmarshal(line, &r) != nil || r.IsSidechain || r.Message == nil {
		return turns
	}
	switch r.Type {
	case recordUser:
		if r.IsMeta {
			return turns
		}
		if p := strings.TrimSpace(StripContext(userPromptText(r.Message.Content))); p != "" {
			turns = append(turns, Turn{Prompt: p, At: parseTime(r.Timestamp)})
		}
	case recordAssistant:
		var blocks []contentBlock
		if len(turns) > 0 && json.Unmarshal(r.Message.Content, &blocks) == nil {
			t := &turns[len(turns)-1]
			before := t.Reply
			addReply(t, blocks)
			if t.Reply != before {
				t.RepliedAt = parseTime(r.Timestamp)
			}
		}
	}
	return turns
}

// addReply adds what Claude wrote and the tools it called to a turn.
func addReply(t *Turn, blocks []contentBlock) {
	for _, b := range blocks {
		switch b.Type {
		case blockText:
			if text := strings.TrimSpace(b.Text); text != "" {
				if t.Reply != "" {
					t.Reply += "\n\n"
				}
				t.Reply += text
			}
		case blockToolUse:
			t.Tools = append(t.Tools, describeToolCall(b))
		}
	}
}
