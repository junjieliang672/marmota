package internal

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

// ownDownloads makes the test's own Downloads folder, with a folder inside
// it, and a folder elsewhere: it returns the two.
func ownDownloads(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	downloads, elsewhere := filepath.Join(root, "Downloads"), filepath.Join(root, "work")
	utils.SetupDirectories(t, downloads, filepath.Join(downloads, "unpacked"), elsewhere)
	original := variable.DownloadsDir
	variable.DownloadsDir = downloads                      //nolint:reassign // the test's own Downloads
	t.Cleanup(func() { variable.DownloadsDir = original }) //nolint:reassign // restored
	return downloads, elsewhere
}

func sortOf(panel *filePanel) (string, bool) {
	data := panel.sortOptions.data
	return data.options[data.selected], data.reversed
}

// The Downloads folder opens newest first. Other folders, those inside it
// too, keep the panel's own sort, which leaving Downloads gives back even
// after another sort was chosen there.
func TestDownloadsOpensNewestFirst(t *testing.T) {
	downloads, elsewhere := ownDownloads(t)
	panel := defaultFilePanel(elsewhere, true)
	own := panel.sortOptions.data
	require.Equal(t, string(sortingName), own.options[own.selected])

	require.NoError(t, panel.updateCurrentFilePanelDir(downloads))
	kind, reversed := sortOf(&panel)
	assert.Equal(t, string(sortingDateModified), kind)
	assert.Equal(t, "↓", sortArrow(kind, reversed), "newest first")
	assert.Equal(t, panel.sortOptions.data.selected, panel.sortOptions.cursor, "the sort menu opens on it")

	require.NoError(t, panel.updateCurrentFilePanelDir(filepath.Join(downloads, "unpacked")))
	assert.Equal(t, own, panel.sortOptions.data, "a folder inside Downloads keeps the panel's own sort")

	require.NoError(t, panel.updateCurrentFilePanelDir(downloads))
	panel.sortOptions.data.selected = 1 // Size, chosen there
	require.NoError(t, panel.updateCurrentFilePanelDir(downloads))
	kind, _ = sortOf(&panel)
	assert.Equal(t, string(sortingSize), kind, "a sort chosen in Downloads lasts while the panel stays")
	require.NoError(t, panel.updateCurrentFilePanelDir(elsewhere))
	assert.Equal(t, own, panel.sortOptions.data, "leaving it gives the panel back its own sort")

	require.NoError(t, panel.updateCurrentFilePanelDir(downloads))
	kind, _ = sortOf(&panel)
	assert.Equal(t, string(sortingDateModified), kind, "and the next visit is newest first again")
}

func TestAPanelStartedInDownloadsIsNewestFirst(t *testing.T) {
	downloads, elsewhere := ownDownloads(t)
	panel := defaultFilePanel(downloads, true)
	kind, reversed := sortOf(&panel)
	assert.Equal(t, string(sortingDateModified), kind)
	assert.Equal(t, "↓", sortArrow(kind, reversed))

	require.NoError(t, panel.updateCurrentFilePanelDir(elsewhere))
	kind, _ = sortOf(&panel)
	assert.Equal(t, string(sortingName), kind, "elsewhere it has the configured sort")
}

// A panel opened beside one that shows Downloads gets that panel's sort for
// other folders, unless it opens in Downloads too. The sidebar and the Ask
// bar's context name the sort as it is: newest first.
func TestPanelsOpenedFromDownloadsGetTheSortForOtherFolders(t *testing.T) {
	downloads, elsewhere := ownDownloads(t)
	m := defaultTestModel(elsewhere)
	TeaUpdate(m, nil)
	require.NoError(t, m.updateCurrentFilePanelDir(downloads))
	assert.Contains(t, ansi.Strip(m.View()), "date modified ↓", "the sort on the panel's edge")
	assert.Equal(t, "date modified ↓", m.askContext().Sort)

	require.NoError(t, m.createNewFilePanel(elsewhere)) // it is focused then
	kind, _ := sortOf(m.getFocusedFilePanel())
	assert.Equal(t, string(sortingName), kind)

	require.NoError(t, m.createNewFilePanel(downloads))
	kind, _ = sortOf(m.getFocusedFilePanel())
	assert.Equal(t, string(sortingDateModified), kind)
}
