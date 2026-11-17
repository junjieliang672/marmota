package sessionhost

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Window is one of Marmota's tmux windows: one session.
type Window struct {
	ID     string // tmux's window id, "@3"
	Name   string // "cc-<session id>" for Claude Code; see NameFor
	PaneID string // its pane, "%5"
	PID    int    // the pane's process
	Cwd    string // where the pane's process is now
	Title  string // the title the program set, "✳ Fix the login bug"
	Width  int
	Height int
	// Active is set when a client looks at it: a pane beside Marmota.
	Active bool
}

// Name prefixes: a window's name says whose session it holds.
const (
	ClaudePrefix = "cc-"
	CodexPrefix  = "co-"
)

// NameFor is the window name for an agent's session id: "cc-<id>".
func NameFor(prefix, id string) string { return prefix + id }

// SessionOf returns the prefix and the id a window's name holds, and false
// for a window that holds no session, such as the first one.
func SessionOf(name string) (prefix, id string, ok bool) {
	for _, p := range []string{ClaudePrefix, CodexPrefix} {
		if rest, found := strings.CutPrefix(name, p); found && rest != "" {
			return p, rest, true
		}
	}
	return "", "", false
}

const windowFormat = "#{window_id}\t#{window_name}\t#{pane_id}\t#{pane_pid}\t#{pane_current_path}\t" +
	"#{window_width}\t#{window_height}\t#{window_active_clients}\t#{pane_title}"

// Windows lists Marmota's windows that hold a session.
func (c *Client) Windows() ([]Window, error) {
	lines, err := c.Command("list-panes -s -t " + Quote(TmuxSession) + " -F " + Quote(windowFormat))
	if err != nil {
		return nil, err
	}
	out := make([]Window, 0, len(lines))
	for _, line := range lines {
		f := strings.SplitN(line, "\t", 9)
		if len(f) < 9 {
			continue
		}
		if _, _, ok := SessionOf(f[1]); !ok {
			continue
		}
		pid, _ := strconv.Atoi(f[3])
		w, _ := strconv.Atoi(f[5])
		h, _ := strconv.Atoi(f[6])
		active, _ := strconv.Atoi(f[7])
		out = append(out, Window{ID: f[0], Name: f[1], PaneID: f[2], PID: pid, Cwd: f[4],
			Width: w, Height: h, Active: active > 0, Title: f[8]})
	}
	return out, nil
}

// Start opens a window named name in folder cwd, running command in the
// login shell with Homebrew's and Claude Code's folders on PATH, and returns
// it. The window opens in the background: no pane switches to it.
func (c *Client) Start(name, cwd, command string) (Window, error) {
	if strings.ContainsAny(name, " \t\n'\"") || name == "" {
		return Window{}, fmt.Errorf("bad window name %q", name)
	}
	script := `cd ` + dirWord(cwd) + ` 2>/dev/null || cd "$HOME"` + "\n" + `exec "$SHELL" -lc ` + shellQuote(command)
	lines, err := c.Command("new-window -d -P -F " + Quote(windowFormat) + " -t " + Quote(TmuxSession+":") +
		" -n " + Quote(name) + " " + Quote(Script(script)))
	if err != nil {
		return Window{}, err
	}
	if len(lines) == 0 {
		return Window{}, errors.New("tmux didn't say which window it opened")
	}
	f := strings.SplitN(lines[0], "\t", 9)
	if len(f) < 3 {
		return Window{}, fmt.Errorf("tmux answered %q", lines[0])
	}
	pid, _ := strconv.Atoi(f[3])
	return Window{ID: f[0], Name: name, PaneID: f[2], PID: pid, Cwd: cwd}, nil
}

// Rename names a window, as when a started Codex session's id is known.
func (c *Client) Rename(windowID, name string) error {
	_, err := c.Command("rename-window -t " + Quote(windowID) + " " + Quote(name))
	return err
}

// Close ends a window and the program in it.
func (c *Client) CloseWindow(windowID string) error {
	_, err := c.Command("kill-window -t " + Quote(windowID))
	return err
}

// Capture returns what a pane shows, colors included, the way
// `wezterm cli get-text --escapes` does.
func (c *Client) Capture(paneID string) (string, error) {
	lines, err := c.Command("capture-pane -p -e -N -t " + Quote(paneID))
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

// sendChunk keeps each send-keys line a reasonable length.
const sendChunk = 512

// SendBytes types bytes into a pane as if they came from the keyboard: keys
// are their escape sequences ("\r" for enter, "\x1b" for esc).
func (c *Client) SendBytes(paneID string, b []byte) error {
	for len(b) > 0 {
		n := min(len(b), sendChunk)
		hexes := make([]string, n)
		for i := range n {
			hexes[i] = hex.EncodeToString(b[i : i+1])
		}
		if _, err := c.Command("send-keys -t " + Quote(paneID) + " -H " + strings.Join(hexes, " ")); err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// Paste puts text into a pane as a paste, the way `wezterm cli send-text`
// does: bracketed, when the program asked for that.
func (c *Client) Paste(paneID, text string) error {
	lines, err := c.Command("display-message -p -t " + Quote(paneID) + " " + Quote("#{bracketed_paste}"))
	if err != nil {
		return err
	}
	b := []byte(text)
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "1" {
		b = append(append([]byte("\x1b[200~"), b...), []byte("\x1b[201~")...)
	}
	return c.SendBytes(paneID, b)
}

// AttachCommand is the shell command, for this computer's pane, that shows a
// window: a tmux client of a session of its own in the group of "marmota",
// so two panes can show two windows, which ends when the pane closes. view
// names it, one per Marmota.
func AttachCommand(view, windowID string) string {
	return Tmux() + " new-session -A -t " + TmuxSession + " -s " + shellQuote(view) +
		` \; set-option destroy-unattached on \; select-window -t ` + shellQuote(windowID)
}

// Show makes the view's pane show a window: the view is the session
// AttachCommand made.
func (c *Client) Show(view, windowID string) error {
	_, err := c.Command("select-window -t " + Quote(view+":"+windowID))
	return err
}

// ViewWindow is the window a view shows, or "" when there is no such view.
func (c *Client) ViewWindow(view string) string {
	lines, err := c.Command("display-message -p -t " + Quote(view+":") + " " + Quote("#{window_id}"))
	if err != nil || len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}

// dirWord quotes a folder for the shell over there, with a leading ~ for
// that computer's home: "~/Developer" is /Users/kingjj/Developer on one and
// /Users/junjieliang/Developer on another.
func dirWord(dir string) string {
	switch {
	case dir == "~":
		return `"$HOME"`
	case strings.HasPrefix(dir, "~/"):
		return `"$HOME"/` + shellQuote(dir[2:])
	}
	return shellQuote(dir)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
