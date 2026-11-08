package zoxide

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	zoxidelib "github.com/lazysegtree/go-zoxide"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsKeyAlphaNum(t *testing.T) {
	assert.True(t, isKeyAlphaNum(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}), "'j' should be alphanumeric")
	assert.True(t, isKeyAlphaNum(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}), "'k' should be alphanumeric")
	assert.True(t, isKeyAlphaNum(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}}), "'5' should be alphanumeric")
	assert.True(t, isKeyAlphaNum(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}}), "'A' should be alphanumeric")
	assert.False(t, isKeyAlphaNum(tea.KeyMsg{Type: tea.KeyUp}), "up arrow should not be alphanumeric")
	assert.False(t, isKeyAlphaNum(tea.KeyMsg{Type: tea.KeyEnter}), "enter should not be alphanumeric")
	assert.False(
		t,
		isKeyAlphaNum(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}),
		"space should not be alphanumeric",
	)
}

func TestOpenResetsState(t *testing.T) {
	m := setupTestModelWithClient(t)
	m.textInput.SetValue("old")
	m.cursor = 2
	m.renderIndex = 1

	cmd := m.Open()

	assert.True(t, m.open, "open should be true after Open()")
	assert.True(t, m.justOpened, "justOpened should be true after Open()")
	assert.Empty(t, m.textInput.Value(), "textInput should be empty after Open()")
	assert.NotNil(t, cmd, "Open() should return non-nil Cmd for async query")
}

func TestCloseClearsState(t *testing.T) {
	m := setupTestModelWithResults(5)
	m.open = true
	m.cursor = 2
	m.renderIndex = 1
	m.textInput.SetValue("test")

	m.Close()

	assert.False(t, m.open, "open should be false after Close()")
	assert.Empty(t, m.results, "results should be empty after Close()")
	assert.Equal(t, 0, m.cursor, "cursor should be 0 after Close()")
	assert.Equal(t, 0, m.renderIndex, "renderIndex should be 0 after Close()")
	assert.Empty(t, m.textInput.Value(), "textInput should be empty after Close()")
}

func TestGetResultsReturnsCopy(t *testing.T) {
	m := setupTestModelWithResults(3)
	originalPath := m.results[0].Path

	results := m.GetResults()
	results[0].Path = "/modified/path"

	assert.Equal(
		t,
		originalPath,
		m.results[0].Path,
		"modifying returned results should not affect original model.results",
	)
}

func TestSetWidthBoundsChecking(t *testing.T) {
	m := setupTestModel()

	m.SetWidth(5)
	assert.Equal(t, ZoxideMinWidth, m.width, "width should be set to ZoxideMinWidth when value < ZoxideMinWidth")

	m.SetWidth(100)
	assert.Equal(t, 100, m.width, "width should be set to provided value when >= ZoxideMinWidth")
}

func TestSetMaxHeightBoundsChecking(t *testing.T) {
	m := setupTestModel()

	m.SetMaxHeight(1)
	assert.Equal(
		t,
		ZoxideMinHeight,
		m.maxHeight,
		"maxHeight should be set to ZoxideMinHeight when value < ZoxideMinHeight",
	)

	m.SetMaxHeight(50)
	assert.Equal(t, 50, m.maxHeight, "maxHeight should be set to provided value when >= ZoxideMinHeight")
}

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
