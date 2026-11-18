package internal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

func askTurn(prompt, reply string, at time.Time) claudesession.Turn {
	return claudesession.Turn{Prompt: prompt, Reply: reply, At: at}
}

// The reply is what Claude wrote in the turn the message started, once that
// turn has ended: not an earlier turn's, and not before it is whole.
func TestAskWaitsForItsOwnTurnToEnd(t *testing.T) {
	setForTest(t, &sessionAskPoll, time.Millisecond)
	t0 := time.Now().Add(-time.Hour)
	earlier := askTurn("something else", "an older reply", t0)
	looks := []askedState{
		// not in the conversation yet: it is queued behind a turn
		{turns: []claudesession.Turn{earlier}, lastTurnEnd: t0.Add(time.Second)},
		// asked, and Claude has begun to write
		{turns: []claudesession.Turn{earlier, askTurn("Summarize\n@notes.md#L3-9", "Half a", t0.Add(time.Minute))},
			lastTurnEnd: t0.Add(time.Second)},
		// the turn ended
		{turns: []claudesession.Turn{earlier, askTurn("Summarize\n@notes.md#L3-9", "Half a reply, then all of it.",
			t0.Add(time.Minute))}, lastTurnEnd: t0.Add(2 * time.Minute)},
	}
	n := 0
	look := func() askedState {
		st := looks[min(n, len(looks)-1)]
		n++
		return st
	}

	reply, err := awaitAskedTurn(look, askKey("Summarize\n@notes.md#L3-9"), time.Time{}, time.Minute)

	require.NoError(t, err)
	assert.Equal(t, "Half a reply, then all of it.", reply)
	assert.Equal(t, 3, n, "it looked until the turn had ended")
}

// The same message sent again is answered by its new turn, not the old one.
func TestAskTheSameMessageAgain(t *testing.T) {
	setForTest(t, &sessionAskPoll, time.Millisecond)
	t0 := time.Now().Add(-time.Hour)
	first := askTurn("again?", "the first answer", t0)
	before := []claudesession.Turn{first}
	prior := askedAt(before, askKey("again?"))
	require.Equal(t, t0, prior)

	looks := []askedState{
		{turns: before, lastTurnEnd: t0.Add(time.Second)},
		{turns: []claudesession.Turn{first, askTurn("again?", "the second answer", t0.Add(time.Minute))},
			lastTurnEnd: t0.Add(2 * time.Minute)},
	}
	n := 0
	reply, err := awaitAskedTurn(func() askedState {
		st := looks[min(n, len(looks)-1)]
		n++
		return st
	}, askKey("again?"), prior, time.Minute)

	require.NoError(t, err)
	assert.Equal(t, "the second answer", reply)
}

// A session that stops to ask for a permission says so, with what it wrote
// before; one that only says so for a moment goes on.
func TestAskSaysWhenTheSessionNeedsYou(t *testing.T) {
	setForTest(t, &sessionAskPoll, time.Millisecond)
	t0 := time.Now().Add(-time.Hour)
	asked := askTurn("run the tests", "I'll run them.", t0.Add(time.Minute))
	working := askedState{turns: []claudesession.Turn{asked}, lastTurnEnd: t0}
	waiting := working
	waiting.needsYou = "Bash(go test ./...)"

	seq := []askedState{working, waiting, working, waiting, waiting}
	n := 0
	_, err := awaitAskedTurn(func() askedState {
		st := seq[min(n, len(seq)-1)]
		n++
		return st
	}, askKey("run the tests"), time.Time{}, time.Minute)

	needs, ok := IsNeedsYou(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "Bash(go test ./...)", needs.What)
	assert.Equal(t, "I'll run them.", needs.Reply)
	assert.Equal(t, 5, n, "one look that says it waits isn't enough")
}

func TestAskGivesUpAfterItsTimeout(t *testing.T) {
	setForTest(t, &sessionAskPoll, time.Millisecond)
	_, err := awaitAskedTurn(func() askedState { return askedState{} }, askKey("hello"), time.Time{},
		20*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "isn't in its conversation")
}

func TestAskKeyIsTheStartWithoutSpaces(t *testing.T) {
	assert.Equal(t, "Fixthelogin", askKey("Fix the\n  login"))
	assert.Len(t, []rune(askKey(string(make([]rune, 0))+"中文 测试 "+string(make([]byte, 0)))), 4)
	long := ""
	for range 200 {
		long += "ab "
	}
	assert.Len(t, askKey(long), sessionAskKeyRunes)
	_, ok := askedTurn([]claudesession.Turn{askTurn("", "x", time.Now())}, "", time.Time{})
	assert.False(t, ok, "no message matches no turn")
}

// Two programs that type into the same prompt take turns: the second waits
// for the first to be done.
func TestOnePromptIsTypedIntoByOneProgramAtATime(t *testing.T) {
	path := t.TempDir() + "/prompt.lock"
	unlock := lockFile(path)
	got := make(chan struct{})
	go func() {
		defer lockFile(path)()
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("the second got the lock while the first held it")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("the second never got the lock")
	}
}
