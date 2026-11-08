// Package sessions is the Sessions box's list of running Claude sessions,
// grouped by the folder they work in: which one is selected, or a new one,
// and the box that renames one. It also draws a session's state.
package sessions

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// State is what a session is doing, which its glyph shows.
type State int

const (
	// NeedsYou: Claude asks you something and waits for the answer.
	NeedsYou State = iota
	// Working: Claude is in the middle of a turn.
	Working
	// Unread: Claude finished a turn you haven't looked at.
	Unread
	// Idle: running and waiting for your next prompt.
	Idle
	// Ended: not running; opening it resumes it.
	Ended
)

// Row is one session of the list.
type Row struct {
	ID string
	// Name is what Claude Code calls the session, which mentions it; ""
	// when it was never renamed.
	Name    string
	Summary string
}

// Group is one folder of the list.
type Group struct {
	Folder string
	Rows   []Row
}

const renameLimit = 200

type List struct {
	groups []Group
	// newSession says the Ask box writes to a new session (n): no row is
	// selected then.
	newSession bool
	// sel is the selected session's id. It is kept by session and not by
	// position, so the list may change around it.
	sel      string
	rename   textinput.Model
	renameID string
}

// New returns an empty list.
func New() List {
	rename := textinput.New()
	rename.Prompt = ""
	rename.CharLimit = renameLimit
	return List{rename: rename}
}

// SetGroups replaces what the list holds. The selection stays on its
// session while it is listed, else it goes to the first row.
func (l *List) SetGroups(groups []Group) {
	l.groups = groups
	if !l.has(l.sel) {
		l.sel = ""
		for _, g := range groups {
			if len(g.Rows) > 0 {
				l.sel = g.Rows[0].ID
				break
			}
		}
	}
}

func (l *List) has(id string) bool {
	_, ok := l.row(id)
	return ok
}

func (l *List) row(id string) (Row, bool) {
	if id == "" {
		return Row{}, false
	}
	for _, g := range l.groups {
		for _, r := range g.Rows {
			if r.ID == id {
				return r, true
			}
		}
	}
	return Row{}, false
}

// Selected returns the selected session, false while a new session is
// chosen or the list is empty.
func (l *List) Selected() (Row, bool) {
	if l.newSession {
		return Row{}, false
	}
	return l.row(l.sel)
}

// SelectedID returns the selected session's id, or "".
func (l *List) SelectedID() string {
	if r, ok := l.Selected(); ok {
		return r.ID
	}
	return ""
}

// NewSelected reports whether a new session is chosen instead of a row.
func (l *List) NewSelected() bool {
	return l.newSession
}

// Select moves the selection to a session. It reports whether it is listed.
func (l *List) Select(id string) bool {
	if !l.has(id) {
		return false
	}
	l.sel = id
	l.newSession = false
	return true
}

// SelectNew chooses a new session instead of a row: none is selected.
func (l *List) SelectNew() {
	l.newSession = true
}

// StartRename puts the keyboard in a box over the selected session's
// summary, holding it to edit.
func (l *List) StartRename() tea.Cmd {
	r, ok := l.Selected()
	if !ok {
		return nil
	}
	l.renameID = r.ID
	name := r.Summary
	if r.Name != "" && r.Name != r.Summary {
		name = r.Name
	}
	l.rename.SetValue(name)
	l.rename.CursorEnd()
	return l.rename.Focus()
}

// IsRenaming reports whether a name is being typed.
func (l *List) IsRenaming() bool { return l.renameID != "" }

// UpdateRename passes a key to the rename box.
func (l *List) UpdateRename(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.rename, cmd = l.rename.Update(msg)
	return cmd
}

// EndRename closes the rename box and returns which session it was for and
// the name typed.
func (l *List) EndRename() (string, string) {
	id, name := l.renameID, strings.TrimSpace(l.rename.Value())
	l.CancelRename()
	return id, name
}

// RenameView is the rename box, width wide, for the box that draws the list.
func (l *List) RenameView(width int) string {
	l.rename.Width = width
	return l.rename.View()
}

// CancelRename closes the rename box.
func (l *List) CancelRename() {
	l.renameID = ""
	l.rename.Blur()
}

// StateColor is the color a state's glyph is drawn in.
func StateColor(state State) lipgloss.Color {
	switch state {
	case NeedsYou:
		return lipgloss.Color(common.ClaudeNeedsYouColor)
	case Working:
		return lipgloss.Color(common.ClaudeAgentColor)
	case Unread, Idle:
		return lipgloss.Color(common.Theme.Hint)
	case Ended:
	}
	return common.FilePanelBorderColor
}

// Symbol is a state's glyph as plain text.
func Symbol(state State) string {
	switch state {
	case NeedsYou:
		return "⚑"
	case Working:
		return "✳"
	case Unread:
		return "●"
	case Idle:
		return "○"
	case Ended:
		return "·"
	}
	return "·"
}

// Glyph draws a state on bg, as the Sessions box shows it: bold when it
// needs you or has news.
func Glyph(state State, bg lipgloss.Color) string {
	st := lipgloss.NewStyle().Foreground(StateColor(state)).Background(bg)
	if state == NeedsYou || state == Unread {
		st = st.Bold(true)
	}
	return st.Render(Symbol(state))
}
