package internal

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/sysstat"
)

func detailsTestModel(t *testing.T) *model {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	utils.SetupDirectories(t, root)
	for i := range 3 {
		utils.SetupFiles(t, filepath.Join(root, string(rune('a'+i))+".txt"))
	}
	m := defaultTestModel(root)
	TeaUpdate(m, tea.WindowSizeMsg{Width: 2 * DefaultTestModelWidth, Height: DefaultTestModelHeight})
	if !m.toggleFooter {
		m.toggleFooterController()
	}
	require.True(t, m.toggleFooter)
	return m
}

func TestDetailsReplacesTheFooterBoxes(t *testing.T) {
	m := detailsTestModel(t)
	m.macStats = &sysstat.Stats{MemOK: true, MemUsed: 1 << 30, MemTotal: 4 << 30}

	screen := ansi.Strip(m.View())

	assert.Contains(t, screen, "Details")
	assert.Contains(t, screen, "MAC")
	assert.NotContains(t, screen, "Processes")
	assert.NotContains(t, screen, "Metadata")
}

func TestMFocusesDetailsAndArrowsScrollIt(t *testing.T) {
	m := detailsTestModel(t)
	m.macStats = &sysstat.Stats{CPUOK: true, MemOK: true, MemTotal: 1, DiskOK: true, DiskTotal: 1}
	m.details.SetDimensions(m.fullWidth, 3) // one line shows, so it can scroll
	m.View()

	TeaUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(common.Hotkeys.FocusOnMetaData[0])})
	require.Equal(t, metadataFocus, m.focusPanel)

	before := ansi.Strip(m.detailsRender())
	m.listDownKey()
	assert.NotEqual(t, before, ansi.Strip(m.detailsRender()), "down scrolls Details")

	TeaUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(common.Hotkeys.FocusOnMetaData[0])})
	assert.Equal(t, nonePanelFocus, m.focusPanel, "M again gives the keyboard back")
}

func TestStatusLineMeterOnlyWhileDetailsIsHidden(t *testing.T) {
	m := detailsTestModel(t)
	p := processbar.NewProcess("1", "copy 12", 12)
	p.Done = 6
	require.NoError(t, m.processBarModel.AddProcess(p))

	assert.Empty(t, m.opsStatus(), "Details is on screen, so it shows the copy")

	m.toggleFooterController()
	require.False(t, m.toggleFooter)
	assert.Equal(t, "copy 12 · 6 of 12 · 50% ▓▓▓▓▓░░░░░", ansi.Strip(m.opsStatus()))
}

func TestWheelOverDetails(t *testing.T) {
	m := detailsTestModel(t)
	top := m.mainPanelHeight + common.BorderPadding

	x := m.dockSessionsWidth() // Details starts after the Sessions box
	assert.Equal(t, detailsArea, m.previewUnder(x, top))
	assert.Equal(t, detailsArea, m.previewUnder(x, top+m.footerHeight+1))
	assert.NotEqual(t, detailsArea, m.previewUnder(x, top-1))
	assert.NotEqual(t, detailsArea, m.previewUnder(x-1, top), "the Sessions box isn't Details")

	m.toggleFooterController()
	assert.NotEqual(t, detailsArea, m.previewUnder(x, top))
}

func TestMOpensAHiddenDetailsBox(t *testing.T) {
	m := detailsTestModel(t)
	m.toggleFooterController()
	require.False(t, m.toggleFooter)

	TeaUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(common.Hotkeys.FocusOnMetaData[0])})

	assert.True(t, m.toggleFooter, "M shows Details")
	assert.Equal(t, metadataFocus, m.focusPanel, "and gives it the keyboard")
	assert.Contains(t, ansi.Strip(m.View()), "Details")
}

func TestShortPath(t *testing.T) {
	home := t.TempDir()
	setForTest(t, &variable.HomeDir, home)
	for _, d := range []string{"Library/CloudStorage/Dropbox", "Library/CloudStorage/GoogleDrive-a@x.com",
		"Library/CloudStorage/GoogleDrive-b@x.com"} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, d), 0o755))
	}
	for path, want := range map[string]string{
		home + "/Developer/superfile/": "~/Developer/superfile/",
		home:                           "~",
		home + "/Library/Mobile Documents/com~apple~CloudDocs/MyMarmot/important docs": "iCloud/MyMarmot/important docs",
		home + "/Library/Mobile Documents/iCloud~md~obsidian/Documents/vault":          "obsidian (iCloud)/Documents/vault",
		home + "/Library/Mobile Documents/57T9237FN3~net~whatsapp~WhatsApp":            "WhatsApp (iCloud)",
		home + "/Library/CloudStorage/Dropbox/Photos":                                  "Dropbox/Photos",
		home + "/Library/CloudStorage/GoogleDrive-a@x.com/My Drive":                    "Google Drive (a@x.com)/My Drive",
		"/private/tmp/build/out": "/tmp/build/out",
		"/etc/hosts":             "/etc/hosts",
		home + "x/a":             home + "x/a",
	} {
		assert.Equal(t, want, shortPath(path), path)
	}
}
