package internal

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudeprompt"
)

// Writing the Ask box's question in the editor, as ctrl+g does in Claude
// Code: the editor opens in Marmota's place on a file that holds the
// question, and when it closes what the file holds is the question. Nothing
// is sent. An editor that ends with an error, as nvim does after :cq, leaves
// the question as it was.

// runAskEditor runs the editor on the file at path, its cursor at line and
// col of it (both from 1, in characters), and reports how it ended.
//
//nolint:gochecknoglobals // test seam
var runAskEditor = func(path string, line, col int, env []string, done func(error) tea.Msg) tea.Cmd {
	name, args := editorCommand(common.Config.Editor, os.Getenv("EDITOR"), runtime.GOOS, []string{path})
	c := inPlaceCommand(name, editorAt(name, args, line, col))
	c.Env = append(os.Environ(), env...)
	return tea.ExecProcess(c, done)
}

// editorAt is args with what has the editor name start at line and col of
// the file, the last of args: the question's cursor in the Ask box, so the
// editor goes on where you were writing. An editor it doesn't know how to
// tell opens at the file's start, as it would anyway.
func editorAt(name string, args []string, line, col int) []string {
	if len(args) == 0 || line < 1 || col < 1 {
		return args
	}
	var at string
	switch base := filepath.Base(name); {
	case base == "micro", strings.HasSuffix(name, "/marmota/edit"): // the wrapper that runs micro for $EDITOR
		at = "+" + strconv.Itoa(line) + ":" + strconv.Itoa(col)
	case strings.HasSuffix(base, "vim"), base == "vi": // vim, nvim
		// To the line's first character, then right by characters: a
		// column given outright would count bytes.
		at = "+normal! " + strconv.Itoa(line) + "G0"
		if col > 1 {
			at += strconv.Itoa(col-1) + "l"
		}
	default:
		return args
	}
	file := len(args) - 1
	return append(append(append([]string{}, args[:file]...), at), args[file])
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
	line, col := m.sessions.composer.CursorAt()
	// The editor owns the terminal until askDraftEditedMsg arrives; stop
	// driving the input method's mode in the meantime.
	m.externalProcessRunning = true
	// micro, with Marmota's plugin, writes where its cursor is as the file
	// closes: the question's cursor goes there.
	env := append(m.editorEnv(), editorCursorEnv+"="+path+editorCursorSuffix)
	return runAskEditor(path, line, col, env,
		func(err error) tea.Msg { return askDraftEditedMsg{path: path, err: err} })
}

const (
	// editorCursorEnv names, to the editor, the file to write its cursor's
	// place in as the question's file closes (the micro plugin's leaveCursor).
	editorCursorEnv = "MARMOTA_CURSOR_FILE"
	// editorCursorSuffix makes that file's name from the question's.
	editorCursorSuffix = ".cursor"
)

// applyAskDraftEdited takes what the editor left as the question, and puts
// the cursor where the editor's was when it says.
func (m *model) applyAskDraftEdited(msg askDraftEditedMsg) tea.Cmd {
	m.externalProcessRunning = false
	defer os.Remove(msg.path)
	line, col, at := claudeprompt.EditorCursor(msg.path+editorCursorSuffix, msg.path)
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
	if at {
		m.sessions.composer.SetCursorAt(line, col)
	}
	if kept := len([]rune(m.sessions.composer.Value())); kept < len([]rune(question)) {
		m.flashClaude("The Ask box holds " + strconv.Itoa(kept) + " characters: the rest was cut off")
	}
	return restoreMouseReporting
}
