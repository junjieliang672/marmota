package internal

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// The editor put away. An editor e opened in Marmota's place can stop itself
// instead of ending: micro on cmd+M (its Suspend, through the marmota
// plugin), vim on ctrl+z. Marmota then takes the terminal back with the
// editor's files, tabs, undo history and replies' pane kept as they are, and
// the status line says an editor is put away. b brings it back; e stays what
// it was, a new editor on the file under the cursor.
//
// The editor doesn't run while it is put away. What it waits for does: a
// question asked with cmd+L is awaited by a process of its own, and its
// reply shows once the editor is back.
//
// One editor is kept. micro is told when one is put away already
// (MARMOTA_EDITOR_AWAY) and says so on cmd+M instead of stopping.
//
// Marmota and micro tell each other two things through files, micro having
// no other way to be told anything from outside: before it stops, micro
// writes how many files it holds and how many aren't saved in the file named
// by MARMOTA_EDITOR_STATE, for the status line and for q; and Marmota writes
// what micro is to do once it continues beside that file, with ".do" added:
// "quit", or "say" and a line for its message bar.

const (
	editorStateEnv = "MARMOTA_EDITOR_STATE"
	editorAwayEnv  = "MARMOTA_EDITOR_AWAY"
	// editorOrderSuffix is added to the state file's path for the file with
	// what the editor is to do when it continues.
	editorOrderSuffix = ".do"
	editorOrderQuit   = "quit"
	editorOrderSay    = "say "
)

// editorStatePath is the file an editor in Marmota's place writes what it
// holds to before it stops. One Marmota has one such editor at a time.
var editorStatePath = func() string { //nolint:gochecknoglobals // test seam
	return filepath.Join(os.TempDir(), "marmota-editor-"+strconv.Itoa(os.Getpid()))
}

// putEditorsAway reports whether editors in Marmota's place run as jobs that
// can be put away.
var putEditorsAway = canPutEditorsAway //nolint:gochecknoglobals // test seam

// awayEditor is the editor that was put away.
type awayEditor struct {
	job *editorJob
	// name is the editor's program, "micro".
	name string
	// told is set when the editor said what it holds: files of them,
	// unsaved not saved. Only micro with the marmota plugin does.
	told           bool
	files, unsaved int
}

type editorAwayMsg struct{ job *editorJob }

type editorJobEndedMsg struct {
	job *editorJob
	err error
}

// startEditorJob runs the editor in Marmota's place as a job that can be put
// away.
func (m *model) startEditorJob(name string, args []string) tea.Cmd {
	return m.runEditorJob(m.newEditorJob(name, args))
}

// newEditorJob is the editor as a job, told where to write what it holds
// before it stops, or that it can't be put away since one is already.
func (m *model) newEditorJob(name string, args []string) *editorJob {
	env := append(os.Environ(), m.editorEnv()...)
	if m.editorAway == nil {
		forgetEditorFiles()
		env = append(env, editorStateEnv+"="+editorStatePath())
	} else {
		env = append(env, editorAwayEnv+"=1")
	}
	return &editorJob{name: name, args: args, env: env}
}

// runEditorJob gives the terminal to the job until it ends or stops.
func (m *model) runEditorJob(job *editorJob) tea.Cmd {
	// The child owns the terminal from here until it ends or stops; stop
	// driving the input method's mode in the meantime.
	m.externalProcessRunning = true
	return tea.Exec(job, func(err error) tea.Msg {
		if job.stopped {
			return editorAwayMsg{job: job}
		}
		return editorJobEndedMsg{job: job, err: err}
	})
}

// applyEditorAway takes the terminal back from an editor that stopped.
func (m *model) applyEditorAway(msg editorAwayMsg) tea.Cmd {
	if m.editorAway != nil && m.editorAway.job != msg.job {
		// One editor is kept: this one goes on.
		return m.runEditorJob(msg.job)
	}
	m.externalProcessRunning = false
	m.editorAway = readAwayEditor(msg.job)
	m.quitAfterEditor = nil // put away again, not closed: Marmota stays
	return restoreMouseReporting
}

// applyEditorJobEnded takes the terminal back from an editor that ended, and
// quits when it was continued to end for that.
func (m *model) applyEditorJobEnded(msg editorJobEndedMsg) tea.Cmd {
	if m.editorAway != nil && m.editorAway.job == msg.job {
		m.editorAway = nil
	}
	if m.editorAway == nil {
		forgetEditorFiles()
	}
	cmd := m.applyEditorFinished(editorFinishedMsg{msg.err})
	if m.quitAfterEditor == nil {
		return cmd
	}
	cdOnQuit := *m.quitAfterEditor
	m.quitAfterEditor = nil
	if msg.err != nil || m.editorAway != nil {
		return cmd
	}
	m.modelQuitState = quitInitiated
	return m.quitIfAsked(cmd, cdOnQuit)
}

// backToEditor brings back the editor that was put away.
func (m *model) backToEditor() tea.Cmd {
	if m.editorAway == nil {
		m.flashClaude("No editor is put away: cmd+M in micro puts it away")
		return nil
	}
	return m.continueEditor("")
}

// continueEditor continues the editor that was put away, with what it is to
// do first: "" nothing.
func (m *model) continueEditor(order string) tea.Cmd {
	path := editorStatePath() + editorOrderSuffix
	_ = os.Remove(path)
	if order != "" {
		if err := os.WriteFile(path, []byte(order+"\n"), 0o600); err != nil { //nolint:mnd // yours alone
			slog.Error("Could not tell the editor what to do", "error", err)
		}
	}
	return m.runEditorJob(m.editorAway.job)
}

// quitWithEditorAway is q while an editor is put away. One that says
// nothing in it is unsaved is continued to end, and Marmota quits when it
// has. Any other is brought back for you to save or close what it holds,
// and Marmota stays.
func (m *model) quitWithEditorAway(cdOnQuit bool) tea.Cmd {
	if a := m.editorAway; a.told && a.unsaved == 0 {
		m.quitAfterEditor = &cdOnQuit
		return m.continueEditor(editorOrderQuit)
	}
	return m.continueEditor(editorOrderSay + "Marmota didn't quit: save or close what is open here first")
}

// readAwayEditor is the editor that stopped, with what it wrote of what it
// holds.
func readAwayEditor(job *editorJob) *awayEditor {
	a := &awayEditor{job: job, name: filepath.Base(job.name)}
	data, err := os.ReadFile(editorStatePath())
	if err != nil {
		return a
	}
	if _, err := fmt.Sscan(string(data), &a.files, &a.unsaved); err == nil {
		a.told = true
	}
	return a
}

// forgetEditorFiles removes the files an editor and Marmota told each other
// things through.
func forgetEditorFiles() {
	_ = os.Remove(editorStatePath())
	_ = os.Remove(editorStatePath() + editorOrderSuffix)
}

// editorAwayStatus is the status line's word of the editor that is put
// away: "✎ micro · 3 files", "" with none.
func (m *model) editorAwayStatus() string {
	a := m.editorAway
	if a == nil || m.externalProcessRunning {
		return ""
	}
	parts := []string{"✎ " + a.name}
	switch {
	case !a.told:
	case a.files == 1:
		parts = append(parts, "1 file")
	default:
		parts = append(parts, strconv.Itoa(a.files)+" files")
	}
	if a.unsaved > 0 {
		parts = append(parts, strconv.Itoa(a.unsaved)+" unsaved")
	}
	parts = append(parts, firstKey(common.Hotkeys.BackToEditor)+" back")
	return lipgloss.NewStyle().Foreground(lipgloss.Color(common.Theme.Hint)).
		Background(lipgloss.Color(common.Theme.FilePanelBG)).Render(strings.Join(parts, " · "))
}
