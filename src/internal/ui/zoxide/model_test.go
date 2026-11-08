package zoxide

import (
	"testing"

	"github.com/adrg/xdg"

	tea "github.com/charmbracelet/bubbletea"
	zoxidelib "github.com/lazysegtree/go-zoxide"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestMain(m *testing.M) {
	originalZoxideSupport := common.Config.ZoxideSupport
	common.Config.ZoxideSupport = true
	defer func() {
		common.Config.ZoxideSupport = originalZoxideSupport
	}()
	m.Run()
}

func TestHandleConfirmWithValidSelection(t *testing.T) {
	m := setupTestModelWithResults(3)
	m.navigateDown()

	action := m.handleConfirm()

	cdAction, ok := action.(common.CDCurrentPanelAction)
	require.True(t, ok, "action should be CDCurrentPanelAction")
	assert.Equal(t, m.results[1].Path, cdAction.Location, "action should navigate to results[1].Path")
}

func TestHandleConfirmWithNothingTypedGoesHome(t *testing.T) {
	// Opening the bar lists every zoxide entry with the cursor on the
	// first; enter with nothing typed still goes Home.
	m := setupTestModelWithResults(3)

	cdAction, ok := m.handleConfirm().(common.CDCurrentPanelAction)
	require.True(t, ok)
	assert.Equal(t, xdg.Home, cdAction.Location)

	panelAction, ok := m.handleConfirmNewPanel().(common.OpenPanelAction)
	require.True(t, ok)
	assert.Equal(t, xdg.Home, panelAction.Location)
}

func TestHandleConfirmWithTypedQueryTakesTopResult(t *testing.T) {
	m := setupTestModelWithResults(3)
	m.textInput.SetValue("path")

	cdAction, ok := m.handleConfirm().(common.CDCurrentPanelAction)
	require.True(t, ok)
	assert.Equal(t, m.results[0].Path, cdAction.Location)
}

func TestNewResultsForgetThePick(t *testing.T) {
	m := setupTestModelWithResults(3)
	m.navigateDown()
	NewUpdateMsg("", m.results, 0).Apply(&m)

	cdAction, ok := m.handleConfirm().(common.CDCurrentPanelAction)
	require.True(t, ok)
	assert.Equal(t, xdg.Home, cdAction.Location)
}

func TestHandleConfirmWithNoResults(t *testing.T) {
	m := setupTestModel()

	action := m.handleConfirm()

	// With empty text input and no results, should navigate to Home
	cdAction, ok := action.(common.CDCurrentPanelAction)
	assert.True(t, ok, "action should be CDCurrentPanelAction to Home when no results and empty input")
	assert.NotEmpty(t, cdAction.Location, "should navigate to Home")
}

func TestHandleConfirmWithInvalidCursor(t *testing.T) {
	m := setupTestModelWithResults(3)
	m.cursor = 5

	action := m.handleConfirm()

	// With empty text input and invalid cursor, should navigate to Home
	cdAction, ok := action.(common.CDCurrentPanelAction)
	assert.True(t, ok, "action should be CDCurrentPanelAction to Home when cursor is invalid and empty input")
	assert.NotEmpty(t, cdAction.Location, "should navigate to Home")
}

func TestJKKeyHandling(t *testing.T) {
	m := setupTestModelWithClient(t)

	m.Open()

	originalHotkeys := common.Hotkeys.ListDown
	common.Hotkeys.ListDown = []string{"j", "down"}
	defer func() {
		common.Hotkeys.ListDown = originalHotkeys
	}()

	action, cmd := m.HandleUpdate(utils.TeaRuneKeyMsg("j"))

	assert.NotNil(t, cmd, "HandleUpdate should return cmd for text input update")
	_, isNoAction := action.(common.NoAction)
	assert.True(t, isNoAction, "action should be NoAction for text input")
	assert.Equal(t, "j", m.textInput.Value(), "'j' should be added to textInput")

	action, cmd = m.HandleUpdate(utils.TeaRuneKeyMsg("k"))
	assert.NotNil(t, cmd, "HandleUpdate should return cmd for text input update")
	_, isNoAction = action.(common.NoAction)
	assert.True(t, isNoAction, "action should be NoAction for text input")
	assert.Equal(t, "jk", m.textInput.Value(), "'k' should be added to textInput")

	m.textInput.SetValue("")
	m.results = []zoxidelib.Result{
		{Path: "/test/path1", Score: 100},
		{Path: "/test/path2", Score: 90},
	}
	m.cursor = 0

	action, cmd = m.HandleUpdate(tea.KeyMsg{Type: tea.KeyDown})
	assert.Nil(t, cmd, "HandleUpdate with down arrow should not return cmd")
	_, isNoAction = action.(common.NoAction)
	assert.True(t, isNoAction, "action should be NoAction for navigation")
	assert.Equal(t, 1, m.cursor, "down arrow should navigate down")
	assert.Empty(t, m.textInput.Value(), "down arrow should not add to textInput")
}

func TestApplyWithMatchingQuery(t *testing.T) {
	m := setupTestModel()
	m.textInput.SetValue("test")
	m.cursor = 5
	m.renderIndex = 2

	results := []zoxidelib.Result{
		{Path: "/test/path1", Score: 100},
		{Path: "/test/path2", Score: 90},
		{Path: "/test/path3", Score: 80},
	}
	msg := NewUpdateMsg("test", results, 1)

	cmd := msg.Apply(&m)
	assert.Nil(t, cmd)
	assert.Len(t, m.results, 3, "results should be updated to 3 items")
	assert.Equal(t, 0, m.cursor, "cursor should be reset to 0")
	assert.Equal(t, 0, m.renderIndex, "renderIndex should be reset to 0")
	assert.Equal(t, results, m.results, "results should match the update message")
}

func TestApplyWithStaleQuery(t *testing.T) {
	m := setupTestModel()
	m.textInput.SetValue("new")
	m.cursor = 1
	m.renderIndex = 1
	originalResults := []zoxidelib.Result{
		{Path: "/original/path", Score: 50},
	}
	m.results = originalResults

	staleResults := []zoxidelib.Result{
		{Path: "/test/path1", Score: 100},
		{Path: "/test/path2", Score: 90},
		{Path: "/test/path3", Score: 80},
	}
	msg := NewUpdateMsg("old", staleResults, 1)

	cmd := msg.Apply(&m)
	assert.Nil(t, cmd)
	assert.Equal(t, originalResults, m.results, "results should remain unchanged")
	assert.Equal(t, 1, m.cursor, "cursor should remain unchanged")
	assert.Equal(t, 1, m.renderIndex, "renderIndex should remain unchanged")
}
