//go:build darwin

package ime

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// holdFile is where the hold under test publishes itself.
func holdFile(dir string) string {
	return filepath.Join(dir, strconv.Itoa(os.Getpid()))
}

// waitFor polls until cond holds, so the tests do not depend on how quickly the
// worker goroutine gets scheduled.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	h := New(true, dir)
	defer h.Close()

	file := holdFile(dir)
	assert.False(t, exists(file), "a fresh hold must not be published until asked")

	h.Set(true)
	waitFor(t, func() bool { return exists(file) }, "the hold file to appear")

	h.Set(false)
	waitFor(t, func() bool { return !exists(file) }, "the hold file to disappear")
}

func TestCloseReleasesSynchronously(t *testing.T) {
	dir := t.TempDir()
	h := New(true, dir)

	h.Set(true)
	waitFor(t, func() bool { return exists(holdFile(dir)) }, "the hold file to appear")

	h.Close()
	// No polling: Close is documented to block until the release landed. If
	// it did not, superfile would exit leaving the input method in ASCII mode.
	assert.False(t, exists(holdFile(dir)), "Close must release before returning")

	h.Close()
}

func TestSetAfterCloseIsIgnored(t *testing.T) {
	dir := t.TempDir()
	h := New(true, dir)
	h.Close()

	h.Set(true)
	time.Sleep(20 * time.Millisecond)
	assert.False(t, exists(holdFile(dir)), "a closed hold must never publish itself again")
}

func TestLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	h := New(true, dir)
	defer h.Close()

	h.Set(true)
	waitFor(t, func() bool { return exists(holdFile(dir)) }, "the hold file to appear")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the atomic rename must not leave the temp file behind")
	assert.Equal(t, strconv.Itoa(os.Getpid()), entries[0].Name())
}

func TestRepeatedSetIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	h := New(true, dir)
	defer h.Close()

	// Set runs on every Bubble Tea message, so the no-change path has to be
	// free and must not disturb what is already on disk.
	for range 50 {
		h.Set(true)
	}
	waitFor(t, func() bool { return exists(holdFile(dir)) }, "the hold file to appear")

	before, err := os.Stat(holdFile(dir))
	require.NoError(t, err)

	for range 50 {
		h.Set(true)
	}
	time.Sleep(20 * time.Millisecond)

	after, err := os.Stat(holdFile(dir))
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime(), "an unchanged Set must not rewrite the file")
}

func TestConstructionPrunesDeadHolds(t *testing.T) {
	dir := t.TempDir()

	// A plausible-looking hold from a process that is definitely gone. Without
	// pruning this would pin the input method to ASCII mode forever, with
	// nothing on screen to explain why.
	dead := filepath.Join(dir, "999999")
	require.NoError(t, os.WriteFile(dead, nil, holdFilePerm))

	// Entries we cannot interpret belong to somebody else and must survive.
	keep := []string{".DS_Store", "readme.txt", "0", "-1", " 12", "12 ", "01"}
	for _, name := range keep {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, holdFilePerm))
	}

	// A live holder must survive too: several clients can hold at once.
	live := filepath.Join(dir, strconv.Itoa(os.Getppid()))
	require.NoError(t, os.WriteFile(live, nil, holdFilePerm))

	h := New(true, dir)
	defer h.Close()

	assert.False(t, exists(dead), "a hold whose process exited must be pruned")
	assert.True(t, exists(live), "a live holder's hold must be left alone")
	for _, name := range keep {
		assert.True(t, exists(filepath.Join(dir, name)),
			"%s is not a PID, so it is not ours to delete", name)
	}
}

func TestParseHoldPID(t *testing.T) {
	for _, name := range []string{"1", "54321", "999999"} {
		pid, ok := parseHoldPID(name)
		assert.True(t, ok, "%q is a valid hold name", name)
		assert.Equal(t, name, strconv.Itoa(pid))
	}

	// Rejecting ".<pid>.tmp" is what makes the atomic write safe: a reader
	// must never treat a half-written temp file as a hold.
	for _, name := range []string{
		"", "0", "-1", "+1", "01", "54321.tmp", ".54321.tmp", ".DS_Store",
		" 54321", "54321 ", "0x10", "1e3", "12a", "１２３",
	} {
		_, ok := parseHoldPID(name)
		assert.False(t, ok, "%q must not be read as a hold", name)
	}
}

func TestProcessAlive(t *testing.T) {
	assert.True(t, processAlive(os.Getpid()), "this test process is running")
	// pid 1 is launchd and owned by root: kill(2) answers EPERM, which still
	// means the process exists.
	assert.True(t, processAlive(1), "EPERM means alive, not gone")

	assert.False(t, processAlive(0))
	assert.False(t, processAlive(-1))
	assert.False(t, processAlive(999999))
}

func TestUnusableDirectoryDegradesToNoop(t *testing.T) {
	// A missing input method must never keep superfile from starting.
	h := New(true, filepath.Join(t.TempDir(), "file-in-the-way", "sub"))
	require.NotNil(t, h)
	h.Set(true)
	h.Close()
}

func TestScopeBodyFollowsTermProgram(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WezTerm")
	assert.JSONEq(t, `{"apps":["com.github.wez.wezterm"]}`, string(scopeBody()))

	// An unknown terminal yields no scope, which the reader reads as
	// "any app" — see the comment on terminalBundleIDs.
	t.Setenv("TERM_PROGRAM", "something-else")
	assert.Nil(t, scopeBody())

	t.Setenv("TERM_PROGRAM", "")
	assert.Nil(t, scopeBody())
}

func TestHoldFileCarriesTheScope(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WezTerm")

	dir := t.TempDir()
	h := New(true, dir)
	defer h.Close()

	h.Set(true)
	waitFor(t, func() bool { return exists(holdFile(dir)) }, "the hold file to appear")

	body, err := os.ReadFile(holdFile(dir))
	require.NoError(t, err)
	assert.JSONEq(t, `{"apps":["com.github.wez.wezterm"]}`, string(body),
		"a background superfile must not disable Chinese input in other apps")
}
