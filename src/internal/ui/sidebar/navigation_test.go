package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_lastRenderIndex(t *testing.T) {
	// Setup test data
	sidebarA := Model{
		directories: formDirctorySlice(
			dirSlice(10), dirSlice(10), dirSlice(10),
		),
	}
	sidebarB := Model{
		directories: formDirctorySlice(
			dirSlice(1), nil, dirSlice(5),
		),
	}

	testCases := []struct {
		name              string
		sidebar           Model
		mainPanelHeight   int
		startIndex        int
		expectedLastIndex int
		explanation       string
	}{
		{
			name:              "Small viewport with home directories",
			sidebar:           sidebarA,
			mainPanelHeight:   10,
			startIndex:        0,
			expectedLastIndex: 6,
			explanation:       "2(initialHeight) + 2(0-pinned divider) + 6(1-6 pinned dirs)",
		},
		{
			name:              "Medium viewport showing home and some pinned",
			sidebar:           sidebarA,
			mainPanelHeight:   20,
			startIndex:        0,
			expectedLastIndex: 15,
			explanation:       "2(initialHeight) + 2(0-pinned divider) + 10(1-10 pinned dirs) + 2(11-locations divider) + 4(12-15 home dirs)",
		},
		{
			name:              "Medium viewport starting from pinned dirs",
			sidebar:           sidebarA,
			mainPanelHeight:   20,
			startIndex:        11,
			expectedLastIndex: 26,
			explanation:       "2(initialHeight) + 2(11-locations divider) + 10(12-21 home dirs) + 2(22-disk divider) + 4(23-26 disk dirs)",
		},
		{
			name:              "Large viewport showing all directories",
			sidebar:           sidebarA,
			mainPanelHeight:   100,
			startIndex:        11,
			expectedLastIndex: 32,
			explanation:       "Last dir index is 32",
		},
		{
			name:              "Start index beyond directory count",
			sidebar:           sidebarA,
			mainPanelHeight:   100,
			startIndex:        33,
			expectedLastIndex: 32,
			explanation:       "When startIndex > len(directories), return last valid index",
		},
		{
			name:              "Asymmetric directory distribution",
			sidebar:           sidebarB,
			mainPanelHeight:   12,
			startIndex:        0,
			expectedLastIndex: 6,
			explanation:       "2(initialHeight) + 2(0-pinned divider) + 2(1-locations divider) + 1(2-home dir) + 2(3-disk divider) + 3(4-6 disk dirs)",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sidebar.lastRenderedIndex(tt.mainPanelHeight, tt.startIndex)
			assert.Equal(t, tt.expectedLastIndex, result,
				"lastRenderedIndex failed: %s", tt.explanation)
		})
	}
}

func Test_firstRenderIndex(t *testing.T) {
	sidebarA := Model{
		directories: fullDirSlice(10),
	}
	sidebarB := Model{
		directories: formDirctorySlice(
			dirSlice(1), nil, dirSlice(5),
		),
	}
	sidebarC := Model{
		directories: formDirctorySlice(
			nil, dirSlice(5), dirSlice(5),
		),
	}
	sidebarD := Model{
		directories: formDirctorySlice(
			nil, nil, dirSlice(3),
		),
	}

	// Empty sidebar with only dividers
	sidebarE := Model{
		directories: fullDirSlice(0),
	}

	testCases := []struct {
		name               string
		sidebar            Model
		mainPanelHeight    int
		endIndex           int
		expectedFirstIndex int
		explanation        string
	}{
		{
			name:               "Basic calculation from end index",
			sidebar:            sidebarA,
			mainPanelHeight:    10,
			endIndex:           10,
			expectedFirstIndex: 3,
			explanation:        "2(InitialHeight) + 3(3-5 pinned dirs) + 3(6-8 pinned dirs) + 1(9 pinned dir) + 3(10-pinned dir)",
		},
		{
			name:               "Small panel height",
			sidebar:            sidebarA,
			mainPanelHeight:    5,
			endIndex:           15,
			expectedFirstIndex: 13,
			explanation:        "2(InitialHeight) + 3(13-15 home dirs)",
		},
		{
			name:               "End index near beginning",
			sidebar:            sidebarA,
			mainPanelHeight:    20,
			endIndex:           3,
			expectedFirstIndex: 0,
			explanation:        "When end index is near beginning, first index should be 0",
		},
		{
			name:               "End index at disk divider",
			sidebar:            sidebarA,
			mainPanelHeight:    15,
			endIndex:           22, // Disk divider in sidebar_a
			expectedFirstIndex: 12,
			explanation:        "2(InitialHeight) + 10(12-21 home dirs) + 2(22-disk divider)",
		},
		{
			name:               "Very large panel height showing all items",
			sidebar:            sidebarA,
			mainPanelHeight:    100,
			endIndex:           32, // Last disk dir in sidebar_a
			expectedFirstIndex: 0,
			explanation:        "Large panel should show all directories from start",
		},
		{
			name:               "Asymetric sidebar with few directories",
			sidebar:            sidebarB,
			mainPanelHeight:    12,
			endIndex:           8, // Last disk dir in sidebar_b (0:pinDiv, 1:locDiv, 2:home, 3:diskDiv, 4-8:disk)
			expectedFirstIndex: 1,
			explanation:        "2(InitialHeight) + 2(1-locations divider) + 1(2-home dir) + 2(3-disk divider) + 5(4-8 disk dirs)",
		},
		{
			name:               "No home directories case",
			sidebar:            sidebarC,
			mainPanelHeight:    10,
			endIndex:           8, // Disk dir in sidebar_c (0:pinDiv, 1-5:5pinned, 6:locDiv, 7:diskDiv, 8-12:5disk)
			expectedFirstIndex: 3,
			explanation:        "2(InitialHeight) + 3(3-5 pinned dirs) + 2(6-locations divider) + 2(7-disk divider) + 1(8-disk dir)",
		},
		{
			name:               "Only disk directories case",
			sidebar:            sidebarD,
			mainPanelHeight:    8,
			endIndex:           5, // Last disk dir
			expectedFirstIndex: 2, // Locations divider
			explanation:        "2(InitialHeight) + 2(2-disk divider) + 3(3-5 disk dirs); the locations divider doesn't fit",
		},
		{
			name:               "Empty sidebar case",
			sidebar:            sidebarE,
			mainPanelHeight:    10,
			endIndex:           2, // Disk divider (0:pinDiv, 1:locDiv, 2:diskDiv)
			expectedFirstIndex: 0,
			explanation:        "Empty sidebar should show all dividers",
		},
		{
			name:               "End index at the start",
			sidebar:            sidebarA,
			mainPanelHeight:    5,
			endIndex:           0,
			expectedFirstIndex: 0,
			explanation:        "When end index is at start, first index should be the same",
		},
		{
			name:               "End index out of bounds",
			sidebar:            sidebarA,
			mainPanelHeight:    20,
			endIndex:           33, // Out of bounds for sidebar_a
			expectedFirstIndex: 34, // endIndex + 1
			explanation:        "When end index is out of bounds, should return endIndex+1",
		},
		{
			name:               "Very small panel height",
			sidebar:            sidebarA,
			mainPanelHeight:    1, // Too small to fit anything
			endIndex:           11,
			expectedFirstIndex: 12,
			explanation:        "With panel height less than initialHeight, first index is invalid",
		},
		{
			name:               "Panel height exactly matches divider",
			sidebar:            sidebarA,
			mainPanelHeight:    4,  // Just enough for initialHeight + divider
			endIndex:           11, // Locations divider
			expectedFirstIndex: 11,
			explanation:        "When panel height only fits the divider, start index should be the same",
		},
		{
			name:               "Boundary case between directory types",
			sidebar:            sidebarA,
			mainPanelHeight:    5,
			endIndex:           12, // First home dir
			expectedFirstIndex: 11, // Locations divider
			explanation:        "2(InitialHeight) + 2(11-locations divider) + 1(12-home dir)",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sidebar.firstRenderedIndex(tt.mainPanelHeight, tt.endIndex)
			assert.Equal(t, tt.expectedFirstIndex, result,
				"firstRenderedIndex failed: %s", tt.explanation)
		})
	}
}

func Test_updateRenderIndex(t *testing.T) {
	testCases := []struct {
		name                string
		sidebar             Model
		mainPanelHeight     int
		initialRenderIndex  int
		initialCursor       int
		expectedRenderIndex int
		explanation         string
	}{
		{
			name: "Case I: Cursor moved above render range",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 10, // Start rendering from pinned divider
				cursor:      5,  // Cursor moved to home directory
			},
			mainPanelHeight:     15,
			expectedRenderIndex: 5,
			explanation:         "When cursor moves above render range, renderIndex should be set to cursor",
		},
		{
			name: "Case II: Cursor within render range",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 5, // Start rendering from a home directory
				cursor:      8, // Cursor within visible range
			},
			mainPanelHeight:     15,
			expectedRenderIndex: 5, // No change expected
			explanation:         "When cursor is within render range, renderIndex should not change",
		},
		{
			name: "Case III: Cursor moved below render range",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 0,  // Start rendering from beginning
				cursor:      20, // Cursor moved to a home directory outside visible range
			},
			mainPanelHeight:     10,
			expectedRenderIndex: 13, // Should adjust to make cursor visible
			// 2(Initial height) + 3(13-15 home dirs) + 3(16-18 home dirs) + 2(19-20 home dirs)
			explanation: "When cursor moves below render range, renderIndex should adjust to make cursor visible",
		},
		{
			name: "Edge case: Small panel with cursor at end",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 0,
				cursor:      32, // Last disk directory
			},
			mainPanelHeight:     5,
			expectedRenderIndex: 30, // Should show only the last couple items
			explanation:         "With small panel and cursor at end, should adjust renderIndex to show cursor",
		},
		{
			name: "Edge case: Large panel showing everything",
			sidebar: Model{
				directories: formDirctorySlice(dirSlice(1), nil, dirSlice(5)),
				renderIndex: 2,
				cursor:      4,
			},
			mainPanelHeight:     50, // Large enough to show all directories
			expectedRenderIndex: 2,  // No change needed as everything is visible
			explanation:         "With large panel showing all items, renderIndex should remain unchanged",
		},
		{
			name: "Edge case: Empty sidebar",
			sidebar: Model{
				directories: fullDirSlice(0),
				renderIndex: 0,
				cursor:      1,
			},
			mainPanelHeight:     10,
			expectedRenderIndex: 0, // No change needed for empty sidebar
			explanation:         "With empty sidebar, renderIndex should remain at 0",
		},
		{
			name: "Case I and III overlap: Cursor exactly at current renderIndex",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 15,
				cursor:      15,
			},
			mainPanelHeight:     10,
			expectedRenderIndex: 15, // No change needed, Case I takes precedence
			explanation: "When cursor is exactly at renderIndex, " +
				"Case I takes precedence and renderIndex remains unchanged",
		},
		{
			name: "Boundary case: Cursor at edge of visible range",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 5,
				cursor:      9, // Just at the edge of what's visible
			},
			mainPanelHeight:     8,
			expectedRenderIndex: 5, // Still visible, no change needed
			explanation:         "When cursor is at the edge of visible range, renderIndex should not change",
		},
		{
			name: "Boundary case: Cursor just beyond visible range",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 5,
				cursor:      12, // Just beyond visible range (12 is the first home dir)
			},
			mainPanelHeight:     10,
			expectedRenderIndex: 6, // 2 + 5(6-10 pinned) + 2(11-locations divider) + 1(12)
			explanation:         "When cursor is just beyond visible range, renderIndex should adjust",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the sidebar to avoid modifying the original
			sidebar := tt.sidebar

			// Update render index
			sidebar.updateRenderIndex(tt.mainPanelHeight)

			// Check the result
			assert.Equal(t, tt.expectedRenderIndex, sidebar.renderIndex,
				"updateRenderIndex failed: %s", tt.explanation)
		})
	}
}

func Test_listUp(t *testing.T) {
	testCases := []struct {
		name                string
		sidebar             Model
		mainPanelHeight     int
		expectedCursor      int
		expectedRenderIndex int
		explanation         string
	}{
		{
			name: "Basic cursor movement from middle position",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 5,
				cursor:      5, // Starting from a home directory
			},
			mainPanelHeight:     15,
			expectedCursor:      4, // Should move up one position
			expectedRenderIndex: 4, // Render index should follow cursor
			explanation:         "When cursor is in the middle, it should move up one position",
		},
		{
			name: "Skip divider when moving up",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 8,
				cursor:      12, // Position just after locations divider
			},
			mainPanelHeight:     10,
			expectedCursor:      10, // Should skip divider (11) and move to pinned dir (10)
			expectedRenderIndex: 8,
			explanation:         "When moving up to a divider, cursor should skip it and move to previous item",
		},
		{
			name: "Wrap around from top to bottom",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 0,
				cursor:      1, // At the very top (first pinned dir after divider)
			},
			mainPanelHeight:     10,
			expectedCursor:      32, // Should wrap to last directory (index 32)
			expectedRenderIndex: 25, // Should adjust render to show cursor
			// 2(Initial Height) + 3(25-27 disk dirs) + 3(28-30 disk dirs) + 2(31-32 disk dirs)
			explanation: "When at the top, cursor should wrap to the bottom",
		},
		{
			name: "Skip multiple consecutive dividers",
			sidebar: Model{
				// Create a sidebar with consecutive dividers for testing (0:pinDiv, 1:locDiv, 2-6:5home, 7:diskDiv, 8-12:5disk)
				directories: formDirctorySlice(dirSlice(5), nil, dirSlice(5)),
				renderIndex: 5,
				cursor:      8, // Position after disk divider
			},
			mainPanelHeight:     10,
			expectedCursor:      6, // Should skip disk divider and move to last home dir
			expectedRenderIndex: 5, // Should adjust render index accordingly
			explanation:         "When encountering multiple consecutive dividers, cursor should skip all of them",
		},
		{
			name: "No actual directories case",
			sidebar: Model{
				directories: fullDirSlice(0), // Empty sidebar with just dividers
				renderIndex: 0,
				cursor:      0,
			},
			mainPanelHeight:     10,
			expectedCursor:      0, // Should remain unchanged
			expectedRenderIndex: 0, // Should remain unchanged
			explanation:         "When there are no actual directories, cursor should not move",
		},
		{
			name: "Large panel showing all directories",
			sidebar: Model{
				directories: formDirctorySlice(dirSlice(2), dirSlice(2), dirSlice(2)),
				renderIndex: 0,
				cursor:      4, // Some directory in the middle
			},
			mainPanelHeight:     50, // Large enough to show all directories
			expectedCursor:      2,  // Should move up one position
			expectedRenderIndex: 0,  // No change needed as everything is visible
			explanation:         "With large panel showing all items, cursor should move up and renderIndex remain unchanged",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the sidebar to avoid modifying the original
			sidebar := tt.sidebar

			// Call the function to test
			sidebar.ListUp(tt.mainPanelHeight)

			// Check the results
			assert.Equal(t, tt.expectedCursor, sidebar.cursor,
				"listUp cursor position: %s", tt.explanation)
			assert.Equal(t, tt.expectedRenderIndex, sidebar.renderIndex,
				"listUp render index: %s", tt.explanation)
		})
	}
}

func Test_listDown(t *testing.T) {
	testCases := []struct {
		name                string
		sidebar             Model
		mainPanelHeight     int
		expectedCursor      int
		expectedRenderIndex int
		explanation         string
	}{
		{
			name: "Basic cursor movement from middle position",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 5,
				cursor:      5, // Starting from a home directory
			},
			mainPanelHeight:     15,
			expectedCursor:      6, // Should move down one position
			expectedRenderIndex: 5, // Render index should remain the same as cursor is still visible
			explanation:         "When cursor is in the middle, it should move down one position",
		},
		{
			name: "Skip divider when moving down",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 8,
				cursor:      10, // Position just before locations divider
			},
			mainPanelHeight:     10,
			expectedCursor:      12, // Should skip divider (11) and move to home dir (12)
			expectedRenderIndex: 8,  // Should adjust render index to keep cursor visible
			explanation:         "When moving down to a divider, cursor should skip it and move to next item",
		},
		{
			name: "Wrap around from bottom to top",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 26,
				cursor:      32, // At the very bottom
			},
			mainPanelHeight:     10,
			expectedCursor:      1, // Should wrap to first directory (index 1, first pinned dir)
			expectedRenderIndex: 0, // Should adjust render to show cursor
			explanation:         "When at the bottom, cursor should wrap to the top",
		},
		{
			name: "Skip multiple consecutive dividers",
			sidebar: Model{
				// Create a sidebar with consecutive dividers for testing (0:pinDiv, 1:locDiv, 2-6:5home, 7:diskDiv, 8-12:5disk)
				directories: formDirctorySlice(dirSlice(5), nil, dirSlice(5)),
				renderIndex: 0,
				cursor:      6, // Last home dir before disk divider
			},
			mainPanelHeight:     10,
			expectedCursor:      8, // Should skip disk divider and move to first disk dir
			expectedRenderIndex: 2, // Should adjust render index accordingly
			// 2 (Initial Height) + 5(2-6 home dirs) + 2(7-disk divider) + 1(8 disk dir)
			explanation: "When encountering divider, cursor should skip it",
		},
		{
			name: "No actual directories case",
			sidebar: Model{
				directories: fullDirSlice(0), // Empty sidebar with just dividers
				renderIndex: 0,
				cursor:      0,
			},
			mainPanelHeight:     10,
			expectedCursor:      0, // Should remain unchanged
			expectedRenderIndex: 0, // Should remain unchanged
			explanation:         "When there are no actual directories, cursor should not move",
		},
		{
			name: "Move down from home to disk section",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 6,
				cursor:      21, // Last home directory
			},
			mainPanelHeight:     10,
			expectedCursor:      23, // Should move to first disk directory
			expectedRenderIndex: 17, // 2 + 5(17-21 home dirs) + 2(22-disk divider) + 1(23)
			explanation: "When moving down from last home directory," +
				" cursor should skip divider and go to first disk directory",
		},
		{
			name: "Large panel showing all directories",
			sidebar: Model{
				directories: formDirctorySlice(dirSlice(2), dirSlice(2), dirSlice(2)),
				renderIndex: 0,
				cursor:      4, // Some directory in the middle
			},
			mainPanelHeight:     50, // Large enough to show all directories
			expectedCursor:      5,  // Should move down one position
			expectedRenderIndex: 0,  // No change needed as everything is visible
			explanation:         "With large panel showing all items, cursor should move down and renderIndex remain unchanged",
		},
		{
			name: "Cursor at the end of visible range",
			sidebar: Model{
				directories: fullDirSlice(10),
				renderIndex: 5,
				cursor:      14, // At the end of visible range
			},
			mainPanelHeight:     15,
			expectedCursor:      15, // Should move down one position
			expectedRenderIndex: 5,  // Should remain the same as cursor is still visible
			explanation:         "When cursor is at the end of visible range, moving down should adjust renderIndex",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the sidebar to avoid modifying the original
			sidebar := tt.sidebar

			// Call the function to test
			sidebar.ListDown(tt.mainPanelHeight)

			// Check the results
			assert.Equal(t, tt.expectedCursor, sidebar.cursor,
				"listDown cursor position: %s", tt.explanation)
			assert.Equal(t, tt.expectedRenderIndex, sidebar.renderIndex,
				"listDown render index: %s", tt.explanation)
		})
	}
}
