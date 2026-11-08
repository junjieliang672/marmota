package stringfunction

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// clamp
// ---------------------------------------------------------------------------

func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		v        int
		lower    int
		upper    int
		expected int
	}{
		{"within range", 5, 0, 10, 5},
		{"below lower", -3, 0, 10, 0},
		{"above upper", 15, 0, 10, 10},
		{"at lower bound", 0, 0, 10, 0},
		{"at upper bound", 10, 0, 10, 10},
		{"lower equals upper", 5, 7, 7, 7},
		{"negative range", -5, -10, -3, -5},
		{"negative below lower", -15, -10, -3, -10},
		{"negative above upper", 0, -10, -3, -3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clamp(tt.v, tt.lower, tt.upper)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// getLines
// ---------------------------------------------------------------------------

func TestGetLines(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedLines []string
		expectedWidth int
	}{
		{
			name:          "single line",
			input:         "hello",
			expectedLines: []string{"hello"},
			expectedWidth: 5,
		},
		{
			name:          "empty string",
			input:         "",
			expectedLines: []string{""},
			expectedWidth: 0,
		},
		{
			name:          "two lines",
			input:         "hello\nworld",
			expectedLines: []string{"hello", "world"},
			expectedWidth: 5,
		},
		{
			name:          "varying width lines",
			input:         "hi\nhello\nbye",
			expectedLines: []string{"hi", "hello", "bye"},
			expectedWidth: 5, // "hello" is widest
		},
		{
			name:          "trailing newline",
			input:         "hello\n",
			expectedLines: []string{"hello", ""},
			expectedWidth: 5,
		},
		{
			name:          "unicode characters",
			input:         "abc\n日本語",
			expectedLines: []string{"abc", "日本語"},
			expectedWidth: 6, // each CJK char is width 2
		},
		{
			name:          "single wide character",
			input:         "日",
			expectedLines: []string{"日"},
			expectedWidth: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, width := getLines(tt.input)
			assert.Equal(t, tt.expectedLines, lines, "lines mismatch")
			assert.Equal(t, tt.expectedWidth, width, "width mismatch")
		})
	}
}

// ---------------------------------------------------------------------------
// PlaceOverlay
// ---------------------------------------------------------------------------

func TestPlaceOverlay(t *testing.T) {
	tests := []struct {
		name     string
		x        int
		y        int
		fg       string
		bg       string
		validate func(t *testing.T, result string)
	}{
		{
			name: "fg larger than bg returns fg",
			x:    0,
			y:    0,
			fg:   "AAAAA\nBBBBB\nCCCCC",
			bg:   "xy\nzw",
			validate: func(t *testing.T, result string) {
				// When fg >= bg in both dimensions, fg is returned
				assert.Equal(t, "AAAAA\nBBBBB\nCCCCC", result)
			},
		},
		{
			name: "place at top-left (0,0)",
			x:    0,
			y:    0,
			fg:   "XX",
			bg:   "AAAA\nBBBB\nCCCC",
			validate: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.Len(t, lines, 3)
				// First line should start with fg "XX"
				assert.True(t, strings.HasPrefix(lines[0], "XX"),
					"first line should start with XX, got: %q", lines[0])
				// Remaining lines unchanged
				assert.Equal(t, "BBBB", lines[1])
				assert.Equal(t, "CCCC", lines[2])
			},
		},
		{
			name: "place at bottom-right (clamped)",
			x:    100,
			y:    100,
			fg:   "XX",
			bg:   "AAAA\nBBBB\nCCCC",
			validate: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.Len(t, lines, 3)
				// y is clamped to bgHeight-fgHeight = 3-1 = 2, fg on last line
				assert.True(t, strings.HasSuffix(lines[2], "XX"),
					"last line should end with XX, got: %q", lines[2])
			},
		},
		{
			name: "place in middle",
			x:    1,
			y:    1,
			fg:   "ZZ",
			bg:   "AAAA\nBBBB\nCCCC",
			validate: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.Len(t, lines, 3)
				assert.Equal(t, "AAAA", lines[0], "line 0 should be unchanged")
				// fg is placed at x=1 on line 1: "B" + "ZZ" + "B"
				assert.Contains(t, lines[1], "ZZ", "fg should appear on line 1")
				assert.Equal(t, "CCCC", lines[2], "line 2 should be unchanged")
			},
		},
		{
			name: "negative x clamped to 0",
			x:    -5,
			y:    0,
			fg:   "XX",
			bg:   "AAAA\nBBBB\nCCCC",
			validate: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.Len(t, lines, 3)
				assert.True(t, strings.HasPrefix(lines[0], "XX"),
					"fg should be at x=0 after clamping, got: %q", lines[0])
			},
		},
		{
			name: "negative y clamped to 0",
			x:    0,
			y:    -5,
			fg:   "XX",
			bg:   "AAAA\nBBBB\nCCCC",
			validate: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.Len(t, lines, 3)
				assert.True(t, strings.HasPrefix(lines[0], "XX"),
					"fg should be at y=0 after clamping, got: %q", lines[0])
			},
		},
		{
			name: "multiline fg in multiline bg",
			x:    0,
			y:    0,
			fg:   "AB\nCD",
			bg:   "XXXX\nYYYY\nZZZZ",
			validate: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.Len(t, lines, 3)
				assert.True(t, strings.HasPrefix(lines[0], "AB"),
					"first line should start with AB, got: %q", lines[0])
				assert.True(t, strings.HasPrefix(lines[1], "CD"),
					"second line should start with CD, got: %q", lines[1])
				assert.Equal(t, "ZZZZ", lines[2])
			},
		},
		{
			name: "result has same number of lines as bg",
			x:    0,
			y:    0,
			fg:   "hi",
			bg:   "aaa\nbbb\nccc\nddd\neee",
			validate: func(t *testing.T, result string) {
				bgLines := strings.Split("aaa\nbbb\nccc\nddd\neee", "\n")
				resultLines := strings.Split(result, "\n")
				assert.Len(t, resultLines, len(bgLines),
					"result should have same number of lines as bg")
			},
		},
		{
			name: "unicode fg on ascii bg",
			x:    0,
			y:    0,
			fg:   "日",
			bg:   "XXXXXX\nYYYYYY\nZZZZZZ",
			validate: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.Len(t, lines, 3)
				assert.True(t, strings.HasPrefix(lines[0], "日"),
					"first line should start with 日, got: %q", lines[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PlaceOverlay(tt.x, tt.y, tt.fg, tt.bg)
			tt.validate(t, result)
		})
	}
}

// TestPlaceOverlayPreservesLineCount verifies that the number of output lines
// always matches the background's line count (when fg < bg in at least one
// dimension).
func TestPlaceOverlayPreservesLineCount(t *testing.T) {
	bg := "1234567890\n1234567890\n1234567890\n1234567890\n1234567890"
	fg := "AB\nCD"

	positions := []struct{ x, y int }{
		{0, 0}, {0, 1}, {0, 2}, {0, 3},
		{4, 0}, {4, 2}, {8, 3},
	}

	bgLineCount := len(strings.Split(bg, "\n"))

	for _, pos := range positions {
		result := PlaceOverlay(pos.x, pos.y, fg, bg)
		resultLines := strings.Split(result, "\n")
		assert.Len(t, resultLines, bgLineCount,
			"x=%d y=%d: result line count should match bg", pos.x, pos.y)
	}
}

// TestPlaceOverlayFgContentPresent verifies fg text actually appears in output.
func TestPlaceOverlayFgContentPresent(t *testing.T) {
	bg := strings.Repeat(".", 10) + "\n" +
		strings.Repeat(".", 10) + "\n" +
		strings.Repeat(".", 10)

	fg := "HELLO"

	for y := range 3 {
		result := PlaceOverlay(0, y, fg, bg)
		assert.Contains(t, result, "HELLO",
			"HELLO should appear in output when placed at y=%d", y)
	}
}

// TestWhitespaceRender exercises the unexported whitespace.render method via
// PlaceOverlay when the fg is narrower than the bg, ensuring no panic occurs
// and the output width stays consistent.
func TestPlaceOverlayWidthConsistency(t *testing.T) {
	bg := "AAAAAAAAAA\nBBBBBBBBBB\nCCCCCCCCCC" // 10 chars wide, 3 lines
	fg := "ZZ"                                 // 2 chars wide, 1 line

	// Place at x=4, y=1
	result := PlaceOverlay(4, 1, fg, bg)
	lines := strings.Split(result, "\n")
	require.Len(t, lines, 3)

	assert.Equal(t, "AAAAAAAAAA", lines[0])
	// Line 1: "BBBB" + "ZZ" + "BBBB" = 10 chars
	assert.Len(t, lines[1], 10,
		"overlaid line should still be 10 chars wide, got: %q", lines[1])
	assert.Contains(t, lines[1], "ZZ")
	assert.Equal(t, "CCCCCCCCCC", lines[2])
}
