package internal

import (
	"fmt"
	"log/slog"
	"os/exec"
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

// sendToShelf hands the selected items, else the one under the cursor, to the
// shelf app (Yoink by default), to drag from there into another app.
func (m *model) sendToShelf() tea.Cmd {
	if runtime.GOOS != utils.OsDarwin {
		m.flashClaude("Sending to a shelf app works on macOS only")
		return nil
	}
	app := strings.TrimSpace(common.Config.ShelfApp)
	if app == "" {
		m.flashClaude("Set shelf_app in config.toml to send files to a shelf app")
		return nil
	}
	paths := m.getFocusedFilePanel().actionTargets()
	if len(paths) == 0 {
		return nil
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
