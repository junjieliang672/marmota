package internal

import (
	"os"
	"path/filepath"
	"strconv"
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
	// Details is as tall as FILE, MAC and two operations: with twelve
	// operations, it can scroll.
	for i := range 12 {
		require.NoError(t, m.processBarModel.AddProcess(processbar.NewProcess(strconv.Itoa(i), "copy", 1)))
	}
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
	d := m.lay.details
	require.True(t, d.shown(), "Details is at the sidebar's foot")
	require.Equal(t, m.lay.body, d.y+d.h)

	assert.Equal(t, detailsArea, m.previewUnder(d.x, d.y))
	assert.Equal(t, detailsArea, m.previewUnder(d.x+d.w-1, d.y+d.h-1))
	assert.NotEqual(t, detailsArea, m.previewUnder(d.x, d.y-1), "the Ask box above it isn't Details")
	assert.NotEqual(t, detailsArea, m.previewUnder(d.x+d.w, d.y), "nor is what is right of the sidebar")

	m.toggleFooterController()
	assert.NotEqual(t, detailsArea, m.previewUnder(d.x, d.y))
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
