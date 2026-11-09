package internal

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClaudePane plays a Claude Code pane for the WezTerm seams: it draws the
// prompt box as Claude Code 2.1.318 does and takes a paste, ctrl+s, ctrl+_
// and enter the way it does.
type fakeClaudePane struct {
	mu     sync.Mutex
	paneID int
	input  string
	stash  *string
	// question is set while Claude asks something: no prompt box shows,
	// and typing goes to the question.
	question bool
	// asksOnSend opens a question when this is sent, as /cd does for a
	// folder the session hasn't worked in.
	asksOnSend string
	// stray is typed into the box along with the next paste, as if
	// someone typed in the pane at the same moment.
	stray   string
	pastes  int
	history []string // the box before each paste, for ctrl+_
	sent    []string
	keys    []string // everything typed into the pane, in order
}

// newFakeClaudePane plays the session in pane 20, beside Marmota in the
// test panes.
func newFakeClaudePane(t *testing.T) *fakeClaudePane {
	t.Helper()
	f := &fakeClaudePane{paneID: 20}
	getText, sendText, sendKeys, wait := weztermGetText, weztermSendText, weztermSendKeys, claudePromptWait
	weztermGetText = func(id int) (string, error) {
		if id != f.paneID {
			return "", fmt.Errorf("no pane %d", id)
		}
		return f.screen(), nil
	}
	weztermSendText = func(id int, text string) error {
		if id != f.paneID {
			return fmt.Errorf("no pane %d", id)
		}
		f.paste(text)
		return nil
	}
	weztermSendKeys = func(id int, keys string) error {
		if id != f.paneID {
			return fmt.Errorf("no pane %d", id)
		}
		f.press(keys)
		return nil
	}
	claudePromptWait = 300 * time.Millisecond
	t.Cleanup(func() {
		weztermGetText, weztermSendText, weztermSendKeys, claudePromptWait = getText, sendText, sendKeys, wait
	})
	return f
}

func (f *fakeClaudePane) set(input string, stash *string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.input, f.stash = input, stash
}

func (f *fakeClaudePane) state() (string, *string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.input, f.stash, append([]string(nil), f.sent...)
}

func (f *fakeClaudePane) screen() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	rule := "\x1b[38:2::138:138:138m" + strings.Repeat("─", 60) + "\r"
	var b strings.Builder
	b.WriteString("\x1b[38:2::215:119:87m ▐▛███▛█   \x1b[0;1mClaude Code\x1b[0m v2.1.318\r\n\r\n")
	b.WriteString("\x1b[48:2::240:240:240m❯\x1b[39m \x1b[38:2::0:0:0mearlier prompt\x1b[39m\x1b[K\r\n\r\n")
	if f.question {
		b.WriteString(rule + "\n Do you prefer red or blue?\r\n\r\n")
		b.WriteString("\x1b[38:2::87:105:247m❯\x1b[39m 1. Red\r\n  2. Blue\r\n" + rule + "\n")
		b.WriteString("Enter to select · ↑/↓ to navigate · Esc to cancel\r\n")
		return b.String()
	}
	if f.stash != nil {
		b.WriteString(strings.Repeat(" ", 50) + "\x1b[38:2::102:102:102m › stashed\x1b[39m\r\n")
	} else {
		b.WriteString("\r\n")
	}
	b.WriteString(rule + "\n")
	if f.input == "" {
		b.WriteString("\x1b[39m❯\u00a0\x1b(B\x1b[0;2mTry \"how does <filepath> work?\"\r\n\x1b(B\x1b[0m")
	} else {
		lines := strings.Split(f.input, "\n")
		b.WriteString("\x1b[39m❯\u00a0" + lines[0] + "\r\n")
		for _, line := range lines[1:] {
			b.WriteString("  " + line + "\r\n")
		}
	}
	b.WriteString(rule + "\n")
	b.WriteString("\x1b[39m  \x1b[38:2::171:43:63m⏵⏵ bypass permissions on\x1b[39m\r\n")
	return b.String()
}

func (f *fakeClaudePane) paste(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, "paste:"+text)
	if f.question {
		return
	}
	f.history = append(f.history, f.input)
	f.pastes++
	shown := text
	if strings.Count(text, "\n") >= 2 {
		shown = fmt.Sprintf("[Pasted text #%d +%d lines]", f.pastes, strings.Count(text, "\n"))
	}
	f.input += f.stray + shown
	f.stray = ""
}

func (f *fakeClaudePane) press(keys string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, fmt.Sprintf("key:%q", keys))
	if f.question {
		return
	}
	switch keys {
	case claudeKeyStash:
		switch {
		case strings.TrimSpace(f.input) == "" && f.stash != nil:
			f.input, f.stash = *f.stash, nil
		case strings.TrimSpace(f.input) != "":
			stashed := f.input
			f.input, f.stash = "", &stashed
		}
	case claudeKeyUndo:
		if n := len(f.history); n > 0 {
			f.input, f.history = f.history[n-1], f.history[:n-1]
		}
	case claudeKeyEnter:
		if f.input == "" {
			return
		}
		f.sent = append(f.sent, f.input)
		if f.asksOnSend != "" && f.input == f.asksOnSend {
			f.question = true
		}
		f.input, f.history = "", nil
		if f.stash != nil {
			f.input, f.stash = *f.stash, nil // "Draft restored"
		}
	}
}

func ptr(s string) *string { return &s }

func TestSendToClaudePromptIntoAnEmptyBox(t *testing.T) {
	f := newFakeClaudePane(t)
	box, err := sendToClaudePrompt(20, "/rename fix sidebar")
	require.NoError(t, err)
	assert.True(t, box.Shown)
	input, stash, sent := f.state()
	assert.Equal(t, []string{"/rename fix sidebar"}, sent)
	assert.Empty(t, input)
	assert.Nil(t, stash)
}

func TestSendToClaudePromptKeepsWhatYouTyped(t *testing.T) {
	f := newFakeClaudePane(t)
	f.set("half typed", nil)
	_, err := sendToClaudePrompt(20, "/rename fix sidebar")
	require.NoError(t, err)
	input, stash, sent := f.state()
	assert.Equal(t, []string{"/rename fix sidebar"}, sent, "never joined with what you typed")
	assert.Equal(t, "half typed", input, "Claude Code puts it back")
	assert.Nil(t, stash)
}

func TestSendToClaudePromptKeepsYourStashStashed(t *testing.T) {
	f := newFakeClaudePane(t)
	f.set("", ptr("for later"))
	_, err := sendToClaudePrompt(20, "/rename fix sidebar")
	require.NoError(t, err)
	input, stash, sent := f.state()
	assert.Equal(t, []string{"/rename fix sidebar"}, sent)
	assert.Empty(t, input)
	require.NotNil(t, stash, "sending brought it back into the box; superfile stashes it again")
	assert.Equal(t, "for later", *stash)
}

func TestSendToClaudePromptTypesNothingWhenItCantKeepEverything(t *testing.T) {
	f := newFakeClaudePane(t)
	f.set("half typed", ptr("for later"))
	_, err := sendToClaudePrompt(20, "/rename fix sidebar")
	require.ErrorIs(t, err, errClaudeDraftAndStash)

	f.set("", nil)
	f.question = true
	_, err = sendToClaudePrompt(20, "/rename fix sidebar")
	require.ErrorIs(t, err, errNoClaudePrompt, "a question is open: enter would answer it")
	assert.Empty(t, f.keys, "nothing was typed")
}

func TestSendToClaudePromptTakesBackAPasteThatLandedWrong(t *testing.T) {
	f := newFakeClaudePane(t)
	f.set("half typed", nil)
	f.stray = "x"
	_, err := sendToClaudePrompt(20, "/rename fix sidebar")
	require.ErrorIs(t, err, errClaudePromptTyping)
	input, stash, sent := f.state()
	assert.Empty(t, sent, "enter is never pressed on text superfile didn't type")
	assert.Equal(t, "half typed", input, "the paste is undone and your text comes back")
	assert.Nil(t, stash)
}

func TestSendToClaudePromptReportsAQuestion(t *testing.T) {
	f := newFakeClaudePane(t)
	f.asksOnSend = "/cd /work/other"
	box, err := sendToClaudePrompt(20, "/cd /work/other")
	require.NoError(t, err)
	assert.False(t, box.Shown, "the session asks whether to work there")
}

func TestSendToClaudePromptSendsALongRequest(t *testing.T) {
	f := newFakeClaudePane(t)
	request := "also update the docs\n\n<marmota-context>\ncwd: ~/work\n</marmota-context>"
	_, err := sendToClaudePrompt(20, request)
	require.NoError(t, err, "it shows as [Pasted text #1 +4 lines]")
	_, _, sent := f.state()
	assert.Equal(t, []string{"[Pasted text #1 +4 lines]"}, sent)
}

func TestSendToClaudePromptPassesWeztermErrorsOn(t *testing.T) {
	newFakeClaudePane(t)
	weztermGetText = func(int) (string, error) { return "", errors.New("no such pane") }
	_, err := sendToClaudePrompt(20, "/rename x")
	require.EqualError(t, err, "no such pane")
}
