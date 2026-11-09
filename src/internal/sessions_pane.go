package internal

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/internal/common"
	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// shortCommitLen is how much of a commit names a detached HEAD.
const shortCommitLen = 7

// sessionsPaneView is the session the preview shows: the selected
// session, or what a new one would be.
func (m *model) sessionsPaneView() askui.Session {
	s, ok := m.selectedSession()
	if !ok || m.sessions.list.NewSelected() {
		return m.newSessionPaneView()
	}
	if st := m.unlistedStart(s.ID); st != nil {
		return m.startingSessionPaneView(st)
	}
	state := m.sessionState(s)
	title := s.DisplayTitle()
	if s.Name != "" && s.Name != title {
		title = s.Name + " · " + title
	}
	v := askui.Session{
		Glyph:      sessionsui.Symbol(state),
		GlyphColor: sessionsui.StateColor(state),
		Title:      title,
		State:      m.sessionStateWords(s, state),
		Folder:     tildePath(s.Cwd),
		Branch:     m.sessions.convo.branch,
		Turns:      m.sessions.convo.turns,
		Tasks:      s.Todos,
		Files:      newestFirstFiles(s.EditedFiles, s.Cwd),
	}
	if s.Live {
		v.Place = m.sessionPlace(s)
		v.Beside = s.ID == m.visibleSessionID()
		if s.RemoteControl {
			v.Place += " · Remote Control"
		}
		if s.PermissionMode != "" {
			v.Mode = claudeask.PermissionModeLabel(s.PermissionMode)
		}
	}
	if s.LastError != "" {
		v.Error = "Last error: " + s.LastError
	}
	switch state {
	case sessionsui.NeedsYou:
		m.questionPaneView(s, &v)
	case sessionsui.Working:
		v.Now = s.PendingTool
	case sessionsui.Unread, sessionsui.Idle, sessionsui.Ended:
	}
	return v
}

// sessionStateWords says what a session is doing, as the mockup words it:
// "working", "needs you 40s", "finished 1h ago".
func (m *model) sessionStateWords(s claudesession.Session, state sessionsui.State) string {
	now := time.Now()
	switch state {
	case sessionsui.NeedsYou:
		if age := waitingAge(now, m.needsYouSince(s.ID)); age != "" {
			return "needs you " + age
		}
		return "needs you"
	case sessionsui.Working:
		return "working"
	case sessionsui.Unread:
		return "replied, unread"
	case sessionsui.Idle:
		return "waiting for your next prompt"
	case sessionsui.Ended:
	}
	if age := shortAge(now, sessionAgeAnchor(s)); age != "" && age != "now" {
		return "finished " + age + " ago"
	}
	return "finished just now"
}

// questionPaneView shows what a waiting session asks, as its pane draws it,
// or what it waits on until the pane has been read.
func (m *model) questionPaneView(s claudesession.Session, v *askui.Session) {
	if q := m.sessions.question; q.id == s.ID && len(q.lines) > 0 {
		v.Question = q.lines
		v.Answering = m.sessions.focus == sessionsFocusAnswer
		return
	}
	v.QuestionNote = "Asks: " + firstNonEmpty(s.PendingTool, s.WaitingFor, "a question")
}

// newSessionPaneView says what sending to a new session (n) does.
func (m *model) newSessionPaneView() askui.Session {
	if st := m.awaitedAskStart(); st != nil {
		return m.startingSessionPaneView(st)
	}
	here := m.sessionsHere()
	mode := claudeask.PermissionModeLabel(m.sessions.composer.PermissionMode())
	return askui.Session{
		Glyph:      "+",
		GlyphColor: lipgloss.Color(common.Theme.Correct),
		Title:      "New session in " + folderName(here),
		Folder:     tildePath(here),
		Branch:     gitBranch(here),
		AboutTitle: "What sending does",
		About: []string{
			"Your question starts a session in " + folderName(here) + " in the background, " +
				"in **⏵⏵ " + mode + "** (`" + firstKey(common.Hotkeys.AskChoosePermissionMode) + "` changes it): " +
				"the session beside Marmota stays, the keyboard stays here, and its answer shows above.",
			"It is this folder's session then: `" + firstKey(common.Hotkeys.OpenAskBar) + "` writes to it next time.",
		},
	}
}

// startingSessionPaneView is a session the Ask bar started, until Claude
// Code lists it: what it asks first, if anything, and only that of its
// screen.
func (m *model) startingSessionPaneView(st *askStart) askui.Session {
	v := askui.Session{
		Glyph:      "+",
		GlyphColor: lipgloss.Color(common.Theme.Correct),
		Title:      firstNonEmpty(st.title, "New session in "+folderName(st.folder)),
		State:      "starting",
		Folder:     tildePath(st.folder),
		Branch:     gitBranch(st.folder),
		AboutTitle: "Starting",
		About: []string{
			"Your question started it in the background. It is listed once it has started, in a moment.",
			"What you send it now waits for its first reply, and is typed into it then. `" +
				firstKey(common.Hotkeys.Confirm) + "` brings it beside Marmota.",
		},
	}
	if st.asks == "" {
		return v
	}
	v.Glyph, v.GlyphColor = sessionsui.Symbol(sessionsui.NeedsYou), sessionsui.StateColor(sessionsui.NeedsYou)
	v.State = "needs you"
	if age := waitingAge(time.Now(), st.asksSince); age != "" {
		v.State += " " + age
	}
	v.About = []string{"It starts once you answer. `" + firstKey(common.Hotkeys.Confirm) +
		"` brings it beside Marmota, to see all it shows."}
	v.Question, v.Answering = st.dialog, m.sessions.focus == sessionsFocusAnswer
	if q := m.sessions.question; !q.start || q.id != st.id {
		// Not its row: the Ask box writes to it while the list is on New.
		v.Question, v.QuestionNote = nil, "Asks: "+st.asks+"\nIn the Sessions box, "+
			firstKey(common.Hotkeys.OpenAskBar)+" on its row answers it."
	}
	return v
}

// gitBranch is the branch checked out where dir is, the commit when none
// is, or "" outside a repository.
func gitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		gitPath := filepath.Join(d, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			return branchOf(d, gitPath, info.IsDir())
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

func branchOf(dir, gitPath string, isDir bool) string {
	gitDir := gitPath
	if !isDir {
		// A worktree's .git names the folder git keeps it in.
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return ""
		}
		line, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
		if !ok {
			return ""
		}
		if gitDir = strings.TrimSpace(line); !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(dir, gitDir)
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) //nolint:gosec // git's own HEAD, by the session's folder
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(head))
	if branch, ok := strings.CutPrefix(ref, "ref: refs/heads/"); ok {
		return branch
	}
	if len(ref) >= shortCommitLen {
		return ref[:shortCommitLen]
	}
	return ref
}
