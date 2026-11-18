package internal

// `y session list --rows`: the sessions as rows for a picker, such as the
// editor's "Ask which session?" in fzf. Each line is the session's id,
// agent, host and title, tab-separated, for the program that reads the pick,
// then the row to show: who runs the session, what it does, its title and
// where it works, drawn apart by color and by a mark each, so Claude Code's
// and Codex's are told at a glance.

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// NewSessionRow is the id of a picker's row that stands for a session not
// started yet.
const NewSessionRow = "new"

// Colors of a picker's rows, of the 256 a terminal has by number. The
// terminal pane micro runs fzf in draws those, and neither 24-bit colors
// nor dim text (measured on micro 2.0.15).
const (
	rowReset  = "\x1b[0m"
	rowBold   = "\x1b[1m"
	rowDim    = "\x1b[38;5;245m" // grey
	rowClaude = "\x1b[38;5;209m" // Claude Code: its orange
	rowCodex  = "\x1b[38;5;44m"  // Codex: teal
	rowWorks  = "\x1b[38;5;114m" // working: green
	rowNeeds  = "\x1b[38;5;203m" // needs you: red
)

const (
	rowAgentWidth  = 13
	rowStatusWidth = 11
	rowTitleWidth  = 34
)

// rowAgent is who runs a session, as a row shows it: a mark and the name,
// in the agent's color.
func rowAgent(agent string) string {
	if agent == AgentCodex {
		return rowCodex + rowPad("◆ Codex", rowAgentWidth) + rowReset
	}
	return rowClaude + rowPad("✳ Claude Code", rowAgentWidth) + rowReset
}

// rowStatus is what a session does, as a row shows it.
func rowStatus(s ListedSession) string {
	switch claudesession.Status(s.Status) {
	case claudesession.StatusWorking:
		return rowWorks + rowPad("● working", rowStatusWidth) + rowReset
	case claudesession.StatusWaiting:
		return rowNeeds + rowPad("! needs you", rowStatusWidth) + rowReset
	case claudesession.StatusIdle:
		return rowDim + rowPad("○ idle", rowStatusWidth) + rowReset
	default:
		return rowDim + rowPad(s.Status, rowStatusWidth) + rowReset
	}
}

// rowPad fits text to width cells: cut with an ellipsis, or filled with
// spaces, by the cells its characters take, as Chinese ones take two.
func rowPad(text string, width int) string {
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

// rowField keeps a field on its line and in its column.
func rowField(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// WriteSessionRows writes the sessions as a picker's rows. With newIn, a
// folder, the first rows stand for a session not started yet, one for each
// agent, to start there.
func WriteSessionRows(w io.Writer, sessions []ListedSession, newIn string) error {
	if newIn != "" {
		folder := filepath.Base(newIn)
		for _, agent := range []string{AgentClaudeCode, AgentCodex} {
			show := rowAgent(agent) + "  " + rowBold + "+ New session" + rowReset + " in " + folder +
				"  " + rowDim + tildePath(newIn) + rowReset
			if _, err := fmt.Fprintf(w, "%s\t%s\there\tnew · %s\t%s\n", NewSessionRow, agent, rowField(folder), show); err != nil {
				return err
			}
		}
	}
	for _, s := range sessions {
		host, where := s.Host, filepath.Base(s.Cwd)
		if host == "" {
			host = "here"
		} else {
			where = "☁ " + s.Host + " · " + where
		}
		title := rowField(s.Title)
		show := rowAgent(s.Agent) + "  " + rowStatus(s) + "  " + rowPad(title, rowTitleWidth) + "  " +
			rowDim + where + rowReset
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Agent, rowField(host), title, show); err != nil {
			return err
		}
	}
	return nil
}
