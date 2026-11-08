package sidebar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

func Test_Load(t *testing.T) {
	tempDir := t.TempDir()
	pDirName := "pinnedDir"
	pinnedDir := filepath.Join(tempDir, pDirName)
	utils.SetupDirectories(t, pinnedDir)

	emptyBytes, err := json.Marshal([]directory{})
	require.NoError(t, err)
	emptyPath := filepath.Join(pinnedDir, "empty.json")
	utils.SetupFilesWithData(t, emptyBytes, emptyPath)

	invalidPath := filepath.Join(pinnedDir, "invalid.json")
	utils.SetupFilesWithData(t, []byte("{ invalid json }"), invalidPath)

	validData := []directory{
		{
			Location: pinnedDir,
			Name:     pDirName,
		},
	}
	validBytes, err := json.Marshal(validData)
	require.NoError(t, err)
	validPath := filepath.Join(pinnedDir, "valid.json")
	utils.SetupFilesWithData(t, validBytes, validPath)

	orderedData := []directory{
		{
			Location: filepath.Join(tempDir, "older"),
			Name:     "older",
			PinnedAt: 10,
		},
		{
			Location: filepath.Join(tempDir, "newest"),
			Name:     "newest",
			PinnedAt: 30,
		},
		{
			Location: filepath.Join(tempDir, "middle"),
			Name:     "middle",
			PinnedAt: 20,
		},
	}
	utils.SetupDirectories(t,
		orderedData[0].Location,
		orderedData[1].Location,
		orderedData[2].Location,
	)
	sortedBytes, err := json.Marshal(orderedData)
	require.NoError(t, err)
	sortedPath := filepath.Join(pinnedDir, "sorted.json")
	utils.SetupFilesWithData(t, sortedBytes, sortedPath)

	nonexistData := []directory{
		{
			Location: pinnedDir,
			Name:     pDirName,
		},
		{
			Location: filepath.Join(pinnedDir, "nonexistent9"),
			Name:     "nonexistent9",
		},
	}
	nonexistBytes, err := json.Marshal(nonexistData)
	require.NoError(t, err)
	nonexistentPath := filepath.Join(pinnedDir, "nonexistent.json")
	utils.SetupFilesWithData(t, nonexistBytes, nonexistentPath)

	cleanDirs := []directory{
		{
			Location: pinnedDir,
			Name:     pDirName,
		},
	}

	testCases := []struct {
		name      string
		pinnedMgr PinnedManager
		expected  []directory
	}{
		{
			name:      "Empty No Pinned Directories",
			pinnedMgr: PinnedManager{filePath: emptyPath},
			expected:  []directory{},
		},
		{
			name:      "Invalid Format File",
			pinnedMgr: PinnedManager{filePath: invalidPath},
			expected:  []directory{},
		},
		{
			name:      "Valid With No Non-Existent Directories",
			pinnedMgr: PinnedManager{filePath: validPath},
			expected:  cleanDirs,
		},
		{
			name:      "Valid With One Non-Existent Directory",
			pinnedMgr: PinnedManager{filePath: nonexistentPath},
			expected:  cleanDirs,
		},
		{
			name:      "Valid Preserves Pinned Order",
			pinnedMgr: PinnedManager{filePath: sortedPath},
			expected:  orderedData,
		},
		{
			name:      "Invalid filePath",
			pinnedMgr: PinnedManager{filePath: filepath.Join(pinnedDir, "pinned_not_exists.json")},
			expected:  []directory{},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.pinnedMgr.Load())
		})
	}
}

func Test_Save(t *testing.T) {
	tempDir := t.TempDir()
	pDirName := "pinnedDir"
	pinnedDir := filepath.Join(tempDir, pDirName)
	utils.SetupDirectories(t, pinnedDir)

	savePath := filepath.Join(pinnedDir, "pinned.json")

	dirs := []directory{
		{
			Location: pinnedDir,
			Name:     pDirName,
		},
	}

	testCases := []struct {
		name      string
		pinnedMgr PinnedManager
		noError   bool
		expected  []directory
		argDirs   []directory
	}{
		{
			name:      "Valid Normal Case",
			pinnedMgr: PinnedManager{filePath: savePath},
			noError:   true,
			expected:  dirs,
			argDirs:   dirs,
		},
		{
			name:      "Empty Slice",
			pinnedMgr: PinnedManager{filePath: savePath},
			noError:   true,
			expected:  []directory{},
			argDirs:   []directory{},
		},
		{
			name:      "Write Failure",
			pinnedMgr: PinnedManager{filePath: pinnedDir},
			noError:   false,
			expected:  nil,
			argDirs:   dirs,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pinnedMgr.Save(tt.argDirs)
			if tt.noError {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, tt.pinnedMgr.Load())
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSave_CapsAtMaximumAndPreservesOrder(t *testing.T) {
	tempDir := t.TempDir()
	pinnedFile := filepath.Join(tempDir, "pinned.json")
	pinnedMgr := PinnedManager{filePath: pinnedFile}

	dirs := make([]directory, 0, 22)
	for i := range 22 {
		dirPath := filepath.Join(tempDir, "dir_"+string(rune('a'+i)))
		utils.SetupDirectories(t, dirPath)
		dirs = append(dirs, directory{
			Location: dirPath,
			Name:     filepath.Base(dirPath),
			PinnedAt: int64(i + 1),
		})
	}

	require.NoError(t, pinnedMgr.Save(dirs))

	loaded := pinnedMgr.Load()
	require.Len(t, loaded, maxPinnedDirectories)
	assert.Equal(t, filepath.Join(tempDir, "dir_a"), loaded[0].Location)
	assert.Equal(t, filepath.Join(tempDir, "dir_t"), loaded[len(loaded)-1].Location)
}

func Test_Toggle(t *testing.T) {
	tempDir := t.TempDir()
	pDirName := "pinnedDir"
	pinnedDir := filepath.Join(tempDir, pDirName)
	utils.SetupDirectories(t, pinnedDir)

	pinnedFile := filepath.Join(pinnedDir, "pinned.json")

	testCases := []struct {
		name              string
		pinnedMgr         PinnedManager
		expected          []directory
		noError           bool
		argDir            string
		assertNewPinnedAt bool
	}{
		{
			name:      "Add Non-Existing Directory to Pinned",
			pinnedMgr: PinnedManager{filePath: pinnedFile},
			expected:  []directory{},
			noError:   true,
			argDir:    filepath.Join(tempDir, "nonExistentDir"),
		},
		{
			name:      "Add a Directory to Pinned",
			pinnedMgr: PinnedManager{filePath: pinnedFile},
			expected: []directory{
				{
					Location: pinnedDir,
					Name:     pDirName,
				},
			},
			noError:           true,
			argDir:            pinnedDir,
			assertNewPinnedAt: true,
		},
		{
			name:      "Remove a Directory from Pinned",
			pinnedMgr: PinnedManager{filePath: pinnedFile},
			expected:  []directory{},
			noError:   true,
			argDir:    pinnedDir,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pinnedMgr.Toggle(tt.argDir)
			if tt.noError {
				require.NoError(t, err)
				loaded := tt.pinnedMgr.Load()
				if tt.assertNewPinnedAt {
					require.Len(t, loaded, len(tt.expected))
					assert.Equal(t, tt.expected[0].Location, loaded[0].Location)
					assert.Equal(t, tt.expected[0].Name, loaded[0].Name)
					assert.Positive(t, loaded[0].PinnedAt)
					return
				}
				assert.Equal(t, tt.expected, loaded)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func Test_Toggle_AddsNewestPinFirst(t *testing.T) {
	tempDir := t.TempDir()
	firstDir := filepath.Join(tempDir, "first")
	secondDir := filepath.Join(tempDir, "second")
	utils.SetupDirectories(t, firstDir, secondDir)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	pinnedMgr := PinnedManager{filePath: pinnedFile}
	require.NoError(t, pinnedMgr.Save([]directory{
		{
			Location: firstDir,
			Name:     "first",
			PinnedAt: 1,
		},
	}))

	require.NoError(t, pinnedMgr.Toggle(secondDir))

	loaded := pinnedMgr.Load()
	require.Len(t, loaded, 2)
	assert.Equal(t, secondDir, loaded[0].Location)
	assert.Equal(t, firstDir, loaded[1].Location)
	assert.Positive(t, loaded[0].PinnedAt)
	assert.Equal(t, int64(1), loaded[1].PinnedAt)
}

func Test_MoveToIndex_ReordersPinnedEntries(t *testing.T) {
	tempDir := t.TempDir()
	firstDir := filepath.Join(tempDir, "first")
	secondDir := filepath.Join(tempDir, "second")
	thirdDir := filepath.Join(tempDir, "third")
	utils.SetupDirectories(t, firstDir, secondDir, thirdDir)

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	pinnedMgr := PinnedManager{filePath: pinnedFile}
	require.NoError(t, pinnedMgr.Save([]directory{
		{Location: firstDir, Name: "first", PinnedAt: 1},
		{Location: secondDir, Name: "second", PinnedAt: 2},
		{Location: thirdDir, Name: "third", PinnedAt: 3},
	}))

	reordered := movePinnedDirectoryToIndex(pinnedMgr.Load(), firstDir, 3)
	require.NoError(t, pinnedMgr.Save(reordered))

	loaded := pinnedMgr.Load()
	require.Len(t, loaded, 3)
	assert.Equal(t, []string{secondDir, thirdDir, firstDir}, []string{
		loaded[0].Location,
		loaded[1].Location,
		loaded[2].Location,
	})
}

func Test_Clean(t *testing.T) {
	tempDir := t.TempDir()
	pDirName := "pinnedDir"
	pinnedDir := filepath.Join(tempDir, pDirName)
	utils.SetupDirectories(t, pinnedDir)

	pinnedFile := filepath.Join(pinnedDir, "pinned.json")

	cleanDirs := []directory{
		{
			Location: pinnedDir,
			Name:     pDirName,
		},
	}
	badDirs := append([]directory{}, cleanDirs...)
	badDirs = append(badDirs, directory{
		Location: filepath.Join(tempDir, "nonexistentDir"),
		Name:     "nonexistentDir",
	})

	testCases := []struct {
		name      string
		pinnedMgr PinnedManager
		modified  bool
		expected  []directory
		argDirs   []directory
	}{
		{
			name:      "All Directories Exist",
			pinnedMgr: PinnedManager{filePath: pinnedFile},
			modified:  false,
			expected:  cleanDirs,
			argDirs:   cleanDirs,
		},
		{
			name:      "Some Directories Exist",
			pinnedMgr: PinnedManager{filePath: pinnedFile},
			modified:  true,
			expected:  cleanDirs,
			argDirs:   badDirs,
		},
		{
			name:      "Save Fails",
			pinnedMgr: PinnedManager{filePath: pinnedDir},
			modified:  false,
			expected:  cleanDirs,
			argDirs:   badDirs,
		},
		{
			name:      "Empty Input Slice",
			pinnedMgr: PinnedManager{filePath: pinnedFile},
			modified:  false,
			expected:  []directory{},
			argDirs:   []directory{},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			subjectPath := tt.pinnedMgr.filePath
			if subjectPath == pinnedFile {
				_ = tt.pinnedMgr.Save(tt.argDirs)
			}
			beforeInfo, beforeErr := os.Stat(subjectPath)

			cleaned := tt.pinnedMgr.Clean(tt.argDirs)

			afterInfo, afterErr := os.Stat(subjectPath)
			if beforeErr == nil && afterErr == nil && !tt.modified {
				require.Equal(t, beforeInfo.ModTime(), afterInfo.ModTime())
			}

			assert.Equal(t, tt.expected, cleaned)
		})
	}
}
