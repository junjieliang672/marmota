package internal

import (
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudeask"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// The Ask bar sends what you write to the session the list selects, typed
// into its pane; after n, or when the folder has none, to a new session it
// starts in the background. The session beside superfile, and the
// keyboard, stay where they are. Sessions it starts run in the permission
// mode the bar shows (bypass unless you change it). The session it sends to
// becomes the folder's: where alt+k writes next time. What you send a new
// session before its first reply waits, and is typed into it then.

// Test seams.
var (
	// newAskSessionID names a session the Ask bar starts.
	newAskSessionID = uuid.NewString //nolint:gochecknoglobals // test seam
)

// askState is where the Ask bar writes from each folder.
type askState struct {
	// loaded is set once the history has been read from disk.
	loaded bool
	// defaultModel and defaultEffort are Claude Code's, from its settings,
	// read as the Ask box opens; codexModel Codex's.
	defaultModel, defaultEffort string
	codexModel                  string
	// folderSessions is each folder's session: where alt+k writes.
	folderSessions map[string]string
	// change is a running Codex session's model and effort chosen in the
	// box, sent with the next message to it.
	change sessionChange
	// starts are the sessions the bar started that it still watches, by id.
	starts map[string]*askStart
	// resume is the ended session the bar resumes in place of starting one
	// (ask_resume), nil when it starts one.
	resume *resumeTarget
}

// resumeState is what the list of sessions to resume read: the ended
// sessions of any day.
type resumeState struct {
	ended []claudesession.Session
}

// sessionChange is a model and effort chosen for a running session; ""
// keeps its own.
type sessionChange struct {
	id, model, effort string
}

// askStart is a session the Ask bar started in the background, watched until
// Claude Code lists it, and while what you sent it meanwhile waits for its
// first reply. Until it is listed the Sessions box shows it as a row of its
// own, with what it asks first, such as whether to trust its folder (see
// starting_sessions.go).
type askStart struct {
	id     string
	paneID int
	folder string
	polls  int
	// title is what you asked it, to name its row.
	title string
	// asks is what its pane asks before it starts, "" for nothing, since
	// asksSince; dialog is that question and its choices, as they show.
	asks      string
	asksSince time.Time
	dialog    []string
	// listed is set once Claude Code lists it.
	listed bool
	// queued is what you sent it before its first reply, oldest first.
	queued []askQueued
	// began is when it was started. codex is set for a Codex session, whose
	// id isn't known until it is found: the first in its folder since began
	// (see findCodexStart). turn is what its first prompts sent through
	// Codex's server set.
	began time.Time
	codex bool
	turn  codexsession.TurnOptions
	// resumed is set for an ended session resumed: its id is known from
	// the start, a Codex one's too.
	resumed bool
}

// askQueued is a request waiting for the session the bar started.
type askQueued struct {
	prompt string
	// draft and mentions are the request as written, for the bar to get
	// back when it couldn't be sent.
	draft    string
	mentions map[string]string
}

// askStartTickMsg is a look at the pane of a session the Ask bar started.
type askStartTickMsg struct {
	id     string
	screen string
	err    error
}

const (
	// askStartPoll is how often superfile looks at a session the Ask bar
	// started; after askStartGiveUp looks it stops.
	askStartPoll   = time.Second
	askStartGiveUp = 600
)

// askDelegatedMsg reports a question typed into a running session.
type askDelegatedMsg struct {
	title string
	// draft and mentions are the question as written, for the bar to get
	// back when it couldn't be sent.
	draft    string
	mentions map[string]string
	err      error
}

// freshGitStatusCmd reads the folder's git status again. Files change
// outside superfile, by a session for one, and the status superfile keeps
// would tell Claude they hadn't.
func (m *model) freshGitStatusCmd() tea.Cmd {
	dir := m.getFocusedFilePanel().location
	reqCnt := m.ioReqCnt
	m.ioReqCnt++
	return func() tea.Msg {
		return NewGitStatusUpdateMsg(sidebar.FetchGitStatus(dir), reqCnt)
	}
}

// askSending says what sending does, for the Ask bar to show.
func (m *model) askSending() askui.Sending {
	if st := m.awaitedAskStart(); st != nil {
		if st.asks != "" {
			return askui.Sending{Text: "it asks you something first (" + firstKey(common.Hotkeys.OpenAskBar) +
				" in the list); sent once it has replied", Fixed: true}
		}
		return askui.Sending{Text: "sent once the new session has replied", Fixed: true}
	}
	mode := "⏵⏵ " + claudeask.PermissionModeLabel(m.sessions.composer.PermissionMode())
	s, ok := m.selectedSession()
	if !ok || m.sessions.list.NewSelected() {
		sending := askui.Sending{Text: "starts in the background, " + mode, New: true,
			DefaultModel: m.ask.defaultModel, DefaultEffort: m.ask.defaultEffort}
		if r, ok := m.resumeTargetFor(); ok {
			sending.Text = "resumes " + r.title + " in the background, " + mode + " (enter alone: beside Marmota)"
			sending.Resume = r.title
		}
		if m.askStartsCodex() {
			sending.DefaultModel, sending.DefaultEffort = m.ask.codexModel, m.sessions.composer.CodexStartEffort()
			if sending.DefaultModel == "" {
				sending.DefaultModel = "codex default"
			}
		}
		return sending
	}
	if s.IsCodex() {
		return m.codexSending(s)
	}
	if _, inPane := m.weztermPaneForSession(s); !s.Live || !inPane {
		return askui.Sending{Text: "can't reach it: it is no longer in a WezTerm pane", Fixed: true}
	}
	if s.Status == claudesession.StatusWaiting {
		return askui.Sending{Text: "it asks you something: " + firstKey(common.Hotkeys.OpenAskBar) +
			" in the list answers it", Fixed: true}
	}
	return askui.Sending{Text: "typed into its pane (" + m.sessionPlace(s) + ")", Fixed: true,
		Model: s.Model, Effort: s.Effort}
}

// sendAsk sends the question. When it can't be sent, it stays in the bar
// with the reason.
func (m *model) sendAsk() tea.Cmd {
	c := &m.sessions.composer
	question := strings.TrimSpace(c.Value())
	if question == "" {
		if r, ok := m.resumeTargetFor(); ok && m.askSending().New {
			return m.resumeFromAsk(r)
		}
		return nil
	}
	here := m.sessionsHere()
	var target string
	var cmd tea.Cmd
	var err error
	switch s, ok := m.selectedSession(); {
	case m.awaitedAskStart() != nil:
		st := m.awaitedAskStart()
		target = st.id
		m.queueForStart(st, m.questionFor(question, st.folder))
	case ok && !m.sessions.list.NewSelected():
		target = s.ID
		cmd, err = m.sendAskTo(s, m.questionFor(question, s.Cwd))
	case m.ask.resume != nil:
		r, ok := m.resumeTargetFor()
		if !ok {
			m.ask.resume = nil
			err = errors.New("that session is no longer listed: ctrl+r picks another")
			break
		}
		target = r.id
		if cmd, err = m.resumeInBackground(r, m.questionFor(question, here)); err == nil {
			m.sessions.pendingSelect = target
			c.ResetChoices()
		}
	default:
		target = newAskSessionID()
		if m.askStartsCodex() {
			target = newCodexStartID()
		}
		if cmd, err = m.startSession(here, target, m.questionFor(question, here)); err == nil {
			m.sessions.pendingSelect = target
			c.ResetChoices() // the next new session starts from the defaults again
		}
	}
	if err != nil {
		slog.Error("Could not send from the Ask bar", "error", err)
		c.SetNotice(err.Error(), true)
		return nil
	}
	c.Sent(question)
	if m.ask.folderSessions == nil {
		m.ask.folderSessions = make(map[string]string)
	}
	m.ask.folderSessions[here] = target
	m.saveAskHistory()
	m.refreshSessionsList()
	return cmd
}

// sendAskTo types the question into a listed session.
func (m *model) sendAskTo(s claudesession.Session, question string) (tea.Cmd, error) {
	switch {
	case !s.Live:
		return nil, errors.New(s.DisplayTitle() + " has ended")
	case s.Status == claudesession.StatusWaiting:
		return nil, errors.New(s.DisplayTitle() + " asks you something: " + firstKey(common.Hotkeys.OpenAskBar) +
			" in the list answers it")
	case s.IsCodex():
		return m.sendToCodexSession(s, question), nil
	}
	return m.typeIntoSession(s, question)
}

// questionFor writes the question's file mentions for a session working in
// cwd: relative to it when inside it, else whole.
func (m *model) questionFor(question, cwd string) string {
	mentions := m.sessions.composer.Mentions()
	texts := make([]string, 0, len(mentions))
	for text := range mentions {
		texts = append(texts, text)
	}
	// Longest first, so a mention inside another one is left alone.
	sort.Slice(texts, func(i, j int) bool { return len(texts[i]) > len(texts[j]) })
	refs := make([]string, len(texts))
	for i, text := range texts {
		path := mentions[text]
		info, err := os.Stat(path)
		refs[i] = formatAgentReferences([]agentPath{{path: path, dir: err == nil && info.IsDir()}}, cwd)
		question = strings.ReplaceAll(question, text, "\x00"+strconv.Itoa(i)+"\x00")
	}
	for i, ref := range refs {
		question = strings.ReplaceAll(question, "\x00"+strconv.Itoa(i)+"\x00", ref)
	}
	return question
}

// typeIntoSession types the question into a running session's prompt and
// sends it, setting aside anything typed there (see sendToClaudePrompt).
// Claude Code queues it while it is busy.
func (m *model) typeIntoSession(s claudesession.Session, question string) (tea.Cmd, error) {
	pane, ok := m.weztermPaneForSession(s)
	if !ok {
		return nil, errors.New(s.DisplayTitle() + " isn't in a WezTerm pane Marmota can type into")
	}
	home, _ := os.UserHomeDir()
	prompt := claudeask.Prompt(question, m.askContext(), home)
	title, paneID := s.DisplayTitle(), pane.PaneID
	draft, mentions := m.sessions.composer.Value(), m.sessions.composer.Mentions()
	m.flashClaude("Sending to " + title + "…")
	return func() tea.Msg {
		_, err := sendToClaudePrompt(paneID, prompt)
		return askDelegatedMsg{title: title, draft: draft, mentions: mentions, err: err}
	}, nil
}

func (m *model) applyAskDelegated(msg askDelegatedMsg) {
	if msg.err == nil {
		m.flashClaude("Sent to " + msg.title)
		return
	}
	slog.Error("Could not send the question to Claude", "session", msg.title, "error", msg.err)
	c := &m.sessions.composer
	if strings.TrimSpace(c.Value()) == "" {
		c.SetValue(msg.draft)
		c.SetMentions(msg.mentions)
	}
	c.SetNotice("Couldn't send to "+msg.title+": "+msg.err.Error(), true)
	m.flashClaude("Couldn't send to " + msg.title + ": " + msg.err.Error())
}

// startSession starts a session with the question in the background, in the
// permission mode the bar shows (bypass unless you change it), and watches
// it until it is listed. id is the new session's id.
func (m *model) startSession(folder, id, question string) (tea.Cmd, error) {
	if _, err := currentWeztermPaneID(); err != nil {
		return nil, errors.New("starting a session needs Marmota running in WezTerm")
	}
	if m.askStartsCodex() {
		return m.startCodexSession(folder, id, question)
	}
	home, _ := os.UserHomeDir()
	prompt := claudeask.Prompt(question, m.askContext(), home)
	c := &m.sessions.composer
	model, effort := c.Choices()
	parts := []string{"claude", "--session-id", shellQuote(id)}
	for _, arg := range append(claudeask.PermissionArgs(c.PermissionMode()), claudeask.ChoiceArgs(model, effort)...) {
		parts = append(parts, shellQuote(arg))
	}
	command := strings.Join(append(parts, "--", shellQuote(prompt)), " ")
	paneID, err := m.launchParkedAgent(folder, command, managedAgentProviderClaude)
	if err != nil {
		return nil, err
	}
	if m.ask.starts == nil {
		m.ask.starts = make(map[string]*askStart)
	}
	m.ask.starts[id] = &askStart{id: id, paneID: paneID, folder: folder, title: oneLine(c.Value()),
		began: time.Now()}
	m.flashClaude("Started a background session in " + folderName(folder))
	return m.watchAskStart(id), nil
}

// awaitedAskStart is the session the bar started that it writes to, while
// what you send it waits for its first reply, or nil: its row, or New until
// the row is there; once it is listed, while something already waits.
func (m *model) awaitedAskStart() *askStart {
	if m.sessions.list.NewSelected() {
		return m.ask.starts[m.sessions.pendingSelect]
	}
	if st := m.ask.starts[m.sessions.list.SelectedID()]; st != nil && (!st.listed || len(st.queued) > 0) {
		return st
	}
	return nil
}

// queueForStart keeps a request for a session the bar started until its
// first reply. Typed in sooner, it could meet the folder's trust question,
// or the screen Claude Code draws as it starts.
func (m *model) queueForStart(st *askStart, question string) {
	home, _ := os.UserHomeDir()
	c := &m.sessions.composer
	ctx := m.askContext()
	if st.codex {
		ctx = m.codexAskContext()
	}
	st.queued = append(st.queued, askQueued{
		prompt:   claudeask.Prompt(question, ctx, home),
		draft:    c.Value(),
		mentions: c.Mentions(),
	})
	m.flashClaude("Queued for the new session until it has replied")
}

func (m *model) watchAskStart(id string) tea.Cmd {
	st := m.ask.starts[id]
	if st == nil {
		return nil
	}
	paneID, listed := st.paneID, st.listed
	return tea.Tick(askStartPoll, func(time.Time) tea.Msg {
		if listed {
			return askStartTickMsg{id: id} // the listing says how it is
		}
		screen, err := weztermGetText(paneID)
		return askStartTickMsg{id: id, screen: screen, err: err}
	})
}

// applyAskStartTick watches a session the Ask bar started until Claude Code
// lists it, and types in what you sent it meanwhile once it has replied.
// Until then it reads what the session asks first, such as whether its
// folder is trusted, for its row and the preview.
func (m *model) applyAskStartTick(msg askStartTickMsg) tea.Cmd {
	st := m.ask.starts[msg.id]
	if st == nil {
		return nil
	}
	if st.codex && !st.resumed {
		if s, ok := m.findCodexStart(st); ok {
			return m.codexStartFound(st, s)
		}
	} else if s, ok := m.claude.snapshot.Find(st.id); ok && s.Live {
		st.listed = true
		switch {
		case len(st.queued) == 0:
			m.endAskStart(st)
			return nil
		case s.Status == claudesession.StatusIdle && (!s.LastTurnEnd.IsZero() || s.LastError != ""):
			// It has replied, or failed to: its prompt is drawn and free.
			m.endAskStart(st)
			return m.sendQueued(s.DisplayTitle(), st)
		}
		return m.watchAskStart(st.id) // still on its first reply, or asking you something
	}
	if st.listed {
		m.endAskStart(st)
		m.giveBackQueued(st.queued, "The new session in "+folderName(st.folder)+" ended first")
		return nil
	}
	st.polls++
	switch {
	case msg.err != nil: // its pane has gone
		m.endAskStart(st)
		m.giveBackQueued(st.queued, "The new session in "+folderName(st.folder)+" closed before it started")
		return nil
	case st.polls >= askStartGiveUp:
		m.endAskStart(st)
		m.giveBackQueued(st.queued, "The new session in "+folderName(st.folder)+" hasn't started")
		return nil
	}
	dialog, asks := startDialog(msg.screen)
	return tea.Batch(m.watchAskStart(st.id), m.setStartDialog(st, dialog, asks))
}

// endAskStart stops watching a session the bar started.
func (m *model) endAskStart(st *askStart) {
	delete(m.ask.starts, st.id)
	if q := m.sessions.question; q.start && q.id == st.id {
		m.forgetClaudeQuestion()
	}
	if m.sessions.pendingSelect == st.id {
		m.sessions.pendingSelect = ""
	}
	m.refreshSessionsList()
}

// showStartingSession brings a session the bar started in beside superfile,
// with the keyboard: enter on its row, to see all it shows.
func (m *model) showStartingSession(started *askStart) {
	superfilePaneID, err := currentWeztermPaneID()
	if err == nil {
		err = m.foregroundManagedPane(superfilePaneID, started.paneID)
	}
	if err == nil {
		err = activateWeztermPane(started.folder, started.paneID)
	}
	if err != nil {
		slog.Error("Could not show the new session", "pane", started.paneID, "error", err)
		m.flashClaude("Couldn't bring the new session beside Marmota: " + err.Error())
		return
	}
	m.flashClaude("The new session in " + folderName(started.folder) + " is beside Marmota")
}

// sendQueued types what you sent a new session before its first reply into
// its pane, oldest first.
func (m *model) sendQueued(title string, started *askStart) tea.Cmd {
	if len(started.queued) == 0 {
		return nil
	}
	queued, paneID := started.queued, started.paneID
	m.flashClaude("Sending to " + title + "…")
	if started.codex {
		prompts := make([]string, len(queued))
		for i, q := range queued {
			prompts[i] = q.prompt
		}
		draft, mentions := joinQueued(queued)
		return sendToCodex(started.id, title, prompts, draft, mentions, started.turn)
	}
	return func() tea.Msg {
		for i, q := range queued {
			if _, err := sendToClaudePrompt(paneID, q.prompt); err != nil {
				draft, mentions := joinQueued(queued[i:])
				return askDelegatedMsg{title: title, draft: draft, mentions: mentions, err: err}
			}
		}
		return askDelegatedMsg{title: title}
	}
}

// giveBackQueued puts requests that weren't sent back in the bar, when it is
// empty, and says why.
func (m *model) giveBackQueued(queued []askQueued, why string) {
	if len(queued) == 0 {
		return
	}
	c := &m.sessions.composer
	if strings.TrimSpace(c.Value()) == "" {
		draft, mentions := joinQueued(queued)
		c.SetValue(draft)
		c.SetMentions(mentions)
	}
	c.SetNotice(why+": your request wasn't sent", true)
	m.flashClaude(why)
}

// joinQueued is the text and mentions of requests, for the bar.
func joinQueued(queued []askQueued) (string, map[string]string) {
	drafts := make([]string, 0, len(queued))
	mentions := map[string]string{}
	for _, q := range queued {
		drafts = append(drafts, q.draft)
		maps.Copy(mentions, q.mentions)
	}
	return strings.Join(drafts, "\n"), mentions
}

// askContext is what the focused panel shows, for the request.
func (m *model) askContext() claudeask.Context {
	panel := m.getFocusedFilePanel()
	c := claudeask.Context{Folder: panel.location, ShowHidden: m.toggleDotFile}
	if item := panel.getSelectedItemPtr(); item != nil {
		c.Cursor = relativeName(panel.location, item.location)
		c.CursorDir = item.directory
	}
	if panel.panelMode == selectMode {
		for _, e := range panel.element {
			if _, ok := panel.selected[e.location]; ok {
				c.Selected = append(c.Selected, relativeName(panel.location, e.location)+dirMark(e.directory))
			}
		}
	}
	if opts := panel.sortOptions.data; opts.selected >= 0 && opts.selected < len(opts.options) {
		kind := opts.options[opts.selected]
		c.Sort = strings.ToLower(kind) + " " + sortArrow(kind, opts.reversed)
	}
	if !panel.searchStack.IsEmpty() {
		if top := panel.searchStack.Peek(); top != nil {
			c.Filter = top.searchTerm
		}
	}
	if gs := m.sidebarModel.GetGitStatus(); gs.IsGitRepo && gs.LastPath == panel.location {
		c.InGit, c.GitBranch, c.GitChanged = true, gs.BranchName, gs.StagedCount+gs.UnstagedCount
	}
	c.Notes = nearestClaudeNotes(panel.location)
	return c
}

func relativeName(folder, path string) string {
	if rel, err := filepath.Rel(folder, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

func dirMark(dir bool) string {
	if dir {
		return "/"
	}
	return ""
}

// nearestClaudeNotes is the CLAUDE.md nearest to dir, in it or above it:
// the instructions Claude Code reads there.
func nearestClaudeNotes(dir string) string {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		for _, name := range []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")} {
			if info, err := os.Stat(filepath.Join(d, name)); err == nil && !info.IsDir() {
				return filepath.Join(d, name)
			}
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// askHistoryFile is what is kept between runs of superfile: what was sent,
// the question being written, and the bar's choices.
// The model, effort and agent a new session gets aren't kept: each starts
// from the defaults (Claude Code, its default model, medium effort).
type askHistoryFile struct {
	Prompts        []string          `json:"prompts"`
	PermissionMode string            `json:"permissionMode,omitempty"`
	Draft          string            `json:"draft,omitempty"`
	Mentions       map[string]string `json:"mentions,omitempty"`
	FolderSessions map[string]string `json:"folderSessions,omitempty"`
}

func (m *model) loadAskHistory() {
	if m.ask.loaded {
		return
	}
	m.ask.loaded = true
	if m.ask.folderSessions == nil {
		m.ask.folderSessions = make(map[string]string)
	}
	data, err := os.ReadFile(variable.AskHistoryFile)
	if err != nil {
		return
	}
	var h askHistoryFile
	if err := json.Unmarshal(data, &h); err != nil {
		slog.Debug("Ignoring an unreadable Ask history", "error", err)
		return
	}
	c := &m.sessions.composer
	c.SetHistory(h.Prompts)
	c.SetPermissionMode(h.PermissionMode)
	if c.Value() == "" {
		c.SetValue(h.Draft)
		c.SetMentions(h.Mentions)
	}
	for folder, id := range h.FolderSessions {
		m.ask.folderSessions[folder] = id
	}
}

func (m *model) saveAskHistory() {
	if !m.ask.loaded {
		return // nothing was read, so nothing changed
	}
	c := &m.sessions.composer
	data, err := json.Marshal(askHistoryFile{
		Prompts: c.History(), PermissionMode: c.PermissionMode(),
		Draft: c.Value(), Mentions: c.Mentions(), FolderSessions: m.ask.folderSessions,
	})
	if err == nil {
		err = os.MkdirAll(filepath.Dir(variable.AskHistoryFile), utils.ConfigDirPerm)
	}
	if err == nil {
		err = os.WriteFile(variable.AskHistoryFile, data, utils.ConfigFilePerm)
	}
	if err != nil {
		slog.Debug("Could not save the Ask history", "error", err)
	}
}
