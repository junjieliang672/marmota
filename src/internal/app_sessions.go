package internal

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	variable "github.com/junjieliang672/marmota/src/config"
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
	// appSessionsInterval is how often claude.ai is asked.
	appSessionsInterval = 15 * time.Second
	// appSessionsTimeout bounds one listing.
	appSessionsTimeout = 10 * time.Second
	// appIDPrefix starts the id of a row of the Remote section, which is
	// claude.ai's id: it never meets a Claude Code session's.
	appIDPrefix = "app:"
	// remoteSectionTitle names the Remote section: its rows' cloud mark.
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
		if file := os.Getenv(appSessionsFileEnv); file != "" {
			return appSessionsFromFile(file)
		}
		return claudecloud.Client{HTTP: &http.Client{Timeout: appSessionsTimeout}}.List(ctx)
	}
	// openAppURL opens a session in the Claude app, else on claude.ai.
	openAppURL = openURLDetached //nolint:gochecknoglobals // test seam
)

// appSessionsFileEnv names a file the Remote section's sessions are read from
// in place of asking claude.ai, as JSON: a list of claudecloud.Session. The
// tutorial's screenshots are taken with one (testsuite/shots), so that no
// session of yours shows in a picture; nothing is asked of claude.ai then,
// and no login is read.
const appSessionsFileEnv = "MARMOTA_APP_SESSIONS_FILE"

func appSessionsFromFile(path string) ([]claudecloud.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sessions []claudecloud.Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

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
	// answered is set once a listing has come back, with sessions or with
	// why not.
	answered bool
	// seen is, by claude.ai id, how far each session has been seen, saved
	// in AppSessionsSeenFile; seenRead says it has been read from there.
	seen     map[string]appSeen
	seenRead bool
}

// appSeen is how far a session has been seen. claude.ai's unread is only
// cleared by opening the session in the Claude app or on claude.ai, so a
// session read anywhere else would stay unread: a session is unread only
// when claude.ai says so of an event after At. At is the last event of a
// session as it stood when Marmota first listed it (a session it first
// lists while running has none: claude.ai's word goes), or when enter
// opened it in the Claude app; then Opened, and it is answered too, if it
// needed you, until a later event.
type appSeen struct {
	At     time.Time `json:"at"`
	Opened bool      `json:"opened,omitempty"`
}

type appSessionsTickMsg struct{}

type appSessionsMsg struct {
	sessions []claudecloud.Session
	bridge   map[string]string
	err      error
	at       time.Time
	// seen is AppSessionsSeenFile, read with the first listing (seenRead).
	seen     map[string]appSeen
	seenRead bool
}

type appOpenedMsg struct {
	id  string
	was appSeen
	had bool
	err error
}

func scheduleAppSessionsTick() tea.Cmd {
	return tea.Tick(appSessionsInterval, func(time.Time) tea.Msg { return appSessionsTickMsg{} })
}

// applyAppSessionsTick asks claude.ai again, and schedules the next tick.
func (m *model) applyAppSessionsTick() tea.Cmd {
	return tea.Batch(m.appSessionsRefreshCmd(), scheduleAppSessionsTick())
}

// appSessionsRefreshCmd asks claude.ai in the background, while no listing
// is under way: the keychain, the request and
// reading this computer's sessions all happen off the screen's loop.
func (m *model) appSessionsRefreshCmd() tea.Cmd {
	if !appSessionsAutoRefresh || m.app.inflight {
		return nil
	}
	m.app.inflight = true
	projects, readSeen := claudeProjectsDir(), !m.app.seenRead
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), appSessionsTimeout)
		defer cancel()
		list, err := listAppSessions(ctx)
		msg := appSessionsMsg{sessions: list, err: err, at: time.Now(),
			bridge: claudesession.BridgeIDs(projects)}
		if readSeen {
			msg.seen, msg.seenRead = loadAppSeen(), true
		}
		return msg
	}
}

// loadAppSeen reads AppSessionsSeenFile; nil when there is none.
func loadAppSeen() map[string]appSeen {
	data, err := os.ReadFile(variable.AppSessionsSeenFile)
	if err != nil {
		return nil
	}
	var seen map[string]appSeen
	if err := json.Unmarshal(data, &seen); err != nil {
		slog.Warn("Could not read the Remote section's seen sessions", "error", err)
		return nil
	}
	return seen
}

// appSeenSaving keeps two saves from writing the file at once.
var appSeenSaving sync.Mutex

// saveAppSeenCmd writes AppSessionsSeenFile off the screen's loop.
func (m *model) saveAppSeenCmd() tea.Cmd {
	data, err := json.Marshal(m.app.seen)
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		appSeenSaving.Lock()
		defer appSeenSaving.Unlock()
		if err := os.MkdirAll(filepath.Dir(variable.AppSessionsSeenFile), 0o755); err != nil {
			slog.Error("Could not save the Remote section's seen sessions", "error", err)
			return nil
		}
		tmp := variable.AppSessionsSeenFile + ".tmp"
		err := os.WriteFile(tmp, append(data, '\n'), 0o600)
		if err == nil {
			err = os.Rename(tmp, variable.AppSessionsSeenFile)
		}
		if err != nil {
			slog.Error("Could not save the Remote section's seen sessions", "error", err)
		}
		return nil
	}
}

// applyAppSessions keeps a listing. One that failed keeps the last list,
// shown greyed with why.
func (m *model) applyAppSessions(msg appSessionsMsg) tea.Cmd {
	m.app.inflight, m.app.answered = false, true
	m.app.bridge = msg.bridge
	if msg.err != nil {
		if m.app.err == nil || m.app.err.Error() != msg.err.Error() {
			slog.Warn("Could not list the Claude app's sessions", "error", msg.err)
		}
		m.app.err = msg.err
	} else {
		m.app.listed, m.app.err, m.app.at = msg.sessions, nil, msg.at
	}
	var save tea.Cmd
	if msg.seenRead {
		m.app.seenRead = true
		for id, s := range msg.seen {
			if _, ok := m.app.seen[id]; !ok {
				m.setAppSeen(id, s)
			}
		}
	}
	if msg.err == nil && m.app.seenRead && m.noteAppSeen(msg.seenRead) {
		save = m.saveAppSeenCmd()
	}
	m.refreshSessionsList()
	return save
}

func (m *model) setAppSeen(id string, s appSeen) {
	if m.app.seen == nil {
		m.app.seen = map[string]appSeen{}
	}
	m.app.seen[id] = s
}

// noteAppSeen records the sessions listed for the first time: as seen up to
// their last event in the first listing since Marmota started, so it doesn't
// open with every session unread; with nothing seen when one turns up while
// it runs. It forgets those no longer listed, and reports a change.
func (m *model) noteAppSeen(first bool) bool {
	changed := false
	listed := map[string]bool{}
	for _, c := range m.app.listed {
		listed[c.ID] = true
		if _, ok := m.app.seen[c.ID]; ok {
			continue
		}
		s := appSeen{}
		if first {
			s.At = c.Updated
		}
		m.setAppSeen(c.ID, s)
		changed = true
	}
	for id := range m.app.seen {
		if !listed[id] {
			delete(m.app.seen, id)
			changed = true
		}
	}
	return changed
}

// appSessions is the Remote section's sessions, as rows of the box: what
// claude.ai lists, but those on this computer, which the box lists above.
func (m *model) appSessions() []claudesession.Session {
	// Not before this computer's sessions are listed: claude.ai lists those
	// too, and which of its rows are theirs isn't known until then. Shown
	// sooner, they would be rows of the Remote section for a moment, with
	// the ones their ended sessions leave on claude.ai, and then go.
	if len(m.app.listed) == 0 || !m.claude.loaded {
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
		if here[c.LinkID()] {
			continue
		}
		if seen, ok := m.app.seen[c.ID]; ok && !c.Updated.After(seen.At) {
			c.Unread = false
			if seen.Opened && c.Bucket == "blocked" {
				c.Bucket, c.NeedsAction = "", ""
			}
		}
		out = append(out, appSession(c))
	}
	// Those online first, the latest event first; the offline ones after.
	slices.SortStableFunc(out, func(a, b claudesession.Session) int {
		if a.App.Offline != b.App.Offline {
			if a.App.Offline {
				return 1
			}
			return -1
		}
		return b.LastActivity.Compare(a.LastActivity)
	})
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

// appDoingWithoutState is what appSession says a session does, without the
// state it starts with: "bring b2 back online" of "needs you: bring b2 back
// online", for a row that says the state apart.
func appDoingWithoutState(doing string) string {
	for _, state := range []string{"needs you: ", "offline · ", "working · ", "replied, unread · "} {
		if rest, ok := strings.CutPrefix(doing, state); ok {
			return rest
		}
	}
	switch doing {
	case "offline", "working", "replied, unread":
		return ""
	}
	return doing
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
		url, id := s.App.URL, strings.TrimPrefix(s.ID, appIDPrefix)
		was, had := m.app.seen[id]
		m.setAppSeen(id, appSeen{At: s.LastActivity, Opened: true}) // so ! goes on to the next one
		m.refreshSessionsList()
		return tea.Batch(m.saveAppSeenCmd(),
			func() tea.Msg { return appOpenedMsg{id: id, was: was, had: had, err: openAppURL(url)} }), true
	case slices.ContainsFunc([][]string{common.Hotkeys.OpenAskBar, common.Hotkeys.SessionMention,
		common.Hotkeys.SessionRename, common.Hotkeys.SessionFork, common.Hotkeys.SessionStop,
		common.Hotkeys.MoveSessionHere, common.Hotkeys.ShowSessionFiles, common.Hotkeys.CopyPath,
		common.Hotkeys.SendToShelf}, func(keys []string) bool { return slices.Contains(keys, key) }):
		m.flashClaude(inTheClaudeApp)
		return nil, true
	}
	return nil, false
}

func (m *model) applyAppOpened(msg appOpenedMsg) tea.Cmd {
	if msg.err == nil {
		return nil
	}
	if msg.had {
		m.app.seen[msg.id] = msg.was
	} else {
		delete(m.app.seen, msg.id)
	}
	m.refreshSessionsList()
	slog.Error("Could not open the session in the Claude app", "error", msg.err)
	m.flashClaude("Could not open it: " + msg.err.Error())
	return m.saveAppSeenCmd()
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
