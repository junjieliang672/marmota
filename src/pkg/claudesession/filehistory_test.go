package claudesession

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupHashesAndOriginals(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "s1.jsonl")
	lines := `{"type":"user","message":{"role":"user","content":"go"}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"src/a.go":{"backupFileName":"aaaa@v2","version":2},"/abs/b.go":{"backupFileName":"bbbb@v2","version":2},"/abs/c.go":{"backupFileName":null,"version":1}}}}
{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{"src/a.go":{"backupFileName":"aaaa@v3","version":3}}}}
`
	require.NoError(t, os.WriteFile(transcript, []byte(lines), 0o600))

	hashes, err := BackupHashes(transcript, "/repo")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"/repo/src/a.go": "aaaa", "/abs/b.go": "bbbb"}, hashes,
		"relative paths from the session's folder; a null backup names nothing")

	config := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(config, "file-history", "s1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(config, "file-history", "s1", "aaaa@v1"), []byte("old"), 0o600))

	orig, ok := OriginalFile(config, "s1", "aaaa")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(config, "file-history", "s1", "aaaa@v1"), orig)
	_, ok = OriginalFile(config, "s1", "bbbb")
	assert.False(t, ok, "no @v1: the session created it")
}

func TestPathHash(t *testing.T) {
	// The naming checked against Claude Code's own backups on 2026-11-08.
	assert.Equal(t, "6caa1272ac3870ca", PathHash("/repo/README.md"))
}

func TestShellHunks(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	line := `{"type":"user","timestamp":"2026-11-07T10:00:00Z","toolUseResult":{"bashEditDiff":{"files":[` +
		`{"filePath":"/r/a.go","hunks":[{"oldStart":3,"oldLines":1,"newStart":3,"newLines":1,"lines":["-x","+y"]}]}],` +
		`"changedFiles":["/r/a.go"]}}}`
	require.NoError(t, os.WriteFile(transcript, []byte(line+"\n"), 0o600))

	hunks, err := ShellHunks(transcript)
	require.NoError(t, err)
	require.Len(t, hunks["/r/a.go"], 1)
	h := hunks["/r/a.go"][0]
	assert.Equal(t, 3, h.OldStart)
	assert.Equal(t, []string{"-x", "+y"}, h.Lines)
	assert.Equal(t, 10, h.At.Hour())
}
