package sessions

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func TestMain(m *testing.M) {
	if err := common.PopulateGlobalConfigs(); err != nil {
		panic(err)
	}
	m.Run()
}

func testGroups() []Group {
	return []Group{
		{Folder: "/w/repo", Rows: []Row{
			{ID: "a", Name: "proj-1", Summary: "fix the login"},
			{ID: "b", Summary: "old talk"},
		}},
		{Folder: "/w/docs", Rows: []Row{
			{ID: "c", Name: "docs-1", Summary: "write the guide"},
		}},
	}
}

func testList() List {
	l := New()
	l.SetGroups(testGroups())
	return l
}

func TestTheFirstRowIsSelected(t *testing.T) {
	l := testList()
	assert.Equal(t, "a", l.SelectedID())
	assert.False(t, l.NewSelected())
}

func TestTheSelectionStaysOnItsSession(t *testing.T) {
	l := testList()
	require.True(t, l.Select("c"))
	groups := testGroups()
	groups[0].Rows = append([]Row{{ID: "z", Summary: "new one"}}, groups[0].Rows...)
	l.SetGroups(groups)
	assert.Equal(t, "c", l.SelectedID(), "a session listed above doesn't move the selection")

	groups[1].Rows = nil
	l.SetGroups(groups)
	assert.Equal(t, "z", l.SelectedID(), "gone, it goes to the first row")
	assert.False(t, l.Select("nope"))
}

// A new session (n) is chosen without a row of its own: no row is
// selected until one is chosen again.
func TestANewSessionHasNoRow(t *testing.T) {
	l := testList()
	require.True(t, l.Select("b"))
	l.SelectNew()
	assert.True(t, l.NewSelected())
	_, ok := l.Selected()
	assert.False(t, ok)
	assert.Empty(t, l.SelectedID())

	require.True(t, l.Select("c"))
	assert.False(t, l.NewSelected())
}

func TestRenameStartsWithTheName(t *testing.T) {
	l := testList()
	l.StartRename()
	require.True(t, l.IsRenaming())
	l.UpdateRename(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	id, name := l.EndRename()
	assert.Equal(t, "a", id)
	assert.Equal(t, "proj-1!", name)
	assert.False(t, l.IsRenaming())

	require.True(t, l.Select("b"))
	l.StartRename()
	assert.Equal(t, "old talk", ansi.Strip(l.RenameView(20))[:len("old talk")], "an unnamed one starts with its summary")
	l.CancelRename()
	assert.False(t, l.IsRenaming())
}

func TestEachStateHasItsGlyph(t *testing.T) {
	for state, want := range map[State]string{NeedsYou: "⚑", Working: "✳", Unread: "●", Idle: "○", Ended: "·"} {
		assert.Equal(t, want, ansi.Strip(Glyph(state, common.FilePanelBGColor)))
	}
}
