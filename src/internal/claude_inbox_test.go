package internal

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// inboxTestModel has superfile in pane 10, "beside" running in pane 20 next
// to it and "hidden" waiting in the superfile-agents workspace (pane 30).
func inboxTestModel(t *testing.T) *model {
	t.Helper()
	m := newClaudeTestModel(t)
	TeaUpdate(m, nil)
	m.fullWidth, m.fullHeight = 160, 40
	refreshInbox(m, idleSessions(inboxTurnEnd))
	return m
}

// inboxTurnEnd is when the sessions' last turns ended, the same for every
// refresh unless a test moves it on.
var inboxTurnEnd = time.Now().Add(-time.Hour) //nolint:gochecknoglobals // fixed for the tests

func idleSessions(turnEnd time.Time) []claudesession.Session {
	return []claudesession.Session{
		{ID: "beside", Title: "fix sidebar", Live: true, TTY: "/dev/ttys020", LastTurnEnd: turnEnd,
			Status: claudesession.StatusIdle},
		{ID: "hidden", Title: "ime hold tests", Live: true, TTY: "/dev/ttys030", LastTurnEnd: turnEnd,
			Status: claudesession.StatusIdle},
	}
}

// refreshInbox applies a refresh the way a listing does, and returns what it
// would notify.
func refreshInbox(m *model, live []claudesession.Session) []string {
	var notified []string
	original := terminalNotify
	terminalNotify = func(text string) { notified = append(notified, text) }
	defer func() { terminalNotify = original }()
	m.claude.snapshot = claudesession.Snapshot{Live: live}
	m.claude.panes = tabPanes()
	m.claude.loaded = true
	m.recomputeClaudeUnread()
	if cmd := m.updateClaudeInbox(); cmd != nil {
		cmd()
	}
	return notified
}

func TestInboxToastsWhenASessionYouCantSeeNeedsYou(t *testing.T) {
	m := inboxTestModel(t)
	_, ok := m.visibleClaudeToast()
	require.False(t, ok, "what was already so when superfile started is no news")

	live := idleSessions(inboxTurnEnd)
	live[1].Status, live[1].WaitingFor = claudesession.StatusWaiting, "dialog open"
	live[1].PendingTool = "Bash(go test ./src/pkg/ime/...)"
	assert.Empty(t, refreshInbox(m, live), "superfile is focused: the toast is enough")

	toast, ok := m.visibleClaudeToast()
	require.True(t, ok)
	assert.Equal(t, "hidden", toast.id)
	view := ansi.Strip(m.View())
	assert.Contains(t, view, "⚑ ime hold tests · needs you")
	assert.Contains(t, view, "Bash(go test ./src/pkg/ime/...)")
	assert.Contains(t, view, "hidden")
	assert.Contains(t, view, "! open  esc later")
	m.fullWidth = 60
	narrow := ansi.Strip(m.View())
	assert.Contains(t, narrow, "· needs you", "the name gives way first")
	assert.Contains(t, ansi.Strip(m.claudeStatusSummary()), "⚑ 1 needs you")

	TeaUpdate(m, utils.TeaKeyMsg("esc"))
	_, ok = m.visibleClaudeToast()
	assert.False(t, ok, "esc puts it away for later")
	assert.Equal(t, claudesession.StatusWaiting, m.claude.snapshot.Live[1].Status)

	live = idleSessions(inboxTurnEnd)
	live[0].Status = claudesession.StatusWaiting
	refreshInbox(m, live)
	_, ok = m.visibleClaudeToast()
	assert.False(t, ok, "the session beside Marmota you can see for yourself")
}

func TestInboxToastGoesOnceTheSessionNoLongerWaits(t *testing.T) {
	m := inboxTestModel(t)
	live := idleSessions(inboxTurnEnd)
	live[1].Status = claudesession.StatusWaiting
	refreshInbox(m, live)
	_, ok := m.visibleClaudeToast()
	require.True(t, ok)

	refreshInbox(m, idleSessions(inboxTurnEnd))
	_, ok = m.visibleClaudeToast()
	assert.False(t, ok, "it was answered")
}

func TestInboxNotifiesWhileSuperfileIsntFocused(t *testing.T) {
	m := inboxTestModel(t)
	m.terminalFocused = false
	live := idleSessions(inboxTurnEnd)
	live[1].Status, live[1].PendingTool = claudesession.StatusWaiting, "Red or blue?"
	assert.Equal(t, []string{"⚑ ime hold tests needs you: Red or blue?"}, refreshInbox(m, live))

	live = idleSessions(time.Now())
	live[1].LastError = "Login expired · Please run /login"
	assert.Equal(t, []string{"✗ ime hold tests failed: Login expired · Please run /login"}, refreshInbox(m, live),
		"a failure counts more than the reply it also is")
}

func TestInboxToastsAFinishedReplyForAWhile(t *testing.T) {
	m := inboxTestModel(t)
	refreshInbox(m, idleSessions(time.Now()))
	toast, ok := m.visibleClaudeToast()
	require.True(t, ok)
	assert.Equal(t, inboxDone, toast.kind)
	assert.Equal(t, "hidden", toast.id, "the reply beside Marmota you saw")
	assert.Contains(t, ansi.Strip(m.View()), "✓ ime hold tests · done")

	m.claude.toast.until = time.Now().Add(-time.Second)
	_, ok = m.visibleClaudeToast()
	assert.False(t, ok, "it goes after a while; the reply stays unread for !")
	assert.True(t, m.claude.unread["hidden"])
}

func TestInboxKeyGoesToTheToastsSession(t *testing.T) {
	m := inboxTestModel(t)
	now := time.Now()
	live := idleSessions(inboxTurnEnd)
	live = append(live, claudesession.Session{ID: "older", Title: "older", Live: true, Status: claudesession.StatusIdle,
		LastTurnEnd: now.Add(-2 * time.Hour)})
	refreshInbox(m, live)
	live[2].LastTurnEnd = now.Add(-90 * time.Minute)
	live[1].Status = claudesession.StatusWaiting
	refreshInbox(m, live)
	require.True(t, m.claude.unread["older"], "an older reply waits too")

	wez := newFakeWezterm(t, superfileTab(), weztermPane{PaneID: 30, TabID: 30, WindowID: 30,
		Workspace: managedAgentHiddenWorkspace, TTYName: "/dev/ttys030", Size: weztermPaneSize{Rows: 24, Cols: 80}})
	m.claude.panes = wez.paneList()
	toast, ok := m.visibleClaudeToast()
	require.True(t, ok)
	m.toggleFooter = true
	TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.JumpToUnreadSession[0]))
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "! gives the Sessions box the keyboard")
	assert.Equal(t, toast.id, m.sessions.list.SelectedID(), "on the session the toast is about")
	_, ok = m.visibleClaudeToast()
	assert.False(t, ok, "and puts the toast away")
}

func TestWaitingAge(t *testing.T) {
	now := time.Now()
	assert.Empty(t, waitingAge(now, time.Time{}))
	assert.Empty(t, waitingAge(now, now))
	assert.Equal(t, "40s", waitingAge(now, now.Add(-40*time.Second)))
	assert.Equal(t, "4m", waitingAge(now, now.Add(-4*time.Minute)))
}

func TestInboxToastsEveryReplyYouHaventSeen(t *testing.T) {
	m := inboxTestModel(t)
	refreshInbox(m, idleSessions(inboxTurnEnd.Add(time.Minute)))
	require.True(t, m.claude.unread["hidden"])
	m.claude.toast = nil

	// Another reply while the first is still unread is news all the same.
	refreshInbox(m, idleSessions(inboxTurnEnd.Add(2*time.Minute)))
	toast, ok := m.visibleClaudeToast()
	require.True(t, ok)
	assert.Equal(t, inboxDone, toast.kind)

	m.claude.toast = nil
	refreshInbox(m, idleSessions(inboxTurnEnd.Add(2*time.Minute)))
	_, ok = m.visibleClaudeToast()
	assert.False(t, ok, "nothing new since")
}

// A reply the preview shows you, as while the Sessions box is on its
// session, is read: no toast waits for it, and it doesn't count as unread.
// With the file's preview there instead, it is news as before.
func TestAReplyShownInThePreviewIsRead(t *testing.T) {
	m := inboxTestModel(t)
	if !m.toggleFooter {
		m.toggleFooterController()
	}
	m.fileModel.filePreview.SetOpen(true)
	m.setFilePreviewPanelSize()
	m.refreshSessionsList()
	pressKey(m, common.Hotkeys.JumpToUnreadSession[0])
	require.True(t, m.sessions.list.Select("hidden"))

	assert.Empty(t, refreshInbox(m, idleSessions(inboxTurnEnd.Add(time.Minute))))
	assert.False(t, m.claude.unread["hidden"])
	assert.Nil(t, m.claude.toast)

	pressKey(m, "esc") // to the files: the file's preview
	refreshInbox(m, idleSessions(inboxTurnEnd.Add(2*time.Minute)))
	assert.True(t, m.claude.unread["hidden"])
	require.NotNil(t, m.claude.toast)
	assert.Equal(t, inboxDone, m.claude.toast.kind)
}
