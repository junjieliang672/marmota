package claudesession

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	firstTurn = `{"type":"user","cwd":"/repo","sessionId":"s1","timestamp":"2026-11-22T10:00:00Z",` +
		`"message":{"role":"user","content":"group the logos"}}` + "\n"
	secondTurn = `{"type":"user","cwd":"/repo","sessionId":"s1","timestamp":"2026-11-22T10:05:00Z",` +
		`"message":{"role":"user","content":"and the icons"}}` + "\n"
)

// A tracker kept between runs reads only what was written to a transcript
// since, and says of it what a tracker that read it whole says.
func TestATrackerKeptBetweenRunsReadsOnlyWhatIsNew(t *testing.T) {
	dir := t.TempDir()
	transcript, kept := filepath.Join(dir, "s1.jsonl"), filepath.Join(dir, "cache", "summaries.json")
	require.NoError(t, os.WriteFile(transcript, []byte(firstTurn), 0o600))

	first := NewTracker()
	sum, err := first.Summary(transcript)
	require.NoError(t, err)
	require.Equal(t, "group the logos", sum.LastPrompt)
	wrote, err := first.Save(kept, "build 1", time.Minute)
	require.NoError(t, err)
	assert.True(t, wrote, "the first time, at once")
	wrote, err = first.Save(kept, "build 1", 0)
	require.NoError(t, err)
	assert.False(t, wrote, "nothing read since: nothing to write")

	// The next run: nothing was written to the transcript meanwhile.
	next := NewTracker()
	require.True(t, next.Load(kept, "build 1"))
	require.NoError(t, os.Chmod(transcript, 0o000)) // it can't be read: it isn't
	again, err := next.Summary(transcript)
	require.NoError(t, err)
	assert.Equal(t, sum, again, "the summary kept, the transcript unread")
	require.NoError(t, os.Chmod(transcript, 0o600))

	// A turn more: read on from where the summary ends.
	f, err := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(secondTurn)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	more, err := next.Summary(transcript)
	require.NoError(t, err)
	whole, err := NewTracker().Summary(transcript)
	require.NoError(t, err)
	assert.Equal(t, "and the icons", more.LastPrompt)
	assert.Equal(t, whole, more, "as one that read it whole")
	assert.Equal(t, int64(len(firstTurn)+len(secondTurn)), next.files[transcript].offset)

	wrote, err = next.Save(kept, "build 1", time.Hour)
	require.NoError(t, err)
	assert.True(t, wrote, "this run's first Save, whatever the interval")
	_, err = next.Summary(transcript)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(transcript, []byte(firstTurn+secondTurn+firstTurn), 0o600))
	_, err = next.Summary(transcript)
	require.NoError(t, err)
	wrote, err = next.Save(kept, "build 1", time.Hour)
	require.NoError(t, err)
	assert.False(t, wrote, "read again within the hour: not written again yet")
}

// Another build may read a transcript differently: it doesn't use what this
// one kept. Nor is a file that isn't one a cache.
func TestATrackerDoesNotUseWhatAnotherBuildKept(t *testing.T) {
	dir := t.TempDir()
	transcript, kept := filepath.Join(dir, "s1.jsonl"), filepath.Join(dir, "summaries.json")
	require.NoError(t, os.WriteFile(transcript, []byte(firstTurn), 0o600))
	first := NewTracker()
	_, err := first.Summary(transcript)
	require.NoError(t, err)
	_, err = first.Save(kept, "build 1", 0)
	require.NoError(t, err)

	other := NewTracker()
	assert.False(t, other.Load(kept, "build 2"))
	assert.Empty(t, other.files)
	assert.False(t, other.Load(kept, ""), "a build that can't be named keeps nothing")
	assert.False(t, other.Load(filepath.Join(dir, "none.json"), "build 1"), "no file")
	require.NoError(t, os.WriteFile(kept, []byte("{half"), 0o600))
	assert.False(t, other.Load(kept, "build 1"), "not a cache")

	// A transcript that got shorter than its summary is read whole again.
	_, err = first.Save(kept, "build 1", 0)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(transcript, []byte(firstTurn+secondTurn), 0o600))
	_, err = first.Summary(transcript)
	require.NoError(t, err)
	_, err = first.Save(kept, "build 1", 0)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(transcript, []byte(firstTurn), 0o600))
	shorter := NewTracker()
	require.True(t, shorter.Load(kept, "build 1"))
	sum, err := shorter.Summary(transcript)
	require.NoError(t, err)
	assert.Equal(t, "group the logos", sum.LastPrompt)
}
