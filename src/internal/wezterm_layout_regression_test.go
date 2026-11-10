package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// A session that exits leaves its pane id in the saved layout. Pruning has
// to drop that id too: normalizing used to bring it back as an unnamed
// session, so a resumed session started hidden and its swap failed trying to
// park the dead pane.
func TestPruneDeadManagedPanesForgetsClosedPanes(t *testing.T) {
	state := weztermPaneLayoutState{
		ManagedPaneIDs:   []int{152},
		ForegroundPaneID: 152,
		Sessions:         []managedAgentSession{{PaneID: 152, Provider: managedAgentProviderClaude, VisibleSlot: 1}},
	}

	state = normalizeWeztermPaneLayoutState(pruneDeadManagedPanes(state, []weztermPane{{PaneID: 10}}))

	assert.Empty(t, state.Sessions)
	assert.Empty(t, state.ManagedPaneIDs)
	assert.Zero(t, state.ForegroundPaneID)
}

// superfileTab is superfile's pane (10) in tab 1 of a 200-column window.
func superfileTab() weztermPane {
	return weztermPane{PaneID: 10, TabID: 1, WindowID: 1, Size: weztermPaneSize{Rows: 50, Cols: 200}}
}

func TestResumeAfterTheShownSessionExitedComesInBesideSuperfile(t *testing.T) {
	m := newClaudeTestModel(t) // sets up the state folder, so save after it
	require.NoError(t, saveWeztermPaneLayoutState(10, weztermPaneLayoutState{
		ManagedPaneIDs:     []int{152},
		ForegroundPaneID:   152,
		FixedSuperfileCols: 62,
		Sessions:           []managedAgentSession{{PaneID: 152, Provider: managedAgentProviderClaude, VisibleSlot: 1}},
	}))
	wez := newFakeWezterm(t, superfileTab())
	m.claude.panes = wez.paneList()
	past := claudesession.Session{ID: "past1", Title: "old work", Cwd: t.TempDir()}

	m.focusClaudeSession(past)

	require.Len(t, wez.panes, 2)
	resumed := wez.nextPane
	assert.Equal(t, 1, wez.tabOf(resumed), "the resumed session is beside Marmota: %v", wez.commands)
	assert.Equal(t, resumed, wez.active, "and has the keyboard")
	assert.Equal(t, "Resumed old work", m.claude.flash)
}

func TestResumeWithAParkedSessionKeepsItReachable(t *testing.T) {
	m := newClaudeTestModel(t)
	require.NoError(t, saveWeztermPaneLayoutState(10, weztermPaneLayoutState{
		FixedSuperfileCols: 62,
		Sessions:           []managedAgentSession{{PaneID: 30, Provider: managedAgentProviderClaude}},
	}))
	wez := newFakeWezterm(t, superfileTab(), weztermPane{PaneID: 30, TabID: 30, WindowID: 30,
		Workspace: managedAgentHiddenWorkspace, TTYName: "/dev/ttys030", Size: weztermPaneSize{Rows: 24, Cols: 80}})
	m.claude.panes = wez.paneList()

	m.focusClaudeSession(claudesession.Session{ID: "past1", Title: "old work", Cwd: t.TempDir()})

	resumed := wez.nextPane
	assert.Equal(t, 1, wez.tabOf(resumed), "the resumed session is beside Marmota: %v", wez.commands)
	assert.Equal(t, managedAgentHiddenWorkspace, wez.panes[30].Workspace, "the parked session stays parked")
	assert.True(t, m.isManagedAgentPane(30), "and managed")

	m.claude.panes = wez.paneList()
	m.focusClaudeSession(claudesession.Session{ID: "s30", Title: "parked", Live: true, TTY: "/dev/ttys030"})
	assert.Equal(t, 1, wez.tabOf(30), "] or Enter can still bring it in: %v", wez.commands)
	assert.Equal(t, 30, wez.active)
}

func TestManagedAgentSplitWidthNeverLeavesASideEmpty(t *testing.T) {
	cases := []struct {
		total, superfile, want int
	}{
		{200, 100, 99}, // the recorded width
		{80, 0, 17},    // nothing recorded: the first split's widths
		{80, 80, 17},   // superfile had the whole tab
		{80, 79, 17},
	}
	for _, c := range cases {
		got, err := managedAgentSplitWidth(c.total, c.superfile)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, "total %d, superfile %d", c.total, c.superfile)
	}
	_, err := managedAgentSplitWidth(40, 0)
	assert.Error(t, err, "too narrow to split at all")
}

// After superfile restarts, sessions the old one parked are unknown to the
// new one. Showing one takes it over and brings it in, instead of focusing
// a pane in a workspace nobody looks at.
func TestShowingAPaneParkedByAnEarlierSuperfileTakesItOver(t *testing.T) {
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab(), weztermPane{PaneID: 70, TabID: 70, WindowID: 70,
		Workspace: managedAgentHiddenWorkspace, TTYName: "/dev/ttys070", Size: weztermPaneSize{Rows: 24, Cols: 80}})
	m.claude.panes = wez.paneList()
	parked := claudesession.Session{ID: "s70", Title: "parked earlier", Live: true, TTY: "/dev/ttys070"}

	m.focusClaudeSession(parked)

	assert.Equal(t, 1, wez.tabOf(70), "it is brought in beside Marmota: %v", wez.commands)
	assert.Equal(t, 70, wez.active)
	assert.Equal(t, "Switched to parked earlier", m.claude.flash)
	assert.True(t, m.isManagedAgentPane(70))
}

// A session beside another Marmota, in another tab, comes in beside this
// one; enter used to switch WezTerm to that tab. The other Marmota lets go
// of it, so that it doesn't park it again, and its stale view of the panes
// doesn't park it when it next shows a session.
func TestShowingASessionBesideAnotherMarmotaTakesItOver(t *testing.T) {
	m := newClaudeTestModel(t)
	require.NoError(t, saveWeztermPaneLayoutState(10, weztermPaneLayoutState{
		FixedSuperfileCols: 100,
		Sessions:           []managedAgentSession{{PaneID: 8, Provider: managedAgentProviderClaude, VisibleSlot: 1}},
	}))
	otherState := weztermPaneLayoutState{
		FixedSuperfileCols: 100,
		Sessions: []managedAgentSession{
			{PaneID: 4, Provider: managedAgentProviderClaude, VisibleSlot: 1},
			{PaneID: 6, Provider: managedAgentProviderClaude},
		},
	}
	require.NoError(t, saveWeztermPaneLayoutState(2, otherState))
	wez := newFakeWezterm(t,
		weztermPane{PaneID: 10, TabID: 1, WindowID: 1, Size: weztermPaneSize{Rows: 50, Cols: 100}},
		weztermPane{PaneID: 8, TabID: 1, WindowID: 1, LeftCol: 101, TTYName: "/dev/ttys008", Title: "✳ doubao",
			Size: weztermPaneSize{Rows: 50, Cols: 99}},
		weztermPane{PaneID: 2, TabID: 2, WindowID: 1, Size: weztermPaneSize{Rows: 50, Cols: 100}},
		weztermPane{PaneID: 4, TabID: 2, WindowID: 1, LeftCol: 101, TTYName: "/dev/ttys004", Title: "✳ m5-1",
			Size: weztermPaneSize{Rows: 50, Cols: 99}},
		weztermPane{PaneID: 6, TabID: 6, WindowID: 6, Workspace: managedAgentHiddenWorkspace,
			TTYName: "/dev/ttys006", Size: weztermPaneSize{Rows: 24, Cols: 80}},
	)
	m.claude.panes = wez.paneList()

	m.focusClaudeSession(claudesession.Session{ID: "s4", Title: "m5-1", Live: true, TTY: "/dev/ttys004"})

	assert.Equal(t, 1, wez.tabOf(4), "it is brought in beside this Marmota: %v", wez.commands)
	assert.Equal(t, 4, wez.active)
	assert.Equal(t, managedAgentHiddenWorkspace, wez.panes[8].Workspace, "the one that was there is parked")
	assert.True(t, m.isManagedAgentPane(4))

	other, err := loadWeztermPaneLayoutState(2)
	require.NoError(t, err)
	require.Len(t, other.Sessions, 1, "the other Marmota lets go of it, and keeps the rest")
	assert.Equal(t, 6, other.Sessions[0].PaneID)

	// The other Marmota, with its view from before, shows its parked one.
	stale := defaultTestModel(t.TempDir())
	_, err = stale.foregroundManagedAgentSessionByPaneID(2, otherState, 6)
	require.NoError(t, err)
	assert.Equal(t, 1, wez.tabOf(4), "it isn't taken back: %v", wez.commands)
	assert.Equal(t, 2, wez.tabOf(6))
}

// Starting Claude from the prompt while another session is beside Marmota
// shows the new one there, with the keyboard, and parks the other.
func TestClaudeFromThePromptComesInBesideSuperfile(t *testing.T) {
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())

	_, err := m.launchClaudeInWezterm(false)
	require.NoError(t, err)
	first := wez.nextPane
	require.Equal(t, 1, wez.tabOf(first), "the first one splits superfile's tab: %v", wez.commands)

	msg, err := m.launchClaudeInWezterm(false)
	require.NoError(t, err)
	second := wez.nextPane
	assert.Equal(t, "Claude Code launched in new pane", msg)
	assert.Equal(t, 1, wez.tabOf(second), "the second one is beside Marmota: %v", wez.commands)
	assert.Equal(t, second, wez.active, "and has the keyboard")
	assert.Equal(t, managedAgentHiddenWorkspace, wez.panes[first].Workspace, "the first one is parked")
	assert.True(t, m.isManagedAgentPane(first), "and still managed")
}

// Stopping the session beside Marmota brings a parked one in its place,
// and the keyboard stays in superfile, where the sheet is.
func TestStoppingTheShownSessionKeepsTheKeyboardInSuperfile(t *testing.T) {
	m := newClaudeTestModel(t)
	wez := newFakeWezterm(t, superfileTab())
	_, err := m.launchClaudeInWezterm(false)
	require.NoError(t, err)
	parked := wez.nextPane
	_, err = m.launchClaudeInWezterm(false)
	require.NoError(t, err)
	shown := wez.nextPane
	wez.panes[shown].TTYName = "/dev/ttys777"
	m.claude.panes = wez.paneList()
	m.claude.snapshot.Live = []claudesession.Session{{ID: "s", Title: "shown", Live: true, TTY: "/dev/ttys777"}}
	m.claude.pendingStop = "s"

	m.stopPendingClaudeSession()

	assert.Nil(t, wez.panes[shown], "its pane is closed")
	assert.Equal(t, 1, wez.tabOf(parked), "the parked session takes its place: %v", wez.commands)
	assert.Equal(t, 10, wez.active, "the keyboard is back in superfile")
	assert.Equal(t, "Stopped shown", m.claude.flash)
}
