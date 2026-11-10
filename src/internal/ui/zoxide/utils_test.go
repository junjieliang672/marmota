package zoxide

import (
	"os"
	"path/filepath"
	"testing"

	zoxidelib "github.com/lazysegtree/go-zoxide"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterExistingDirsDropsMissingPaths(t *testing.T) {
	realDir1 := t.TempDir()
	realDir2 := t.TempDir()

	// Create a regular file; it should be dropped since we filter to dirs only.
	realFilePath := filepath.Join(t.TempDir(), "somefile.txt")
	err := os.WriteFile(realFilePath, []byte("hi"), 0o600)
	require.NoError(t, err, "writing temp file should succeed")

	input := []zoxidelib.Result{
		{Path: realDir1, Score: 100},
		{Path: "/does/not/exist/xyz", Score: 90},
		{Path: realDir2, Score: 80},
		{Path: "/another/missing/path", Score: 70},
		{Path: realFilePath, Score: 60},
	}

	got := filterExistingDirs(input)

	assert.Len(t, got, 2, "only the two real directories should survive")
	gotPaths := []string{got[0].Path, got[1].Path}
	assert.Contains(t, gotPaths, realDir1, "real directory 1 should be kept")
	assert.Contains(t, gotPaths, realDir2, "real directory 2 should be kept")
	for _, r := range got {
		assert.NotEqual(t, realFilePath, r.Path, "regular file should be dropped")
		assert.NotEqual(t, "/does/not/exist/xyz", r.Path, "missing path should be dropped")
	}
}

func TestFilterExistingDirsAllValid(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()
	input := []zoxidelib.Result{
		{Path: d1, Score: 50},
		{Path: d2, Score: 40},
	}
	got := filterExistingDirs(input)
	assert.Len(t, got, 2, "both valid directories should be kept")
}

func TestFilterExistingDirsEmpty(t *testing.T) {
	got := filterExistingDirs([]zoxidelib.Result{})
	assert.Empty(t, got, "empty input should return empty output")
}

func TestFilterExistingDirsAllStale(t *testing.T) {
	input := []zoxidelib.Result{
		{Path: "/nope/1", Score: 10},
		{Path: "/nope/2", Score: 5},
	}
	got := filterExistingDirs(input)
	assert.Empty(t, got, "all stale entries should be filtered out")
}
