// Package claudesession discovers Claude Code sessions on this computer.
//
// Live sessions come from `claude agents --json`, which lists every running
// interactive and background session. Their state (title, last reply, Remote
// Control, files edited) and the history of finished sessions come from the
// transcripts Claude Code writes under ~/.claude/projects.
package claudesession

import (
	"path/filepath"
	"strings"
	"time"
)

// Status is what a session is doing right now.
type Status string

const (
	// StatusWorking means Claude is in the middle of a turn.
	StatusWorking Status = "working"
	// StatusIdle means the session is running and waiting for the next prompt.
	StatusIdle Status = "idle"
	// StatusWaiting means Claude needs you: it asks a question or for a
	// permission, or shows a menu, and does nothing until you answer.
	StatusWaiting Status = "waiting"
	// StatusEnded means the session is not running; it can be resumed.
	StatusEnded Status = "ended"
	// StatusUnknown is a live session whose status string we don't recognise.
	StatusUnknown Status = "unknown"
)

// Agent is the program a session runs in.
type Agent string

const (
	// AgentClaude is Claude Code, and the zero value.
	AgentClaude Agent = ""
	// AgentCodex is OpenAI's Codex CLI (see package codexsession).
	AgentCodex Agent = "codex"
)

// Session is one Claude Code session, live or past, or one of Codex's,
// which package codexsession lists in the same form.
type Session struct {
	ID    string
	Agent Agent
	Name  string // name from `claude agents`, accepted by `claude attach`
	// Title is the best human label: the AI title, else the last prompt,
	// else the agents name, else the folder name.
	Title string
	Cwd   string

	Live       bool
	Background bool // started with `claude --bg`; has no terminal of its own
	Status     Status
	RawStatus  string
	PID        int
	TTY        string // e.g. /dev/ttys004, empty when the session has no terminal
	Command    string // the process command line
	StartedAt  time.Time
	// ForkedFrom is the session this one continues a copy of; set for
	// Codex's.
	ForkedFrom string

	RemoteControl bool
	RemoteURL     string

	LastActivity   time.Time // newest transcript record
	LastTurnEnd    time.Time // when Claude last finished a turn
	LastPrompt     string
	AwaySummary    string
	EditedFiles    []string
	Todos          []Todo
	PermissionMode string
	// Model and Effort are what it last answered with ("claude-opus-5-5",
	// "medium"), from its transcript; "" when unknown.
	Model, Effort  string
	TranscriptPath string

	// WaitingFor is what a waiting session waits on, as Claude Code puts
	// it: "input needed", "dialog open".
	WaitingFor string
	// PendingTool is the tool call it waits on, or the question it asks.
	PendingTool string
	// LastError is the API error its last reply was.
	LastError string
	// PrintOnly reports a session only `claude -p` ever ran, such as a
	// question superfile's Ask sent.
	PrintOnly bool
}

// IsCodex reports a Codex session.
func (s Session) IsCodex() bool {
	return s.Agent == AgentCodex
}

// UpdateMark is the moment that counts as "something new to read": the end of
// the latest turn, falling back to the latest activity for transcripts that
// have no turn records.
func (s Session) UpdateMark() time.Time {
	if !s.LastTurnEnd.IsZero() {
		return s.LastTurnEnd
	}
	return s.LastActivity
}

// DisplayTitle returns Title, or a sensible fallback when it is empty.
func (s Session) DisplayTitle() string {
	if t := strings.TrimSpace(s.Title); t != "" {
		return t
	}
	if s.Name != "" {
		return s.Name
	}
	if s.Cwd != "" {
		return filepath.Base(s.Cwd)
	}
	const shortIDLen = 8
	if len(s.ID) >= shortIDLen {
		return s.ID[:shortIDLen]
	}
	return s.ID
}

func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// Todo is one entry of a session's task list, as Claude Code's TodoWrite
// tool records it.
type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"` // pending, in_progress or completed
	ActiveForm string `json:"activeForm"`
}
