package claudesession

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	if start > 0 {
		_, _ = reader.ReadBytes('\n') // the cut first line
	}
	var turns []Turn
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			turns = applyTurnLine(turns, line)
		}
		if err != nil {
			break
		}
	}
	if limit > 0 && len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return turns, nil
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
		if p := strings.TrimSpace(StripSuperfileContext(userPromptText(r.Message.Content))); p != "" {
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
