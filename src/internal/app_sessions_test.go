package internal

import (
	"context"
	"errors"
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

func TestTheRemoteSectionListsClaudeAppSessionsBelowThisComputers(t *testing.T) {
	m := appTestModel(t)
	var ids []string
	for _, s := range m.dockSessions() {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{"s20", "s50", "app:cse_01b1", "app:cse_01b2"}, ids,
		"this computer's first; s20 is left out of the Remote section, the box lists it above")

	box := ansi.Strip(m.dockSessionsRender())
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
	assert.Equal(t, at("ime hold tests")+2, at(" b1 "), "right below this computer's sessions")
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

// ! stops at sessions that need you and at those that replied unread, here
// and in the Remote section.
func TestBangStopsAtUnreadAndRemoteSessionsToo(t *testing.T) {
	m := appTestModel(t)
	m.claude.unread = map[string]bool{"s50": true}
	m.refreshSessionsList()

	bang(m)
	assert.Equal(t, "s50", m.sessions.list.SelectedID(), "replied, unread")
	bang(m)
	assert.Equal(t, "app:cse_01b1", m.sessions.list.SelectedID(), "b1 needs you, on another computer")
	bang(m)
	assert.Equal(t, "s50", m.sessions.list.SelectedID(), "round at the end")
}

func TestAFailedListingKeepsTheLastOneGreyedWithWhy(t *testing.T) {
	m := appTestModel(t)
	m.applyAppSessions(appSessionsMsg{err: errors.New("dial tcp: timeout")})
	assert.Len(t, m.appSessions(), 2, "the last list stays")
	assert.Contains(t, ansi.Strip(m.dockSessionsRender()), "can't reach claude.ai · ")

	m.applyAppSessions(appSessionsMsg{err: claudecloud.ErrExpired})
	assert.Contains(t, ansi.Strip(m.dockSessionsRender()), "login expired: run claude")
}

func TestNotLoggedInSaysHowToLogIn(t *testing.T) {
	m := dockTestModel(t)
	m.applyAppSessions(appSessionsMsg{err: claudecloud.ErrNoLogin})
	assert.Contains(t, ansi.Strip(m.dockSessionsRender()), "not logged in: run claude")
}

func TestTheRefreshAsksOnceAtATimeAndOnlyWithTheDockShown(t *testing.T) {
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
	assert.Nil(t, m.appSessionsRefreshCmd(), "not while the dock is hidden")
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
	assert.NotEqual(t, sessionsui.NeedsYou, m.sessionState(row), "answered in the app: ! goes on")
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
	assert.Contains(t, b1, "bring b2 back online", "what it waits for, in its folder's place")
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
