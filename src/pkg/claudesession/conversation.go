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
}

// conversationTail is how much of a transcript's end Conversation reads.
const conversationTail = 512 * 1024

const (
	// cutPromptLimit is how far before a tail its turn's prompt is looked
	// for, and cutPromptChunk how much is read at a time going back.
	cutPromptLimit = 16 << 20
	cutPromptChunk = 256 * 1024
)

// Conversation returns the last limit turns of a transcript. Only the file's
// tail is read, so a long session costs no more than a short one.
func Conversation(path string, limit int) ([]Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(0, info.Size()-conversationTail)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(f, 64*1024) //nolint:mnd // read buffer
	var turns []Turn
	first := start
	if start > 0 {
		cut, _ := reader.ReadBytes('\n') // the cut first line
		first += int64(len(cut))
		turns = []Turn{{}} // see SettleCutTurn
	}
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			turns = applyTurnLine(turns, line)
		}
		if err != nil {
			break
		}
	}
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
	return turns, nil
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
