package sidebar

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestRecentEditsManager_AddCapsAndSortsDescending(t *testing.T) {
	tempDir := t.TempDir()
	recentFile := filepath.Join(tempDir, "recent.json")
	mgr := NewRecentEditsManager(recentFile)

	var newestPath string
	for i := range 12 {
		filePath := filepath.Join(tempDir, "file_"+string(rune('a'+i))+".txt")
		utils.SetupFiles(t, filePath)
		require.NoError(t, mgr.Add(filePath))
		newestPath = filePath
	}

	loaded := mgr.Load()
	require.Len(t, loaded, maxRecentEditedFiles)
	assert.Equal(t, newestPath, loaded[0].Location)
	assert.Equal(t, filepath.Base(newestPath), loaded[0].Name)
	assert.GreaterOrEqual(t, loaded[0].EditedAt, loaded[len(loaded)-1].EditedAt)
}

func TestRecentEditsManager_AddMovesExistingFileToFront(t *testing.T) {
	tempDir := t.TempDir()
	first := filepath.Join(tempDir, "first.txt")
	second := filepath.Join(tempDir, "second.txt")
	utils.SetupFiles(t, first, second)

	recentFile := filepath.Join(tempDir, "recent.json")
	mgr := NewRecentEditsManager(recentFile)

	require.NoError(t, mgr.Add(first))
	require.NoError(t, mgr.Add(second))
	require.NoError(t, mgr.Add(first))

	loaded := mgr.Load()
	require.Len(t, loaded, 2)
	assert.Equal(t, first, loaded[0].Location)
	assert.Equal(t, second, loaded[1].Location)
}

func TestRecentEditsManager_RemoveDeletesMatchingEntry(t *testing.T) {
	tempDir := t.TempDir()
	first := filepath.Join(tempDir, "first.txt")
	second := filepath.Join(tempDir, "second.txt")
	utils.SetupFiles(t, first, second)

	recentFile := filepath.Join(tempDir, "recent.json")
	mgr := NewRecentEditsManager(recentFile)

	require.NoError(t, mgr.Add(first))
	require.NoError(t, mgr.Add(second))
	require.NoError(t, mgr.Remove(second))

	loaded := mgr.Load()
	require.Len(t, loaded, 1)
	assert.Equal(t, first, loaded[0].Location)
}
