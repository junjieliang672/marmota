package internal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

func newClaudeTestModel(t *testing.T) *model {
	t.Helper()
	setupWeztermStateDirForTest(t)
	t.Setenv("WEZTERM_PANE", "10")
	// The list's order is saved as sessions come and go; a test's own file
	// keeps it from leaking into the next test.
	orderFile := variable.ClaudeSessionsOrderFile
	variable.ClaudeSessionsOrderFile = filepath.Join(t.TempDir(), "order.json") //nolint:reassign // its own file
	t.Cleanup(func() { variable.ClaudeSessionsOrderFile = orderFile })          //nolint:reassign // restored
	m := defaultTestModel(t.TempDir())
	m.claude.seen = claudesession.LoadSeen(filepath.Join(t.TempDir(), "seen.json"))
	return m
}

func tabPanes() []weztermPane {
	return []weztermPane{
		{WindowID: 9, TabID: 1, PaneID: 10, Size: weztermPaneSize{Rows: 53, Cols: 74}},
		{
			WindowID: 9,
			TabID:    1,
			PaneID:   20,
			LeftCol:  75,
			TTYName:  "/dev/ttys020",
			Size:     weztermPaneSize{Rows: 53, Cols: 148},
		},
		{WindowID: 12, TabID: 2, PaneID: 30, TTYName: "/dev/ttys030", Workspace: managedAgentHiddenWorkspace,
			Size: weztermPaneSize{Rows: 53, Cols: 148}},
		{WindowID: 3, TabID: 7, PaneID: 50, TTYName: "/dev/ttys050", Size: weztermPaneSize{Rows: 40, Cols: 100}},
	}
}

func TestOpenClaudeSession_ResumesPastSessionInBypassMode(t *testing.T) {
	m := newClaudeTestModel(t)
	sessionDir := t.TempDir()
	m.claude.snapshot = claudesession.Snapshot{Past: []claudesession.Session{
		{ID: "past1", Cwd: sessionDir, Title: "Old work", Status: claudesession.StatusEnded, RemoteControl: true},
	}}

	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, Size: weztermPaneSize{Rows: 53, Cols: 223}},
		)},
		fakeExecResult{retCode: 0, output: "20\n"},
		fakeExecResult{retCode: 0, output: ""},
	)

	m.openClaudeSession("past1")

	require.Len(t, *calls, 3)
	assert.Equal(t, "wezterm cli activate-pane --pane-id 10", (*calls)[2].cmd,
		"the new pane took the keyboard; superfile gets it back")
	assert.Contains(t, (*calls)[1].cmd, "--cwd \""+sessionDir+"\"", "resumes in the session's own folder")
	assert.True(t, strings.HasSuffix((*calls)[1].cmd,
		"-- claude --resume past1 --dangerously-skip-permissions --remote-control"),
		"bypass by default, and Remote Control comes back on for a Remote Control session: %s", (*calls)[1].cmd)
	assert.Equal(t, "Resumed Old work", m.claude.flash)
}

func TestOpenClaudeSession_ShowsHiddenManagedPaneBesideSuperfile(t *testing.T) {
	m := newClaudeTestModel(t)
	state := weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20},
		FixedSuperfileCols: 74,
		Sessions: []managedAgentSession{
			{PaneID: 20, Provider: managedAgentProviderClaude, Cwd: "/tmp/a", VisibleSlot: managedAgentSlotOne},
			{PaneID: 30, Provider: managedAgentProviderClaude, Cwd: "/tmp/b"},
		},
	}
	require.NoError(t, saveWeztermPaneLayoutState(10, state))
	m.setManagedWeztermState(10, state)
	m.claude.panes = tabPanes()
	m.claude.snapshot = claudesession.Snapshot{Live: []claudesession.Session{
		{ID: "s30", Title: "hidden one", Live: true, Status: claudesession.StatusIdle, TTY: "/dev/ttys030"},
	}}

	before := marshalWeztermPanesForTest(tabPanes()...)
	afterPark := marshalWeztermPanesForTest(
		weztermPane{WindowID: 9, TabID: 1, PaneID: 10, Size: weztermPaneSize{Rows: 53, Cols: 223}},
		weztermPane{WindowID: 13, TabID: 3, PaneID: 20, Workspace: managedAgentHiddenWorkspace},
		weztermPane{WindowID: 12, TabID: 2, PaneID: 30, Workspace: managedAgentHiddenWorkspace},
	)
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: before}, // refresh
		fakeExecResult{retCode: 0, output: before}, // foreground: inspect
		fakeExecResult{retCode: 0, output: ""},     // park the shown pane
		fakeExecResult{retCode: 0, output: afterPark},
		fakeExecResult{retCode: 0, output: "30\n"}, // pull the hidden pane in
		fakeExecResult{retCode: 0, output: ""},     // give the keyboard back to superfile
	)

	m.openClaudeSession("s30")

	require.Len(t, *calls, 6)
	assert.Equal(t, "wezterm cli activate-pane --pane-id 10", (*calls)[5].cmd,
		"showing a session leaves the keyboard in superfile")
	assert.Equal(t, "wezterm cli move-pane-to-new-tab --pane-id 20 --new-window --workspace superfile-agents",
		(*calls)[2].cmd, "the shown session is parked in the hidden workspace, not in a visible tab")
	assert.Equal(t, "wezterm cli split-pane --pane-id 10 --right --cells 148 --move-pane-id 30", (*calls)[4].cmd)
	assert.Equal(t, 30, m.managedWeztermState.ForegroundPaneID)
	assert.Contains(t, m.claude.flash, "Showing hidden one")

	shown, _, ok := m.visibleClaudeSessionPane()
	require.True(t, ok)
	assert.Equal(t, "s30", shown.ID, "the local pane list follows the swap before the next refresh")
	parked, _ := m.weztermPaneForSession(claudesession.Session{TTY: "/dev/ttys020"})
	assert.Equal(t, -1, parked.TabID, "the pane that was shown is known to be parked")
}

func TestOpenClaudeSession_FocusesSessionAlreadyBesideSuperfile(t *testing.T) {
	m := newClaudeTestModel(t)
	m.claude.panes = tabPanes()
	m.claude.snapshot = claudesession.Snapshot{Live: []claudesession.Session{
		{ID: "s20", Title: "shown", Live: true, TTY: "/dev/ttys020"},
	}}
	calls := installFakeExec(t, fakeExecResult{retCode: 0, output: ""})

	m.openClaudeSession("s20")

	require.Len(t, *calls, 1)
	assert.Equal(t, "wezterm cli activate-pane --pane-id 20", (*calls)[0].cmd)
	assert.Equal(t, "Focused shown", m.claude.flash)
}

func TestOpenClaudeSession_AttachesBackgroundSession(t *testing.T) {
	m := newClaudeTestModel(t)
	m.claude.snapshot = claudesession.Snapshot{Live: []claudesession.Session{
		{ID: "bg1", Name: "nightly lint", Title: "Nightly lint", Live: true, Background: true},
	}}
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, Size: weztermPaneSize{Rows: 53, Cols: 223}},
		)},
		fakeExecResult{retCode: 0, output: "21\n"},
		fakeExecResult{retCode: 0, output: ""},
	)

	m.openClaudeSession("bg1")

	require.Len(t, *calls, 3)
	assert.True(t, strings.HasSuffix((*calls)[1].cmd, "-- claude attach 'nightly lint'"), (*calls)[1].cmd)
	assert.Equal(t, "Attached to Nightly lint", m.claude.flash)
}

func TestOpenClaudeSession_ExplainsSessionsOutsideWezterm(t *testing.T) {
	m := newClaudeTestModel(t)
	m.claude.snapshot = claudesession.Snapshot{Live: []claudesession.Session{
		{ID: "term", Title: "in iTerm", Live: true, PID: 42, TTY: "/dev/ttys099"},
	}}
	m.openClaudeSession("term")
	assert.Equal(t, "in iTerm isn't running in WezTerm (pid 42)", m.claude.flash)

	m.openClaudeSession("gone")
	assert.Equal(t, "That session is no longer listed", m.claude.flash)
}

func TestApplySnapshot_FlagsNewTurnsUnlessTheSessionIsOnScreen(t *testing.T) {
	m := newClaudeTestModel(t)
	t1 := time.Now().Add(-time.Hour)
	t2 := t1.Add(time.Minute)
	snapshot := func(turn time.Time) claudeSessionsSnapshotMsg {
		return claudeSessionsSnapshotMsg{
			snapshot: claudesession.Snapshot{Live: []claudesession.Session{
				{
					ID:            "hidden",
					Title:         "hidden",
					Live:          true,
					TTY:           "/dev/ttys030",
					LastTurnEnd:   turn,
					RemoteControl: true,
				},
				{ID: "shown", Title: "shown", Live: true, TTY: "/dev/ttys020", LastTurnEnd: turn},
			}},
			panes:       tabPanes(),
			panesListed: true,
		}
	}

	m.applyClaudeSessionsSnapshot(snapshot(t1))
	assert.Empty(t, m.claude.unread, "sessions seen for the first time aren't unread")

	m.applyClaudeSessionsSnapshot(snapshot(t2))
	assert.Equal(t, map[string]bool{"hidden": true}, m.claude.unread,
		"a new turn is unread, except in the session shown beside Marmota")

	summary := ansi.Strip(m.claudeStatusSummary())
	assert.Contains(t, summary, "● 1 unread (!)")
	assert.NotContains(t, summary, "remote", "Remote Control sessions aren't counted separately")
}

// A fork has no transcript before its first message, so its pane title,
// which Claude Code sets to the conversation's title, names it.
func TestApplySnapshot_NamesNewSessionsAfterTheirPaneTitle(t *testing.T) {
	m := newClaudeTestModel(t)
	panes := tabPanes()
	panes[1].Title = "✳ Hello.txt file creation"
	panes[2].Title = "✳ Claude Code"
	m.applyClaudeSessionsSnapshot(claudeSessionsSnapshotMsg{
		snapshot: claudesession.Snapshot{Live: []claudesession.Session{
			{ID: "fork", Name: "quokka-98", Live: true, TTY: "/dev/ttys020"},
			{ID: "empty", Name: "project-12", Live: true, TTY: "/dev/ttys030"},
			{ID: "titled", Title: "from the transcript", Live: true, TTY: "/dev/ttys050"},
		}},
		panes:       panes,
		panesListed: true,
	})

	title := func(id string) string {
		s, ok := m.claude.snapshot.Find(id)
		require.True(t, ok)
		return s.DisplayTitle()
	}
	assert.Equal(t, "Hello.txt file creation", title("fork"))
	assert.Equal(t, "project-12", title("empty"), "a new conversation's pane says only Claude Code")
	assert.Equal(t, "from the transcript", title("titled"))
}

func TestClaudePaneConversationTitle(t *testing.T) {
	for title, want := range map[string]string{
		"✳ Fix the login bug": "Fix the login bug",
		"⠂ Fix the login bug": "Fix the login bug",
		"✳ Claude Code":       "",
		"superfile":           "",
		"zsh in ~/repo":       "",
		"":                    "",
	} {
		assert.Equal(t, want, claudePaneConversationTitle(title), title)
	}
}

func TestSendSelectionToAgent_TypesMentionsIntoTheSessionBesideSuperfile(t *testing.T) {
	setupWeztermStateDirForTest(t)
	t.Setenv("WEZTERM_PANE", "10")
	root := t.TempDir()
	utils.SetupDirectories(t, filepath.Join(root, "assets"))
	utils.SetupFiles(t, filepath.Join(root, "notes.md"))
	m := defaultTestModel(root)
	m.claude.seen = claudesession.LoadSeen(filepath.Join(t.TempDir(), "seen.json"))
	TeaUpdate(m, nil)
	m.claude.panes = tabPanes()
	m.claude.snapshot = claudesession.Snapshot{Live: []claudesession.Session{
		{ID: "s20", Title: "shown", Live: true, TTY: "/dev/ttys020", Cwd: root},
	}}

	var sentPane int
	var sentText string
	original := weztermSendText
	weztermSendText = func(paneID int, text string) error {
		sentPane, sentText = paneID, text
		return nil
	}
	t.Cleanup(func() { weztermSendText = original })
	calls := installFakeExec(t, fakeExecResult{retCode: 0, output: ""})

	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), filepath.Join(root, "assets"))
	m.sendSelectionToAgent()

	assert.Equal(t, 20, sentPane)
	assert.Equal(t, " @assets/", sentText,
		"folders are sent relative to the session, with a trailing slash, after a space that keeps them apart "+
			"from what the prompt holds")
	require.Len(t, *calls, 1)
	assert.Equal(t, "wezterm cli activate-pane --pane-id 20", (*calls)[0].cmd, "then the pane is focused for typing")
	assert.Equal(t, "Sent 1 item to shown", m.claude.flash)
}

func TestSendSelectionToAgent_NeedsASessionBesideSuperfile(t *testing.T) {
	m := newClaudeTestModel(t)
	TeaUpdate(m, nil)
	m.claude.panes = tabPanes()
	utils.SetupFiles(t, filepath.Join(m.getFocusedFilePanel().location, "a.txt"))
	TeaUpdate(m, nil)

	m.sendSelectionToAgent()
	assert.Equal(t, "No session next to Marmota. Pick one with ! then enter, or start one with >claude or >codex",
		m.claude.flash)
}

func TestFormatAgentReferences(t *testing.T) {
	got := formatAgentReferences([]agentPath{
		{path: "/repo/src/main.go"},
		{path: "/repo/assets", dir: true},
		{path: "/repo/my notes.md"},
		{path: "/elsewhere/x.txt"},
	}, "/repo")
	assert.Equal(t, `@src/main.go @assets/ @"my notes.md" @/elsewhere/x.txt`, got, "no space before or after")
	assert.Equal(t, "@/a/b/", formatAgentReferences([]agentPath{{path: "/a/b", dir: true}}, ""))
}

func TestReconcileParksStrayHiddenPanesInHiddenWorkspace(t *testing.T) {
	setupWeztermStateDirForTest(t)
	state := weztermPaneLayoutState{
		ManagedPaneIDs: []int{20},
		Sessions: []managedAgentSession{
			{PaneID: 20, Provider: managedAgentProviderClaude, VisibleSlot: managedAgentSlotOne},
			{PaneID: 40, Provider: managedAgentProviderClaude},
		},
	}
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, Workspace: "default"},
			weztermPane{TabID: 1, PaneID: 20, LeftCol: 75, Workspace: "default"},
			// parked by an older superfile in an ordinary, visible tab
			weztermPane{TabID: 4, PaneID: 40, Workspace: "default"},
		)},
		fakeExecResult{retCode: 0, output: ""},
	)

	_, err := reconcileManagedAgentSessionsWithWezterm("/tmp", 10, state)

	require.NoError(t, err)
	require.Len(t, *calls, 2)
	assert.Equal(t, "wezterm cli move-pane-to-new-tab --pane-id 40 --new-window --workspace superfile-agents",
		(*calls)[1].cmd)
}

func TestApplySnapshot_IgnoresPaneListsFromBeforeASwap(t *testing.T) {
	m := newClaudeTestModel(t)
	m.claude.panes = tabPanes()
	stale := claudeSessionsSnapshotMsg{panes: []weztermPane{{PaneID: 99}}, panesListed: true,
		layoutGen: m.claude.layoutGen}

	m.markPaneTabLocally(30) // superfile moved a pane while the refresh ran
	m.applyClaudeSessionsSnapshot(stale)

	_, ok := m.weztermPaneForSession(claudesession.Session{TTY: "/dev/ttys030"})
	assert.True(t, ok, "the pane list from before the swap is not applied")

	fresh := claudeSessionsSnapshotMsg{panes: []weztermPane{{PaneID: 99, TTYName: "/dev/ttys099"}},
		panesListed: true, layoutGen: m.claude.layoutGen}
	m.applyClaudeSessionsSnapshot(fresh)
	_, ok = m.weztermPaneForSession(claudesession.Session{TTY: "/dev/ttys099"})
	assert.True(t, ok, "a refresh started after the swap is applied")
}

// A refresh that couldn't ask WezTerm for its panes keeps the ones known
// before: else every session would drop out of the list until the next one.
func TestApplySnapshot_KeepsThePanesWhenWeztermCantBeAsked(t *testing.T) {
	m := newClaudeTestModel(t)
	m.claude.panes = tabPanes()

	m.applyClaudeSessionsSnapshot(claudeSessionsSnapshotMsg{layoutGen: m.claude.layoutGen})

	_, ok := m.weztermPaneForSession(claudesession.Session{TTY: "/dev/ttys030"})
	assert.True(t, ok)
}

func TestClaudeSessionsRefreshCmdCollectsSessions(t *testing.T) {
	claudeSessionsAutoRefresh = true
	t.Cleanup(func() { claudeSessionsAutoRefresh = false })
	original := claudeSessionRunner
	claudeSessionRunner = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "claude" {
			return []byte(
				`[{"pid":"1","cwd":"/x","kind":"interactive","sessionId":"live1","name":"x","status":"busy"}]`,
			), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { claudeSessionRunner = original })

	m := newClaudeTestModel(t)
	cmd := m.claudeSessionsRefreshCmd()
	require.NotNil(t, cmd)
	assert.Nil(t, m.claudeSessionsRefreshCmd(), "only one refresh runs at a time")

	msg, ok := cmd().(claudeSessionsSnapshotMsg)
	require.True(t, ok)
	require.Len(t, msg.snapshot.Live, 1)
	assert.NotNil(t, m.applyClaudeSessionsSnapshot(msg), "the next refresh is scheduled")
	assert.False(t, m.claude.inflight)
	assert.Contains(t, ansi.Strip(m.claudeStatusSummary()), "✳ 1 working")
}

// A refresh reads the day's finished sessions; older ones are left out.
func TestARefreshReadsTodaysFinishedSessions(t *testing.T) {
	claudeSessionsAutoRefresh = true
	t.Cleanup(func() { claudeSessionsAutoRefresh = false })
	projects := t.TempDir()
	originalDir := claudeProjectsDir
	claudeProjectsDir = func() string { return projects }
	t.Cleanup(func() { claudeProjectsDir = originalDir })
	m := newClaudeTestModel(t)
	folder := m.getFocusedFilePanel().location
	yesterday := time.Now().Add(-30 * time.Hour)
	transcript := func(id string, at time.Time, extra ...string) {
		path := filepath.Join(projects, claudesession.ProjectSlug(folder), id+".jsonl")
		lines := append([]string{`{"type":"ai-title","aiTitle":"` + id + `","cwd":"` + folder + `"}`}, extra...)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
		require.NoError(t, os.Chtimes(path, at, at))
	}
	transcript("today", time.Now())
	transcript("yesterday", yesterday)

	msg, ok := m.claudeSessionsRefreshCmd()().(claudeSessionsSnapshotMsg)
	require.True(t, ok)
	var ids []string
	for _, s := range msg.snapshot.Past {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{"today"}, ids)
}
