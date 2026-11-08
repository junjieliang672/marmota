package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestReturnDirElement(t *testing.T) {
	// using 'testDir' set up by testMain of our internal package.
	require.DirExists(t, testDir, "Main test directory should be pre created")
	curTestDir := filepath.Join(testDir, "TestRDE")
	dir1 := filepath.Join(curTestDir, "dir1")
	dir2 := filepath.Join(curTestDir, "dir2")
	utils.SetupDirectories(t, curTestDir, dir1, dir2)

	creationDelay := time.Millisecond * 5
	// Cleanup is handled by TestMain

	// Setup files
	// All files with 10 bytes of text

	// dir1
	// - file1.txt
	// dir2 (Empty)
	// .xyz
	// 1.json
	// abc - Add 15 bytes of text
	// aBcD
	// file1.txt
	// file2.txt - Add 20 bytes of text
	// xyz.json

	fileSetup := []struct {
		path string
		data []byte
	}{
		{filepath.Join(curTestDir, ".xyz"), []byte("0123456789")},
		{filepath.Join(dir1, "file1.txt"), []byte("0123456789")},
		{filepath.Join(curTestDir, "aBcD"), []byte("0123456789")},
		{filepath.Join(curTestDir, "file1.txt"), []byte("0123456789")},
		{filepath.Join(curTestDir, "xyz.json"), []byte("0123456789")},
		{filepath.Join(curTestDir, "abc"), []byte("012345678901234")},
		{filepath.Join(curTestDir, "file2.txt"), []byte("01234567890123456789")},
		{filepath.Join(curTestDir, "1.json"), []byte("0123456789")},
	}

	for _, f := range fileSetup {
		utils.SetupFilesWithData(t, f.data, f.path)
		time.Sleep(creationDelay)
	}

	testdata := []struct {
		name              string
		location          string
		dotFiles          bool
		sortOption        string
		reversed          bool
		sortOptions       sortOptionsModelData
		searchString      string
		expectedElemNames []string
	}{
		{
			name:              "Empty Directory",
			location:          dir2,
			dotFiles:          false,
			sortOption:        "Name",
			reversed:          false,
			expectedElemNames: []string{},
		},
		{
			name:       "Sort by Name",
			location:   curTestDir,
			dotFiles:   false,
			sortOption: "Name",
			reversed:   false,
			expectedElemNames: []string{"dir1", "dir2", "1.json", "abc", "aBcD", "file1.txt",
				"file2.txt", "xyz.json"},
		},
		{
			name:       "Sort by Name, with dotfiles",
			location:   curTestDir,
			dotFiles:   true,
			sortOption: "Name",
			reversed:   false,
			expectedElemNames: []string{"dir1", "dir2", ".xyz", "1.json", "abc", "aBcD",
				"file1.txt", "file2.txt", "xyz.json"},
		},
		{
			name:       "Sort by Name Reversed",
			location:   curTestDir,
			dotFiles:   false,
			sortOption: "Name",
			reversed:   true,
			expectedElemNames: []string{"dir2", "dir1", "xyz.json", "file2.txt",
				"file1.txt", "aBcD", "abc", "1.json"},
		},
		{
			name:       "Sort by Size",
			location:   curTestDir,
			dotFiles:   false,
			sortOption: "Size",
			reversed:   false,
			expectedElemNames: []string{"dir2", "dir1", "1.json", "aBcD",
				"file1.txt", "xyz.json", "abc", "file2.txt"},
		},
		{
			name:       "Sort by Size Reversed",
			location:   curTestDir,
			dotFiles:   false,
			sortOption: "Size",
			reversed:   true,
			expectedElemNames: []string{"dir1", "dir2", "file2.txt", "abc", "xyz.json",
				"file1.txt", "aBcD", "1.json"},
		},
		// This one could be flakey if files are created to quickly, or maybe created in
		// parallel
		{
			name:       "Sort by Date",
			location:   curTestDir,
			dotFiles:   false,
			sortOption: "Date Modified",
			reversed:   false,
			expectedElemNames: []string{"1.json", "file2.txt", "abc",
				"xyz.json", "file1.txt", "aBcD", "dir1", "dir2"},
		},
		{
			name:       "Sort by Type",
			location:   curTestDir,
			dotFiles:   false,
			sortOption: "Type",
			reversed:   false,
			expectedElemNames: []string{"dir1", "dir2", "abc", "aBcD", "1.json", "xyz.json",
				"file1.txt", "file2.txt"},
		},
		{
			name:       "Sort by Type Reversed and dotfiles",
			location:   curTestDir,
			dotFiles:   true,
			sortOption: "Type",
			reversed:   true,
			expectedElemNames: []string{"dir2", "dir1", ".xyz", "file2.txt", "file1.txt",
				"xyz.json", "1.json", "aBcD", "abc"},
		},
		{
			name:              "Sort by Type Reversed and dotfiles with search",
			location:          curTestDir,
			dotFiles:          true,
			sortOption:        "Type",
			reversed:          true,
			searchString:      "x",
			expectedElemNames: []string{".xyz", "file2.txt", "file1.txt", "xyz.json"},
		},
		{
			name:              "Sort by Size Reversed with search ftt",
			location:          curTestDir,
			dotFiles:          false,
			sortOption:        "Size",
			reversed:          true,
			searchString:      "ftt",
			expectedElemNames: []string{"file2.txt", "file1.txt"},
		},
		{
			name:              "Sort by Size Reversed with search d",
			location:          curTestDir,
			dotFiles:          false,
			sortOption:        "Size",
			reversed:          true,
			searchString:      "d",
			expectedElemNames: []string{"dir1", "dir2", "aBcD"},
		},
	}

	for _, tt := range testdata {
		t.Run(tt.name, func(t *testing.T) {
			sortOptionsModel := sortOptionsModelData{
				options:  []string{tt.sortOption},
				selected: 0,
				reversed: tt.reversed,
			}
			var res []element
			if tt.searchString == "" {
				res = returnDirElement(tt.location, tt.dotFiles, sortOptionsModel)
			} else {
				// Search results now default to FZF-score order; tests that
				// used the old "sort by Name afterwards" behaviour apply the
				// sort here to preserve their expected ordering.
				res = returnDirElementBySearchString(tt.location, tt.dotFiles, tt.searchString)
				sortElements(res, sortOptionsModel)
			}

			assert.Len(t, res, len(tt.expectedElemNames))
			actualNames := []string{}
			for i := range res {
				actualNames = append(actualNames, res[i].name)
			}
			assert.Equal(t, tt.expectedElemNames, actualNames)
		})
	}
}

func TestCheckFileNameValidity(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{name: "Invalid - single dot",
			input:   ".",
			wantErr: true,
			errMsg:  "file name cannot be '.' or '..'",
		}, {
			name:    "invalid - double dot",
			input:   "..",
			wantErr: true,
			errMsg:  "file name cannot be '.' or '..'",
		}, {
			name:    "invalid - ends with /.. (platform separator)",
			input:   fmt.Sprintf("testDir%c..", filepath.Separator),
			wantErr: true,
			errMsg:  fmt.Sprintf("file name cannot end with '%c.' or '%c..'", filepath.Separator, filepath.Separator),
		}, {
			name:    "invalid - ends with /. (platform separator)",
			input:   fmt.Sprintf("testDir%c.", filepath.Separator),
			wantErr: true,
			errMsg:  fmt.Sprintf("file name cannot end with '%c.' or '%c..'", filepath.Separator, filepath.Separator),
		}, {
			name:    "valid - normal file name",
			input:   "valid_file.txt",
			wantErr: false,
		},
		{
			name:    "valid - contains dot inside",
			input:   "some.folder.name/file.txt",
			wantErr: false,
		},
		{
			name:    "valid - ends with dot not after separator",
			input:   "somefile.",
			wantErr: false,
		},
		{
			name:    "valid - ends with .. not after separator",
			input:   "somefile..",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkFileNameValidity(tt.input)

			if !tt.wantErr {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			}
		})
	}
}

func Test_renameIfDuplicate(t *testing.T) {
	curTestDir := t.TempDir()
	f1NonExistent := filepath.Join(curTestDir, "file.txt")
	f2 := filepath.Join(curTestDir, "file2.txt")
	f3 := filepath.Join(curTestDir, "file3(3).txt")
	d1 := filepath.Join(curTestDir, "dir1")

	utils.SetupFiles(t, f2, f3)
	utils.SetupDirectories(t, d1)

	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{
			name:     "file does not exist",
			fileName: f1NonExistent,
			want:     filepath.Base(f1NonExistent),
		},
		{
			name:     "file exists without suffix",
			fileName: f2,
			want:     "file2(1).txt",
		},
		{
			name:     "file exists with suffix",
			fileName: f3,
			want:     "file3(4).txt",
		},
		{
			name:     "directory exists",
			fileName: d1,
			want:     "dir1(1)", // without extension
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := renameIfDuplicate(tt.fileName)
			require.NoError(t, err)
			assert.Equal(t, filepath.Base(tt.want), filepath.Base(results))
		})
	}
}

func Benchmark_renameIfDuplicate(b *testing.B) {
	dir := b.TempDir()

	existingFile := filepath.Join(dir, "file.txt")
	err := os.WriteFile(existingFile, utils.SampleDataBytes, 0644)
	require.NoError(b, err)

	existingDir := filepath.Join(dir, "docs")
	err = os.Mkdir(existingDir, 0o755)
	require.NoError(b, err)

	b.Run("file_exists", func(b *testing.B) {
		for range b.N {
			_, err := renameIfDuplicate(existingFile)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("dir_exists", func(b *testing.B) {
		for range b.N {
			_, err := renameIfDuplicate(existingDir)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("file_not_exists", func(b *testing.B) {
		nonExistent := filepath.Join(dir, "nofile.txt")
		for range b.N {
			_, err := renameIfDuplicate(nonExistent)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestParseSearchTerm(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantFilter string
		wantQuery  string
	}{
		{
			name:       "files filter with query",
			input:      "f:main",
			wantFilter: "files",
			wantQuery:  "main",
		},
		{
			name:       "dirs filter with query",
			input:      "d:test",
			wantFilter: "dirs",
			wantQuery:  "test",
		},
		{
			name:       "no filter - normal search",
			input:      "normal",
			wantFilter: "",
			wantQuery:  "normal",
		},
		{
			name:       "files filter - empty query",
			input:      "f:",
			wantFilter: "files",
			wantQuery:  "",
		},
		{
			name:       "dirs filter - empty query",
			input:      "d:",
			wantFilter: "dirs",
			wantQuery:  "",
		},
		{
			name:       "mixed colons in query",
			input:      "f:d:mixed",
			wantFilter: "files",
			wantQuery:  "d:mixed",
		},
		{
			name:       "empty input",
			input:      "",
			wantFilter: "",
			wantQuery:  "",
		},
		{
			name:       "colon in middle of query",
			input:      "test:value",
			wantFilter: "",
			wantQuery:  "test:value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, query := parseSearchTerm(tt.input)
			assert.Equal(t, tt.wantFilter, filter, "filter mismatch")
			assert.Equal(t, tt.wantQuery, query, "query mismatch")
		})
	}
}

// TestSortArrow verifies the sortArrow helper maps every (kind, reversed) pair to
// the correct arrow rune, and that an unknown kind returns "?" without panicking.
// Arrow semantics per spec-003:
//   - Name / false  → "↑"   (ascending = low-to-high = up)
//   - Name / true   → "↓"
//   - Size / false  → "↑"
//   - Size / true   → "↓"
//   - Type / false  → "↑"
//   - Type / true   → "↓"
//   - Date Modified / false → "↓"  (newest-first = descending = down)
//   - Date Modified / true  → "↑"
//   - unknown kind          → "?"  (graceful fallback, no panic)
func TestSortArrow(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		reversed bool
		want     string
	}{
		{
			name:     "Name ascending",
			kind:     "Name",
			reversed: false,
			want:     "↑",
		},
		{
			name:     "Name descending",
			kind:     "Name",
			reversed: true,
			want:     "↓",
		},
		{
			name:     "Size ascending",
			kind:     "Size",
			reversed: false,
			want:     "↑",
		},
		{
			name:     "Size descending",
			kind:     "Size",
			reversed: true,
			want:     "↓",
		},
		{
			name:     "Type ascending",
			kind:     "Type",
			reversed: false,
			want:     "↑",
		},
		{
			name:     "Type descending",
			kind:     "Type",
			reversed: true,
			want:     "↓",
		},
		{
			name:     "Date Modified ascending (reversed=false means newest first, so down arrow)",
			kind:     "Date Modified",
			reversed: false,
			want:     "↓",
		},
		{
			name:     "Date Modified descending (reversed=true means oldest first, so up arrow)",
			kind:     "Date Modified",
			reversed: true,
			want:     "↑",
		},
		{
			name:     "Unknown kind returns question mark without panic",
			kind:     "UnknownSortKind",
			reversed: false,
			want:     "?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortArrow(tt.kind, tt.reversed)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShouldIncludeByFilter(t *testing.T) {
	tests := []struct {
		name       string
		isDir      bool
		filterType string
		want       bool
	}{
		{
			name:       "directory with no filter",
			isDir:      true,
			filterType: "",
			want:       true,
		},
		{
			name:       "file with no filter",
			isDir:      false,
			filterType: "",
			want:       true,
		},
		{
			name:       "directory with files filter - excluded",
			isDir:      true,
			filterType: "files",
			want:       false,
		},
		{
			name:       "file with files filter - included",
			isDir:      false,
			filterType: "files",
			want:       true,
		},
		{
			name:       "directory with dirs filter - included",
			isDir:      true,
			filterType: "dirs",
			want:       true,
		},
		{
			name:       "file with dirs filter - excluded",
			isDir:      false,
			filterType: "dirs",
			want:       false,
		},
		{
			name:       "directory with unknown filter",
			isDir:      true,
			filterType: "unknown",
			want:       true,
		},
		{
			name:       "file with unknown filter",
			isDir:      false,
			filterType: "unknown",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldIncludeByFilter(tt.isDir, tt.filterType)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRecursivelyCollectFiles_ParityAfterRefactor is a spec-006 parity guard
// for decision 006-005 (keep function.go's recursivelyCollectFiles as a thin
// adapter over search.RecursiveCollector.CollectStream).
//
// This test exercises the same semantics as the existing recursive-walk
// users: hidden-file skipping, depth limit, f:/d: filter behaviour. After
// the refactor it must still return element sets that exactly match what
// the original implementation produced on main@HEAD.
//
// We only compare SETS (keyed by full path), not ordering, because T1
// permits non-deterministic ordering from the parallel walk. Callers that
// need a stable order apply sortElements themselves.
func TestRecursivelyCollectFiles_ParityAfterRefactor(t *testing.T) {
	// Build the canonical test tree used elsewhere in this file.
	root := t.TempDir()
	dirA := filepath.Join(root, "dirA")
	dirB := filepath.Join(root, "dirB")
	nested := filepath.Join(dirA, "nested")
	hidden := filepath.Join(root, ".hidden")

	utils.SetupDirectories(t, dirA, dirB, nested, hidden)
	utils.SetupFiles(t,
		filepath.Join(root, "top.txt"),
		filepath.Join(root, ".hiddentop"),
		filepath.Join(dirA, "a1.txt"),
		filepath.Join(dirA, "a2.txt"),
		filepath.Join(dirB, "b1.txt"),
		filepath.Join(nested, "n1.txt"),
		filepath.Join(hidden, "secret.txt"),
	)

	type expectation struct {
		name                string
		displayDotFile      bool
		filterType          string
		wantLocationsSubset []string // names (basenames) that MUST be present
		wantNotPresent      []string // basenames that MUST NOT be present
	}

	cases := []expectation{
		{
			name:           "no filter, hidden skipped",
			displayDotFile: false,
			filterType:     "",
			wantLocationsSubset: []string{
				"top.txt", "dirA", "dirB", "a1.txt", "a2.txt", "b1.txt",
				"nested", "n1.txt",
			},
			wantNotPresent: []string{
				".hidden", ".hiddentop", "secret.txt",
			},
		},
		{
			name:           "no filter, hidden shown",
			displayDotFile: true,
			filterType:     "",
			wantLocationsSubset: []string{
				"top.txt", "dirA", ".hidden", "secret.txt", ".hiddentop",
			},
		},
		{
			name:           "files filter — matches current main@HEAD semantics",
			displayDotFile: false,
			filterType:     "files",
			// Current behaviour (spec AC-CORRECT-2 demands byte-identical
			// parity): when filterType="files" the walk returns
			// filepath.SkipDir for every directory, so ONLY top-level files
			// survive. Deeply nested files (a1.txt, n1.txt, ...) are NOT
			// included. The new parallel implementation MUST preserve this
			// quirky-but-existing behaviour. See decision 006-005.
			wantLocationsSubset: []string{
				"top.txt",
			},
			wantNotPresent: []string{
				"dirA", "dirB", "nested",
				// These nested files ARE excluded by the current
				// quirky-but-spec'd filter behaviour — documenting for
				// implementer visibility.
				"a1.txt", "a2.txt", "b1.txt", "n1.txt",
			},
		},
		{
			name:           "dirs filter",
			displayDotFile: false,
			filterType:     "dirs",
			wantLocationsSubset: []string{
				"dirA", "dirB", "nested",
			},
			wantNotPresent: []string{
				"top.txt", "a1.txt", "n1.txt",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			elems, err := recursivelyCollectFiles(nil, root, tc.displayDotFile, tc.filterType)
			require.NoError(t, err)

			names := make(map[string]struct{}, len(elems))
			for _, e := range elems {
				names[e.name] = struct{}{}
			}

			for _, want := range tc.wantLocationsSubset {
				_, ok := names[want]
				assert.True(t, ok, "expected %q to appear after parity-refactor (filter=%q hidden=%v)",
					want, tc.filterType, tc.displayDotFile)
			}
			for _, unwanted := range tc.wantNotPresent {
				_, ok := names[unwanted]
				assert.False(t, ok, "expected %q to be absent after parity-refactor (filter=%q hidden=%v)",
					unwanted, tc.filterType, tc.displayDotFile)
			}
		})
	}
}
