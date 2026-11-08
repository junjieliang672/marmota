package internal

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// panelRow returns the file panel row that shows name.
func panelRow(t *testing.T, m *model, name string) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(m.filePanelRender()), "\n") {
		if strings.Contains(line, " "+name+" ") || strings.HasSuffix(strings.TrimRight(line, " │"), " "+name) {
			return line
		}
	}
	require.Failf(t, "no row", "for %s", name)
	return ""
}

func TestFileRowsShowWhatSessionsAndGitChanged(t *testing.T) {
	dir := t.TempDir()
	utils.SetupDirectories(t, filepath.Join(dir, "src"), filepath.Join(dir, "docs"))
	utils.SetupFiles(t, filepath.Join(dir, "build.sh"), filepath.Join(dir, "README.md"),
		filepath.Join(dir, "src", "a.go"), filepath.Join(dir, "src", "b.go"))
	m := defaultTestModel(dir)
	TeaUpdate(m, nil)
	m.claude.snapshot = claudesession.Snapshot{Live: []claudesession.Session{{ID: "s1", Live: true,
		EditedFiles: []string{filepath.Join(dir, "src", "a.go"), filepath.Join(dir, "src", "b.go"),
			filepath.Join(dir, "README.md")}}}}
	m.sidebarModel.SetGitStatus(sidebar.GitStatus{IsGitRepo: true, LastPath: dir, Root: dir,
		Changes: map[string]string{"build.sh": "M", "src/a.go": "M"}})

	assert.True(t, strings.HasSuffix(strings.TrimRight(panelRow(t, m, "src"), " │"), "✳2 M"),
		"a folder counts the files sessions edited inside, then git's letter: %q", panelRow(t, m, "src"))
	assert.True(t, strings.HasSuffix(strings.TrimRight(panelRow(t, m, "README.md"), " │"), "✳"),
		panelRow(t, m, "README.md"))
	assert.True(t, strings.HasSuffix(strings.TrimRight(panelRow(t, m, "build.sh"), " │"), "M"),
		panelRow(t, m, "build.sh"))
	assert.NotContains(t, panelRow(t, m, "docs"), "✳")

	m.claude.snapshot.Live[0].Live = false
	m.claude.snapshot.Past, m.claude.snapshot.Live = m.claude.snapshot.Live, nil
	assert.NotContains(t, panelRow(t, m, "README.md"), "✳", "only running sessions count")
}
