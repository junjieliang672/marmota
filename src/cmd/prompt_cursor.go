package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/uniseg"
	"github.com/urfave/cli/v3"

	"github.com/junjieliang672/marmota/src/pkg/claudeprompt"
	"github.com/junjieliang672/marmota/src/pkg/sessionhost"
)

// The cursor of a session's prompt and the cursor of the editor the session
// opens on it (ctrl+G in Claude Code and Codex) go on from each other, both
// ways. The editor's wrapper (scripts/micro/marmota/edit) asks here:
//
//	y prompt-cursor FILE            before the editor starts: where the
//	                                prompt's cursor was, as LINE:COL of FILE
//	y prompt-cursor --back FILE CURSOR   after it closed: the prompt's cursor
//	                                goes where the editor's was, which the
//	                                editor wrote in the file CURSOR
//
// FILE holds the prompt's text. Going in, Marmota's tmux has kept the screen
// and the cursor as the key was pressed (sessionhost.Config). Going back, the
// session only takes the text again: once it has drawn its prompt, the place
// of its cursor is read off the screen the same way, and left or right is
// pressed as many times as the two places are apart.

const (
	// promptDrawnWithin is how long the session is given to draw its prompt
	// again after the editor closed, and promptLookEvery how often its
	// screen is looked at meanwhile.
	promptDrawnWithin = 4 * time.Second
	promptLookEvery   = 100 * time.Millisecond
	// promptMostKeys is the most presses of an arrow sent: further than
	// that, the cursor stays where the session put it.
	promptMostKeys = 4000
)

func promptCursorCommand() *cli.Command {
	return &cli.Command{
		Name:      "prompt-cursor",
		Usage:     "Carry the cursor between a session's prompt and the editor it opens on it",
		ArgsUsage: "FILE [CURSOR]",
		Hidden:    true,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "back", Usage: "the prompt's cursor goes where the editor's was"},
			&cli.StringFlag{
				Name:  "move",
				Usage: "LINE:COL to move to, in a process that outlives the editor's wrapper",
			},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			pane := os.Getenv("TMUX_PANE")
			if pane == "" || os.Getenv("TMUX") == "" {
				return cli.Exit("", 1)
			}
			switch {
			case c.String("move") != "":
				movePromptCursor(c.Args().First(), c.String("move"), pane, tmuxOutput, time.Sleep)
			case c.Bool("back"):
				promptCursorBack(c.Args().Get(0), c.Args().Get(1))
			default:
				line, col, ok := promptCursor(c.Args().First(), pane, tmuxOutput)
				if !ok {
					return cli.Exit("", 1)
				}
				fmt.Printf("%d:%d\n", line, col)
			}
			return nil
		},
	}
}

// tmuxOutput runs tmux, the server this process runs in, and gives what it
// printed.
func tmuxOutput(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output() //nolint:gosec // tmux, asked about this process's own pane
	return string(out), err
}

// promptCursor is the cursor's line and column in the text of file, for the
// prompt of the session in pane. What was kept of a key press is used once:
// an editor the session opens later for something else, as for its memory
// file, finds nothing kept.
func promptCursor(file, pane string, tmux func(args ...string) (string, error)) (int, int, bool) {
	if file == "" {
		return 0, 0, false
	}
	kept, err := tmux("show-options", "-pqv", "-t", pane, sessionhost.CursorOption)
	if err != nil {
		return 0, 0, false
	}
	_, _ = tmux("set-option", "-pu", "-t", pane, sessionhost.CursorOption)
	var x, y int // the cursor's column and row, as the key binding wrote them
	if _, scanErr := fmt.Sscan(kept, &x, &y); scanErr != nil {
		return 0, 0, false
	}
	screen, err := tmux("show-buffer", "-b", sessionhost.PromptBuffer)
	text, errFile := os.ReadFile(file)
	if err != nil || errFile != nil {
		return 0, 0, false
	}
	return claudeprompt.CursorIn(strings.Split(screen, "\n"), x, y, string(text))
}

// promptCursorBack starts the move of the prompt's cursor to where the
// editor left its own in file, and returns at once: the session takes its
// screen back only when the wrapper that runs this has ended, so the move is
// another process's, which is handed the text as it is now (the session may
// remove file once it has read it).
func promptCursorBack(file, cursorFile string) {
	line, col, ok := claudeprompt.EditorCursor(cursorFile, file)
	text, err := os.ReadFile(file)
	exe, errExe := os.Executable()
	if !ok || err != nil || errExe != nil {
		return
	}
	kept, err := os.CreateTemp("", "marmota-prompt-*.txt")
	if err != nil {
		return
	}
	_, err = kept.Write(text)
	if closeErr := kept.Close(); err != nil || closeErr != nil {
		_ = os.Remove(kept.Name())
		return
	}
	if exec.Command(exe, "prompt-cursor", "--move", fmt.Sprintf("%d:%d", line, col), kept.Name()).Start() != nil {
		_ = os.Remove(kept.Name())
	}
}

// movePromptCursor waits for the session in pane to draw its prompt again,
// then presses left or right until its cursor is at the line and column at
// names ("3:12") of the text in textFile, which it removes. It gives up
// when the screen doesn't come to hold that text.
func movePromptCursor(textFile, at, pane string, tmux func(args ...string) (string, error), sleep func(time.Duration)) {
	data, err := os.ReadFile(textFile)
	_ = os.Remove(textFile)
	var line, col int
	if _, scanErr := fmt.Sscanf(at, "%d:%d", &line, &col); err != nil || scanErr != nil {
		return
	}
	// An editor ends a file with a line break; a session may keep it in its
	// prompt or leave it out.
	texts := []string{string(data)}
	if trimmed := strings.TrimRight(texts[0], "\n"); trimmed != texts[0] {
		texts = append(texts, trimmed)
	}
	seen := ""
	for waited := time.Duration(0); waited < promptDrawnWithin; waited += promptLookEvery {
		sleep(promptLookEvery)
		state, errState := tmux("display-message", "-p", "-t", pane, "#{alternate_on} #{cursor_x} #{cursor_y}")
		screen, errScreen := tmux("capture-pane", "-p", "-t", pane)
		var other, x, y int
		if _, scanErr := fmt.Sscan(state, &other, &x, &y); errState != nil || errScreen != nil || scanErr != nil {
			return
		}
		// The same twice running: the session has finished drawing. Until it
		// takes its screen back the editor's is still up.
		if now := state + screen; other != 0 || now != seen {
			seen = now
			continue
		}
		for _, text := range texts {
			if keys, n, ok := arrowsTo(strings.Split(screen, "\n"), x, y, text, line, col); ok {
				if n > 0 && n <= promptMostKeys {
					_, _ = tmux("send-keys", "-t", pane, "-N", strconv.Itoa(n), keys)
				}
				return
			}
		}
	}
}

// arrowsTo is the arrow to press, and how many times, for the cursor at x, y
// of the screen rows to reach line and col of text, which the rows around it
// hold. An arrow moves by what a person sees as one character.
func arrowsTo(rows []string, x, y int, text string, line, col int) (string, int, bool) {
	from, ok := claudeprompt.CursorOffset(rows, x, y, text)
	if !ok {
		return "", 0, false
	}
	to := claudeprompt.Offset(text, line, col)
	runes := []rune(strings.ReplaceAll(text, "\r\n", "\n"))
	if to < from {
		return "Left", uniseg.GraphemeClusterCount(string(runes[to:from])), true
	}
	return "Right", uniseg.GraphemeClusterCount(string(runes[from:to])), true
}
