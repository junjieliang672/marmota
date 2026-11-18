package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// The editor beside Marmota (editor_beside): e opens files in a WezTerm pane
// on Marmota's right, and gives it the keyboard, so Marmota and its sessions
// stay in view while you write. The pane starts in the file panel's folder,
// which is the folder a session the editor starts works in. With a session
// shown beside Marmota the editor goes above it, and a session shown later
// goes under the editor: the column on the right is the editor, then the
// session.
//
// micro has no way to be handed a file from outside, so one opened while its
// pane is still there is typed into its command bar: "tab <file>", a tab of
// its own. Any other editor gets a pane for each time e is pressed.

const (
	// editorBesidePercent is how much of Marmota's pane the editor takes,
	// editorOverViewerPercent how much of the pane a session shows in, and
	// viewerUnderEditorPercent what a session shown later takes of the
	// editor's.
	editorBesidePercent      = 60
	editorOverViewerPercent  = 60
	viewerUnderEditorPercent = 40
	// microCommandBar opens micro's command bar: Alt-x in Marmota's
	// bindings for it (scripts/micro/bindings.json).
	microCommandBar = "\x1bx"
)

// editorPaneIn is the editor's pane, when it is still among panes.
func (m *model) editorPaneIn(panes []weztermPane) (int, bool) {
	if m.editorPane == 0 {
		return 0, false
	}
	for _, p := range panes {
		if p.PaneID == m.editorPane {
			return m.editorPane, true
		}
	}
	m.editorPane = 0
	return 0, false
}

// openEditorBeside opens paths in the editor's pane beside Marmota, which it
// opens when there is none, and gives it the keyboard.
func (m *model) openEditorBeside(paths []string) error {
	self, err := currentWeztermPaneID()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("no file to open")
	}
	cwd := m.getFocusedFilePanel().location
	panes, err := listWeztermTabPanes(cwd, self)
	if err != nil {
		return err
	}
	// The pane starts in cwd: a file under it goes by its path from there,
	// which is what the editor shows for it.
	paths = append([]string(nil), paths...)
	for i, p := range paths {
		if rel, err := filepath.Rel(cwd, p); err == nil && filepath.IsLocal(rel) {
			paths[i] = rel
		}
	}
	name, args := editorCommand(common.Config.Editor, os.Getenv("EDITOR"), runtime.GOOS, paths)
	if pane, open := m.editorPaneIn(panes); open && filepath.Base(name) == "micro" {
		for _, p := range paths {
			if err := weztermSendKeys(pane, microCommandBar+"tab "+microQuote(p)+"\r"); err != nil {
				return err
			}
		}
		return activateWeztermPane(cwd, pane)
	}

	anchor, where := self, fmt.Sprintf("--right --percent %d", editorBesidePercent)
	if viewer, shown := m.tmuxViewerPane(); shown {
		anchor, where = viewer.PaneID, fmt.Sprintf("--top --percent %d", editorOverViewerPercent)
	}
	words := []string{"env"}
	for _, v := range m.editorEnv() {
		words = append(words, shellQuote(v))
	}
	words = append(words, shellQuote(name))
	for _, a := range args {
		words = append(words, shellQuote(a))
	}
	// Through a login shell: WezTerm starts a pane's program with its own
	// PATH, which may lack the editor's folder and its plugins' tools.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	pane, err := splitWeztermPane(cwd, fmt.Sprintf("wezterm cli split-pane --pane-id %d %s --cwd %s -- %s -lc %s",
		anchor, where, shellQuote(cwd), shellQuote(shell), shellQuote("exec "+strings.Join(words, " "))))
	if err != nil {
		return err
	}
	m.editorPane = pane
	return activateWeztermPane(cwd, pane)
}

// microQuote quotes a path for micro's command bar, which splits what is
// typed as a shell does.
func microQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// editorEnv is what the editor is told of Marmota, for Marmota's plugin for
// micro (scripts/micro/marmota): the y to ask sessions through, and the
// session the Ask box writes to now, which is the one a question from the
// editor goes to until another is picked there. A Codex session isn't one:
// `y session` reaches Claude Code's.
func (m *model) editorEnv() []string {
	env := []string{}
	if exe, err := os.Executable(); err == nil {
		env = append(env, "MARMOTA_BIN="+exe)
	}
	if s, ok := m.claude.snapshot.Find(m.dockAskTarget()); ok && s.Live && !s.IsCodex() && s.Unreachable == "" {
		host := s.Host
		if host == "" {
			host = "here" // as `y session list` says it
		}
		env = append(env, "MARMOTA_SESSION="+s.ID, "MARMOTA_SESSION_TITLE="+s.DisplayTitle(),
			"MARMOTA_SESSION_HOST="+host)
	}
	return env
}
