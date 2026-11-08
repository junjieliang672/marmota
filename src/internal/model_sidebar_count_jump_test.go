package internal

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestSidebarRender_ShowsContinuousLineNumbersAcrossPinnedAndRecent(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	pinnedOne := filepath.Join(tempDir, "pinned-one")
	pinnedTwo := filepath.Join(tempDir, "pinned-two")
	recentOne := filepath.Join(tempDir, "recent-one.txt")
	recentTwo := filepath.Join(tempDir, "recent-two.txt")
	utils.SetupDirectories(t, root, pinnedOne, pinnedTwo)
	utils.SetupFiles(t, recentOne, recentTwo)

	configureSidebarPinnedAndRecentForTest(t, tempDir,
		[]sidebarTestEntry{
			{Location: pinnedOne, Name: "pinned-one", Timestamp: 2},
			{Location: pinnedTwo, Name: "pinned-two", Timestamp: 1},
		},
		[]sidebarTestEntry{
			{Location: recentOne, Name: filepath.Base(recentOne), Timestamp: 2},
			{Location: recentTwo, Name: filepath.Base(recentTwo), Timestamp: 1},
		},
	)

	m := defaultTestModel(root)
	m.focusOnSideBar()

	plain := ansi.Strip(m.sidebarPopupRender())
	assert.Regexp(t, `(?m)\s1 pinned-one`, plain)
	assert.Regexp(t, `(?m)\s2 pinned-two`, plain)
	assert.Regexp(t, `(?m)\s3 recent-one\.txt`, plain)
	assert.Regexp(t, `(?m)\s4 recent-two\.txt`, plain)
}

func TestSidebarCountJump_JumpsAcrossPinnedAndRecent(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	pinnedOne := filepath.Join(tempDir, "pinned-one")
	pinnedTwo := filepath.Join(tempDir, "pinned-two")
	recentOne := filepath.Join(tempDir, "recent-one.txt")
	recentTwo := filepath.Join(tempDir, "recent-two.txt")
	utils.SetupDirectories(t, root, pinnedOne, pinnedTwo)
	utils.SetupFiles(t, recentOne, recentTwo)

	configureSidebarPinnedAndRecentForTest(t, tempDir,
		[]sidebarTestEntry{
			{Location: pinnedOne, Name: "pinned-one", Timestamp: 2},
			{Location: pinnedTwo, Name: "pinned-two", Timestamp: 1},
		},
		[]sidebarTestEntry{
			{Location: recentOne, Name: filepath.Base(recentOne), Timestamp: 2},
			{Location: recentTwo, Name: filepath.Base(recentTwo), Timestamp: 1},
		},
	)

	m := defaultTestModel(root)
	m.focusOnSideBar()

	TeaUpdate(m, utils.TeaRuneKeyMsg("3"))
	TeaUpdate(m, utils.TeaRuneKeyMsg("G"))

	assert.Equal(t, recentOne, m.sidebarModel.GetCurrentDirectoryLocation())
	assert.Empty(t, m.sidebarJumpCount)
}

type sidebarTestEntry struct {
	Location  string `json:"location"`
	Name      string `json:"name"`
	Timestamp int64  `json:"timestamp"`
}

func configureSidebarPinnedAndRecentForTest(
	t *testing.T,
	tempDir string,
	pinned []sidebarTestEntry,
	recent []sidebarTestEntry,
) {
	t.Helper()

	pinnedFile := filepath.Join(tempDir, "pinned.json")
	recentFile := filepath.Join(tempDir, "recent.json")

	pinnedData := make([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		PinnedAt int64  `json:"pinned_at,omitempty"`
	}, 0, len(pinned))
	for _, entry := range pinned {
		pinnedData = append(pinnedData, struct {
			Location string `json:"location"`
			Name     string `json:"name"`
			PinnedAt int64  `json:"pinned_at,omitempty"`
		}{
			Location: entry.Location,
			Name:     entry.Name,
			PinnedAt: entry.Timestamp,
		})
	}

	recentData := make([]struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		EditedAt int64  `json:"edited_at,omitempty"`
	}, 0, len(recent))
	for _, entry := range recent {
		recentData = append(recentData, struct {
			Location string `json:"location"`
			Name     string `json:"name"`
			EditedAt int64  `json:"edited_at,omitempty"`
		}{
			Location: entry.Location,
			Name:     entry.Name,
			EditedAt: entry.Timestamp,
		})
	}

	pinnedJSON, err := json.Marshal(pinnedData)
	require.NoError(t, err)
	recentJSON, err := json.Marshal(recentData)
	require.NoError(t, err)
	utils.SetupFilesWithData(t, pinnedJSON, pinnedFile)
	utils.SetupFilesWithData(t, recentJSON, recentFile)

	setForTest(t, &variable.PinnedFile, pinnedFile)
	setForTest(t, &variable.RecentEditsFile, recentFile)
}
