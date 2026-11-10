package internal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func utilsKey(key string) tea.KeyMsg { return utils.TeaKeyMsg(key) }

// Every text input in the model has to be listed in isTextInputActive. When one
// is missing, the IME ascii-hold stays in force while that field has focus and
// the user simply cannot type Chinese into it — so this table is the guard
// against a new input being added and forgotten.
// modelWithOnePinnedFolder gives the s list's cases an item to act on.
func modelWithOnePinnedFolder(t *testing.T) *model {
	t.Helper()
	dir := t.TempDir()
	pinned := []sidebarTestEntry{{Location: dir, Name: "pinned", Timestamp: 1}}
	configureSidebarPinnedAndRecentForTest(t, dir, pinned, nil)
	return defaultTestModel(dir)
}

// pickInJump opens the s list and picks its first item.
func pickInJump(m *model) {
	m.openJump()
	m.jumpKey(utilsKey("down"))
}

func TestIsTextInputActive(t *testing.T) {
	testCases := []struct {
		name  string
		focus func(m *model)
	}{
		{"create file or directory", func(m *model) { m.typingModal.open = true }},
		{"SPF or shell prompt", func(m *model) { m.promptModal.Open(false) }},
		{"s list search", func(m *model) { m.openJump() }},
		{"file panel rename", func(m *model) { m.fileModel.renaming = true }},
		{"s list rename", func(m *model) {
			pickInJump(m)
			m.jumpKey(utilsKey(common.Hotkeys.FilePanelItemRename[0]))
		}},
		{"file panel search", func(m *model) { m.getFocusedFilePanel().searchBar.Focus() }},
		{"Ask box", func(m *model) { m.openDockAsk("") }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := modelWithOnePinnedFolder(t)
			require.False(t, m.isTextInputActive(), "a fresh model is in hotkey mode")

			tc.focus(m)
			assert.True(t, m.isTextInputActive(),
				"%s is a text field: the input method must be released for it", tc.name)
		})
	}
}

// The help menu focuses its search bar as soon as it opens. Treating that as a
// text input would release the ascii-hold for the whole menu and leave its
// up/down/esc navigation being swallowed by the input method.
func TestHelpMenuIsNotATextInput(t *testing.T) {
	m := defaultTestModel(t.TempDir())
	m.openHelpMenu()

	require.True(t, m.helpMenu.open)
	require.True(t, m.helpMenu.searchBar.Focused(), "openHelpMenu focuses the search bar")

	assert.False(t, m.isTextInputActive(), "the help menu takes hotkeys, not text")
	assert.True(t, m.isBlockingModalOpen(), "but it does own the keyboard")
}

func TestIsBlockingModalOpen(t *testing.T) {
	testCases := []struct {
		name string
		open func(m *model)
	}{
		{"help menu", func(m *model) { m.openHelpMenu() }},
		{"sort options", func(m *model) { m.getFocusedFilePanel().sortOptions.open = true }},
		{"iCloud filter", func(m *model) { m.getFocusedFilePanel().iCloudFilter.open = true }},
		{"s list picking", pickInJump},
		{"first use", func(m *model) { m.firstUse = true }},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := modelWithOnePinnedFolder(t)
			require.False(t, m.isBlockingModalOpen())

			tc.open(m)
			assert.True(t, m.isBlockingModalOpen())
			assert.False(t, m.isTextInputActive(),
				"%s consumes keys but needs no input method", tc.name)
		})
	}
}

// Both predicates are called from Update on every message, before anything has
// necessarily been set up, so neither may panic on an empty model.
func TestTypingStatePredicatesAreTotal(t *testing.T) {
	var nilModel *model
	assert.False(t, nilModel.isTextInputActive())
	assert.False(t, nilModel.isBlockingModalOpen())

	empty := &model{}
	assert.False(t, empty.isTextInputActive(), "no file panels must not panic")
	assert.False(t, empty.isBlockingModalOpen())
}
