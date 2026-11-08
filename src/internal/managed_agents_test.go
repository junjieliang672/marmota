package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshManagedWeztermState_AutoPromotesBackgroundSessionWhenForegroundPaneExited(t *testing.T) {
	setupWeztermStateDirForTest(t)
	t.Setenv("WEZTERM_PANE", "10")

	state := weztermPaneLayoutState{
		ManagedPaneIDs:     []int{20},
		ForegroundPaneID:   20,
		FixedSuperfileCols: 74,
		Sessions: []managedAgentSession{
			{PaneID: 20, Provider: managedAgentProviderClaude, Cwd: "/tmp/api", VisibleSlot: managedAgentSlotOne},
			{PaneID: 30, Provider: managedAgentProviderCodex, Cwd: "/tmp/tests", VisibleSlot: managedAgentHiddenSlot},
		},
	}
	require.NoError(t, saveWeztermPaneLayoutState(10, state))

	m := defaultTestModel(testDir)
	m.toggleFooter = true
	m.setHeightValues(DefaultTestModelHeight)

	calls := installFakeExec(t,
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{
				WindowID: 9,
				TabID:    1,
				PaneID:   10,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 223},
			},
			weztermPane{
				WindowID: 9,
				TabID:    2,
				PaneID:   30,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 148},
			},
		)},
		fakeExecResult{retCode: 0, output: ""},
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{
				WindowID: 9,
				TabID:    1,
				PaneID:   10,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 223},
			},
			weztermPane{
				WindowID: 9,
				TabID:    2,
				PaneID:   30,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 148},
			},
		)},
		fakeExecResult{retCode: 0, output: marshalWeztermPanesForTest(
			weztermPane{
				WindowID: 9,
				TabID:    1,
				PaneID:   10,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 223},
			},
			weztermPane{
				WindowID: 9,
				TabID:    2,
				PaneID:   30,
				LeftCol:  0,
				TopRow:   0,
				Size:     weztermPaneSize{Rows: 53, Cols: 148},
			},
		)},
		fakeExecResult{retCode: 0, output: "30\n"},
	)

	m.refreshManagedWeztermState()

	require.Len(t, *calls, 5)
	assert.Equal(t, "wezterm cli list --format json", (*calls)[0].cmd)
	assert.Equal(t, "wezterm cli get-text --pane-id 30 --start-line -20", (*calls)[1].cmd)
	assert.Equal(t, "wezterm cli list --format json", (*calls)[2].cmd)
	assert.Equal(t, "wezterm cli list --format json", (*calls)[3].cmd)
	assert.Equal(t, "wezterm cli split-pane --pane-id 10 --right --cells 148 --move-pane-id 30", (*calls)[4].cmd)
	require.Len(t, m.managedWeztermState.Sessions, 1)
	assert.Equal(t, 30, m.managedWeztermState.Sessions[0].PaneID)
	assert.Equal(t, managedAgentSlotOne, m.managedWeztermState.Sessions[0].VisibleSlot)
	assert.Equal(t, []int{30}, m.managedWeztermState.ManagedPaneIDs)
	assert.Equal(t, 74, m.managedWeztermState.FixedSuperfileCols)
}
