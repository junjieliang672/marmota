package internal

import (
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// The Ask box resumes a session that has ended instead of starting one
// (ask_resume, ctrl+r, while it writes to a new session): the s popup opens
// on its Resume section alone, the five ended sessions last used of Claude
// Code and of Codex, and the one picked is where enter goes. With a draft,
// it is resumed in the background with the draft as its first message, as
// a new session starts; with none, too. Either way the Sessions box goes
// on its row.

// resumeTarget is the ended session the Ask box resumes.
type resumeTarget struct {
	id, title string
	codex     bool
}

// resumeShown is how many ended sessions of each agent the s popup lists:
// the ones last used. It reads that many of the panel's folder and of each
// of that many folders sessions were last active in, which holds them.
const resumeShown = 5

// resumeEndedMsg is the ended sessions read for the s popup, older ones
// than the day's the sessions list keeps.
type resumeEndedMsg struct {
	sessions []claudesession.Session
}

// readEndedCmd reads the ended sessions of Claude Code and Codex, of any
// day, for the s popup.
func (m *model) readEndedCmd() tea.Cmd {
	if m.claude.tracker == nil {
		m.claude.tracker = claudesession.NewTracker()
	}
	if m.codex.tracker == nil {
		m.codex.tracker = codexsession.NewTracker()
	}
	running := map[string]bool{}
	for _, s := range m.claude.snapshot.Live {
		running[s.ID] = true
	}
	folders := append([]string{m.getFocusedFilePanel().location}, m.claudeSessionsOrder().Folders...)
	tracker, codexTracker, codexDir := m.claude.tracker, m.codex.tracker, codexHome()
	return func() tea.Msg {
		o := claudesession.Options{ProjectsDir: claudeProjectsDir(), Tracker: tracker, Folders: folders,
			PerFolder: resumeShown, RecentFolders: resumeShown}
		ended := claudesession.Ended(o, running)
		folders = append([]string(nil), folders...)
		for _, f := range claudesession.RecentFolders(o.ProjectsDir, tracker, resumeShown) {
			if !slices.Contains(folders, f) {
				folders = append(folders, f)
			}
		}
		for _, s := range collectCodexSessions(codexsession.Options{Home: codexDir, Tracker: codexTracker,
			Folders: folders}) {
			if !running[s.ID] {
				s.Live, s.Status = false, claudesession.StatusEnded
				ended = append(ended, s)
			}
		}
		return resumeEndedMsg{sessions: ended}
	}
}

// applyResumeEnded keeps what was read, and lists it in the s popup.
func (m *model) applyResumeEnded(msg resumeEndedMsg) {
	m.resume.ended = msg.sessions
	m.refreshJump()
}

// endedSessions are the five ended sessions of Codex, or of Claude Code,
// last used, newest first: of the day's the sessions list keeps, and the
// older ones read for the s popup.
func (m *model) endedSessions(codex bool) []claudesession.Session {
	var sessions []claudesession.Session
	listed := map[string]bool{}
	for _, s := range append(append([]claudesession.Session(nil), m.claude.snapshot.Past...), m.resume.ended...) {
		if !listed[s.ID] {
			listed[s.ID] = true
			sessions = append(sessions, s)
		}
	}
	slices.SortStableFunc(sessions, func(a, b claudesession.Session) int { return b.LastActivity.Compare(a.LastActivity) })
	var out []claudesession.Session
	for _, s := range sessions {
		if s.Live || s.IsCodex() != codex || m.isRunning(s.ID) {
			continue
		}
		out = append(out, s)
		if len(out) == resumeShown {
			break
		}
	}
	return out
}

func (m *model) isRunning(id string) bool {
	for _, s := range m.claude.snapshot.Live {
		if s.ID == id {
			return true
		}
	}
	return false
}

// findResumable is a session the s popup listed: in the snapshot, or read
// for it.
func (m *model) findResumable(id string) (claudesession.Session, bool) {
	if s, ok := m.claude.snapshot.Find(id); ok {
		return s, true
	}
	for _, s := range m.resume.ended {
		if s.ID == id {
			return s, true
		}
	}
	return claudesession.Session{}, false
}

// setResumeTarget has the Ask box resume s, in the agent it ran in.
func (m *model) setResumeTarget(s claudesession.Session) {
	m.sessions.composer.SetStartsCodex(s.IsCodex())
	m.ask.resume = &resumeTarget{id: s.ID, title: s.DisplayTitle(), codex: s.IsCodex()}
}

// resumeFromAsk resumes the picked session in a hidden pane: enter with no
// draft. The Sessions box goes on its row.
func (m *model) resumeFromAsk(r resumeTarget) tea.Cmd {
	s, ok := m.findResumable(r.id)
	if !ok {
		m.flashClaude(r.title + " is no longer listed")
		return nil
	}
	cmd, err := m.resumeHidden(s, resumeCommand(s))
	if err != nil {
		m.flashClaude(err.Error())
		return nil
	}
	m.ask.resume = nil
	m.sessions.pendingSelect, m.dock.lastID = s.ID, s.ID
	m.refreshSessionsList()
	return cmd
}

// resumeInBackground resumes the picked session in a hidden pane, with
// question as its first message, in the permission mode, model and effort
// the bar shows, as a new session would start.
func (m *model) resumeInBackground(r resumeTarget, question string) (tea.Cmd, error) {
	s, ok := m.findResumable(r.id)
	if !ok {
		return nil, errors.New(r.title + " is no longer listed")
	}
	home, _ := os.UserHomeDir()
	c := &m.sessions.composer
	model, effort := c.Choices()
	var command string
	if r.codex {
		args := append([]string{"resume", r.id}, codexsession.PermissionArgs(c.PermissionMode())...)
		args = append(args, codexsession.ModelArgs(model)...)
		command = codexLaunch(append(args, "--", claudeask.Prompt(question, m.codexAskContext(), home))...)
	} else {
		command = claudeResumeCommand(r.id, append(claudeask.PermissionArgs(c.PermissionMode()),
			claudeask.ChoiceArgs(model, effort)...), claudeask.Prompt(question, m.askContext(), home))
	}
	cmd, err := m.resumeHidden(s, command)
	if err != nil {
		return nil, err
	}
	m.ask.resume = nil
	return cmd, nil
}

// resumeHidden runs command, which resumes s, in a hidden pane: resumed
// sessions wait there as the ones the Ask box starts, and are listed in the
// Sessions box at once, starting, until Claude Code or Codex lists them.
func (m *model) resumeHidden(s claudesession.Session, command string) (tea.Cmd, error) {
	if _, err := currentWeztermPaneID(); err != nil {
		return nil, errors.New("resuming a session needs Marmota running in WezTerm")
	}
	folder := m.claudeSessionDir(s)
	provider := managedAgentProviderClaude
	if s.IsCodex() {
		provider = managedAgentProviderCodex
	}
	paneID, err := m.launchParkedAgent(folder, command, provider)
	if err != nil {
		return nil, err
	}
	if s.IsCodex() {
		m.recordCodexPane(s.ID, paneID)
	}
	if m.ask.starts == nil {
		m.ask.starts = make(map[string]*askStart)
	}
	m.ask.starts[s.ID] = &askStart{id: s.ID, paneID: paneID, folder: folder, title: s.DisplayTitle(),
		began: time.Now(), codex: s.IsCodex(), resumed: true}
	m.flashClaude("Resuming " + s.DisplayTitle() + " in the background")
	return m.watchAskStart(s.ID), nil
}

// resumeCommand resumes s as it ran, with no message.
func resumeCommand(s claudesession.Session) string {
	if s.IsCodex() {
		return codexLaunch(append([]string{"resume", s.ID}, codexsession.LaunchArgs()...)...)
	}
	command := "claude --resume " + shellQuote(s.ID) + " --dangerously-skip-permissions"
	if s.RemoteControl {
		command += " --remote-control"
	}
	return command
}

// claudeResumeCommand continues session id with prompt as its next message.
// Without --fork-session it keeps its id.
func claudeResumeCommand(id string, args []string, prompt string) string {
	parts := []string{"claude", "--resume", shellQuote(id)}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(append(parts, "--", shellQuote(prompt)), " ")
}

// resumeTargetFor is the session the Ask box resumes, while it writes to a
// new session.
func (m *model) resumeTargetFor() (resumeTarget, bool) {
	if m.ask.resume == nil {
		return resumeTarget{}, false
	}
	if _, ok := m.findResumable(m.ask.resume.id); !ok {
		return resumeTarget{}, false
	}
	return *m.ask.resume, true
}
