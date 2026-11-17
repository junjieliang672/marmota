package sessionhost

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOnMakesSSHAHostOfItsOwn(t *testing.T) {
	h, err := ParseOn("ssh me@192.168.1.20")
	require.NoError(t, err)
	assert.Equal(t, Host{Name: "192.168.1.20", SSH: "me@192.168.1.20"}, h)

	h, err = ParseOn("ssh -p 2222 -J jump.corp me@gpu-1")
	require.NoError(t, err)
	assert.Equal(t, "me@gpu-1", h.SSH)
	assert.Equal(t, []string{"-p", "2222", "-J", "jump.corp"}, h.SSHArgs)

	h, err = ParseOn(`corp ssh "gpu 1" --`)
	require.NoError(t, err)
	assert.Equal(t, []string{"corp", "ssh", "gpu 1", "--"}, h.Command)
	assert.Empty(t, h.SSH)

	_, err = ParseOn("ssh host ls")
	require.Error(t, err, "a command to run there isn't how to get there")
	_, err = ParseOn("")
	require.Error(t, err)
}

// asRemote reaches this computer the way a remote one is reached: through a
// command its scripts follow.
func asRemote(t *testing.T) *Client {
	t.Helper()
	localClient(t) // its own tmux server and home
	c, err := Dial(context.Background(), Host{Name: "here", Command: []string{"/bin/sh", "-c"}})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func TestAFileIsReadOverThereFromWhereItWasLeft(t *testing.T) {
	c := asRemote(t)
	file := t.TempDir() + "/t.jsonl"
	require.NoError(t, os.WriteFile(file, []byte("one\n"), 0o600))

	data, size, err := c.ReadFrom(file, 0)
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(data))
	assert.EqualValues(t, 4, size)

	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, _ = f.WriteString("two 中\n")
	_ = f.Close()
	data, size, err = c.ReadFrom(file, 4)
	require.NoError(t, err)
	assert.Equal(t, "two 中\n", string(data))
	assert.EqualValues(t, 12, size)

	require.NoError(t, os.WriteFile(file, []byte("new\n"), 0o600))
	data, _, err = c.ReadFrom(file, 12)
	require.ErrorIs(t, err, ErrShrank)
	assert.Equal(t, "new\n", string(data))
}

func TestATranscriptIsFoundOverThere(t *testing.T) {
	c := asRemote(t)
	dir := os.Getenv("HOME") + "/.claude/projects/-tmp-proj"
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(dir+"/abc.jsonl", []byte("{}\n"), 0o600))
	got, err := c.TranscriptPath("abc")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(got, "/-tmp-proj/abc.jsonl"), got)
	got, err = c.TranscriptPath("none")
	require.NoError(t, err)
	assert.Empty(t, got)
}
