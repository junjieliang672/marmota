package internal

import (
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// Writing the Ask box's question in the editor, as ctrl+g does in Claude
// Code: the editor opens in Marmota's place on a file that holds the
// question, and when it closes what the file holds is the question. Nothing
// is sent. An editor that ends with an error, as nvim does after :cq, leaves
// the question as it was.

// runAskEditor runs the editor on the file at path and reports how it ended.
var runAskEditor = func(path string, env []string, done func(error) tea.Msg) tea.Cmd { //nolint:gochecknoglobals // test seam
	name, args := editorCommand(common.Config.Editor, os.Getenv("EDITOR"), runtime.GOOS, []string{path})
	c := exec.Command(name, args...) //nolint:gosec // the editor you configured
	c.Env = append(os.Environ(), env...)
	return tea.ExecProcess(c, done)
}

// editAskDraft opens the question in the editor.
func (m *model) editAskDraft() tea.Cmd {
	f, err := os.CreateTemp("", "marmota-ask-*.md") // Markdown, so the editor colors it as a prompt
	if err == nil {
		_, err = f.WriteString(m.sessions.composer.Value())
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		m.flashClaude("Couldn't open the editor: " + err.Error())
		return nil
	}
	path := f.Name()
	// The editor owns the terminal until askDraftEditedMsg arrives; stop
	// driving the input method's mode in the meantime.
	m.externalProcessRunning = true
	return runAskEditor(path, m.editorEnv(), func(err error) tea.Msg { return askDraftEditedMsg{path: path, err: err} })
}

// applyAskDraftEdited takes what the editor left as the question.
func (m *model) applyAskDraftEdited(msg askDraftEditedMsg) tea.Cmd {
	m.externalProcessRunning = false
	defer os.Remove(msg.path)
	if msg.err != nil {
		slog.Warn("The editor for the Ask box's question ended with an error", "error", msg.err)
		m.flashClaude("The editor ended with an error: your question is as it was")
		return restoreMouseReporting
	}
	text, err := os.ReadFile(msg.path)
	if err != nil {
		m.flashClaude("Couldn't read what the editor wrote: " + err.Error())
		return restoreMouseReporting
	}
	// Editors end a file with a line break, which isn't part of the question.
	question := strings.TrimRight(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	m.sessions.composer.SetValue(question)
	if kept := len([]rune(m.sessions.composer.Value())); kept < len([]rune(question)) {
		m.flashClaude("The Ask box holds " + strconv.Itoa(kept) + " characters: the rest was cut off")
	}
	return restoreMouseReporting
}
