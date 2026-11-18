package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// `y session ask`: a message sent to a session, and the reply to it. It is
// `y session send`, then a wait for the turn the message starts to end, read
// off the session's transcript; what Claude wrote in that turn is the reply.
// Marmota's plugin for micro asks sessions through it, and so can a coding
// agent that wants another session's answer.

const (
	// SessionAskTimeout is how long `y session ask` waits for a reply.
	SessionAskTimeout = 15 * time.Minute
	// sessionAskTurns is how many of the transcript's last turns are
	// searched for the message.
	sessionAskTurns = 12
	// sessionAskKeyRunes is how much of a message tells its turn from the
	// others: its start, without spaces.
	sessionAskKeyRunes = 80
)

// sessionAskPoll is how often the transcript is read while waiting.
var sessionAskPoll = 1500 * time.Millisecond //nolint:gochecknoglobals // test seam

// NeedsYouError says the session stopped to ask you something before it had
// replied: a permission, or a question of its own.
type NeedsYouError struct {
	// What is what it asks, as Claude Code names it; Reply is what it
	// wrote before asking.
	What, Reply string
}

func (e *NeedsYouError) Error() string {
	return "it stopped to ask you: " + firstNonEmpty(e.What, "a question") +
		" (answer it from Marmota's Sessions box)"
}

// askedState is what `y session ask` reads at each look: the session's last
// turns, when a turn last ended, and what it waits for you on, if anything.
type askedState struct {
	turns       []claudesession.Turn
	lastTurnEnd time.Time
	needsYou    string
}

// SessionAsk sends text to a session and returns what Claude wrote in the
// turn it started, once that turn has ended. A session that stops to ask
// you something first returns a *NeedsYouError.
func SessionAsk(id, text string, timeout time.Duration) (string, error) {
	host, pane, full, transcript, err := sessionByID(id)
	if err != nil {
		return "", err
	}
	tracker := claudesession.NewTracker()
	look := func() askedState {
		var st askedState
		path, err := transcript()
		if err != nil || path == "" {
			return st
		}
		st.turns, _ = claudesession.Conversation(path, sessionAskTurns)
		if sum, err := tracker.Summary(path); err == nil {
			st.lastTurnEnd = sum.LastTurnEnd
		}
		st.needsYou = sessionNeedsYou(host, full)
		return st
	}
	before := look()
	if _, err := sendToClaudePrompt(pane, text); err != nil {
		return "", err
	}
	return awaitAskedTurn(look, askKey(text), askedAt(before.turns, askKey(text)), timeout)
}

// awaitAskedTurn waits for the turn whose prompt starts as key does, begun
// after prior, to end, and returns its reply.
func awaitAskedTurn(look func() askedState, key string, prior time.Time, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	waiting := 0
	for {
		time.Sleep(sessionAskPoll)
		st := look()
		turn, asked := askedTurn(st.turns, key, prior)
		if asked && st.lastTurnEnd.After(turn.At) {
			return strings.TrimSpace(turn.Reply), nil
		}
		// It may say it waits for you for a moment between two tools:
		// only one that still does at the next look has stopped.
		if st.needsYou != "" {
			waiting++
		} else {
			waiting = 0
		}
		if waiting > 1 {
			return "", &NeedsYouError{What: st.needsYou, Reply: strings.TrimSpace(turn.Reply)}
		}
		if time.Now().After(deadline) {
			if asked {
				return "", fmt.Errorf("no reply in %s; it may still be working", timeout)
			}
			return "", fmt.Errorf("the message isn't in its conversation after %s; it may be queued behind a long turn", timeout)
		}
	}
}

// askKey is the start of a message without its spaces: what its turn's
// prompt starts with in the transcript, however Claude Code wrapped it.
func askKey(text string) string {
	var b strings.Builder
	n := 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
		if n++; n == sessionAskKeyRunes {
			break
		}
	}
	return b.String()
}

// askedTurn is the last turn whose prompt starts as key does and that began
// after prior.
func askedTurn(turns []claudesession.Turn, key string, prior time.Time) (claudesession.Turn, bool) {
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		if key != "" && strings.HasPrefix(askKey(t.Prompt), key) && t.At.After(prior) {
			return t, true
		}
	}
	return claudesession.Turn{}, false
}

// askedAt is when the message was last asked before now: the same message
// sent again starts a turn after it.
func askedAt(turns []claudesession.Turn, key string) time.Time {
	t, _ := askedTurn(turns, key, time.Time{})
	return t.At
}

// sessionNeedsYou is what a session waits for you on, "" while it doesn't,
// as Claude Code's own list of sessions says, here or on its computer.
func sessionNeedsYou(host, id string) string {
	var data []byte
	var err error
	if host == "" {
		ctx, cancel := context.WithTimeout(context.Background(), claudeSendTextTimeout)
		defer cancel()
		data, err = claudeSessionRunner(ctx, "claude", "agents", "--json")
	} else {
		var c interface{ Agents() ([]byte, error) }
		if c, err = hostClient(host); err == nil {
			data, err = c.Agents()
		}
	}
	if err != nil {
		return ""
	}
	live, err := claudesession.ParseLive(data)
	if err != nil {
		return ""
	}
	for _, s := range live {
		if s.ID == id && s.Status == claudesession.StatusWaiting {
			return firstNonEmpty(s.PendingTool, s.WaitingFor, "a question")
		}
	}
	return ""
}

// IsNeedsYou reports an error that says the session stopped to ask you
// something, and what it wrote before.
func IsNeedsYou(err error) (*NeedsYouError, bool) {
	var needs *NeedsYouError
	ok := errors.As(err, &needs)
	return needs, ok
}
