package internal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
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
	require.GreaterOrEqual(t, at("── Remote"), 0, box)
	assert.Less(t, at("ime hold tests"), at("── Remote"), "below this computer's sessions")
	assert.Less(t, at("── Remote"), at(" b1 "))
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
	TeaUpdate(m, cmd())
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
	assert.Contains(t, v.Place, "Claude app")
	assert.Equal(t, "needs you: bring b2 back online", v.Now)
}
