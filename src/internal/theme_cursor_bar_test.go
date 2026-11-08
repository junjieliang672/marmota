package internal

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

// withTheme renders in true color with the theme changed by edit, and puts
// everything back afterwards.
func withTheme(t *testing.T, edit func(*common.ThemeType)) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	original := common.Theme
	t.Cleanup(func() {
		common.Theme = original //nolint:reassign // the test changes the theme and puts it back
		common.LoadThemeConfig()
		common.LoadPrerenderedVariables()
		lipgloss.SetColorProfile(profile)
	})
	lipgloss.SetColorProfile(termenv.TrueColor)
	edit(&common.Theme)
	common.LoadThemeConfig()
	common.LoadPrerenderedVariables()
}

const panelLine = "notes.md"

// sgr is the escape code lipgloss writes for style, e.g. "48;2;48;50;68".
func sgr(style lipgloss.Style) string {
	out := style.Render("x")
	start, end := strings.Index(out, "["), strings.Index(out, "m")
	if start < 0 || end < start {
		return "no color"
	}
	return out[start+1 : end]
}

func panelLines(t *testing.T, m *model) []string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(line), "assets") || strings.Contains(ansi.Strip(line), panelLine) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestThemeCursorBarAndFolderColor(t *testing.T) {
	withTheme(t, func(th *common.ThemeType) {
		th.FilePanelCursorBG = "#313244"
		th.FilePanelCursorFG = "#98d0fd"
		th.FilePanelDirectoryFG = "#89b4fa"
	})
	root := t.TempDir()
	utils.SetupDirectories(t, filepath.Join(root, "assets"))
	utils.SetupFiles(t, filepath.Join(root, panelLine))
	m := defaultTestModel(root)
	TeaUpdate(m, nil)
	barBG := sgr(lipgloss.NewStyle().Background(lipgloss.Color("#313244")))
	barFG := sgr(lipgloss.NewStyle().Foreground(lipgloss.Color("#98d0fd")))
	folderFG := sgr(lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa")))

	lines := panelLines(t, m)
	require.Len(t, lines, 2, "the folder row and the file row")
	cursorRow, fileRow := lines[0], lines[1]
	assert.Contains(t, cursorRow, barBG, "the cursor row is drawn on the bar")
	assert.Contains(t, cursorRow, barFG, "in the cursor color")
	assert.NotContains(t, fileRow, barBG, "other rows are not")

	setFilePanelSelectedItemByLocation(t, m.getFocusedFilePanel(), filepath.Join(root, panelLine))
	TeaUpdate(m, nil)
	lines = panelLines(t, m)
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], folderFG, "folder names take the folder color")
	assert.NotContains(t, lines[0], barBG)
	assert.Contains(t, lines[1], barBG)

	barStart := strings.Index(lines[1], barBG)
	assert.Contains(t, lines[1][barStart:], strings.Repeat(" ", 4), "the bar runs past the name to the panel edge")
}

func TestThemesWithoutTheNewKeysLookAsBefore(t *testing.T) {
	withTheme(t, func(th *common.ThemeType) {
		th.FilePanelCursorBG = ""
		th.FilePanelDirectoryFG = ""
	})
	root := t.TempDir()
	utils.SetupDirectories(t, filepath.Join(root, "assets"))
	utils.SetupFiles(t, filepath.Join(root, panelLine))
	m := defaultTestModel(root)
	TeaUpdate(m, nil)
	barBG := sgr(lipgloss.NewStyle().Background(lipgloss.Color("#313244")))
	folderFG := sgr(lipgloss.NewStyle().Foreground(lipgloss.Color("#89b4fa")))

	for _, line := range panelLines(t, m) {
		assert.NotContains(t, line, barBG)
		assert.NotContains(t, line, folderFG)
	}
}
