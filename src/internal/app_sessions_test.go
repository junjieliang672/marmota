package internal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudecloud"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// appTestModel is dockTestModel with claude.ai listing b1, waiting for you
// on another computer; b2, whose computer is off; and s20, the session
// beside Marmota, which Remote Control also lists.
func appTestModel(t *testing.T) *model {
	t.Helper()
	m := dockTestModel(t)
	for i := range m.claude.snapshot.Live {
		if m.claude.snapshot.Live[i].ID == "s20" {
			m.claude.snapshot.Live[i].RemoteURL = "https://claude.ai/code/session_01here"
		}
	}
	m.applyAppSessions(appSessionsMsg{at: time.Now(), sessions: []claudecloud.Session{
		{ID: "cse_01here", Title: "provider registry", Bucket: "working", Connected: true},
		{ID: "cse_01b1", Title: "b1", Bucket: "blocked", Connected: true, Repo: "sec-algo/redteam-root",
			Branch: "arch/agent-swarm", NeedsAction: "bring b2 back online"},
		{ID: "cse_01b2", Title: "b2", Bucket: "completed", Repo: "sec-algo/redteam-root", Branch: "main"},
	}})
	return m
}

// claude.ai often answers before this computer's sessions are listed, and
// lists those too: its rows wait, or the box would show this computer's
// sessions as remote ones for a moment, and change height twice.
func TestRemoteRowsWaitForThisComputersSessions(t *testing.T) {
	setForTest(t, &appSessionsAutoRefresh, true)
	m := appTestModel(t)
	listed := len(m.dockSessions())
	require.Equal(t, 4, listed, "two here, two remote: provider registry isn't listed twice")
	local := m.claude.snapshot

	// A start: claude.ai has answered, this computer's sessions aren't listed.
	m.claude.loaded, m.claude.snapshot = false, claudesession.Snapshot{}
	m.dock.rows, m.dock.rowsRead = listed, true // what the last run left
	m.refreshSessionsList()
	require.True(t, m.app.answered)
	assert.Empty(t, m.dockSessions(), "no remote row yet: three would show, one of them this computer's")
	assert.False(t, m.sessionsListed())
	assert.Equal(t, listed, m.dockSessionLineCount(), "the box keeps the height it had")

	m.claude.loaded, m.claude.snapshot = true, local
	m.refreshSessionsList()
	assert.Len(t, m.dockSessions(), listed, "listed: all of them at once")
	assert.Equal(t, listed, m.dockSessionLineCount(), "and the box is as tall")
}

func TestTheRemoteSectionListsClaudeAppSessionsBelowThisComputers(t *testing.T) {
	m := appTestModel(t)
	var ids []string
	for _, s := range m.dockSessions() {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{"s20", "s50", "app:cse_01b1", "app:cse_01b2"}, ids,
		"this computer's first; s20 is left out of the Remote section, the box lists it above")

	box := ansi.Strip(sessionsBoxRender(m))
	lines := strings.Split(box, "\n")
	at := func(s string) int {
		for i, l := range lines {
			if strings.Contains(l, s) {
				return i
			}
		}
		return -1
	}
	assert.Equal(t, -1, at("── Remote"), "no title: each row's cloud says where it runs")
	assert.Equal(t, at("ime hold tests")+1, at(" b1 "), "right below this computer's sessions, a row each")
	assert.GreaterOrEqual(t, at("needs you: bring b2"), 0, box)
	assert.GreaterOrEqual(t, at("offline · redteam-root · main"), 0, box)
}

func TestTheSessionOnThisComputerIsKnownByItsBridgeID(t *testing.T) {
	m := appTestModel(t)
	m.applyAppSessions(appSessionsMsg{at: time.Now(), bridge: map[string]string{"s50": "session_01b1"},
		sessions: m.app.listed})
	for _, s := range m.dockSessions() {
		assert.NotEqual(t, "app:cse_01b1", s.ID, "s50 runs here: it is listed once, above")
	}
}

func TestEnterOpensARemoteSessionInTheClaudeApp(t *testing.T) {
	m := appTestModel(t)
	var opened []string
	openAppURL = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openAppURL = func(string) error { return errors.New("tests must not open the Claude app") } })

	m.focusDockSessionsOn("app:cse_01b1")
	cmd := m.dockSessionsKey(utils.TeaKeyMsg(common.Hotkeys.Confirm[0]))
	require.NotNil(t, cmd, "opening runs off the screen's loop")
	assert.Empty(t, opened, "not before the command runs")
	runCmds(m, cmd)
	assert.Equal(t, []string{"claude://claude.ai/code/session_01b1"}, opened)
	assert.Equal(t, dockSessionsFocus, m.focusPanel)
}

func TestKeysThatActOnASessionSayRemoteOnesAreInTheApp(t *testing.T) {
	m := appTestModel(t)
	m.focusDockSessionsOn("app:cse_01b1")
	pressKey(m, common.Hotkeys.SessionRename[0])
	assert.False(t, m.sessions.list.IsRenaming())
	assert.Contains(t, m.claude.flash, "Claude app")
	pressKey(m, common.Hotkeys.SessionStop[0])
	assert.Contains(t, m.claude.flash, "Claude app")
	assert.Contains(t, ansi.Strip(m.dockStatusLine()), "open in the Claude app")
}

// s stops at sessions that need you and at those that replied unread, here
// and in the Remote section.
func TestBangStopsAtUnreadAndRemoteSessionsToo(t *testing.T) {
	m := appTestModel(t)
	m.claude.unread = map[string]bool{"s50": true}
	m.refreshSessionsList()

	toSessions(m)
	assert.Equal(t, "s50", m.sessions.list.SelectedID(), "replied, unread")
	toSessions(m)
	assert.Equal(t, "app:cse_01b1", m.sessions.list.SelectedID(), "b1 needs you, on another computer")
	toSessions(m)
	assert.Equal(t, "s50", m.sessions.list.SelectedID(), "round at the end")
}

func TestAFailedListingKeepsTheLastOneGreyedWithWhy(t *testing.T) {
	m := appTestModel(t)
	m.applyAppSessions(appSessionsMsg{err: errors.New("dial tcp: timeout")})
	assert.Len(t, m.appSessions(), 2, "the last list stays")
	assert.Contains(t, ansi.Strip(sessionsBoxRender(m)), "can't reach claude.ai · ")

	m.applyAppSessions(appSessionsMsg{err: claudecloud.ErrExpired})
	assert.Contains(t, ansi.Strip(sessionsBoxRender(m)), "login expired: run claude")
}

func TestNotLoggedInSaysHowToLogIn(t *testing.T) {
	m := dockTestModel(t)
	m.applyAppSessions(appSessionsMsg{err: claudecloud.ErrNoLogin})
	assert.Contains(t, ansi.Strip(sessionsBoxRender(m)), "not logged in: run claude")
}

// The Sessions box is always on screen, so claude.ai is asked whether or not
// Details and the Ask box are put away.
func TestTheRefreshAsksOnceAtATime(t *testing.T) {
	m := dockTestModel(t)
	appSessionsAutoRefresh = true
	t.Cleanup(func() { appSessionsAutoRefresh = false })
	calls := 0
	listAppSessions = func(_ context.Context) ([]claudecloud.Session, error) { calls++; return nil, nil }
	t.Cleanup(func() {
		listAppSessions = func(context.Context) ([]claudecloud.Session, error) {
			return nil, errors.New("tests must not ask claude.ai")
		}
	})

	cmd := m.appSessionsRefreshCmd()
	require.NotNil(t, cmd)
	assert.Nil(t, m.appSessionsRefreshCmd(), "one listing at a time")
	assert.Equal(t, 0, calls, "asked off the screen's loop")
	TeaUpdate(m, cmd())
	assert.Equal(t, 1, calls)

	m.toggleFooterController()
	require.False(t, m.toggleFooter)
	assert.NotNil(t, m.appSessionsRefreshCmd(), "with Details put away too: the Sessions box stays")
}

func TestThePreviewOfARemoteSessionSaysWhereItIs(t *testing.T) {
	m := appTestModel(t)
	m.focusDockSessionsOn("app:cse_01b1")
	v := m.sessionsPaneView()
	assert.Equal(t, "b1", v.Title)
	assert.Equal(t, "on another computer", v.Place)
	assert.Equal(t, "needs you: bring b2 back online", v.About[0])
	assert.Contains(t, v.About[1], "Claude app")
}

func TestRemoteSessionsListTheLatestFirstAndTheOfflineOnesLast(t *testing.T) {
	m := appTestModel(t)
	now := time.Now()
	m.applyAppSessions(appSessionsMsg{at: now, sessions: []claudecloud.Session{
		{ID: "cse_01old", Title: "old", Connected: true, Updated: now.Add(-time.Hour)},
		{ID: "cse_01off", Title: "off", Updated: now},
		{ID: "cse_01new", Title: "new", Connected: true, Updated: now.Add(-time.Minute)},
	}})
	var ids []string
	for _, s := range m.appSessions() {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{"app:cse_01new", "app:cse_01old", "app:cse_01off"}, ids)
}

func TestARemoteSessionOpenedInTheAppIsReadAndAnsweredUntilItMovesOn(t *testing.T) {
	m := appTestModel(t)
	openAppURL = func(string) error { return nil }
	t.Cleanup(func() { openAppURL = func(string) error { return errors.New("tests must not open the Claude app") } })
	at := time.Now().Add(-time.Minute)
	m.applyAppSessions(appSessionsMsg{at: time.Now(), sessions: []claudecloud.Session{
		{ID: "cse_01b1", Title: "b1", Bucket: "blocked", Unread: true, Connected: true, Updated: at,
			NeedsAction: "bring b2 back online"},
	}})
	row, _ := m.appSessionByID("app:cse_01b1")
	require.Equal(t, sessionsui.NeedsYou, m.sessionState(row))

	m.focusDockSessionsOn("app:cse_01b1")
	cmd := m.dockSessionsKey(utils.TeaKeyMsg(common.Hotkeys.Confirm[0]))
	row, _ = m.appSessionByID("app:cse_01b1")
	assert.NotEqual(t, sessionsui.NeedsYou, m.sessionState(row), "answered in the app: s goes on")
	assert.False(t, row.App.Unread)
	assert.NotContains(t, row.App.Doing, "needs you")
	runCmds(m, cmd)

	m.applyAppSessions(appSessionsMsg{at: time.Now(), sessions: []claudecloud.Session{
		{ID: "cse_01b1", Title: "b1", Bucket: "blocked", Unread: true, Connected: true, Updated: at},
	}})
	row, _ = m.appSessionByID("app:cse_01b1")
	assert.NotEqual(t, sessionsui.NeedsYou, m.sessionState(row), "the same event, listed again")

	m.applyAppSessions(appSessionsMsg{at: time.Now(), sessions: []claudecloud.Session{
		{ID: "cse_01b1", Title: "b1", Bucket: "blocked", Unread: true, Connected: true, Updated: time.Now()},
	}})
	row, _ = m.appSessionByID("app:cse_01b1")
	assert.Equal(t, sessionsui.NeedsYou, m.sessionState(row), "a later event needs you again")
}

func TestARemoteSessionThatFailedToOpenStaysUnread(t *testing.T) {
	m := appTestModel(t)
	m.applyAppSessions(appSessionsMsg{at: time.Now(), sessions: []claudecloud.Session{
		{ID: "cse_01b2", Title: "b2", Bucket: "completed", Unread: true, Connected: true},
	}})
	m.focusDockSessionsOn("app:cse_01b2")
	runCmds(m, m.dockSessionsKey(utils.TeaKeyMsg(common.Hotkeys.Confirm[0])))
	row, _ := m.appSessionByID("app:cse_01b2")
	assert.True(t, row.App.Unread)
}

func TestEnterOnARemoteSessionInTheGoToListOpensItInTheClaudeApp(t *testing.T) {
	m := appTestModel(t)
	var opened []string
	openAppURL = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openAppURL = func(string) error { return errors.New("tests must not open the Claude app") } })

	pressKey(m, common.Hotkeys.GoTo[0])
	typeInto(m, "b1")
	cmd := TeaUpdate(m, utils.TeaKeyMsg("enter"))
	require.NotNil(t, cmd, "opening runs off the screen's loop")
	runCmds(m, cmd)
	assert.False(t, m.jumpUI.IsOpen())
	assert.Equal(t, []string{"claude://claude.ai/code/session_01b1"}, opened)
}

// runCmds runs cmd, and those of a batch, handing their messages to m.
func runCmds(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmds(m, c)
		}
	case nil:
	default:
		TeaUpdate(m, msg)
	}
}

func TestTheGoToListShowsEachSessionsStateAndWhatARemoteOneWaitsFor(t *testing.T) {
	m := appTestModel(t)
	pressKey(m, common.Hotkeys.GoTo[0])
	view := ansi.Strip(m.jumpUI.Render(true))
	var b1 string
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, " b1 ") {
			b1 = l
		}
	}
	require.NotEmpty(t, b1, view)
	assert.Contains(t, b1, "⚑ cc", "its mark, as in the Sessions box")
	assert.Contains(t, b1, "b2 back online", "what it waits for, in its folder's place, as much as the sidebar holds")
}

// listedWith is a listing as the first one since Marmota started brings it,
// with the file of seen sessions as it was read.
func listedWith(seen map[string]appSeen, sessions ...claudecloud.Session) appSessionsMsg {
	return appSessionsMsg{at: time.Now(), sessions: sessions, seen: seen, seenRead: true}
}

func TestMarmotaOpensWithTheRemoteSessionsRead(t *testing.T) {
	m := appTestModel(t)
	at := time.Now().Add(-time.Hour)
	runCmds(m, m.applyAppSessions(listedWith(nil,
		claudecloud.Session{ID: "cse_01b2", Title: "b2", Bucket: "completed", Unread: true, Connected: true,
			Updated: at})))
	row, _ := m.appSessionByID("app:cse_01b2")
	assert.False(t, row.App.Unread, "claude.ai says unread of what was there before Marmota started")
	assert.FileExists(t, variable.AppSessionsSeenFile, "and a restart remembers it")

	m.applyAppSessions(appSessionsMsg{at: time.Now(), sessions: []claudecloud.Session{
		{ID: "cse_01b2", Title: "b2", Bucket: "completed", Unread: true, Connected: true, Updated: time.Now()},
		{ID: "cse_01new", Title: "new", Bucket: "completed", Unread: true, Connected: true, Updated: at},
	}})
	row, _ = m.appSessionByID("app:cse_01b2")
	assert.True(t, row.App.Unread, "it replied since")
	row, _ = m.appSessionByID("app:cse_01new")
	assert.True(t, row.App.Unread, "one that turns up while Marmota runs: claude.ai's word goes")
}

func TestARestartRemembersWhatWasSeenAndOpened(t *testing.T) {
	m := appTestModel(t)
	at := time.Now().Add(-time.Hour)
	seen := map[string]appSeen{"cse_01b1": {At: at, Opened: true}, "cse_01b2": {At: at.Add(-time.Hour)}}
	m.applyAppSessions(listedWith(seen,
		claudecloud.Session{ID: "cse_01b1", Title: "b1", Bucket: "blocked", Unread: true, Connected: true,
			Updated: at, NeedsAction: "bring b2 back online"},
		claudecloud.Session{ID: "cse_01b2", Title: "b2", Bucket: "completed", Unread: true, Connected: true,
			Updated: at}))
	row, _ := m.appSessionByID("app:cse_01b1")
	assert.NotEqual(t, sessionsui.NeedsYou, m.sessionState(row), "opened before the restart: answered")
	row, _ = m.appSessionByID("app:cse_01b2")
	assert.True(t, row.App.Unread, "it replied while Marmota was closed")
}

func TestARemoteSessionIsShownWithoutThisComputersNameOrAConversation(t *testing.T) {
	m := appTestModel(t)
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	m.focusDockSessionsOn("app:cse_01b2")
	view := ansi.Strip(viewText(m))
	assert.NotContains(t, view, thisComputerName(), "the Ask box doesn't say it runs here")
	assert.Contains(t, view, "on another computer")
	assert.Contains(t, view, "offline · on another computer", "the preview says it is offline")
	assert.NotContains(t, view, "pgup/pgdown", "no conversation to scroll: it is in the Claude app")
	assert.NotContains(t, view, "waiting for your next prompt · on another", "nor that it waits")
}

func TestTheGoToListSaysARemoteSessionsStateOnce(t *testing.T) {
	m := appTestModel(t)
	pressKey(m, common.Hotkeys.GoTo[0])
	typeInto(m, "b1")
	view := ansi.Strip(m.jumpUI.Render(true))
	assert.Equal(t, 1, strings.Count(view, "needs you"), view)
	assert.Contains(t, view, "b1 · opens in the Claude app", "the foot line says where enter goes")
}

// The tutorial's screenshots show a Remote section of their own: its
// sessions are read from a file, and claude.ai isn't asked.
func TestTheRemoteSectionIsReadFromAFileWhenOneIsNamed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "remote.json")
	require.NoError(t, os.WriteFile(file, []byte(`[
		{"ID": "cse_1", "Title": "Nightly station import", "Bucket": "working", "Connected": true,
		 "Repo": "example/tidepool", "Branch": "main", "Updated": "2026-11-18T10:00:00Z"},
		{"ID": "cse_2", "Title": "Print the Duxbury map", "Bucket": "blocked", "NeedsAction": "permission prompt"}
	]`), 0o600))

	got, err := appSessionsFromFile(file)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "Nightly station import", got[0].Title)
	assert.True(t, got[0].Connected)
	assert.Equal(t, "example/tidepool", got[0].Repo)
	assert.Equal(t, 2026, got[0].Updated.Year())
	assert.Equal(t, "permission prompt", got[1].NeedsAction)

	_, err = appSessionsFromFile(filepath.Join(t.TempDir(), "none.json"))
	require.Error(t, err)
}
