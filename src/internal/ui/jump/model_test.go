package jump

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func items(kind Kind, titles ...string) []Item {
	var out []Item
	for _, t := range titles {
		out = append(out, Item{Kind: kind, Key: t, Title: t, Where: "/home/me/" + t})
	}
	return out
}

func testSections() []Section {
	var files []string
	for i := range 8 {
		files = append(files, "file"+strconv.Itoa(i))
	}
	return []Section{
		{Kind: Pinned, Items: items(Pinned, "marmota", "notes")},
		{Kind: Session, Items: items(Session, "group the logos")},
		{Kind: Ended, Items: items(Ended, "logo sizes")},
		{Kind: Folder, Items: items(Folder, "site", "archive"), Matched: true},
		{Kind: File, Items: items(File, files...)},
	}
}

func titles(m *Model) []string {
	var out []string
	for _, it := range m.Visible() {
		out = append(out, it.Title)
	}
	return out
}

func typeText(m *Model, text string) {
	for _, r := range text {
		m.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestFilesAndFoldersShowFiveUntilSomethingIsTyped(t *testing.T) {
	m := New()
	m.SetSize(140, 40)
	m.Open(testSections())

	assert.Equal(t, []string{"marmota", "notes", "group the logos", "logo sizes", "site", "archive",
		"file0", "file1", "file2", "file3", "file4"}, titles(&m))

	typeText(&m, "file7")
	assert.Equal(t, []string{"site", "archive", "file7"}, titles(&m),
		"typed: every match; zoxide's folders matched already")
}

func TestTypedWordsMustAppearWhole(t *testing.T) {
	m := New()
	m.SetSize(140, 40)
	m.Open(testSections())

	typeText(&m, "logo")
	got := titles(&m)
	assert.Contains(t, got, "group the logos")
	assert.Contains(t, got, "logo sizes")
	assert.NotContains(t, got, "notes", "l, o, g, o scattered over a path don't match")

	m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	it, ok := m.Picked()
	require.True(t, ok)
	assert.Equal(t, "site", it.Title, "enter: a folder zoxide found, before the sessions")
}

func TestTheSectionsSitOneUnderTheOtherInThePanel(t *testing.T) {
	m := New()
	m.SetSize(60, 40)
	m.Open(testSections())

	view := ansi.Strip(m.Render(true))
	assert.Contains(t, view, "go to", "the panel's title")
	assert.Equal(t, []string{"marmota", "notes", "group the logos", "logo sizes", "site", "archive",
		"file0", "file1", "file2", "file3", "file4"}, titles(&m), "Pinned, Sessions, Resume, Folders, Files")
	lines := strings.Split(view, "\n")
	var order []string
	for _, l := range lines {
		for _, k := range Kinds {
			if strings.Contains(l, k.Title()) {
				order = append(order, k.Title())
			}
		}
	}
	assert.Equal(t, []string{"Pinned", "Sessions", "Resume", "Folders", "Files"}, order)
	for _, l := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(l), 60, "fits the panel")
	}
	assert.Len(t, lines, 40, "as tall as the panel")
}

func TestPickingShowsWhereTheCursorIsInTheTitle(t *testing.T) {
	m := New()
	m.SetSize(60, 40)
	m.Open(testSections())
	m.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.HandleKey(tea.KeyMsg{Type: tea.KeyDown})

	assert.Contains(t, ansi.Strip(m.Render(true)), "go to 2/11")
}

func TestRowsAreNumbered(t *testing.T) {
	m := New()
	m.SetSize(140, 40)
	m.Open(testSections())

	view := ansi.Strip(m.Render(true))
	assert.Contains(t, view, " 1 · marmota")
	assert.Contains(t, view, "11 · file4")
}

func TestOnlyKeepsTheOneSection(t *testing.T) {
	m := New()
	m.SetSize(140, 40)
	m.Open(testSections(), Ended)

	assert.Equal(t, []string{"logo sizes"}, titles(&m))
	m.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, []string{"logo sizes"}, titles(&m), "tab can't leave it")
	act := m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, None, act.Kind, "nothing typed, nothing picked: enter waits")
}

func TestTabKeepsTheSize(t *testing.T) {
	sections := testSections()
	for i := range 30 {
		sections[3].Items = append(sections[3].Items, items(Folder, "dir"+strconv.Itoa(i))...)
	}
	m := New()
	m.SetSize(140, 44)
	m.Open(sections)
	height := m.height

	for range len(Kinds) + 1 {
		m.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
		m.SetSections(sections) // zoxide answering, a pin changing
		assert.Equal(t, height, m.height, "scope %d", m.scope)
	}
}

func TestEnterGoesToAFolderBeforeASession(t *testing.T) {
	m := New()
	m.SetSize(140, 40)
	m.Open([]Section{
		{Kind: Session, Items: items(Session, "marmota docs")},
		{Kind: Folder, Items: []Item{{Kind: Folder, Key: "/d/marmota", Title: "marmota", Dir: true}}, Matched: true},
	})
	typeText(&m, "marmota")

	it, ok := m.Picked()
	require.True(t, ok)
	assert.Equal(t, Folder, it.Kind, "listed after the session, still where enter goes")
	assert.Contains(t, ansi.Strip(m.Render(true)), "↵ 2 ▸ marmota")

	m.Open([]Section{{Kind: Session, Items: items(Session, "marmota docs")}, {Kind: Folder}})
	typeText(&m, "marmota")
	it, _ = m.Picked()
	assert.Equal(t, Session, it.Kind, "no place matches: the session")
}

func TestAShortPanelDropsBlankLinesThenEmptySections(t *testing.T) {
	sections := testSections()
	sections[0].Items = nil // nothing pinned
	m := New()
	m.SetSize(60, 15)
	m.Open(sections)

	view := ansi.Strip(m.Render(true))
	assert.NotContains(t, view, "Pinned", "empty, and rows are short")
	for _, title := range []string{"group the logos", "logo sizes", "site", "file0"} {
		assert.Contains(t, view, title, "each section with items shows one at least")
	}

	m.SetSize(60, 40)
	view = ansi.Strip(m.Render(true))
	assert.Contains(t, view, "Pinned", "room enough: every section")
	assert.Contains(t, view, "none")
}
