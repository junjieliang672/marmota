package sessionhost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

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

func TestQuoteKeepsSingleQuotesForTmux(t *testing.T) {
	assert.Equal(t, `'it'\''s'`, Quote("it's"))
}
