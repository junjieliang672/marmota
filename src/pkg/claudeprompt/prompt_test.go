package claudeprompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The testdata screens are Claude Code 2.1.318 panes, as `wezterm cli
// get-text --escapes` prints them.
func screen(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	require.NoError(t, err)
	return string(data)
}

func TestReadsThePromptBoxOffAScreen(t *testing.T) {
	tests := []struct {
		screen string
		want   Box
	}{
		{"fresh", Box{Shown: true}},
		{"empty", Box{Shown: true}},
		{"draft", Box{Shown: true, Text: "fix the sidebar width"}},
		{"draft-two-lines", Box{Shown: true, Text: "fix the sidebar width\nand its icons"}},
		{"stashed", Box{Shown: true, Stashed: true}},
		{"pasted-lines", Box{Shown: true, Text: "[Pasted text #2 +11 lines]", Stashed: true}},
		// Claude asks something, or whether to trust a folder: no box.
		{"question", Box{}},
		{"trust-question", Box{}},
	}
	for _, tt := range tests {
		t.Run(tt.screen, func(t *testing.T) {
			assert.Equal(t, tt.want, Read(screen(t, tt.screen)))
		})
	}
}

func TestEarlierPromptsAreNoBox(t *testing.T) {
	rule := strings.Repeat("─", 40)
	// Your earlier prompts start with "❯" and a plain space.
	assert.False(t, Read(rule+"\n❯ fix the sidebar\n"+rule+"\n").Shown)
	assert.True(t, Read(rule+"\n❯\u00a0fix the sidebar\n"+rule+"\n").Shown)
	// Nor is a "❯" row with no rule under it, as in a menu.
	assert.False(t, Read(rule+"\n❯\u00a0Yes\nNo\n").Shown)
	// The box is the last one on the screen.
	got := Read(rule + "\n❯\u00a0old\n" + rule + "\nanswer\n" + rule + "\n❯\u00a0new\n" + rule + "\n")
	assert.Equal(t, "new", got.Text)
}

func TestPlaceholderIsDimText(t *testing.T) {
	rule := strings.Repeat("─", 40)
	box := func(row string) Box { return Read(rule + "\n" + row + "\n" + rule) }
	assert.Empty(t, box("❯\u00a0\x1b[2mTry \"fix lint errors\"\x1b[0m").Text)
	assert.Empty(t, box("\x1b[38;2;102;102;102m❯\u00a0\x1b[0;2mPress up to edit queued messages").Text)
	assert.Equal(t, "fix it", box("❯\u00a0\x1b[0;2m\x1b[22mfix it").Text, "dim switched off again")
	// 38;2;r;g;b holds a 2 that isn't dim.
	assert.Equal(t, "fix it", box("❯\u00a0\x1b[38;2;2;2;2mfix it").Text)
}

func TestHolds(t *testing.T) {
	exactly, within := Holds("/rename fix the\nsidebar", "/rename fix the sidebar")
	assert.True(t, exactly, "however the box wraps it")
	assert.True(t, within)

	exactly, within = Holds("half typed/rename fix", "/rename fix")
	assert.False(t, exactly)
	assert.True(t, within)

	exactly, within = Holds("[Pasted text #3 +12 lines]", "a\nb\nc")
	assert.True(t, exactly, "a long paste shows as a summary")
	assert.True(t, within)
	exactly, _ = Holds("[Pasted text #3 +12 lines]", "/cd /tmp")
	assert.False(t, exactly, "a one-line command never does")

	exactly, within = Holds("", "/cd /tmp")
	assert.False(t, exactly)
	assert.False(t, within)
}
