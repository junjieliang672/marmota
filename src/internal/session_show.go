package internal

// `y session show`: a session's own window, in a pane under the one this
// runs in. It is how a session Marmota's Sessions box doesn't list is
// reached, as a temporary one that stopped to ask something: the editor
// opens it under itself, you answer in the session's own prompt, and close
// the pane. The session goes on in its window either way.

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// sessionShowPercent is how much of the pane's height the session takes.
const sessionShowPercent = 50

// sessionShowViewLen is how much of a session's id names the view that
// shows it.
const sessionShowViewLen = 8

// ShownSession is where `y session show` put a session.
type ShownSession struct {
	ID   string `json:"id"`
	Pane int    `json:"pane"`
}

// SessionShow opens a pane under the WezTerm pane this runs in that shows a
// session's window, by the session's id or the start of it, and gives it
// the keyboard. Closing the pane leaves the session running.
func SessionShow(id string) (ShownSession, error) {
	host, window, full, err := sessionWindow(id)
	if err != nil {
		return ShownSession{}, err
	}
	attach := attachCommand(host, "show-"+full[:min(sessionShowViewLen, len(full))], window)
	self, err := currentWeztermPaneID()
	if err != nil {
		return ShownSession{}, errors.New("this isn't a WezTerm pane; show it yourself with: " + attach)
	}
	cwd, _ := os.Getwd()
	pane, err := splitWeztermPane(cwd, fmt.Sprintf("wezterm cli split-pane --pane-id %d --bottom --percent %d --cwd %s "+
		"-- /bin/sh -c %s", self, sessionShowPercent, shellQuote(cwd), shellQuote(attach)))
	if err != nil {
		return ShownSession{}, err
	}
	return ShownSession{ID: full, Pane: pane}, nil
}

// sessionWindow finds the tmux window a session runs in, by the session's
// id or the start of it: the computer, the window, and the whole id.
func sessionWindow(id string) (host, window, full string, err error) {
	if s, ok := codexSessionByID(id); ok {
		c, err := localTmux()
		if err != nil {
			return "", "", "", err
		}
		windows, err := c.Windows()
		if err != nil {
			return "", "", "", err
		}
		name := sessionhost.NameFor(sessionhost.CodexPrefix, s.ID)
		for _, w := range windows {
			if w.Name == name {
				return "", w.ID, s.ID, nil
			}
		}
		return "", "", "", fmt.Errorf("no window of Marmota's tmux is named for the Codex session %s", s.ID)
	}
	host, pane, full, _, err := sessionByID(id)
	if err != nil {
		return "", "", "", err
	}
	ref, ok := tmuxPanes.ref(pane)
	if !ok || strings.TrimSpace(ref.window) == "" {
		return "", "", "", fmt.Errorf("the window of session %s isn't known", full)
	}
	return host, ref.window, full, nil
}
