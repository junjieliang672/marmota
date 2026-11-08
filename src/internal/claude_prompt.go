package internal

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/junjieliang672/marmota/src/pkg/claudeprompt"
)

// Typing into a running Claude Code session as if at its prompt. superfile
// reads the prompt box off the session's pane first, sets a half-typed
// message aside with Claude Code's stash, and presses enter only once the box
// holds exactly what it typed. Claude Code puts the stashed message back as
// it sends.

const (
	// ctrl+s stashes what is typed, or brings the stash back into an empty box.
	claudeKeyStash = "\x13"
	// ctrl+_ undoes the last edit, such as a paste.
	claudeKeyUndo  = "\x1f"
	claudeKeyEnter = "\r"
)

var (
	errNoClaudePrompt      = errors.New("it is showing a question or a menu")
	errClaudeDraftAndStash = errors.New("it has a message typed and another stashed")
	errClaudePromptTyping  = errors.New("its prompt didn't take the text")
	errClaudePromptStuck   = errors.New("it didn't send the text; press enter there")
	errClaudePromptTimeout = errors.New("timed out waiting for the prompt")
)

// Test seams.
var (
	// weztermGetText returns what a pane's screen shows, colors included.
	weztermGetText = func(paneID int) (string, error) { //nolint:gochecknoglobals // test seam
		ctx, cancel := context.WithTimeout(context.Background(), claudeSendTextTimeout)
		defer cancel()
		//nolint:gosec // fixed binary and numeric pane id
		out, err := exec.CommandContext(ctx, "wezterm", "cli", "get-text", "--pane-id",
			strconv.Itoa(paneID), "--escapes").Output()
		return string(out), err
	}
	// weztermSendKeys sends keys to a pane as keystrokes, not as a paste.
	weztermSendKeys = func(paneID int, keys string) error { //nolint:gochecknoglobals // test seam
		ctx, cancel := context.WithTimeout(context.Background(), claudeSendTextTimeout)
		defer cancel()
		//nolint:gosec // fixed binary and numeric pane id; keys are an argument, never run by a shell
		return exec.CommandContext(ctx, "wezterm", "cli", "send-text", "--pane-id",
			strconv.Itoa(paneID), "--no-paste", "--", keys).Run()
	}
	// claudePromptPoll and claudePromptWait pace reading a pane back while
	// it takes in what superfile typed.
	claudePromptPoll = 30 * time.Millisecond //nolint:gochecknoglobals // test seam
	claudePromptWait = 2 * time.Second       //nolint:gochecknoglobals // test seam
)

// claudePromptMu keeps two sends from typing into a session at once.
var claudePromptMu sync.Mutex //nolint:gochecknoglobals // guards the panes typed into

// sendToClaudePrompt types text into the prompt of the Claude Code session in
// a pane and sends it, as if it were typed there. A message the person had
// typed there is stashed first, and Claude Code puts it back as the text is
// sent; a message they had stashed themselves stays stashed. Nothing is typed
// when the pane shows no prompt box, as while Claude asks a question, or when
// both a typed message and a stashed one would need keeping.
//
// It returns the box as it shows once the text is sent. The box isn't shown
// when the text opened a question, such as whether to trust a folder.
func sendToClaudePrompt(paneID int, text string) (claudeprompt.Box, error) {
	claudePromptMu.Lock()
	defer claudePromptMu.Unlock()

	before, err := readClaudePrompt(paneID)
	switch {
	case err != nil:
		return before, err
	case !before.Shown:
		return before, errNoClaudePrompt
	case before.Text != "" && before.Stashed:
		return before, errClaudeDraftAndStash
	}
	stashed := before.Text != ""
	if stashed {
		if err := weztermSendKeys(paneID, claudeKeyStash); err != nil {
			return before, err
		}
		if _, err := waitForClaudePrompt(paneID, func(b claudeprompt.Box) bool {
			return b.Shown && b.Text == "" && b.Stashed
		}); err != nil {
			return before, errClaudePromptTyping
		}
	}
	if err := weztermSendText(paneID, text); err != nil {
		takeBackClaudePaste(paneID, text, stashed)
		return before, err
	}
	if _, err := waitForClaudePrompt(paneID, func(b claudeprompt.Box) bool {
		exactly, _ := claudeprompt.Holds(b.Text, text)
		return b.Shown && exactly
	}); err != nil {
		takeBackClaudePaste(paneID, text, stashed)
		return before, errClaudePromptTyping
	}
	if err := weztermSendKeys(paneID, claudeKeyEnter); err != nil {
		return before, err
	}
	return settleClaudePrompt(paneID, text, before, stashed)
}

// settleClaudePrompt waits for the session to take what was sent, then leaves
// the box as the person had it: their message back in it, and a message they
// had stashed stashed again.
func settleClaudePrompt(paneID int, sent string, before claudeprompt.Box, stashed bool,
) (claudeprompt.Box, error) {
	after, err := waitForClaudePrompt(paneID, func(b claudeprompt.Box) bool {
		_, within := claudeprompt.Holds(b.Text, sent)
		return !b.Shown || !within
	})
	if err != nil {
		return after, errClaudePromptStuck
	}
	if !after.Shown {
		return after, nil // it asks something now
	}
	switch {
	case stashed:
		// Claude Code puts the message back as it sends; if it didn't,
		// superfile does.
		after, err = waitForClaudePrompt(paneID, func(b claudeprompt.Box) bool { return !b.Shown || !b.Stashed })
		if err != nil && after.Shown && after.Text == "" && after.Stashed {
			_ = weztermSendKeys(paneID, claudeKeyStash)
		}
	case before.Stashed:
		// Sending brought the person's own stash back into the box.
		after, err = waitForClaudePrompt(paneID, func(b claudeprompt.Box) bool {
			return !b.Shown || (!b.Stashed && b.Text != "")
		})
		if err == nil && after.Shown {
			_ = weztermSendKeys(paneID, claudeKeyStash)
		}
	}
	return after, nil
}

// takeBackClaudePaste undoes a paste that didn't show as typed, and gives back
// the message superfile stashed.
func takeBackClaudePaste(paneID int, pasted string, stashed bool) {
	box, err := readClaudePrompt(paneID)
	if err != nil || !box.Shown {
		return
	}
	if _, within := claudeprompt.Holds(box.Text, pasted); within {
		_ = weztermSendKeys(paneID, claudeKeyUndo)
		box, err = waitForClaudePrompt(paneID, func(b claudeprompt.Box) bool {
			_, within := claudeprompt.Holds(b.Text, pasted)
			return !b.Shown || !within
		})
		if err != nil || !box.Shown {
			return
		}
	}
	if stashed && box.Text == "" && box.Stashed {
		_ = weztermSendKeys(paneID, claudeKeyStash)
	}
}

// waitForClaudePrompt reads the pane until done holds for its prompt box, for
// up to claudePromptWait, and returns the box it read last.
func waitForClaudePrompt(paneID int, done func(claudeprompt.Box) bool) (claudeprompt.Box, error) {
	deadline := time.Now().Add(claudePromptWait)
	for {
		box, err := readClaudePrompt(paneID)
		if err != nil {
			return box, err
		}
		if done(box) {
			return box, nil
		}
		if time.Now().After(deadline) {
			return box, errClaudePromptTimeout
		}
		time.Sleep(claudePromptPoll)
	}
}

func readClaudePrompt(paneID int) (claudeprompt.Box, error) {
	screen, err := weztermGetText(paneID)
	if err != nil {
		return claudeprompt.Box{}, err
	}
	return claudeprompt.Read(screen), nil
}
