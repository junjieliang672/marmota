package internal

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
)

type fakeExecResult struct {
	retCode int
	output  string
	err     error
}

type fakeExecCall struct {
	cwd string
	cmd string
}

func TestLaunchManagedSessionInWeztermForPane_FirstSplitStoresFixedSuperfileWidth(t *testing.T) {
	setupWeztermStateDirForTest(t)
	m := defaultTestModel(testDir)
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, LeftCol: 0, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 223}},
		)},
		fakeExecResult{retCode: 0, output: "20\n"},
	)

	msg, err := m.launchManagedSessionInWeztermForPaneAt(10, m.getFocusedFilePanel().location,
		"claude --dangerously-skip-permissions",
		managedAgentProviderClaude,
		"Claude Code launched in new pane",
	)

	require.NoError(t, err)
	assert.Equal(t, "Claude Code launched in new pane", msg)
	require.Len(t, *calls, 2)
	assert.Equal(t, "wezterm cli list --format json", (*calls)[0].cmd)
	assert.Contains(t, (*calls)[1].cmd, "--pane-id 10 --right --cells 111")

	state, err := loadWeztermPaneLayoutState(10)
	require.NoError(t, err)
	assert.Equal(t, []int{20}, state.ManagedPaneIDs)
	assert.Equal(t, 111, state.FixedSuperfileCols)
}

func TestLaunchClaudeInWezterm_RemoteControlAddsFlag(t *testing.T) {
	setupWeztermStateDirForTest(t)
	t.Setenv("WEZTERM_PANE", "10")

	m := defaultTestModel(testDir)
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, LeftCol: 0, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 223}},
		)},
		fakeExecResult{retCode: 0, output: "20\n"},
		fakeExecResult{retCode: 0, output: ""},
	)

	msg, err := m.launchClaudeInWezterm(true)

	require.NoError(t, err)
	assert.Equal(t, "Claude Code (Remote Control) launched in new pane", msg)
	require.Len(t, *calls, 3)
	assert.Contains(t, (*calls)[1].cmd, "claude --dangerously-skip-permissions --remote-control")
	assert.Equal(t, "wezterm cli activate-pane --pane-id 20", (*calls)[2].cmd, "the keyboard goes to it")

	state, err := loadWeztermPaneLayoutState(10)
	require.NoError(t, err)
	require.Len(t, state.Sessions, 1)
	assert.Equal(t, managedAgentProviderClaude, state.Sessions[0].Provider)
}

func TestLaunchManagedSessionInWeztermForPane_FirstSplitEnforcesMinimumSuperfileWidth(t *testing.T) {
	setupWeztermStateDirForTest(t)
	m := defaultTestModel(testDir)
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, LeftCol: 0, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 100}},
		)},
		fakeExecResult{retCode: 0, output: "20\n"},
	)

	msg, err := m.launchManagedSessionInWeztermForPaneAt(10, m.getFocusedFilePanel().location,
		"claude --dangerously-skip-permissions",
		managedAgentProviderClaude,
		"Claude Code launched in new pane",
	)

	require.NoError(t, err)
	assert.Equal(t, "Claude Code launched in new pane", msg)
	require.Len(t, *calls, 2)
	assert.Equal(t, "wezterm cli list --format json", (*calls)[0].cmd)
	assert.Contains(t, (*calls)[1].cmd, "--pane-id 10 --right --cells 37")

	state, err := loadWeztermPaneLayoutState(10)
	require.NoError(t, err)
	assert.Equal(t, []int{20}, state.ManagedPaneIDs)
	assert.Equal(t, 62, state.FixedSuperfileCols)
}

func TestLaunchManagedSessionInWeztermForPane_SecondSessionStaysBackgroundWhenForegroundExists(t *testing.T) {
	setupWeztermStateDirForTest(t)
	require.NoError(t, saveWeztermPaneLayoutState(10, weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20},
		FixedSuperfileCols: 74,
	}))
	m := defaultTestModel(testDir)

	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, LeftCol: 0, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 74}},
			weztermPane{TabID: 1, PaneID: 20, LeftCol: 75, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 148}},
		)},
		fakeExecResult{retCode: 0, output: "30\n"},
	)

	msg, err := m.launchManagedSessionInWeztermForPaneAt(10, m.getFocusedFilePanel().location,
		"codex --sandbox danger-full-access --ask-for-approval on-request",
		managedAgentProviderCodex,
		"Codex CLI launched in new pane",
	)

	require.NoError(t, err)
	assert.Equal(t, "Codex CLI launched in new pane", msg)
	require.Len(t, *calls, 2)
	assert.Contains(t, (*calls)[1].cmd, "wezterm cli spawn --new-window --workspace superfile-agents",
		"hidden sessions start in their own workspace, out of the tab bar")

	state, err := loadWeztermPaneLayoutState(10)
	require.NoError(t, err)
	assert.Equal(t, []int{20}, state.ManagedPaneIDs)
	require.Len(t, state.Sessions, 2)
	assert.Equal(t, 20, state.Sessions[0].PaneID)
	assert.Equal(t, managedAgentSlotOne, state.Sessions[0].VisibleSlot)
	assert.Equal(t, 30, state.Sessions[1].PaneID)
	assert.Equal(t, managedAgentHiddenSlot, state.Sessions[1].VisibleSlot)
	assert.Equal(t, 74, state.FixedSuperfileCols)
}

func TestLaunchManagedSessionInWeztermForPane_ThirdSessionIsQueuedInHiddenTab(t *testing.T) {
	setupWeztermStateDirForTest(t)
	require.NoError(t, saveWeztermPaneLayoutState(10, weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20},
		FixedSuperfileCols: 74,
		Sessions: []managedAgentSession{
			{PaneID: 20, Provider: managedAgentProviderClaude, Cwd: "/tmp/api", VisibleSlot: managedAgentSlotOne},
			{PaneID: 30, Provider: managedAgentProviderCodex, Cwd: "/tmp/tests", VisibleSlot: managedAgentHiddenSlot},
		},
	}))
	m := defaultTestModel(testDir)

	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{
				WindowID: 9,
				TabID:    1,
				PaneID:   10,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 74},
			},
			weztermPane{
				WindowID: 9,
				TabID:    1,
				PaneID:   20,
				LeftCol:  75,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 74},
			},
			weztermPane{
				WindowID: 9,
				TabID:    2,
				PaneID:   30,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 74},
			},
		)},
		fakeExecResult{retCode: 0, output: "40\n"},
	)

	msg, err := m.launchManagedSessionInWeztermForPaneAt(10, m.getFocusedFilePanel().location,
		"claude --dangerously-skip-permissions",
		managedAgentProviderClaude,
		"Claude Code launched in new pane",
	)

	require.NoError(t, err)
	assert.Equal(t, "Claude Code launched in new pane", msg)
	require.Len(t, *calls, 2)
	assert.Contains(t, (*calls)[1].cmd, "wezterm cli spawn --new-window --workspace superfile-agents",
		"hidden sessions start in their own workspace, out of the tab bar")

	state, err := loadWeztermPaneLayoutState(10)
	require.NoError(t, err)
	assert.Equal(t, []int{20}, state.ManagedPaneIDs)
	require.Len(t, state.Sessions, 3)
	assert.Equal(t, 40, state.Sessions[2].PaneID)
	assert.Equal(t, managedAgentHiddenSlot, state.Sessions[2].VisibleSlot)
	assert.Equal(t, managedAgentProviderClaude, state.Sessions[2].Provider)
}

func TestLaunchManagedSessionInWeztermForPane_RejectsUnmanagedPanesInTab(t *testing.T) {
	setupWeztermStateDirForTest(t)
	require.NoError(t, saveWeztermPaneLayoutState(10, weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20},
		FixedSuperfileCols: 74,
	}))
	m := defaultTestModel(testDir)

	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, LeftCol: 0, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 74}},
			weztermPane{TabID: 1, PaneID: 20, LeftCol: 75, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 148}},
			weztermPane{TabID: 1, PaneID: 77, LeftCol: 224, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 40}},
		)},
	)

	_, err := m.launchManagedSessionInWeztermForPaneAt(10, m.getFocusedFilePanel().location,
		"claude --dangerously-skip-permissions",
		managedAgentProviderClaude,
		"Claude Code launched in new pane",
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "only Marmota and managed agent panes")
	require.Len(t, *calls, 1)
}

func TestReconcileManagedAgentSessionsWithWezterm_RefreshesRunningTaskState(t *testing.T) {
	state := weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20, 30},
		FixedSuperfileCols: 75,
		Sessions: []managedAgentSession{
			{PaneID: 20, Provider: managedAgentProviderClaude, Cwd: "/tmp/api", VisibleSlot: managedAgentSlotOne},
			{PaneID: 30, Provider: managedAgentProviderCodex, Cwd: "/tmp/tests", VisibleSlot: managedAgentSlotTwo},
		},
	}

	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{
				WindowID: 9,
				TabID:    1,
				PaneID:   10,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 75},
			},
			weztermPane{
				WindowID: 9,
				TabID:    1,
				PaneID:   20,
				Title:    "⠐ Plan feature",
				LeftCol:  76,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 73},
			},
			weztermPane{
				WindowID: 9,
				TabID:    2,
				PaneID:   30,
				Title:    "superfile",
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 223},
			},
		)},
		fakeExecResult{retCode: 0, output: "◦ Working (1s • esc to interrupt)\n"},
	)

	refreshed, err := reconcileManagedAgentSessionsWithWezterm(testDir, 10, state)

	require.NoError(t, err)
	require.Len(t, *calls, 2)
	assert.Equal(t, "wezterm cli list --format json", (*calls)[0].cmd)
	assert.Equal(t, "wezterm cli get-text --pane-id 30 --start-line -20", (*calls)[1].cmd)
	require.Len(t, refreshed.Sessions, 2)
	assert.True(t, refreshed.Sessions[0].RunningTask)
	assert.True(t, refreshed.Sessions[1].RunningTask)
	assert.Equal(t, managedAgentSlotOne, refreshed.Sessions[0].VisibleSlot)
	assert.Equal(t, managedAgentHiddenSlot, refreshed.Sessions[1].VisibleSlot)
}

func TestNormalizeWeztermPaneLayoutState_PreservesProviderCwdAndSingleForegroundSession(t *testing.T) {
	state := weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20, 30},
		FixedSuperfileCols: 75,
		Sessions: []managedAgentSession{
			{
				PaneID:      20,
				Provider:    managedAgentProviderClaude,
				Cwd:         "/Users/me/Developer/api",
				VisibleSlot: managedAgentSlotOne,
			},
			{
				PaneID:      30,
				Provider:    managedAgentProviderCodex,
				Cwd:         "/Users/me/Developer/tests",
				VisibleSlot: managedAgentSlotTwo,
			},
		},
	}

	normalized := normalizeWeztermPaneLayoutState(state)

	require.Len(t, normalized.Sessions, 2)
	assert.Equal(t, []int{20}, normalized.ManagedPaneIDs)
	assert.Equal(t, managedAgentProviderClaude, normalized.Sessions[0].Provider)
	assert.Equal(t, "/Users/me/Developer/api", normalized.Sessions[0].Cwd)
	assert.Equal(t, managedAgentProviderCodex, normalized.Sessions[1].Provider)
	assert.Equal(t, "/Users/me/Developer/tests", normalized.Sessions[1].Cwd)
	assert.Equal(t, managedAgentSlotOne, normalized.Sessions[0].VisibleSlot)
	assert.Equal(t, managedAgentHiddenSlot, normalized.Sessions[1].VisibleSlot)
}

func TestSpec007LaunchManagedSessionKeepsNewSessionInBackgroundWhenForegroundExists(t *testing.T) {
	setupWeztermStateDirForTest(t)
	require.NoError(t, saveWeztermPaneLayoutState(10, weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20},
		FixedSuperfileCols: 74,
		Sessions: []managedAgentSession{
			{PaneID: 20, Provider: managedAgentProviderClaude, Cwd: "/tmp/api", VisibleSlot: managedAgentSlotOne},
		},
	}))

	m := defaultTestModel(testDir)
	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{TabID: 1, PaneID: 10, LeftCol: 0, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 74}},
			weztermPane{TabID: 1, PaneID: 20, LeftCol: 75, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: 148}},
		)},
		fakeExecResult{retCode: 0, output: "30\n"},
	)

	_, err := m.launchManagedSessionInWeztermForPaneAt(10, m.getFocusedFilePanel().location,
		"codex --sandbox danger-full-access --ask-for-approval on-request",
		managedAgentProviderCodex,
		"Codex CLI launched in new pane",
	)
	require.NoError(t, err)
	require.Len(t, *calls, 2)
	assert.Contains(t, (*calls)[1].cmd, "wezterm cli spawn --new-window --workspace superfile-agents")

	state, err := loadWeztermPaneLayoutState(10)
	require.NoError(t, err)
	require.Len(t, state.Sessions, 2)
	assert.Equal(t, managedAgentSlotOne, state.Sessions[0].VisibleSlot)
	assert.Equal(t, managedAgentHiddenSlot, state.Sessions[1].VisibleSlot)
}

func TestSpec007NormalizeWeztermPaneLayoutState_CollapsesLegacyVisibleSessionsToOneForegroundSession(t *testing.T) {
	state := weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20, 30},
		FixedSuperfileCols: 75,
		Sessions: []managedAgentSession{
			{PaneID: 20, Provider: managedAgentProviderClaude, Cwd: "/tmp/api", VisibleSlot: managedAgentSlotOne},
			{PaneID: 30, Provider: managedAgentProviderCodex, Cwd: "/tmp/tests", VisibleSlot: managedAgentSlotTwo},
			{PaneID: 40, Provider: managedAgentProviderClaude, Cwd: "/tmp/docs", VisibleSlot: managedAgentHiddenSlot},
		},
	}

	normalized := normalizeWeztermPaneLayoutState(state)

	require.Len(t, normalized.Sessions, 3)
	assert.Equal(t, []int{20}, normalized.ManagedPaneIDs)
	assert.Equal(t, managedAgentSlotOne, normalized.Sessions[0].VisibleSlot)
	assert.Equal(t, managedAgentHiddenSlot, normalized.Sessions[1].VisibleSlot)
	assert.Equal(t, managedAgentHiddenSlot, normalized.Sessions[2].VisibleSlot)
}

func TestSpec007WeztermStateRoundTripsDisplayNameAndDefaultsFolderName(t *testing.T) {
	t.Run("preserves a custom display name", func(t *testing.T) {
		setupWeztermStateDirForTest(t)
		statePath := weztermPaneStatePath(10)
		raw := `{"managed_pane_ids":[20],"fixed_superfile_cols":75,"sessions":[{"pane_id":20,"provider":"claude","cwd":"/tmp/api","display_name":"API Chat","visible_slot":1}]}`
		require.NoError(t, os.WriteFile(statePath, []byte(raw), 0o644))

		state, err := loadWeztermPaneLayoutState(10)
		require.NoError(t, err)
		require.NoError(t, saveWeztermPaneLayoutState(10, state))

		persisted, err := os.ReadFile(statePath)
		require.NoError(t, err)
		assert.Contains(t, string(persisted), `"display_name":"API Chat"`)
	})

	t.Run("defaults to the rooted folder name", func(t *testing.T) {
		setupWeztermStateDirForTest(t)
		statePath := weztermPaneStatePath(10)
		raw := `{"managed_pane_ids":[20],"fixed_superfile_cols":75,"sessions":[{"pane_id":20,"provider":"claude","cwd":"/tmp/api","visible_slot":1}]}`
		require.NoError(t, os.WriteFile(statePath, []byte(raw), 0o644))

		state, err := loadWeztermPaneLayoutState(10)
		require.NoError(t, err)
		require.NoError(t, saveWeztermPaneLayoutState(10, state))

		persisted, err := os.ReadFile(statePath)
		require.NoError(t, err)
		assert.Contains(t, string(persisted), `"display_name":"api"`)
	})
}

func TestPlanManagedWeztermLayout_UsesFixedOneByTwoCapacity(t *testing.T) {
	plan, err := planManagedWeztermLayout(148, 53, 2)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 1}, plan.RowPaneCounts)
	assert.Equal(t, []int{27, 26}, plan.RowHeights)
	assert.Equal(t, 148, plan.AgentWidth)

	_, err = planManagedWeztermLayout(148, 53, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1x2 managed layout capacity")
}

func TestInitialManagedWeztermSplitWidths(t *testing.T) {
	t.Run("uses percentage when it exceeds minimum", func(t *testing.T) {
		superfileCols, agentCols, err := initialManagedWeztermSplitWidths(223)
		require.NoError(t, err)
		assert.Equal(t, 111, superfileCols)
		assert.Equal(t, 111, agentCols)
	})

	t.Run("uses minimum width when percentage is smaller", func(t *testing.T) {
		superfileCols, agentCols, err := initialManagedWeztermSplitWidths(100)
		require.NoError(t, err)
		assert.Equal(t, 62, superfileCols)
		assert.Equal(t, 37, agentCols)
	})

	t.Run("rejects panes that are too narrow to split", func(t *testing.T) {
		_, _, err := initialManagedWeztermSplitWidths(62)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too small")
	})
}

func TestAttachManagedPaneToSlot_DoesNotDriftFixedSuperfileColsAcrossSwaps(t *testing.T) {
	// Each swap cycle through the agent manager hides the foreground agent pane (superfile
	// expands to the full tab width) and then re-splits the superfile pane to surface the
	// next agent. reconcileManagedAgentSessionsWithWezterm subsequently updates
	// FixedSuperfileCols to whatever superfile is measured at after the split, so if the
	// split math does not leave superfile at exactly FixedSuperfileCols, the value drifts
	// one column per swap and the agent pane keeps getting wider.
	const totalWidth = 223
	const startingFixedSuperfileCols = 74
	const swapIterations = 10

	cellsRE := regexp.MustCompile(`--cells (\d+)`)
	fixedSuperfileCols := startingFixedSuperfileCols

	for i := range swapIterations {
		state := weztermPaneLayoutState{
			FixedSuperfileCols: fixedSuperfileCols,
			Sessions: []managedAgentSession{
				{PaneID: 30, Provider: managedAgentProviderClaude, VisibleSlot: managedAgentHiddenSlot},
			},
		}
		tabPanes := []weztermPane{
			{TabID: 1, PaneID: 10, LeftCol: 0, TopRow: 0, Size: weztermPaneSize{Rows: 53, Cols: totalWidth}},
		}
		currentPane := tabPanes[0]

		calls := installFakeExec(t, fakeExecResult{retCode: 0, output: "31\n"})

		err := attachManagedPaneToSlot(testDir, currentPane, tabPanes, state, managedAgentSlotOne, 30)
		require.NoError(t, err, "iteration %d", i)
		require.Len(t, *calls, 1, "iteration %d", i)

		match := cellsRE.FindStringSubmatch((*calls)[0].cmd)
		require.Len(t, match, 2, "iteration %d: expected --cells argument in %q", i, (*calls)[0].cmd)
		agentCells, convErr := strconv.Atoi(match[1])
		require.NoError(t, convErr, "iteration %d", i)

		// WezTerm consumes the requested agent cells plus one divider column from the
		// superfile pane, so superfile ends up at totalWidth - agentCells - divider. If
		// the split forgets the divider (agentCells == totalWidth - FixedSuperfileCols),
		// superfile lands one column short and reconcile saves the shrunken value, which
		// is what compounds into visible drift across swaps.
		newSuperfileCols := totalWidth - agentCells - weztermSplitDividerCols
		require.Equal(t, fixedSuperfileCols, newSuperfileCols,
			"iteration %d: post-split superfile width (%d) must equal FixedSuperfileCols (%d)",
			i, newSuperfileCols, fixedSuperfileCols)
		fixedSuperfileCols = newSuperfileCols
	}

	assert.Equal(t, startingFixedSuperfileCols, fixedSuperfileCols,
		"FixedSuperfileCols must not drift across %d swaps", swapIterations)
}

func setupWeztermStateDirForTest(t *testing.T) {
	t.Helper()

	setForTest(t, &variable.SuperFileStateDir, t.TempDir())
}

func installFakeExec(t *testing.T, results ...fakeExecResult) *[]fakeExecCall {
	t.Helper()

	originalExec := execInShell
	calls := make([]fakeExecCall, 0, len(results))
	resultIdx := 0

	execInShell = func(_ time.Duration, cwd string, cmd string) (int, string, error) {
		if resultIdx >= len(results) {
			t.Fatalf("unexpected shell command: %s", cmd)
		}

		calls = append(calls, fakeExecCall{cwd: cwd, cmd: cmd})
		result := results[resultIdx]
		resultIdx++
		return result.retCode, result.output, result.err
	}

	t.Cleanup(func() {
		execInShell = originalExec
		require.Equal(t, len(results), resultIdx, "all fake shell results should be consumed")
	})

	return &calls
}

func marshalWeztermPanesForTest(panes ...weztermPane) string {
	data, err := json.Marshal(panes)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestProviderTokens(t *testing.T) {
	assert.Equal(t, "c", managedAgentSession{Provider: managedAgentProviderClaude}.providerToken())
	assert.Equal(t, "x", managedAgentSession{Provider: managedAgentProviderCodex}.providerToken())
	assert.Equal(t, "?", managedAgentSession{Provider: "coco"}.providerToken(), "a provider SPF no longer starts")
}
