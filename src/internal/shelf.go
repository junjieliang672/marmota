package internal

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

// runShelfCommand runs open; tests swap it out.
var runShelfCommand = func(name string, args ...string) ([]byte, error) { //nolint:gochecknoglobals // test seam
	return exec.Command(name, args...).CombinedOutput()
}

// builtInShelf names Marmota's own shelf (bin/y-shelf) in messages.
const builtInShelf = "the shelf"

// startShelf starts Marmota's shelf, apart from the terminal so it outlives
// Marmota; tests swap it out.
var startShelf = func(path string, args ...string) error { //nolint:gochecknoglobals // test seam
	cmd := exec.Command(path, args...)
	utils.DetachFromTerminal(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // only reaps it
	return nil
}

// shelfBinary finds Marmota's shelf; tests swap it out.
var shelfBinary = findShelfBinary //nolint:gochecknoglobals // test seam

// findShelfBinary is Marmota's shelf, y-shelf, which build.sh builds beside y.
func findShelfBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	path := filepath.Join(filepath.Dir(exe), "y-shelf")
	if _, err := os.Stat(path); err != nil {
		return "", errors.New("y-shelf isn't built: ./build.sh builds it (it needs Xcode's command line tools)")
	}
	return path, nil
}

// shelfSentMsg reports files handed to the shelf app.
type shelfSentMsg struct {
	app   string
	count int
	err   error
}

// shelfCommand is open's arguments for handing paths to app without bringing
// it to the front (-g), so superfile keeps the focus.
func shelfCommand(app string, paths []string) (string, []string) {
	return "open", append([]string{"-g", "-a", app}, paths...)
}

// sendToShelf hands the selected items, else the one under the cursor, to a
// shelf to drag from there into another app: Marmota's own (y-shelf), or the
// app shelf_app names, such as Yoink.
func (m *model) sendToShelf() tea.Cmd {
	if runtime.GOOS != utils.OsDarwin {
		m.flashClaude("The shelf works on macOS only")
		return nil
	}
	return m.shelfCmd(m.getFocusedFilePanel().actionTargets())
}

// sendSessionFilesToShelf puts the files the selected session changed on
// the shelf, as one pile.
func (m *model) sendSessionFilesToShelf() tea.Cmd {
	s, ok := m.selectedSession()
	if !ok {
		m.flashClaude("No session selected")
		return nil
	}
	var paths []string
	for _, e := range touchedElements(s) {
		paths = append(paths, e.location)
	}
	if len(paths) == 0 {
		m.flashClaude("This session hasn't changed any files")
		return nil
	}
	if runtime.GOOS != utils.OsDarwin {
		m.flashClaude("The shelf works on macOS only")
		return nil
	}
	return m.shelfCmd(paths)
}

// shelfCmd hands paths to Marmota's shelf, or to the app shelf_app names.
func (m *model) shelfCmd(paths []string) tea.Cmd {
	if len(paths) == 0 {
		return nil
	}
	app := strings.TrimSpace(common.Config.ShelfApp)
	if app == "" {
		return func() tea.Msg {
			path, err := shelfBinary()
			if err == nil {
				err = startShelf(path, paths...)
			}
			return shelfSentMsg{app: builtInShelf, count: len(paths), err: err}
		}
	}
	name, args := shelfCommand(app, paths)
	return func() tea.Msg {
		out, err := runShelfCommand(name, args...)
		if err != nil {
			if text := strings.TrimSpace(string(out)); text != "" {
				err = fmt.Errorf("%w: %s", err, text)
			}
		}
		return shelfSentMsg{app: app, count: len(paths), err: err}
	}
}

func (m *model) applyShelfSent(msg shelfSentMsg) {
	if msg.err != nil {
		slog.Error("Could not send files to the shelf app", "app", msg.app, "error", msg.err)
		m.flashClaude("Couldn't send to " + msg.app + ": " + msg.err.Error())
		return
	}
	noun := "item"
	if msg.count > 1 {
		noun = "items"
	}
	m.flashClaude(fmt.Sprintf("%d %s → %s", msg.count, noun, msg.app))
}
