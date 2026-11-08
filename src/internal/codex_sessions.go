package internal

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// Codex sessions are listed with Claude Code's (claudesession.Session, with
// Agent set): package codexsession asks Codex's server for them. What
// differs is here: where a session's pane is, the commands that resume and
// fork one, and sending to one, which goes through the server rather than
// typed into its pane.

// Test seams: tests never reach the Codex server of the computer they run on.
var (
	codexHome      = codexsession.Home      //nolint:gochecknoglobals // test seam
	codexCollect   = codexsession.Collect   //nolint:gochecknoglobals // test seam
	codexSend      = codexsession.Send      //nolint:gochecknoglobals // test seam
	codexRename    = codexsession.Rename    //nolint:gochecknoglobals // test seam
	codexInterrupt = codexsession.Interrupt //nolint:gochecknoglobals // test seam
	codexBinary    = codexsession.Binary    //nolint:gochecknoglobals // test seam
)

// codexState is what superfile keeps about Codex sessions between refreshes.
type codexState struct {
	tracker *codexsession.Tracker
	// panes are the panes superfile started Codex sessions in: session id →
	// pane id. Other sessions' panes are known by their titles.
	panes map[string]int
	// forks are the forks superfile started whose sessions aren't listed
	// yet. A fork's pane title is the forked session's, so it is known by
	// the pane it was started in instead.
	forks []codexFork
	// restored is set once panes are read back from the agent panes'
	// state, which keeps them for the next superfile.
	restored bool
}

// codexFork is a fork superfile started in a pane.
type codexFork struct {
	parent string
	paneID int
	began  time.Time
}

// codexForkWait is how long a fork's session is looked for: Codex first
// asks whether to trust a worktree it hasn't seen.
const codexForkWait = 10 * time.Minute

// codexCommand is the codex superfile runs, quoted for a shell.
func codexCommand() string {
	if c := strings.TrimSpace(common.Config.CodexCommand); c != "" {
		return c
	}
	return shellQuote(codexBinary())
}

// codexLaunch is a codex command line: codex, then args, each quoted.
func codexLaunch(args ...string) string {
	parts := []string{codexCommand()}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// collectCodexSessions lists Codex's sessions for a refresh. No server
// means no Codex session runs: there is nothing to list, and no error.
func collectCodexSessions(o codexsession.Options) []claudesession.Session {
	sessions, err := codexCollect(o)
	if err != nil && !errors.Is(err, codexsession.ErrNoServer) {
		return nil
	}
	return sessions
}

// mergeCodexSessions adds Codex's sessions to the snapshot, once the panes
// are known. Codex keeps a session loaded after its window has closed: one
// that is idle with no pane has nothing running, and is listed as finished.
func (m *model) mergeCodexSessions(sessions []claudesession.Session) {
	if len(sessions) == 0 {
		return
	}
	m.restoreCodexPanes()
	m.matchCodexForks(sessions)
	snap := &m.claude.snapshot
	claimed := map[int]bool{}
	for _, s := range snap.Live {
		if pane, ok := m.weztermPaneForSession(s); ok {
			claimed[pane.PaneID] = true
		}
	}
	// Sessions in panes superfile started first, then those found by title.
	sort.SliceStable(sessions, func(i, j int) bool {
		_, iKnown := m.codex.panes[sessions[i].ID]
		_, jKnown := m.codex.panes[sessions[j].ID]
		return iKnown && !jKnown
	})
	for _, s := range sessions {
		if pane, ok := m.codexPane(s, claimed); ok {
			s.TTY = pane.TTYName
			claimed[pane.PaneID] = true
		} else if s.Live && s.Status == claudesession.StatusIdle {
			s.Live, s.Status = false, claudesession.StatusEnded
		} else {
			s.Background = s.Live // working with no window
		}
		if s.Live {
			snap.Live = append(snap.Live, s)
		} else {
			snap.Past = append(snap.Past, s)
		}
	}
	sort.SliceStable(snap.Live, func(i, j int) bool { return snap.Live[i].StartedAt.After(snap.Live[j].StartedAt) })
	sort.SliceStable(snap.Past, func(i, j int) bool {
		return snap.Past[i].LastActivity.After(snap.Past[j].LastActivity)
	})
}

// matchCodexForks notes the panes of the forks superfile started, once
// their sessions are listed: each the first fork of its session since.
func (m *model) matchCodexForks(sessions []claudesession.Session) {
	kept := m.codex.forks[:0]
	for _, f := range m.codex.forks {
		var found *claudesession.Session
		for i := range sessions {
			s := &sessions[i]
			_, taken := m.codex.panes[s.ID]
			if !taken && s.Live && s.ForkedFrom == f.parent && !s.StartedAt.Before(f.began.Add(-codexStartLookBack)) &&
				(found == nil || s.StartedAt.Before(found.StartedAt)) {
				found = s
			}
		}
		switch {
		case found != nil:
			m.recordCodexPane(found.ID, f.paneID)
		case time.Since(f.began) < codexForkWait:
			kept = append(kept, f)
		}
	}
	m.codex.forks = kept
}

// codexPane is the pane a Codex session runs in: the one superfile started
// it in, else one whose title Codex set for it, "<name> | <folder>", or
// just "<folder>" before it has a name. Panes in claimed are taken.
func (m *model) codexPane(s claudesession.Session, claimed map[int]bool) (weztermPane, bool) {
	if !s.Live {
		return weztermPane{}, false
	}
	self, selfErr := currentWeztermPaneID()
	usable := func(p weztermPane) bool {
		return !claimed[p.PaneID] && p.TTYName != "" && (selfErr != nil || p.PaneID != self)
	}
	if id, ok := m.codex.panes[s.ID]; ok {
		for _, p := range m.claude.panes {
			if p.PaneID == id && usable(p) {
				return p, true
			}
		}
	}
	recorded := map[int]bool{}
	for _, id := range m.codex.panes {
		recorded[id] = true
	}
	// A fork's pane has the title of the session it was forked from; of two
	// panes with one title, the one that says it needs you is the waiting
	// session's.
	folder, waiting := filepath.Base(s.Cwd), s.Status == claudesession.StatusWaiting
	found, ok := weztermPane{}, false
	for _, p := range m.claude.panes {
		if !usable(p) || recorded[p.PaneID] {
			continue
		}
		name, paneFolder, titled := codexPaneTitle(p.Title)
		if !titled || paneFolder != folder || name != strings.TrimSpace(s.Title) {
			continue
		}
		if codexPaneAsks(p.Title) == waiting {
			return p, true
		}
		if !ok {
			found, ok = p, true
		}
	}
	return found, ok
}

// codexPaneAsks reports a Codex pane whose title says it needs you.
func codexPaneAsks(title string) bool {
	return strings.HasPrefix(strings.TrimSpace(title), "[ ! ]")
}

// codexPaneTitle reads the title Codex gives its pane: "<name> | <folder>",
// with "[ ! ] Action Required | " in front while it needs you, and just
// "<folder>" while the session has no name.
func codexPaneTitle(title string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(title), " | ")
	folder := strings.TrimSpace(parts[len(parts)-1])
	if folder == "" || strings.ContainsAny(folder, "/") {
		return "", "", false
	}
	if len(parts) == 1 {
		return "", folder, true
	}
	name := strings.TrimSpace(parts[len(parts)-2])
	// A status glyph may lead, as Claude Code's do: "⠋ Fix the login".
	if glyph, rest, ok := strings.Cut(name, " "); ok && utf8.RuneCountInString(glyph) == 1 {
		if r, _ := utf8.DecodeRuneInString(glyph); !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			name = strings.TrimSpace(rest)
		}
	}
	return name, folder, true
}

// recordCodexPane notes the pane superfile started a Codex session in, also
// in the agent panes' state, for the next superfile.
func (m *model) recordCodexPane(id string, paneID int) {
	if id == "" || paneID <= 0 {
		return
	}
	if m.codex.panes == nil {
		m.codex.panes = make(map[string]int)
	}
	m.codex.panes[id] = paneID
	for i := range m.managedWeztermState.Sessions {
		if s := &m.managedWeztermState.Sessions[i]; s.PaneID == paneID {
			s.CodexSession = id
		}
	}
	superfilePaneID, err := currentWeztermPaneID()
	if err != nil {
		return
	}
	state, err := loadWeztermPaneLayoutState(superfilePaneID)
	if err != nil {
		return
	}
	for i := range state.Sessions {
		if s := &state.Sessions[i]; s.PaneID == paneID && s.CodexSession != id {
			s.CodexSession = id
			persistWeztermPaneLayoutState(superfilePaneID, state)
		}
	}
}

// restoreCodexPanes reads back the panes an earlier superfile noted, once.
func (m *model) restoreCodexPanes() {
	if m.codex.restored {
		return
	}
	m.codex.restored = true
	superfilePaneID, err := currentWeztermPaneID()
	if err != nil {
		return
	}
	state, err := loadWeztermPaneLayoutState(superfilePaneID)
	if err != nil {
		return
	}
	for _, s := range state.Sessions {
		if _, known := m.codex.panes[s.CodexSession]; s.CodexSession != "" && !known {
			if m.codex.panes == nil {
				m.codex.panes = make(map[string]int)
			}
			m.codex.panes[s.CodexSession] = s.PaneID
		}
	}
}

// lastLaunchedPaneID is the agent pane superfile started last.
func (m *model) lastLaunchedPaneID() int {
	sessions := m.managedWeztermState.Sessions
	if len(sessions) == 0 {
		return 0
	}
	return sessions[len(sessions)-1].PaneID
}

// jumpToCodexSession shows a Codex session beside superfile: its pane when
// one shows it, else a new pane resuming it. Codex attaches the new pane
// to a session its server still runs.
func (m *model) jumpToCodexSession(s claudesession.Session, focus bool) (string, error) {
	title := s.DisplayTitle()
	if pane, ok := m.weztermPaneForSession(s); ok {
		return m.showWeztermPaneForSession(pane, title, focus)
	}
	verb := "Resumed "
	if s.Live {
		verb = "Opened "
	}
	args := append([]string{"resume", s.ID}, codexsession.LaunchArgs()...)
	message, err := m.launchVisibleAgent(m.claudeSessionDir(s), codexLaunch(args...), managedAgentProviderCodex,
		verb+title, focus)
	if err == nil {
		m.recordCodexPane(s.ID, m.lastLaunchedPaneID())
	}
	return message, err
}

// forkCodexCommand continues a copy of a Codex session in a new worktree.
func forkCodexCommand(s claudesession.Session) string {
	return codexLaunch(append([]string{"fork", s.ID, "--worktree"}, codexsession.LaunchArgs()...)...)
}

// codexSentMsg reports a prompt sent to a Codex session.
type codexSentMsg = askDelegatedMsg

// sendToCodex sends prompts to a Codex session through its server, which
// runs them after the turn under way. draft and mentions are the request
// as written, for the Ask box to get back if it can't be sent.
func sendToCodex(id, title string, prompts []string, draft string, mentions map[string]string,
	opts codexsession.TurnOptions,
) tea.Cmd {
	home := codexHome()
	return func() tea.Msg {
		for _, p := range prompts {
			if err := codexSend(home, id, p, opts); err != nil {
				return codexSentMsg{title: title, draft: draft, mentions: mentions, err: err}
			}
		}
		return codexSentMsg{title: title}
	}
}

// codexStartLookBack allows for the server's whole-second creation times.
const codexStartLookBack = 2 * time.Second

// findCodexStart is the session a Codex start became: the first one loaded
// in its folder since it began that no one else has. Codex can't be told an
// id to give a session, so this is how superfile knows it.
func (m *model) findCodexStart(st *askStart) (claudesession.Session, bool) {
	taken := map[string]bool{}
	for id := range m.codex.panes {
		taken[id] = true
	}
	var found []claudesession.Session
	for _, s := range m.claude.snapshot.Live {
		if s.IsCodex() && !taken[s.ID] && sameDir(s.Cwd, st.folder) &&
			!s.StartedAt.Before(st.began.Add(-codexStartLookBack)) {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return claudesession.Session{}, false
	}
	slices.SortFunc(found, func(a, b claudesession.Session) int { return a.StartedAt.Compare(b.StartedAt) })
	return found[0], true
}

// codexStartWaiting reports a Codex start in folder not yet matched to its
// session: another one there would be told apart from it only by luck.
func (m *model) codexStartWaiting(folder string) bool {
	for _, st := range m.ask.starts {
		if st.codex && sameDir(st.folder, folder) {
			return true
		}
	}
	return false
}

// newCodexStartID names a Codex start until its session is known.
func newCodexStartID() string {
	return "codex-start-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// nearestAgentsNotes is the AGENTS.md nearest to dir, in it or above it:
// the instructions Codex reads there.
func nearestAgentsNotes(dir string) string {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, "AGENTS.md")); err == nil && !info.IsDir() {
			return filepath.Join(d, "AGENTS.md")
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// askStartsCodex reports that a new session the Ask box starts is a Codex
// one (tab chooses).
func (m *model) askStartsCodex() bool {
	return m.sessions.composer.StartsCodex()
}

// codexAskContext is the request's context for Codex: its notes are
// AGENTS.md.
func (m *model) codexAskContext() claudeask.Context {
	c := m.askContext()
	c.Notes = nearestAgentsNotes(m.getFocusedFilePanel().location)
	return c
}

// codexSending says what sending to a Codex session does. It goes through
// Codex's server, so a session without a pane is reached too.
func (m *model) codexSending(s claudesession.Session) askui.Sending {
	switch {
	case !s.Live:
		return askui.Sending{Text: "can't reach it: it has ended (enter resumes it)", Fixed: true}
	case s.Status == claudesession.StatusWaiting:
		return askui.Sending{Text: "it asks you something: " + firstKey(common.Hotkeys.OpenAskBar) +
			" in the list answers it", Fixed: true}
	}
	sending := askui.Sending{Text: "sent to it (" + m.sessionPlace(s) + ")", Fixed: true,
		Model: s.Model, Effort: s.Effort, Change: true}
	if ch := m.ask.change; ch.id == s.ID && (ch.model != "" || ch.effort != "") {
		sending.Model, sending.Effort = firstNonEmpty(ch.model, s.Model), firstNonEmpty(ch.effort, s.Effort)
		sending.Changed = true
	}
	return sending
}

// changeSessionChoice steps the model (or the effort) of the running
// session the Ask box writes to: Codex takes it with the next message.
// Claude Code would save it as its default for new sessions too, so a
// Claude Code session's stays.
func (m *model) changeSessionChoice(model bool) {
	s, ok := m.selectedSession()
	switch {
	case !ok || m.sessions.list.NewSelected():
		return
	case !s.IsCodex():
		m.sessions.composer.SetNotice("Claude Code would save a new model or effort as your default for new "+
			"sessions too: change it in the session with /model", false)
		return
	case !m.askSending().Change: // ended, or asking you something
		return
	}
	ch := m.ask.change
	if ch.id != s.ID {
		ch = sessionChange{id: s.ID}
	}
	c := &m.sessions.composer
	if model {
		ch.model = askui.NextChoice(c.CodexModelChoices(), ch.model)
	} else {
		ch.effort = askui.NextChoice(c.CodexEffortChoices(firstNonEmpty(ch.model, s.Model)), ch.effort)
	}
	if !slices.Contains(c.CodexEffortChoices(firstNonEmpty(ch.model, s.Model)), ch.effort) {
		ch.effort = "" // the model doesn't take it
	}
	m.ask.change = ch
}

// sendToCodexSession sends the question to a running Codex session.
func (m *model) sendToCodexSession(s claudesession.Session, question string) tea.Cmd {
	home, _ := os.UserHomeDir()
	prompt := claudeask.Prompt(question, m.codexAskContext(), home)
	title := s.DisplayTitle()
	m.flashClaude("Sending to " + title + "…")
	c := &m.sessions.composer
	var opts codexsession.TurnOptions
	if ch := m.ask.change; ch.id == s.ID {
		opts = codexsession.TurnOptions{Model: ch.model, Effort: ch.effort}
		m.ask.change = sessionChange{} // Codex keeps it from this message on
	}
	return sendToCodex(s.ID, title, []string{prompt}, c.Value(), c.Mentions(), opts)
}

// startCodexSession starts a Codex session in a hidden pane, in the
// permission mode, model and effort the box shows. id names it until it is
// found (see findCodexStart). The question waits until then: it goes
// through Codex's server with the effort, and with Codex reviewing the
// approval requests in auto, which as flags would keep the session off
// that server (see codexsession.PermissionArgs).
func (m *model) startCodexSession(folder, id, question string) (tea.Cmd, error) {
	if m.codexStartWaiting(folder) {
		return nil, errors.New("a Codex session is still starting in " + folderName(folder) +
			": send again once it is listed")
	}
	home, _ := os.UserHomeDir()
	c := &m.sessions.composer
	model, _ := c.Choices()
	st := &askStart{id: id, folder: folder, codex: true, turn: codexsession.TurnOptions{
		Effort: c.CodexStartEffort(), Reviewer: codexsession.PermissionReviewer(c.PermissionMode()),
	}}
	st.queued = []askQueued{{
		prompt: claudeask.Prompt(question, m.codexAskContext(), home), draft: c.Value(), mentions: c.Mentions(),
	}}
	args := append(codexsession.PermissionArgs(c.PermissionMode()), codexsession.ModelArgs(model)...)
	st.began = time.Now()
	paneID, err := m.launchParkedAgent(folder, codexLaunch(args...), managedAgentProviderCodex)
	if err != nil {
		return nil, err
	}
	st.paneID = paneID
	if m.ask.starts == nil {
		m.ask.starts = make(map[string]*askStart)
	}
	m.ask.starts[id] = st
	m.flashClaude("Started a background session in " + folderName(folder))
	return m.watchAskStart(id), nil
}

// codexStartFound settles a Codex start on the session it became: the
// session takes its place wherever the start's name was, its pane is
// noted, and what you sent it meanwhile is sent.
func (m *model) codexStartFound(st *askStart, s claudesession.Session) tea.Cmd {
	old := st.id
	delete(m.ask.starts, old)
	st.id = s.ID
	if m.sessions.pendingSelect == old {
		m.sessions.pendingSelect = s.ID
	}
	if m.dock.lastID == old {
		m.dock.lastID = s.ID
	}
	for folder, id := range m.ask.folderSessions {
		if id == old {
			m.ask.folderSessions[folder] = s.ID
		}
	}
	m.saveAskHistory()
	m.attachCodexPane(s.ID, st.paneID)
	m.refreshSessionsList()
	return m.sendQueued(s.DisplayTitle(), st)
}

// attachCodexPane notes the pane a Codex session runs in, and shows it there
// at once rather than after the next refresh.
func (m *model) attachCodexPane(id string, paneID int) {
	m.recordCodexPane(id, paneID)
	for _, p := range m.claude.panes {
		if p.PaneID != paneID {
			continue
		}
		for i := range m.claude.snapshot.Live {
			if s := &m.claude.snapshot.Live[i]; s.ID == id {
				s.TTY, s.Background = p.TTYName, false
			}
		}
	}
}

// sessionConversation is the last limit turns of a session's conversation,
// from Claude Code's transcript or Codex's rollout.
func sessionConversation(s claudesession.Session, limit int) ([]claudesession.Turn, error) {
	if s.IsCodex() {
		return codexsession.Conversation(s.TranscriptPath, limit)
	}
	return claudesession.Conversation(s.TranscriptPath, limit)
}
