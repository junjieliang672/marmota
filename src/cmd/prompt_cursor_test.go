package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// A session's prompt as its window shows it: the second line of the text
// folded at a space, the box's mark and a space before each row.
const promptRows = `──────────────────────────────
❯ first line
  second 中文 line that is
  folded
  third
──────────────────────────────
  auto mode on`

const promptText = "first line\nsecond 中文 line that is folded\nthird"

// fakeTmux answers as tmux does for one pane: what was kept of ctrl+G, and
// the screen and cursor now. It notes the keys it was asked to press.
type fakeTmux struct {
	kept   string   // the cursor option's value
	screen []string // what capture-pane and show-buffer print, one after another, the last one from then on
	state  []string // "alternate_on cursor_x cursor_y", likewise
	looks  int
	sent   []string
}

func (f *fakeTmux) run(args ...string) (string, error) {
	now := func(of []string) string {
		if len(of) == 0 {
			return ""
		}
		return of[min(f.looks, len(of)-1)]
	}
	switch args[0] {
	case "show-options":
		return f.kept, nil
	case "set-option":
		f.kept = ""
		return "", nil
	case "show-buffer":
		return now(f.screen), nil
	case "display-message":
		return now(f.state), nil
	case "capture-pane":
		defer func() { f.looks++ }()
		return now(f.screen), nil
	case "send-keys":
		f.sent = append(f.sent, strings.Join(args[1:], " "))
		return "", nil
	}
	return "", errors.New("not a command the cursor's code runs: " + args[0])
}

// Going in: the place the key press left is told once, as LINE:COL.
func TestTheEditorIsToldWhereThePromptsCursorWas(t *testing.T) {
	file := filepath.Join(t.TempDir(), "prompt.md")
	require.NoError(t, os.WriteFile(file, []byte(promptText), 0o600))
	// On the folded row, three columns into it: after "fol".
	tmux := &fakeTmux{kept: "5 3\n", screen: []string{promptRows}}

	line, col, ok := promptCursor(file, "%1", tmux.run)
	require.True(t, ok)
	assert.Equal(t, [2]int{2, len([]rune("second 中文 line that is fol")) + 1}, [2]int{line, col})

	_, _, ok = promptCursor(file, "%1", tmux.run)
	assert.False(t, ok, "used once: an editor opened later for something else starts at the top")
	assert.Equal(t, sessionhost.CursorOption, "@marmota-cursor")
}

// Going back: once the session has drawn its prompt again, left or right is
// pressed until its cursor is where the editor's was.
func TestThePromptsCursorGoesWhereTheEditorsWas(t *testing.T) {
	kept := func(text string) string {
		file := filepath.Join(t.TempDir(), "kept.txt")
		require.NoError(t, os.WriteFile(file, []byte(text), 0o600))
		return file
	}
	noSleep := func(time.Duration) {}
	editor := "an editor's screen"

	// The session puts its cursor at the text's end (row 4, after "third");
	// the editor's was at line 2, after "second 中": 27 characters back,
	// the line breaks and the folded row's space counted.
	tmux := &fakeTmux{
		screen: []string{editor, editor, promptRows},
		state:  []string{"1 0 0", "1 0 0", "0 7 4"},
	}
	file := kept(promptText + "\n") // as an editor saves it
	movePromptCursor(file, "2:9", "%1", tmux.run, noSleep)
	assert.Equal(t, []string{"-t %1 -N 27 Left"}, tmux.sent)
	assert.NoFileExists(t, file, "the kept text is removed")

	// Left where it was, at the first line's start, with the editor's at
	// the text's end: right, over all 46 characters of it.
	tmux = &fakeTmux{screen: []string{promptRows}, state: []string{"0 2 1"}}
	movePromptCursor(kept(promptText), "3:6", "%1", tmux.run, noSleep)
	assert.Equal(t, []string{"-t %1 -N 46 Right"}, tmux.sent)

	// Already there: nothing is pressed.
	tmux = &fakeTmux{screen: []string{promptRows}, state: []string{"0 7 4"}}
	movePromptCursor(kept(promptText), "3:6", "%1", tmux.run, noSleep)
	assert.Empty(t, tmux.sent)

	// A screen that never comes to hold the text: nothing is pressed.
	tmux = &fakeTmux{screen: []string{"something else"}, state: []string{"0 3 0"}}
	movePromptCursor(kept(promptText), "2:9", "%1", tmux.run, noSleep)
	assert.Empty(t, tmux.sent)
}
