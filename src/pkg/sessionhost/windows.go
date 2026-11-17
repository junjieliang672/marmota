package sessionhost

import (
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
	TTY    string // the pane's terminal, "/dev/ttys012": its program's tty
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
	"#{window_width}\t#{window_height}\t#{window_active_clients}\t#{pane_tty}\t#{pane_title}"

// Windows lists Marmota's windows that hold a session.
func (c *Client) Windows() ([]Window, error) {
	lines, err := c.Command("list-panes -s -t " + Quote(TmuxSession) + " -F " + Quote(windowFormat))
	if err != nil {
		return nil, err
	}
	out := make([]Window, 0, len(lines))
	for _, line := range lines {
		f := strings.SplitN(line, "\t", 10)
		if len(f) < 10 {
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
			Width: w, Height: h, Active: active > 0, TTY: f[8], Title: f[9]})
	}
	return out, nil
}

// Start opens a window named name in folder cwd, running command with sh,
// with Homebrew's and Claude Code's folders on PATH after what PATH has,
// and returns it. The window opens in the background: no pane switches to it.
func (c *Client) Start(name, cwd, command string) (Window, error) {
	if strings.ContainsAny(name, " \t\n'\"") || name == "" {
		return Window{}, fmt.Errorf("bad window name %q", name)
	}
	script := `cd ` + dirWord(cwd) + ` 2>/dev/null || cd "$HOME"` + "\n" + `exec /bin/sh -c ` + shellQuote(command)
	lines, err := c.Command("new-window -d -P -F " + Quote(windowFormat) + " -t " + Quote(TmuxSession+":") +
		" -n " + Quote(name) + " " + Quote(Script(script)))
	if err != nil {
		return Window{}, err
	}
	if len(lines) == 0 {
		return Window{}, errors.New("tmux didn't say which window it opened")
	}
	f := strings.SplitN(lines[0], "\t", 10)
	if len(f) < 10 {
		return Window{}, fmt.Errorf("tmux answered %q", lines[0])
	}
	pid, _ := strconv.Atoi(f[3])
	w, _ := strconv.Atoi(f[5])
	h, _ := strconv.Atoi(f[6])
	return Window{ID: f[0], Name: name, PaneID: f[2], PID: pid, Cwd: cwd, Width: w, Height: h, TTY: f[8]}, nil
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

// bufferChunk keeps each set-buffer line a reasonable length.
const bufferChunk = 2048

// SendBytes types bytes into a pane as if they came from the keyboard: keys
// are their escape sequences ("\r" for enter, "\x1b[B" for down, "\x13"
// for ctrl+s). A key goes through tmux by its name, which tmux encodes as
// the program asked for: a program in tmux took the raw sequence for down
// as nothing at all. Text between keys reaches the program as it is.
func (c *Client) SendBytes(paneID string, b []byte) error {
	for _, part := range splitKeys(b) {
		var err error
		if part.key != "" {
			_, err = c.Command("send-keys -t " + Quote(paneID) + " " + part.key)
		} else {
			err = c.pasteBuffer(paneID, part.text, false)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// keySequences are the keys whose escape sequences become tmux key names.
var keySequences = []struct{ seq, name string }{ //nolint:gochecknoglobals // a table
	{"\x1b[A", "Up"}, {"\x1b[B", "Down"}, {"\x1b[C", "Right"}, {"\x1b[D", "Left"},
	{"\x1bOA", "Up"}, {"\x1bOB", "Down"}, {"\x1bOC", "Right"}, {"\x1bOD", "Left"},
	{"\x1b[H", "Home"}, {"\x1b[F", "End"}, {"\x1b[5~", "PPage"}, {"\x1b[6~", "NPage"},
	{"\x1b[3~", "DC"}, {"\x1b[Z", "BTab"},
}

type keyPart struct {
	key  string // a tmux key name, or "" for text
	text []byte
}

// splitKeys cuts bytes into keys and the text between them. Esc on its own,
// enter, tab, backspace and control characters are keys; a newline is text.
func splitKeys(b []byte) []keyPart {
	var parts []keyPart
	var text []byte
	flush := func() {
		if len(text) > 0 {
			parts = append(parts, keyPart{text: text})
			text = nil
		}
	}
	for i := 0; i < len(b); {
		matched := false
		for _, k := range keySequences {
			if strings.HasPrefix(string(b[i:]), k.seq) {
				flush()
				parts = append(parts, keyPart{key: k.name})
				i += len(k.seq)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		switch ch := b[i]; {
		case ch == '\r':
			flush()
			parts = append(parts, keyPart{key: "Enter"})
		case ch == '\t':
			flush()
			parts = append(parts, keyPart{key: "Tab"})
		case ch == 0x7f:
			flush()
			parts = append(parts, keyPart{key: "BSpace"})
		case ch == 0x1b:
			flush()
			parts = append(parts, keyPart{key: "Escape"})
		case ch == 0x1f:
			flush()
			parts = append(parts, keyPart{key: "C-_"})
		case ch >= 0x01 && ch <= 0x1a && ch != '\n':
			flush()
			parts = append(parts, keyPart{key: "C-" + string(rune('a'+ch-1))})
		default:
			text = append(text, ch)
		}
		i++
	}
	flush()
	return parts
}

// Paste puts text into a pane as a paste, the way `wezterm cli send-text`
// does: bracketed, when the program asked for that.
func (c *Client) Paste(paneID, text string) error {
	return c.pasteBuffer(paneID, []byte(text), true)
}

// pasteBuffer writes b into a pane through a tmux buffer of its own, filled
// in chunks and pasted at once. -r keeps newlines as they are.
func (c *Client) pasteBuffer(paneID string, b []byte, bracketed bool) error {
	if len(b) == 0 {
		return nil
	}
	// What is typed reaches the program even in copy mode, but the pane
	// wouldn't show it, or anything after.
	if err := c.LeaveCopyMode(paneID); err != nil {
		return err
	}
	c.mu.Lock()
	c.shellN++
	buffer := "marmota-send-" + strconv.Itoa(c.shellN)
	c.mu.Unlock()
	for first := true; len(b) > 0; first = false {
		n := min(len(b), bufferChunk)
		// Keep a UTF-8 character whole.
		for n < len(b) && n > 0 && b[n]&0xC0 == 0x80 {
			n--
		}
		command := "set-buffer -b " + buffer + " "
		if !first {
			command = "set-buffer -a -b " + buffer + " "
		}
		if _, err := c.Command(command + bufferWord(b[:n])); err != nil {
			_, _ = c.Command("delete-buffer -b " + buffer)
			return err
		}
		b = b[n:]
	}
	flags := "-d -r"
	if bracketed {
		flags += " -p"
	}
	_, err := c.Command("paste-buffer " + flags + " -b " + buffer + " -t " + Quote(paneID))
	return err
}

// bufferWord writes bytes as a tmux double-quoted string: \, \" and \$
// escaped, control bytes as octal escapes, the rest as they are.
func bufferWord(b []byte) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, c := range b {
		switch {
		case c == '"' || c == '\\' || c == '$':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&sb, "\\%03o", c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// LeaveCopyMode brings a pane scrolled back (in copy mode) back to its
// program, so it shows what the program draws again.
func (c *Client) LeaveCopyMode(paneID string) error {
	_, err := c.Command("if-shell -F -t " + Quote(paneID) + " '#{pane_in_mode}' " +
		Quote("send-keys -t "+Quote(paneID)+" -X cancel"))
	return err
}

// historyLines is how much of a session's history the pane beside Marmota
// is given to scroll back through when it shows the session.
const historyLines = 10000

// AttachCommand is the shell command, for this computer's pane, that shows a
// window: a tmux client of a session of its own in the group of "marmota",
// so two panes can show two windows, which ends when the pane closes. view
// names it, one per Marmota.
//
// The pane's scrollback is the window's history first: it is cleared, and
// what scrolled off the window's top is printed into it, so the wheel
// scrolls back through this session's output alone, as in a pane of its own.
func AttachCommand(view, windowID string) string {
	t := Tmux()
	return `printf '\033[H\033[2J\033[3J'; ` +
		t + ` capture-pane -p -e -J -S -` + strconv.Itoa(historyLines) + ` -E -1 -t ` + shellQuote(windowID) +
		` 2>/dev/null; exec ` +
		t + " new-session -A -t " + TmuxSession + " -s " + shellQuote(view) +
		` \; set-option destroy-unattached on \; select-window -t ` + shellQuote(windowID)
}

// Show makes the view's pane show a window: its tmux client starts over,
// with that window's history in the pane's scrollback (see AttachCommand).
// Without a client, as when the pane is gone, the view just selects it.
func (c *Client) Show(view, windowID string) error {
	clients, err := c.Command("list-clients -t " + Quote(view) + " -F " + Quote("#{client_name}"))
	if err != nil || len(clients) == 0 {
		_, err = c.Command("select-window -t " + Quote(view+":"+windowID))
		return err
	}
	for _, client := range clients {
		if _, err := c.Command("detach-client -t " + Quote(client) + " -E " +
			Quote(Script(AttachCommand(view, windowID)))); err != nil {
			return err
		}
	}
	return nil
}

// ClientTTYs are the terminals of the tmux clients attached here: the
// panes that show a window, whose titles are their windows' own.
func (c *Client) ClientTTYs() ([]string, error) {
	lines, err := c.Command("list-clients -F " + Quote("#{client_tty}"))
	if err != nil {
		return nil, err
	}
	var ttys []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			ttys = append(ttys, l)
		}
	}
	return ttys, nil
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
