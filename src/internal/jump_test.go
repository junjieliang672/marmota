package internal

import (
	"encoding/json"
	"github.com/adrg/xdg"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/junjieliang672/marmota/src/config/icon"
	"path/filepath"
	"strings"
	"testing"
	"time"

	zoxidelib "github.com/lazysegtree/go-zoxide"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

type sidebarTestEntry struct {
	Location  string
	Name      string
	Timestamp int64
}

// configureSidebarPinnedAndRecentForTest writes the pinned and recently
// edited lists to files of the test's own.
func configureSidebarPinnedAndRecentForTest(t *testing.T, tempDir string, pinned, recent []sidebarTestEntry) {
	t.Helper()
	type row struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
		EditedAt int64  `json:"edited_at,omitempty"`
	}
	var pinnedRows, recentRows []row
	for _, e := range pinned {
		pinnedRows = append(pinnedRows, row{Location: e.Location, Name: e.Name, PinnedAt: e.Timestamp})
	}
	for _, e := range recent {
		recentRows = append(recentRows, row{Location: e.Location, Name: e.Name, EditedAt: e.Timestamp})
	}
	pinnedFile := filepath.Join(tempDir, "pinned.json")
	recentFile := filepath.Join(tempDir, "recent.json")
	for file, rows := range map[string][]row{pinnedFile: pinnedRows, recentFile: recentRows} {
		data, err := json.Marshal(rows)
		require.NoError(t, err)
		utils.SetupFilesWithData(t, data, file)
	}
	setForTest(t, &variable.PinnedFile, pinnedFile)
	setForTest(t, &variable.RecentEditsFile, recentFile)
}

// jumpTestModel is a model whose s list lists a pinned folder and a pinned
// file, two recently edited files, the two running sessions of
// dockTestModel and an ended one.
type jumpFixture struct {
	m                              *model
	root, pinnedDir, pinnedFile    string
	recentA, recentB, recentFolder string
}

func jumpTestModel(t *testing.T) jumpFixture {
	t.Helper()
	ownAskHistory(t)
	f := jumpFixture{root: t.TempDir()}
	f.pinnedDir = filepath.Join(f.root, "logos")
	f.recentFolder = filepath.Join(f.root, "notes")
	f.pinnedFile = filepath.Join(f.root, "plan.md")
	f.recentA = filepath.Join(f.recentFolder, "logo-notes.md")
	f.recentB = filepath.Join(f.recentFolder, "todo.txt")
	utils.SetupDirectories(t, f.pinnedDir, f.recentFolder)
	utils.SetupFiles(t, f.pinnedFile, f.recentA, f.recentB)
	now := time.Now()
	configureSidebarPinnedAndRecentForTest(t, f.root,
		[]sidebarTestEntry{{Location: f.pinnedDir, Name: "logos", Timestamp: 2}, {Location: f.pinnedFile, Name: "plan.md", Timestamp: 1}},
		[]sidebarTestEntry{{Location: f.recentA, Name: "logo-notes.md", Timestamp: now.UnixNano()},
			{Location: f.recentB, Name: "todo.txt", Timestamp: now.Add(-time.Hour).UnixNano()}})
	f.m = dockTestModel(t)
	f.m.sidebarModel = sidebar.New()
	f.m.claude.snapshot.Past = []claudesession.Session{
		{ID: "ended-1", Title: "logo sizes for the deck", Cwd: f.root, LastActivity: now.Add(-2 * time.Hour)},
	}
	require.Nil(t, f.m.zClient, "no zoxide in tests: the Folders section stays empty")
	return f
}

func jumpTitles(m *model) []string {
	var titles []string
	for _, it := range m.jumpUI.Visible() {
		titles = append(titles, it.Title)
	}
	return titles
}

func TestSOpensTheListWithEverySection(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m

	pressKey(m, common.Hotkeys.GoTo[0])

	require.True(t, m.jumpUI.IsOpen())
	assert.Equal(t, []string{"logos", "plan.md", "provider registry", "ime hold tests", "logo sizes for the deck",
		"logo-notes.md", "todo.txt"}, jumpTitles(m),
		"numbered down the sections: Pinned, Sessions, Resume, Folders, Files")
	view := viewText(m)
	for _, s := range []string{"Pinned", "Sessions", "Resume", "Folders", "Files", "go to"} {
		assert.Contains(t, view, s)
	}
	assert.Contains(t, view, "enter:go   ↓:pick   tab:section   esc:close", "its keys, in the status line")
}

func TestEnterWithNothingTypedGoesHome(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])

	pressKey(m, "enter")

	assert.False(t, m.jumpUI.IsOpen())
	assert.Equal(t, variable.HomeDir, m.getFocusedFilePanel().location)
}

func TestEnterGoesToTheFirstMatch(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])

	typeInto(m, "todo")
	require.Equal(t, []string{"todo.txt"}, jumpTitles(m))
	assert.Contains(t, viewText(m), icon.Cursor+" 1 · todo.txt", "the cursor on where enter goes")
	pressKey(m, "enter")

	panel := m.getFocusedFilePanel()
	assert.Equal(t, f.recentFolder, panel.location, "a file: its folder")
	require.NotEmpty(t, panel.element)
	assert.Equal(t, "todo.txt", panel.element[panel.cursor].name, "with the cursor on it")
	assert.Equal(t, nonePanelFocus, m.focusPanel)
}

func TestEscStepsBackOneLevelAtATime(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	typeInto(m, "lo")
	pressKey(m, "down")
	require.True(t, m.jumpUI.Picking())

	pressKey(m, "esc")
	assert.False(t, m.jumpUI.Picking(), "picking: back to the search")
	assert.Equal(t, "lo", m.jumpUI.Query(), "what was typed stays")

	pressKey(m, "esc")
	assert.True(t, m.jumpUI.IsOpen())
	assert.Empty(t, m.jumpUI.Query(), "then it is cleared")

	pressKey(m, "esc")
	assert.False(t, m.jumpUI.IsOpen(), "then it closes")
}

func TestPickingMovesAndJumpsByNumber(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])

	pressKey(m, "down")
	it, _ := m.jumpUI.Picked()
	assert.Equal(t, "logos", it.Title, "↓ picks the first")
	typeInto(m, "6")
	pressKey(m, "g")
	it, _ = m.jumpUI.Picked()
	assert.Equal(t, "logo-notes.md", it.Title, "6g: row 6")
	pressKey(m, "g")
	it, _ = m.jumpUI.Picked()
	assert.Equal(t, "logos", it.Title, "g: the first")
	pressKey(m, "G")
	it, _ = m.jumpUI.Picked()
	assert.Equal(t, "todo.txt", it.Title, "G: the last")
	pressKey(m, "g")
	pressKey(m, "up")
	assert.False(t, m.jumpUI.Picking(), "↑ on the first row: back to the search")
}

func TestTPinsAndUnpinsThePickedItem(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	typeInto(m, "todo")
	pressKey(m, "down")

	pressKey(m, common.Hotkeys.PinnedDirectory[0])

	assert.Equal(t, "Pinned todo.txt", m.claude.flash)
	assert.Contains(t, pinnedLocations(m), f.recentB)

	pressKey(m, "esc")
	pressKey(m, "esc")
	pressKey(m, "down") // todo.txt, pinned last so listed first
	pressKey(m, common.Hotkeys.PinnedDirectory[0])
	assert.Equal(t, "Unpinned todo.txt", m.claude.flash)
	assert.NotContains(t, pinnedLocations(m), f.recentB)
}

func pinnedLocations(m *model) []string {
	var out []string
	for _, e := range m.sidebarModel.PinnedEntries() {
		out = append(out, e.Location)
	}
	return out
}

func TestShiftArrowsMoveAPin(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	pressKey(m, "down")

	pressKey(m, "shift+down")

	assert.Equal(t, []string{f.pinnedFile, f.pinnedDir}, pinnedLocations(m))
	it, _ := m.jumpUI.Picked()
	assert.Equal(t, "logos", it.Title, "the moved pin stays picked")
}

func TestRRenamesAPinsNameAndAFileOnDisk(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	pressKey(m, "down")
	pressKey(m, common.Hotkeys.FilePanelItemRename[0])
	require.True(t, m.jumpUI.Renaming())
	for range "logos" {
		pressKey(m, "backspace")
	}
	typeInto(m, "brand")
	pressKey(m, "enter")
	assert.Equal(t, "brand", m.sidebarModel.PinnedEntries()[0].Name, "a pin: its name in the list")
	assert.DirExists(t, f.pinnedDir, "the folder keeps its own name")

	pressKey(m, "G") // todo.txt, a recent file
	pressKey(m, common.Hotkeys.FilePanelItemRename[0])
	for range "todo.txt" {
		pressKey(m, "backspace")
	}
	typeInto(m, "done.txt")
	pressKey(m, "enter")
	assert.NoFileExists(t, f.recentB)
	assert.FileExists(t, filepath.Join(f.recentFolder, "done.txt"), "a file: renamed on disk")
	assert.Contains(t, jumpTitles(m), "done.txt", "and listed by its new name")
}

func TestPCopiesThePickedPath(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	var copied string
	setForTest(t, &writeClipboard, func(text string) error {
		copied = text
		return nil
	})
	pressKey(m, common.Hotkeys.GoTo[0])
	typeInto(m, "plan")
	pressKey(m, "down")

	pressKey(m, common.Hotkeys.CopyPath[0])

	assert.Equal(t, f.pinnedFile, copied)
}

func TestDeleteKeyForgetsARecentFileButKeepsIt(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	pressKey(m, "down")
	pressKey(m, "G")

	pressKey(m, common.Hotkeys.DeleteItems[0])

	assert.NotContains(t, jumpTitles(m), "todo.txt")
	assert.FileExists(t, f.recentB)
}

func TestEOpensThePickedFileInTheEditor(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	typeInto(m, "todo")
	pressKey(m, "down")

	cmd := m.jumpKey(utils.TeaKeyMsg(common.Hotkeys.OpenFileWithEditor[0]))

	assert.NotNil(t, cmd, "the editor runs")
	assert.False(t, m.jumpUI.IsOpen())
}

func TestASessionsKeysAreTheSessionsBoxs(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	typeInto(m, "provider")
	pressKey(m, "down")
	assert.Contains(t, viewText(m), "move here", "its keys are listed")

	pressKey(m, common.Hotkeys.SessionRename[0])

	assert.False(t, m.jumpUI.IsOpen())
	assert.Equal(t, dockSessionsFocus, m.focusPanel, "the Sessions box has it")
	assert.Equal(t, "s20", m.sessions.list.SelectedID())
	assert.True(t, m.sessions.list.IsRenaming(), "r renames it, as in the Sessions box")
}

func TestTabShowsOneSectionAlone(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])

	pressKey(m, "tab")
	assert.Equal(t, []string{"logos", "plan.md"}, jumpTitles(m), "Pinned alone")
	pressKey(m, "tab")
	assert.Equal(t, []string{"provider registry", "ime hold tests"}, jumpTitles(m), "then Sessions")
	pressKey(m, "shift+tab")
	pressKey(m, "shift+tab")
	assert.Len(t, jumpTitles(m), 7, "back to every section")
}

func TestTheListOpensFreshEachTime(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])
	pressKey(m, "tab")
	typeInto(m, "lo")
	pressKey(m, "esc")
	pressKey(m, "esc")

	pressKey(m, common.Hotkeys.GoTo[0])

	assert.Empty(t, m.jumpUI.Query())
	assert.Len(t, jumpTitles(m), 7, "every section")
}

func TestAPinnedFolderIsntListedTwice(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	pressKey(m, common.Hotkeys.GoTo[0])

	m.applyJumpZoxide(jumpZoxideMsg{query: "", results: []zoxidelib.Result{{Path: f.pinnedDir, Score: 9},
		{Path: f.recentFolder, Score: 3}}})

	assert.Equal(t, []string{"logos", "plan.md", "provider registry", "ime hold tests", "logo sizes for the deck",
		"notes", "logo-notes.md", "todo.txt"}, jumpTitles(m), "zoxide's logos is the pinned one")
}

func TestTheListTakesTheFocusedPanelsPlace(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	m.fileModel.filePreview.SetOpen(true)
	TeaUpdate(m, tea.WindowSizeMsg{Width: DefaultTestModelWidth, Height: DefaultTestModelHeight})
	before := strings.Split(viewText(m), "\n")

	pressKey(m, common.Hotkeys.GoTo[0])

	after := strings.Split(viewText(m), "\n")
	require.Len(t, after, len(before), "nothing over the screen: the same rows")
	assert.True(t, strings.HasPrefix(after[0], "╭─ go to "), "the panel's frame, named go to: %q", after[0])
	width := m.filePanelWidthAt(m.filePanelFocusIndex) + common.BorderPadding
	assert.Equal(t, before[0][strings.Index(before[0], "╮")+len("╮"):], after[0][strings.Index(after[0], "╮")+len("╮"):],
		"the preview's frame where it was")
	assert.Equal(t, width, ansi.StringWidth(after[0][:strings.Index(after[0], "╮")+len("╮")]), "the panel's width")
	assert.Contains(t, viewText(m), "Sessions", "the dock stays below")
}

func TestThePreviewShowsWhatTheListIsOn(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	m.fileModel.filePreview.SetOpen(true)
	pressKey(m, common.Hotkeys.GoTo[0])

	path, ok := m.previewPath()
	require.True(t, ok)
	assert.Equal(t, xdg.Home, path, "nothing typed: home, where enter goes")

	typeInto(m, "todo")
	path, _ = m.previewPath()
	assert.Equal(t, f.recentB, path, "the first match: a file")
	m.getFilePreviewCmd(true)
	assert.Equal(t, f.recentB, m.fileModel.filePreview.GetLocation())

	pressKey(m, "esc")
	typeInto(m, "provider")
	s, ok := m.jumpPreviewSession()
	require.True(t, ok, "a session")
	assert.Equal(t, "provider registry", s.DisplayTitle())
	preview := viewText(m)
	assert.Contains(t, preview, "✳ provider registry", "the preview draws it, as the Sessions box does")

	pressKey(m, "esc")
	pressKey(m, "esc")
	require.False(t, m.jumpUI.IsOpen())
	_, ok = m.jumpPreviewSession()
	assert.False(t, ok)
	path, ok = m.previewPath()
	if sel := m.getFocusedFilePanel().getSelectedItemPtr(); sel != nil {
		require.True(t, ok)
		assert.Equal(t, sel.location, path, "closed: the cursor's item again")
	}
}

func TestAnEndedSessionIsPreviewedFromItsTranscript(t *testing.T) {
	f := jumpTestModel(t)
	m := f.m
	m.fileModel.filePreview.SetOpen(true)
	pressKey(m, common.Hotkeys.GoTo[0])

	typeInto(m, "deck")
	s, ok := m.jumpPreviewSession()
	require.True(t, ok)
	assert.Equal(t, "ended-1", s.ID)
	top := strings.Split(viewText(m), "\n")[0]
	assert.Contains(t, top[strings.Index(top, "╮"):], "logo sizes for the deck", "the preview's title")
}
