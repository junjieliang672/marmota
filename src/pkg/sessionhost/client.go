// Package sessionhost runs the agent sessions Marmota starts in tmux, on this
// computer and on others.
//
// Every session is a window of the tmux session "marmota" on a tmux server
// of Marmota's own (`tmux -L marmota`, with its own config), so it keeps
// running when WezTerm, Marmota or the connection to another computer goes
// away. A pane beside Marmota only shows it, attached to its window.
//
// Each computer gets one connection: tmux in control mode (`tmux -C`), where
// tmux takes one command per line and answers each in a %begin … %end block
// instead of drawing a screen. On this computer it runs directly; on another
// it runs through a command that runs a program over there with its input
// and output here, such as `ssh host`. Through that one connection Marmota
// lists the sessions (`claude agents --json`, with tmux's run-shell), reads
// their screens (capture-pane), types into them (send-keys) and starts new
// ones (new-window).
package sessionhost

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Socket names Marmota's tmux server, kept apart from your own tmux. Tests
// use one of their own.
var Socket = socketName() //nolint:gochecknoglobals // test seam

// socketName is "marmota", or MARMOTA_TMUX_SOCKET when set: a Marmota under
// test keeps its sessions apart from yours.
func socketName() string {
	if name := os.Getenv("MARMOTA_TMUX_SOCKET"); name != "" {
		return name
	}
	return "marmota"
}

const (
	// TmuxSession is the tmux session that holds Marmota's windows.
	TmuxSession = "marmota"
	// configPath is where Marmota's tmux config is written, on each
	// computer, before its server starts.
	configPath = "$HOME/.config/marmota/tmux.conf"
)

// Config is Marmota's tmux config: tmux stays out of sight and out of the
// way, so a session looks and behaves in its pane as it would without it.
const Config = `# Written by Marmota for its own tmux server (tmux -L marmota); changes are overwritten.
set -g status off
set -g prefix None
set -g prefix2 None
unbind-key -q -a
unbind-key -q -a -T copy-mode
unbind-key -q -a -T copy-mode-vi
# The mouse is WezTerm's: dragging selects and copies, and the wheel scrolls
# WezTerm's own scrollback, as in a pane without tmux. The pane beside
# Marmota draws on WezTerm's main screen, so what scrolls off its top goes
# into that scrollback.
set -g mouse off
set -s terminal-overrides[90] "*:smcup@:rmcup@"
# No copy mode, ever: a pane in it shows no new output and takes no typing.
# Nothing enters it (no mouse, no prefix), and a pane that does anyway
# leaves it at once.
set -g pane-border-status off
set-hook -g pane-mode-changed 'if-shell -F "#{pane_in_mode}" "send-keys -X cancel"'
set -g history-limit 100000
set -sg escape-time 0
set -g focus-events on
set -g default-terminal "tmux-256color"
set -s terminal-features[90] "*:RGB:hyperlinks:clipboard:extkeys:usstyle:strikethrough:overline:sync:title"
set -g extended-keys always
set -g extended-keys-format csi-u
set -g allow-passthrough on
# Programs in it draw in true color, as they do in WezTerm.
set-environment -g COLORTERM truecolor
# The WezTerm the server was started from may be gone: without these,
# wezterm cli finds the one running now.
set-environment -gu WEZTERM_PANE
set-environment -gu WEZTERM_UNIX_SOCKET
set -g allow-rename off
# The pane beside Marmota, and its WezTerm tab, carry the session's own
# title, as without tmux.
set -g set-titles on
set -g set-titles-string "#{pane_title}"
set -g set-clipboard on
set -g window-size latest
set -g aggressive-resize on
set -g remain-on-exit off
set -g detach-on-destroy on
set -g visual-bell off
set -g bell-action none
# ctrl+G in a session opens its prompt in the editor (Claude Code, Codex),
# with the text and nothing about the cursor, and the session has redrawn its
# screen by the time the editor starts. So the cursor and the screen are kept
# as the key is pressed, and the key goes on to the session: the editor opens
# where the cursor was (y prompt-cursor, asked by scripts/micro/marmota/edit).
bind-key -n C-g set-option -pF ` + CursorOption + ` "#{cursor_x} #{cursor_y}" \; capture-pane -b ` + PromptBuffer + ` \; send-keys C-g
`

const (
	// CursorOption is the option of a window's pane that holds where its
	// cursor was, column and row from 0, when ctrl+G was last pressed in it.
	CursorOption = "@marmota-cursor"
	// PromptBuffer is the tmux buffer that holds the screen as it was then.
	PromptBuffer = "marmota-prompt"
	// binOption holds the y that connected last on this computer, for a
	// program in a window that has no PATH to find it by.
	binOption = "@marmota-bin"
)

// remotePath is put after PATH over there: a command ssh runs gets no login
// profile, so Homebrew's tmux and Claude Code's installer folder may be
// missing from it. What PATH has comes first.
const remotePath = `$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin`

// Host is one computer: this one (Local), or another one reached by SSH or
// Command.
type Host struct {
	// Local is this computer.
	Local bool `json:"-" toml:"-"`
	// Name is how Marmota shows it: "nova".
	Name string `json:"name" toml:"name"`
	// SSH is the ssh destination, user@host or a Host from ~/.ssh/config.
	// Marmota keeps one shared ssh connection to it (ControlMaster), so you
	// log in once, and opening a session in a pane doesn't log in again.
	SSH string `json:"ssh,omitempty" toml:"ssh"`
	// SSHArgs are ssh's options before the destination: ["-p", "2222"].
	SSHArgs []string `json:"ssh_args,omitempty" toml:"ssh_args"`
	// Command runs a command over there, its words followed by the command,
	// for a computer ssh alone doesn't reach: ["corp", "ssh", "gpu-1", "--"].
	Command []string `json:"command,omitempty" toml:"command"`
	// TTYCommand is Command with a terminal, for opening a session in a
	// pane: ["corp", "ssh", "-t", "gpu-1", "--"]. Command when empty.
	TTYCommand []string `json:"tty_command,omitempty" toml:"tty_command"`
	// Login logs in before Command works, in a pane you see, such as
	// ["corp", "login"]. For SSH it is an ssh that opens the shared
	// connection, asking whatever ssh asks.
	Login []string `json:"login,omitempty" toml:"login"`
}

// Valid reports whether the host says how to reach it.
func (h Host) Valid() bool {
	return h.Local || h.Name != "" && (h.SSH != "" || len(h.Command) > 0)
}

// Tmux is the tmux command for Marmota's server, for a shell over there.
// The socket is MARMOTA_TMUX_SOCKET's over there when that is set, which
// lets a test stand in for another computer with a tmux server of its own.
func Tmux() string {
	return `tmux -L "${MARMOTA_TMUX_SOCKET:-` + Socket + `}" -f "` + configPath + `"`
}

// sshShared are the options that share one ssh connection per destination.
// %C is a hash of the destination, which keeps the socket path short.
func sshShared() []string {
	return []string{"-o", "ControlMaster=auto", "-o", "ControlPath=~/.ssh/marmota-%C",
		"-o", "ControlPersist=600", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3"}
}

// Argv is the command that runs script over there, with its input and output
// here and no terminal: a background connection, so ssh asks nothing and
// fails instead when it needs you to log in.
func (h Host) Argv(script string) []string {
	if h.Local {
		return []string{"/bin/sh", "-c", script}
	}
	if h.SSH != "" {
		args := append([]string{"ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, sshShared()...)
		return append(append(args, h.SSHArgs...), h.SSH, script)
	}
	return append(append([]string(nil), h.Command...), script)
}

// TTYArgv is the command that runs script over there in a terminal, for a
// pane.
func (h Host) TTYArgv(script string) []string {
	if h.Local {
		return []string{"/bin/sh", "-c", script}
	}
	if h.SSH != "" {
		return append(append(append([]string{"ssh", "-t"}, sshShared()...), h.SSHArgs...), h.SSH, script)
	}
	words := h.TTYCommand
	if len(words) == 0 {
		words = h.Command
	}
	return append(append([]string(nil), words...), script)
}

// LoginArgv is what logs in, in a pane you see: for ssh, one that opens the
// shared connection and stays open a moment so you see it worked.
func (h Host) LoginArgv() []string {
	if len(h.Login) > 0 {
		return append([]string(nil), h.Login...)
	}
	if h.SSH != "" {
		return h.TTYArgv("echo; echo \"Connected to $(hostname). This pane closes itself.\"; sleep 2")
	}
	return nil
}

// Script wraps a shell command so it survives every layer of quoting on the
// way: it travels base64-encoded and runs in the shell over there, with
// Homebrew's and Claude Code's folders on PATH after what PATH has.
func Script(command string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(
		"export PATH=\"$PATH:" + remotePath + "\"\n" + command))
	return `eval "$(printf %s ` + encoded + ` | base64 --decode)"`
}

// configOption holds which config the server last took.
const configOption = "@marmota-config"

// reattachOnNewConfig attaches the panes that show a window anew when the
// server takes a config it hadn't: a tmux client reads part of it, such as
// whether to draw on WezTerm's main screen, only as it attaches. Each pane
// stays where it is, showing the same window; only its tmux client starts
// over.
func (c *Client) reattachOnNewConfig() {
	sum := sha256.Sum256([]byte(Config))
	version := hex.EncodeToString(sum[:8])
	lines, err := c.Command("show-options -gqv " + configOption)
	if err != nil || strings.TrimSpace(strings.Join(lines, "")) == version {
		return
	}
	if _, err := c.Command("set-option -g " + configOption + " " + version); err != nil {
		return
	}
	clients, err := c.Command("list-clients -F " + Quote("#{client_name}\t#{session_name}\t#{window_id}"))
	if err != nil {
		return
	}
	for _, line := range clients {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 || !strings.HasPrefix(f[1], "view-") {
			continue
		}
		_, _ = c.Command("detach-client -t " + Quote(f[0]) + " -E " +
			Quote(Script(AttachCommand(f[1], f[2]))))
	}
}

// guiServer starts the tmux server on a Mac reached over ssh in the session of
// the person logged in at its screen, through launchd, when it isn't
// running. A program started over ssh can't open the login keychain, where
// Claude Code keeps its sign-in: in a server started from ssh it would say
// "Not logged in". Started this way, the sessions in it are signed in as
// one opened at that Mac is. Elsewhere, or with nobody logged in there, the
// server starts as the control client starts it.
const guiServer = `sock="${MARMOTA_TMUX_SOCKET:-` + "%SOCKET%" + `}"
if [ -n "$SSH_CONNECTION" ] && ! tmux -L "$sock" has-session -t ` + TmuxSession + ` 2>/dev/null &&
	[ "$(uname)" = Darwin ] &&
	launchctl print "gui/$(id -u)" >/dev/null 2>&1; then
	label="com.marmota.tmux.$sock"
	plist="$HOME/.config/marmota/$label.plist"
	bin=$(command -v tmux)
	{
		printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><dict>\n'
		printf '<key>Label</key><string>%s</string>\n' "$label"
		printf '<key>ProgramArguments</key><array>'
		for a in "$bin" -L "$sock" -f "$HOME/.config/marmota/tmux.conf" new-session -d -s ` + TmuxSession + ` -n _ 'exec cat'; do
			printf '<string>%s</string>' "$a"
		done
		printf '</array>\n<key>EnvironmentVariables</key><dict>'
		printf '<key>PATH</key><string>%s</string>' "$PATH"
		printf '<key>LANG</key><string>%s</string>' "${LANG:-en_US.UTF-8}"
		printf '</dict>\n<key>RunAtLoad</key><true/>\n</dict></plist>\n'
	} > "$plist"
	launchctl bootout "gui/$(id -u)/$label" 2>/dev/null
	launchctl bootstrap "gui/$(id -u)" "$plist"
	i=0
	while ! tmux -L "$sock" has-session -t ` + TmuxSession + ` 2>/dev/null && [ $i -lt 50 ]; do
		sleep 0.1
		i=$((i + 1))
	done
fi
`

// ErrClosed is returned once the connection has ended.
var ErrClosed = errors.New("the connection has ended")

// Client is a connection to one computer's tmux in control mode.
type Client struct {
	host  Host
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex // one command written, and queued for its answer, at a time

	mu      sync.Mutex
	replies []chan reply // answers awaited, oldest first
	shells  map[string]*shellRun
	closed  bool
	err     error
	done    chan struct{}
	shellN  int
}

type reply struct {
	lines []string
	err   error
}

type shellRun struct {
	lines []string
	in    bool
	done  chan struct{}
	code  int
}

// Dial starts the connection and waits until tmux answers. What the command
// printed before tmux started, such as a login prompt or ssh's error, comes
// back in the error.
func Dial(ctx context.Context, h Host) (*Client, error) {
	// The config is written first, so a server that starts reads it; the
	// first window is a shell that waits, as a session can't be empty.
	start := `mkdir -p "$HOME/.config/marmota" && cat > "` + configPath + `" <<'MARMOTA_TMUX_CONF'` + "\n" +
		Config + "MARMOTA_TMUX_CONF\n"
	if !h.Local {
		start += strings.ReplaceAll(guiServer, "%SOCKET%", Socket)
	}
	start += "exec " + Tmux() + " -u -C new-session -A -s " + TmuxSession + " -n _ -x 200 -y 50 'exec cat'"
	argv := h.Argv(Script(start))
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // the command config.toml gives
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, left: 4096}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{host: h, cmd: cmd, stdin: stdin, shells: map[string]*shellRun{}, done: make(chan struct{})}
	// tmux answers the command that started it before anything else.
	first := make(chan reply, 1)
	c.replies = append(c.replies, first)
	ready := make(chan []string, 1)
	go c.read(stdout, ready)

	select {
	case r := <-first:
		if r.err != nil {
			before := <-ready
			_ = cmd.Wait()
			return nil, dialError(before, stderr.String())
		}
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	case <-c.done:
		before := <-ready
		_ = cmd.Wait()
		return nil, dialError(before, stderr.String())
	}
	// Each window's output would stream in otherwise.
	if _, err := c.Command("refresh-client -f no-output"); err != nil {
		c.Close()
		return nil, err
	}
	// A server that was already running takes the config as written now,
	// through a tmux client of its own: sourced here, each of its commands
	// would answer this connection, out of turn.
	if _, err := c.Shell(Tmux()+` source-file -q "`+configPath+`"`, commandTimeout); err != nil {
		c.Close()
		return nil, err
	}
	c.reattachOnNewConfig()
	// The editor a session opens asks this y where the prompt's cursor was.
	// Only y itself is named, not a test of it or another program.
	if exe, err := os.Executable(); err == nil && h.Local && filepath.Base(exe) == "y" {
		_, _ = c.Command("set-option -g " + binOption + " " + Quote(exe))
	}
	return c, nil
}

func dialError(stdout []string, stderr string) error {
	text := strings.TrimSpace(strings.Join(append(stdout, stderr), "\n"))
	if text == "" {
		text = "the connection closed before tmux started"
	}
	return &DialError{Output: text}
}

// DialError is a connection that ended before tmux answered, with what it
// printed: ssh's "Permission denied", a login prompt, "tmux: not found".
type DialError struct{ Output string }

func (e *DialError) Error() string { return e.Output }

// NeedsLogin reports an error that logging in may fix.
func NeedsLogin(err error) bool {
	var d *DialError
	if !errors.As(err, &d) {
		return false
	}
	text := strings.ToLower(d.Output)
	for _, s := range []string{"permission denied", "authentication", "log in", "login", "sign in",
		"expired", "password", "passphrase", "host key verification failed"} {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// read sorts tmux's lines into answers, notifications and run-shell output
// until the connection ends. ready gets what came before tmux started.
func (c *Client) read(r io.Reader, ready chan<- []string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var before []string
	started := false
	var block []string
	var guard string // "<time> <number>" of the open %begin
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if guard != "" {
			if end, ok := blockEnd(line, guard); ok {
				c.answer(block, end)
				block, guard = nil, ""
				continue
			}
			block = append(block, line)
			continue
		}
		if rest, ok := strings.CutPrefix(line, "%begin "); ok {
			started = true
			guard = guardOf(rest)
			continue
		}
		if !started {
			before = append(before, line)
			continue
		}
		if strings.HasPrefix(line, "%") {
			continue // a notification: %window-renamed, %sessions-changed, …
		}
		c.shellLine(line)
	}
	if !started {
		ready <- before
	} else {
		ready <- nil
	}
	c.finish(scanner.Err())
}

func guardOf(rest string) string {
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return rest
	}
	return fields[0] + " " + fields[1]
}

// blockEnd reports a line that closes the block opened with guard, and
// whether it failed.
func blockEnd(line, guard string) (failed bool, ok bool) {
	for _, word := range []string{"%end ", "%error "} {
		if rest, found := strings.CutPrefix(line, word); found && guardOf(rest) == guard {
			return word == "%error ", true
		}
	}
	return false, false
}

func (c *Client) answer(lines []string, failed bool) {
	c.mu.Lock()
	if len(c.replies) == 0 {
		c.mu.Unlock()
		return // an answer nobody waits for, such as one to a detach
	}
	ch := c.replies[0]
	c.replies = c.replies[1:]
	c.mu.Unlock()
	var err error
	if failed {
		err = fmt.Errorf("tmux: %s", strings.Join(lines, " "))
	}
	ch <- reply{lines: lines, err: err}
}

const shellMark = "@@marmota-shell "

func (c *Client) shellLine(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if rest, ok := strings.CutPrefix(line, shellMark); ok {
		fields := strings.Fields(rest)
		if len(fields) >= 2 {
			if run, ok := c.shells[fields[0]]; ok {
				switch fields[1] {
				case "begin":
					run.in = true
				case "end":
					if len(fields) >= 3 {
						run.code, _ = strconv.Atoi(fields[2])
					}
					run.in = false
					delete(c.shells, fields[0])
					close(run.done)
				}
			}
		}
		return
	}
	for _, run := range c.shells {
		if run.in {
			run.lines = append(run.lines, line)
			return
		}
	}
}

func (c *Client) finish(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if err == nil {
		err = ErrClosed
	}
	c.err = err
	for _, ch := range c.replies {
		ch <- reply{err: ErrClosed}
	}
	c.replies = nil
	for _, run := range c.shells {
		close(run.done)
	}
	c.shells = map[string]*shellRun{}
	close(c.done)
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is why the connection ended, once it has.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Host is the computer it reaches.
func (c *Client) Host() Host { return c.host }

// Close ends the connection. The tmux session over there, and the sessions
// in it, keep running.
func (c *Client) Close() {
	c.writeMu.Lock()
	_ = c.stdin.Close() // tmux detaches a control client whose input ends
	c.writeMu.Unlock()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

// commandTimeout is how long an answer may take before the connection
// counts as dead.
const commandTimeout = 20 * time.Second

// Command runs one tmux command and returns what it printed. It must be a
// single line: an empty line would detach.
func (c *Client) Command(command string) ([]string, error) {
	if strings.ContainsAny(command, "\n\r") || strings.TrimSpace(command) == "" {
		return nil, errors.New("a tmux command must be one line")
	}
	ch := make(chan reply, 1)
	c.writeMu.Lock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.writeMu.Unlock()
		return nil, ErrClosed
	}
	c.replies = append(c.replies, ch)
	c.mu.Unlock()
	_, err := io.WriteString(c.stdin, command+"\n")
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.lines, r.err
	case <-time.After(commandTimeout):
		return nil, fmt.Errorf("tmux didn't answer %q in %s", firstWord(command), commandTimeout)
	}
}

func firstWord(s string) string {
	word, _, _ := strings.Cut(s, " ")
	return word
}

// Shell runs a shell command over there through tmux's run-shell and returns
// its output. It errs when the command exits with another status than 0.
func (c *Client) Shell(command string, timeout time.Duration) (string, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", ErrClosed
	}
	c.shellN++
	id := strconv.Itoa(c.shellN)
	run := &shellRun{done: make(chan struct{})}
	c.shells[id] = run
	c.mu.Unlock()
	script := "printf '\\n" + shellMark + id + " begin\\n'; ( " + command + "\n) 2>&1; " +
		"printf '\\n" + shellMark + id + " end %s\\n' \"$?\""
	if _, err := c.Command("run-shell " + Quote(Script(script))); err != nil {
		c.dropShell(id)
		return "", err
	}
	select {
	case <-run.done:
	case <-time.After(timeout):
		c.dropShell(id)
		return "", fmt.Errorf("%q took longer than %s over there", firstWord(command), timeout)
	}
	if c.Err() != nil && run.code == 0 && len(run.lines) == 0 {
		return "", ErrClosed
	}
	c.mu.Lock()
	lines := run.lines
	c.mu.Unlock()
	// The leading newline keeps the mark on a line of its own; drop the
	// empty line it leaves.
	out := strings.TrimSuffix(strings.TrimPrefix(strings.Join(lines, "\n"), "\n"), "\n")
	if run.code != 0 {
		return out, fmt.Errorf("%q exited with %d: %s", firstWord(command), run.code, lastLine(out))
	}
	return out, nil
}

func (c *Client) dropShell(id string) {
	c.mu.Lock()
	delete(c.shells, id)
	c.mu.Unlock()
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Quote quotes a word for a tmux command line: in single quotes, which tmux
// takes literally; a single quote in s closes the quotes, is escaped with
// a backslash, and opens them again.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if l.left > 0 {
		take := min(len(p), l.left)
		_, _ = l.w.Write(p[:take])
		l.left -= take
	}
	return n, nil
}
