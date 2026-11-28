package sessionhost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// localClient connects to a tmux server of the test's own, with a home of
// its own, and shuts the server down after.
func localClient(t *testing.T) *Client {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux isn't installed")
	}
	original := Socket
	Socket = fmt.Sprintf("marmota-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	home := t.TempDir()
	t.Setenv("HOME", home)
	socket := Socket
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		_ = os.Remove(tmuxSocketPath(socket))
		Socket = original
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, Host{Local: true})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTheConnectionWritesTheConfigAndAnswersCommands(t *testing.T) {
	c := localClient(t)
	conf, err := os.ReadFile(os.Getenv("HOME") + "/.config/marmota/tmux.conf")
	require.NoError(t, err)
	assert.Equal(t, Config, string(conf))

	lines, err := c.Command("show-options -g status")
	require.NoError(t, err)
	assert.Equal(t, []string{"status off"}, lines, "the server runs on Marmota's config")

	_, err = c.Command("no-such-command")
	require.Error(t, err)
	lines, err = c.Command("display-message -p hello")
	require.NoError(t, err, "a failed command leaves the next one its own answer")
	assert.Equal(t, []string{"hello"}, lines)
}

func TestARunningServerTakesTheConfigAgainOnEachConnection(t *testing.T) {
	c := localClient(t)
	_, err := c.Command("set-option -g set-titles off")
	require.NoError(t, err)
	c.Close()

	again, err := Dial(context.Background(), Host{Local: true})
	require.NoError(t, err)
	defer again.Close()
	lines, err := again.Command("show-options -g set-titles")
	require.NoError(t, err)
	assert.Equal(t, []string{"set-titles on"}, lines)
	features, err := again.Command("show-options -s terminal-features")
	require.NoError(t, err)
	n := 0
	for _, f := range features {
		if strings.Contains(f, ":title") && strings.HasPrefix(f, "terminal-features[90]") {
			n++
		}
	}
	assert.Equal(t, 1, n, "loading it again adds nothing twice: %v", features)
}

// unreadClient runs a tmux control client on the test's server whose output
// nobody reads, gives the server more to say to it than its pipes hold, and
// then closes both ends: a Marmota that died with output still to come.
func unreadClient(t *testing.T, c *Client, script string) {
	t.Helper()
	inR, inW, err := os.Pipe()
	require.NoError(t, err)
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	cmd := exec.Command("/bin/sh", "-c", Script(script))
	cmd.Stdin, cmd.Stdout = inR, outW
	require.NoError(t, cmd.Start())
	_, _ = inR.Close(), outW.Close()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitFor(t, "the second connection", func() bool { return controlClients(t, c) == 2 })

	windows, err := c.Command("list-windows -F '#{window_id}'")
	require.NoError(t, err)
	require.NotEmpty(t, windows)
	for i := range 400 { // each renaming is a line of 1 KB to every control client
		_, err := c.Command("rename-window -t " + windows[0] + " " + strings.Repeat("x", 1000) + strconv.Itoa(i))
		require.NoError(t, err)
	}
	_, _ = inW.Close(), outR.Close()
}

// controlClients is how many control clients the server holds.
func controlClients(t *testing.T, c *Client) int {
	t.Helper()
	lines, err := c.Command("list-clients -F '#{client_control_mode}'")
	require.NoError(t, err)
	n := 0
	for _, line := range lines {
		if line == "1" {
			n++
		}
	}
	return n
}

func TestTheServerLetsGoOfAConnectionWhoseReaderDied(t *testing.T) {
	c := localClient(t)
	unreadClient(t, c, controlClient())
	waitFor(t, "the server to let go of it", func() bool { return controlClients(t, c) == 1 })
}

func TestAServerThatCannotExitIsEndedAndTheConnectionStartsAgain(t *testing.T) {
	c := localClient(t)
	lines, err := c.Command("display-message -p '#{pid}'")
	require.NoError(t, err)
	require.Len(t, lines, 1)
	server, err := strconv.Atoi(lines[0])
	require.NoError(t, err)
	// A connection as Marmota ran it before: tmux alone, which the server
	// never lets go of once its output can't be written.
	unreadClient(t, c, "exec "+Tmux()+" -u -C attach-session -t "+TmuxSession)
	c.Close()
	_ = exec.Command("tmux", "-L", Socket, "kill-server").Run()
	said, _ := exec.Command("tmux", "-L", Socket, "has-session").CombinedOutput()
	if !strings.Contains(string(said), stuckServer) {
		t.Skip("this tmux lets go of a client whose output can't be written: " + string(said))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	again, err := Dial(ctx, Host{Local: true})
	require.NoError(t, err, "the server that couldn't exit is ended, and a new one starts")
	defer again.Close()
	lines, err = again.Command("display-message -p '#{pid}'")
	require.NoError(t, err)
	assert.NotEqual(t, []string{strconv.Itoa(server)}, lines, "a new server")
	waitFor(t, "the old server to be gone", func() bool { return syscall.Kill(server, 0) != nil })
}

func TestShellRunsOverThereAndReturnsItsOutput(t *testing.T) {
	c := localClient(t)
	out, err := c.Shell(`printf 'one\ntwo'; echo " it's $((1+2))"`, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "one\ntwo it's 3", out)

	out, err = c.Shell("echo sorry >&2; exit 3", 5*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited with 3")
	assert.Equal(t, "sorry", out)
}

func TestAWindowIsStartedReadAndTypedInto(t *testing.T) {
	c := localClient(t)
	dir := t.TempDir()
	// A stand-in for an agent: sets its title, reads a line, answers it.
	w, err := c.Start(NameFor(ClaudePrefix, "abc"), dir,
		`printf '\033]2;✳ eval sweep\007'; echo "ready in $(pwd)"; read l; echo "got: $l"; sleep 30`)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(w.PaneID, "%"))

	waitFor(t, "the window to draw", func() bool {
		screen, _ := c.Capture(w.PaneID)
		return strings.Contains(screen, "ready in")
	})
	windows, err := c.Windows()
	require.NoError(t, err)
	require.Len(t, windows, 1, "the first window holds no session")
	assert.Equal(t, "cc-abc", windows[0].Name)
	assert.Equal(t, "✳ eval sweep", windows[0].Title)
	assert.True(t, strings.HasPrefix(windows[0].TTY, "/dev/"), windows[0].TTY)
	assert.Equal(t, w.TTY, windows[0].TTY)
	prefix, id, ok := SessionOf(windows[0].Name)
	assert.True(t, ok)
	assert.Equal(t, ClaudePrefix, prefix)
	assert.Equal(t, "abc", id)

	require.NoError(t, c.Paste(w.PaneID, "hello 'there'"))
	require.NoError(t, c.SendBytes(w.PaneID, []byte("\r")))
	waitFor(t, "the answer", func() bool {
		screen, _ := c.Capture(w.PaneID)
		return strings.Contains(screen, "got: hello 'there'")
	})

	require.NoError(t, c.CloseWindow(w.ID))
	windows, err = c.Windows()
	require.NoError(t, err)
	assert.Empty(t, windows)
}

func TestAWindowSurvivesItsConnection(t *testing.T) {
	c := localClient(t)
	_, err := c.Start(NameFor(CodexPrefix, "x1"), t.TempDir(), "sleep 30")
	require.NoError(t, err)
	c.Close()
	<-c.Done()

	again, err := Dial(context.Background(), Host{Local: true})
	require.NoError(t, err)
	defer again.Close()
	windows, err := again.Windows()
	require.NoError(t, err)
	require.Len(t, windows, 1)
	assert.Equal(t, "co-x1", windows[0].Name)
}

func TestADialThatFailsSaysWhatWasPrinted(t *testing.T) {
	_, err := Dial(context.Background(), Host{Name: "nowhere", Command: []string{"/bin/sh", "-c",
		"echo 'Permission denied (publickey).' >&2; exit 255", "--"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Permission denied")
	assert.True(t, NeedsLogin(err))
}

func TestAFolderUnderHomeIsThatComputersHome(t *testing.T) {
	c := localClient(t)
	home := os.Getenv("HOME")
	require.NoError(t, os.Mkdir(home+"/proj", 0o755))
	w, err := c.Start(NameFor(ClaudePrefix, "h"), "~/proj", "pwd; sleep 30")
	require.NoError(t, err)
	waitFor(t, "pwd", func() bool {
		screen, _ := c.Capture(w.PaneID)
		return strings.Contains(screen, "/proj")
	})
}

func TestBytesReachTheProgramAsTheyAre(t *testing.T) {
	c := localClient(t)
	out := t.TempDir() + "/got"
	// raw mode, so the terminal hands over every byte unchanged
	w, err := c.Start(NameFor(ClaudePrefix, "raw"), t.TempDir(),
		"stty raw -echo; head -c 32 > "+out+"; sleep 30")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	sent := "a\nb \"$HOME\" '~' \\ \x1b[A\x01中\r#{x}!!"
	require.NoError(t, c.SendBytes(w.PaneID, []byte(sent)))
	waitFor(t, "the bytes", func() bool {
		got, _ := os.ReadFile(out)
		return len(got) >= 32
	})
	got, _ := os.ReadFile(out)
	assert.Equal(t, sent[:32], string(got))
}

func TestTextIsTypedNotPasted(t *testing.T) {
	c := localClient(t)
	out := t.TempDir() + "/got"
	// It asks for bracketed paste, as Claude Code does, and reads raw.
	w, err := c.Start(NameFor(ClaudePrefix, "typed"), t.TempDir(),
		`printf '\033[?2004h'; stty raw -echo; head -c 11 > `+out+"; sleep 30")
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, c.Paste(w.PaneID, "do it\nnow!!"))
	waitFor(t, "the text", func() bool {
		got, _ := os.ReadFile(out)
		return len(got) >= 11
	})
	got, _ := os.ReadFile(out)
	// Claude Code would wrap a paste in <pasted_content>, which Claude
	// doesn't follow as the person's own words.
	assert.Equal(t, "do it\nnow!!", string(got))
}

func TestAPaneNeverStaysInCopyMode(t *testing.T) {
	c := localClient(t)
	w, err := c.Start(NameFor(ClaudePrefix, "cm"), t.TempDir(),
		`for i in $(seq 1 100); do echo "row $i"; done; read l; echo "got: $l"; sleep 30`)
	require.NoError(t, err)
	waitFor(t, "the rows", func() bool {
		screen, _ := c.Capture(w.PaneID)
		return strings.Contains(screen, "row 100")
	})
	inMode := func() bool {
		lines, err := c.Command("display-message -p -t " + Quote(w.PaneID) + " '#{pane_in_mode}'")
		require.NoError(t, err)
		return strings.Join(lines, "") == "1"
	}
	_, err = c.Command("copy-mode -t " + Quote(w.PaneID))
	require.NoError(t, err)
	waitFor(t, "the pane out of copy mode", func() bool { return !inMode() })

	require.NoError(t, c.SendBytes(w.PaneID, []byte("hello\r")))
	waitFor(t, "the answer", func() bool {
		screen, _ := c.Capture(w.PaneID)
		return strings.Contains(screen, "got: hello")
	})
}

// ctrl+G pressed in a session's window keeps where the cursor is and what
// the screen shows, then reaches the session as it always did: the editor
// the session opens asks for them (y prompt-cursor).
func TestCtrlGKeepsTheCursorAndTheScreenForTheEditor(t *testing.T) {
	c := localClient(t)
	out := t.TempDir() + "/got"
	// A prompt of two rows, the cursor left four columns into the second,
	// and whatever is typed next written to a file.
	w, err := c.Start(NameFor(ClaudePrefix, "ctrlg"), t.TempDir(),
		`printf '> first row\n  second row\r\033[4C'; stty raw -echo; head -c 1 > `+out+"; sleep 30")
	require.NoError(t, err)
	waitFor(t, "the prompt", func() bool {
		screen, _ := c.Capture(w.PaneID)
		return strings.Contains(screen, "second row")
	})
	time.Sleep(300 * time.Millisecond) // the cursor moved, and raw mode is on

	// The key as a client presses it: looked up in tmux's keys, not handed
	// to the window as send-keys otherwise does.
	_, err = c.Command("select-window -t " + Quote(w.PaneID))
	require.NoError(t, err)
	clients, err := c.Command("list-clients -F '#{client_name}'")
	require.NoError(t, err)
	require.NotEmpty(t, clients)
	_, err = c.Command("send-keys -K -c " + Quote(clients[0]) + " C-g")
	require.NoError(t, err)

	waitFor(t, "the key in the program", func() bool {
		got, _ := os.ReadFile(out)
		return len(got) == 1
	})
	got, _ := os.ReadFile(out)
	assert.Equal(t, "\a", string(got), "ctrl+G still reaches the session")
	at, err := c.Command("show-options -pqv -t " + Quote(w.PaneID) + " " + CursorOption)
	require.NoError(t, err)
	assert.Equal(t, []string{"4 1"}, at, "the cursor's column and row, from 0")
	screen, err := c.Command("show-buffer -b " + PromptBuffer)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(screen), 2)
	assert.Equal(t, []string{"> first row", "  second row"}, screen[:2], "the screen as it was")
}

func TestKeysAreSentByTheirNames(t *testing.T) {
	assert.Equal(t, []keyPart{{text: []byte("hi")}, {key: "Down"}, {key: "Enter"}, {key: "C-s"},
		{text: []byte("a\nb")}, {key: "Escape"}, {text: []byte("x")}},
		splitKeys([]byte("hi\x1b[B\r\x13a\nb\x1bx")))
}

func TestQuoteKeepsSingleQuotesForTmux(t *testing.T) {
	assert.Equal(t, `'it'\''s'`, Quote("it's"))
}

// tmuxSocketPath is where tmux keeps a server's socket, which a server
// killed from outside can leave behind.
func tmuxSocketPath(name string) string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return fmt.Sprintf("%s/tmux-%d/%s", dir, os.Getuid(), name)
}

// A long text is typed in pieces small enough that Claude Code doesn't take
// it for a paste, with no character cut in two.
func TestTypedPiecesAreSmallAndWhole(t *testing.T) {
	text := strings.Repeat("修复登录流程 fix the login flow\n", 200)
	pieces := TypedPieces(text)
	if strings.Join(pieces, "") != text {
		t.Fatal("the pieces don't make up the text")
	}
	for _, p := range pieces {
		if len(p) > typedPiece || !utf8.ValidString(p) {
			t.Fatalf("a piece of %d bytes, valid UTF-8: %v", len(p), utf8.ValidString(p))
		}
	}
	if len(TypedPieces("")) != 0 || len(TypedPieces("short")) != 1 {
		t.Fatal("a short text is one piece, none for no text")
	}
}
