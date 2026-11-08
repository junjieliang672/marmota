package internal

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestFilePanelNavigation(t *testing.T) {
	/*
		We want to test
		(1) Switching to parent directory
		(2) Switching to parent on being at root "/"
		(3) Entering current directory
		(4) Entering via a cd (the z popup's action)
		(5) Cd to itself

		Make sure to validate
		- Search bar is cleared
		- The cursor and render values are restored correctly
	*/

	curTestDir := t.TempDir()
	dir1 := filepath.Join(curTestDir, "dir1")
	dir2 := filepath.Join(curTestDir, "dir2")
	file1 := filepath.Join(curTestDir, "file1.txt")
	// We >=3 files in dir1 and >=2 files in dir2
	// so that cursor=2, and cursor=1 are valid values.
	file2 := filepath.Join(dir1, "file2.txt")
	file3 := filepath.Join(dir1, "file3.txt")
	file4 := filepath.Join(dir1, "file4.txt")
	file5 := filepath.Join(dir2, "file5.txt")
	file6 := filepath.Join(dir2, "file6.txt")

	rootDir := "/"

	if runtime.GOOS == utils.OsWindows {
		rootDir = "\\"
	}

	utils.SetupDirectories(t, dir1, dir2)
	utils.SetupFiles(t, file1, file2, file3, file4, file5, file6)

	testdata := []struct {
		name           string
		startDir       string
		resultDir      string
		startLocation  string
		startCursor    int
		startRender    int
		keyInput       []string
		cdTo           string
		searchBarClear bool
	}{
		{
			name:          "Switch to parent",
			startDir:      dir1,
			resultDir:     curTestDir,
			startLocation: filepath.Join(dir1, "file3.txt"),
			startRender:   0,
			keyInput: []string{
				common.Hotkeys.ParentDirectory[0],
			},
			searchBarClear: true,
		},
		{
			name:          "Switch to parent when at root",
			startDir:      rootDir,
			resultDir:     rootDir,
			startLocation: "",
			startCursor:   0,
			startRender:   0,
			keyInput: []string{
				common.Hotkeys.ParentDirectory[0],
			},
			searchBarClear: false,
		},
		{
			name:          "Enter current directory",
			startDir:      curTestDir,
			resultDir:     dir2,
			startLocation: dir2,
			startRender:   0,
			keyInput: []string{
				common.Hotkeys.Confirm[0],
			},
			searchBarClear: true,
		},
		{
			name:           "Enter via cd",
			startDir:       curTestDir,
			resultDir:      dir1,
			startLocation:  file1,
			startRender:    0,
			cdTo:           dir1,
			searchBarClear: true,
		},
		{
			name:           "cd to . should be ignored",
			startDir:       curTestDir,
			resultDir:      curTestDir,
			startLocation:  file1,
			startRender:    0,
			cdTo:           ".",
			searchBarClear: false,
		},
	}

	for _, tt := range testdata {
		t.Run(tt.name, func(t *testing.T) {
			m := defaultTestModel(tt.startDir)
			if tt.startLocation != "" {
				setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), tt.startLocation)
				tt.startCursor = m.getFocusedFilePanel().cursor
			} else {
				m.getFocusedFilePanel().cursor = tt.startCursor
			}
			m.getFocusedFilePanel().render = tt.startRender
			for _, s := range tt.keyInput {
				TeaUpdate(m, utils.TeaRuneKeyMsg(s))
			}
			if tt.cdTo != "" {
				m.applyZoxideModalAction(common.CDCurrentPanelAction{Location: tt.cdTo})
			}

			assert.Equal(t, tt.resultDir, m.getFocusedFilePanel().location)

			if tt.searchBarClear {
				assert.Empty(t, m.getFocusedFilePanel().searchBar.Value())
			}

			// Go back to original directory

			m.applyZoxideModalAction(common.CDCurrentPanelAction{Location: tt.startDir})

			// Make sure we have original curson and render
			assert.Equal(t, tt.startCursor, m.getFocusedFilePanel().cursor)
			assert.Equal(t, tt.startRender, m.getFocusedFilePanel().render)
		})
	}
}
