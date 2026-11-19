package internal

import (
	"log/slog"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// openLinkMark starts what WezTerm pastes into Marmota when a file's link in
// the session preview is cmd+clicked (the open-uri handler in the block of
// ~/.wezterm.lua, setup-macos-hotkeys.sh): the mark, the file's path, and
// after a newline its line. Marmota then opens the file as e does, in its
// place, at that line. The mark is two characters of Unicode's private use
// area, which nothing you paste holds.
const openLinkMark = "marmota-open"

// openLinkRequest reads a paste WezTerm sent for a link: the file and its
// line, 0 when the link names none.
func openLinkRequest(msg tea.Msg) (string, int, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || !key.Paste {
		return "", 0, false
	}
	text, ok := strings.CutPrefix(string(key.Runes), openLinkMark)
	if !ok {
		return "", 0, false
	}
	path, lineText, _ := strings.Cut(strings.TrimRight(text, "\r\n"), "\n")
	if path == "" {
		return "", 0, false
	}
	line, err := strconv.Atoi(strings.TrimSpace(lineText))
	if err != nil || line < 0 {
		line = 0
	}
	return path, line, true
}

// openLink opens a file a link named in the editor, at its line.
func (m *model) openLink(path string, line int) tea.Cmd {
	if err := m.sidebarModel.RecordRecentEdit(path); err != nil {
		slog.Error("Error while recording recent edit", "path", path, "error", err)
	}
	if common.Config.EditorBeside {
		// The pane beside takes files by name alone.
		if err := m.openEditorBeside([]string{path}); err == nil {
			return nil
		}
	}
	args := []string{path}
	if line > 0 {
		// +12 before the file: micro, vim, nvim and nano all read it so.
		args = []string{"+" + strconv.Itoa(line), path}
	}
	return m.runEditor(args)
}
