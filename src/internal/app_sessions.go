package internal

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudecloud"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// The Remote section of the Sessions box: the account's sessions on other
// computers, as claude.ai lists them (Remote Control ones, the Claude app's
// list). Marmota only shows them; enter opens one in the Claude app, where
// everything else is done. Sessions on this computer stay in the box above,
// so those claude.ai lists too are left out here.

const (
	// appSessionsInterval is how often claude.ai is asked, while the dock
	// is shown.
	appSessionsInterval = 15 * time.Second
	// appSessionsTimeout bounds one listing.
	appSessionsTimeout = 10 * time.Second
	// appIDPrefix starts the id of a row of the Remote section, which is
	// claude.ai's id: it never meets a Claude Code session's.
	appIDPrefix = "app:"
	// remoteSectionTitle heads the Remote section.
	remoteSectionTitle = "Remote"
	// inTheClaudeApp is what keys that act on a session say on one of the
	// Remote section.
	inTheClaudeApp = "It runs on another computer: enter opens it in the Claude app"
)

var (
	// appSessionsAutoRefresh turns asking claude.ai off in tests.
	appSessionsAutoRefresh = true //nolint:gochecknoglobals // test seam
	// listAppSessions asks claude.ai for the account's sessions.
	listAppSessions = func(ctx context.Context) ([]claudecloud.Session, error) { //nolint:gochecknoglobals // test seam
		return claudecloud.Client{HTTP: &http.Client{Timeout: appSessionsTimeout}}.List(ctx)
	}
	// openAppURL opens a session in the Claude app, else on claude.ai.
	openAppURL = openURLDetached //nolint:gochecknoglobals // test seam
)

// appSessionsState is what the Remote section knows.
type appSessionsState struct {
	// listed is claude.ai's last list, bridge this computer's sessions'
	// claude.ai ids by their own, as last read.
	listed []claudecloud.Session
	bridge map[string]string
	// err is why the last listing failed, and at when one last worked.
	err      error
	at       time.Time
	inflight bool
}

type appSessionsTickMsg struct{}

type appSessionsMsg struct {
	sessions []claudecloud.Session
	bridge   map[string]string
	err      error
	at       time.Time
}

type appOpenedMsg struct{ err error }

func scheduleAppSessionsTick() tea.Cmd {
	return tea.Tick(appSessionsInterval, func(time.Time) tea.Msg { return appSessionsTickMsg{} })
}

// applyAppSessionsTick asks claude.ai again, and schedules the next tick.
func (m *model) applyAppSessionsTick() tea.Cmd {
	return tea.Batch(m.appSessionsRefreshCmd(), scheduleAppSessionsTick())
}

// appSessionsRefreshCmd asks claude.ai in the background, while the dock
// is shown and no listing is under way: the keychain, the request and
// reading this computer's sessions all happen off the screen's loop.
func (m *model) appSessionsRefreshCmd() tea.Cmd {
	if !appSessionsAutoRefresh || !m.toggleFooter || m.app.inflight {
		return nil
	}
	m.app.inflight = true
	projects := claudeProjectsDir()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), appSessionsTimeout)
		defer cancel()
		list, err := listAppSessions(ctx)
		return appSessionsMsg{sessions: list, err: err, at: time.Now(),
			bridge: claudesession.BridgeIDs(projects)}
	}
}

// applyAppSessions keeps a listing. One that failed keeps the last list,
// shown greyed with why.
func (m *model) applyAppSessions(msg appSessionsMsg) tea.Cmd {
	m.app.inflight = false
	m.app.bridge = msg.bridge
	if msg.err != nil {
		if m.app.err == nil || m.app.err.Error() != msg.err.Error() {
			slog.Warn("Could not list the Claude app's sessions", "error", msg.err)
		}
		m.app.err = msg.err
	} else {
		m.app.listed, m.app.err, m.app.at = msg.sessions, nil, msg.at
	}
	m.refreshSessionsList()
	return nil
}

// appSessions is the Remote section's sessions, as rows of the box: what
// claude.ai lists, but those on this computer, which the box lists above.
func (m *model) appSessions() []claudesession.Session {
	if len(m.app.listed) == 0 {
		return nil
	}
	here := map[string]bool{}
	mark := func(s claudesession.Session) {
		if id := m.app.bridge[s.ID]; id != "" {
			here[id] = true
		}
		if id := claudesession.LinkID(s.RemoteURL); id != "" {
			here[id] = true
		}
	}
	groups, _ := m.sessionsGroups()
	for _, g := range groups {
		for _, s := range g.Sessions {
			mark(s)
		}
	}
	for _, s := range m.claude.snapshot.Past {
		mark(s)
	}
	out := make([]claudesession.Session, 0, len(m.app.listed))
	for _, c := range m.app.listed {
		if !here[c.LinkID()] {
			out = append(out, appSession(c))
		}
	}
	return out
}

// appSession is a session claude.ai lists as a row of the box.
func appSession(c claudecloud.Session) claudesession.Session {
	s := claudesession.Session{ID: appIDPrefix + c.ID, Title: c.Title, Named: true, Live: true,
		LastActivity: c.Updated, Status: claudesession.StatusIdle,
		App: &claudesession.AppSession{URL: c.AppURL(), Offline: !c.Connected, Unread: c.Unread}}
	where := c.Repo[strings.LastIndex(c.Repo, "/")+1:]
	if c.Branch != "" {
		where = strings.TrimPrefix(joinWords(where, c.Branch), " · ")
	}
	switch {
	case !c.Connected:
		s.App.Doing = joinWords("offline", where)
	case c.Bucket == "blocked":
		s.Status = claudesession.StatusWaiting
		s.App.Doing = "needs you: " + firstNonEmpty(c.NeedsAction, where, "open it to see")
	case c.Bucket == "working":
		s.Status = claudesession.StatusWorking
		s.App.Doing = joinWords("working", where)
	case c.Unread:
		s.App.Doing = joinWords("replied, unread", where)
	default:
		s.App.Doing = firstNonEmpty(where, "waiting for your next prompt")
	}
	return s
}

func joinWords(a, b string) string {
	if b == "" {
		return a
	}
	return a + " · " + b
}

// appSessionByID is the Remote section's row with that id.
func (m *model) appSessionByID(id string) (claudesession.Session, bool) {
	if !strings.HasPrefix(id, appIDPrefix) {
		return claudesession.Session{}, false
	}
	for _, s := range m.appSessions() {
		if s.ID == id {
			return s, true
		}
	}
	return claudesession.Session{}, false
}

// appSectionNote is the line under the Remote section's title when claude.ai
// can't be asked: why, and how to fix it, short enough for the box (running
// claude logs it in, or renews its login). "" when it can.
func (m *model) appSectionNote() string {
	switch {
	case m.app.err == nil:
		return ""
	case errors.Is(m.app.err, claudecloud.ErrNoLogin):
		return "not logged in: run claude"
	case errors.Is(m.app.err, claudecloud.ErrExpired):
		return "login expired: run claude"
	case !m.app.at.IsZero():
		return "can't reach claude.ai · " + m.app.at.Format("15:04")
	}
	return "can't reach claude.ai"
}

// appKey handles a key on a row of the Remote section: enter opens it in
// the Claude app; the keys that act on a session say it's done there. It
// reports whether it took the key.
func (m *model) appKey(s claudesession.Session, key string) (tea.Cmd, bool) {
	switch {
	case slices.Contains(common.Hotkeys.Confirm, key):
		url := s.App.URL
		return func() tea.Msg { return appOpenedMsg{err: openAppURL(url)} }, true
	case slices.ContainsFunc([][]string{common.Hotkeys.OpenAskBar, common.Hotkeys.SessionMention,
		common.Hotkeys.SessionRename, common.Hotkeys.SessionFork, common.Hotkeys.SessionStop,
		common.Hotkeys.MoveSessionHere, common.Hotkeys.ShowSessionFiles, common.Hotkeys.CopyPath,
		common.Hotkeys.SendToShelf}, func(keys []string) bool { return slices.Contains(keys, key) }):
		m.flashClaude(inTheClaudeApp)
		return nil, true
	}
	return nil, false
}

func (m *model) applyAppOpened(msg appOpenedMsg) {
	if msg.err != nil {
		slog.Error("Could not open the session in the Claude app", "error", msg.err)
		m.flashClaude("Could not open it: " + msg.err.Error())
	}
}

// openURLDetached opens a session's claude:// link, which the Claude app
// takes; without the app, its claude.ai page in the browser.
func openURLDetached(url string) error {
	if runtime.GOOS != utils.OsDarwin {
		return startDetached("xdg-open", webURL(url))
	}
	if err := exec.Command("open", url).Run(); err != nil { //nolint:gosec // a link claude.ai gave
		return startDetached("open", webURL(url))
	}
	return nil
}

// webURL is the claude.ai page of a claude://claude.ai/… link.
func webURL(url string) string {
	if rest, ok := strings.CutPrefix(url, "claude://"); ok {
		return "https://" + rest
	}
	return url
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	utils.DetachFromTerminal(cmd)
	return cmd.Start()
}
