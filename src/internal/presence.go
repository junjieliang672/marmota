package internal

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
)

// Presence (deck item 7): in the file panel, a file a running Claude session
// edited shows ✳, a folder shows how many such files are inside (✳3), and
// git's letter shows for a changed file or folder. They sit at the end of
// the row.

// fileBadges is what the file panel shows at the end of its rows.
type fileBadges struct {
	// touched holds the files running sessions edited.
	touched map[string]bool
	git     sidebar.GitStatus
}

// fileBadges collects the badges for the panels on screen.
func (m *model) fileBadges() fileBadges {
	b := fileBadges{git: m.sidebarModel.GetGitStatus()}
	for _, s := range m.claude.snapshot.Live {
		for _, f := range s.EditedFiles {
			if b.touched == nil {
				b.touched = make(map[string]bool)
			}
			b.touched[filepath.Clean(f)] = true
		}
	}
	return b
}

// touchedCount is 1 for a file a running session edited, and for a folder the
// number of such files anywhere inside it.
func (b fileBadges) touchedCount(path string, isDir bool) int {
	if len(b.touched) == 0 {
		return 0
	}
	if !isDir {
		if b.touched[path] {
			return 1
		}
		return 0
	}
	n := 0
	prefix := strings.TrimSuffix(path, string(filepath.Separator)) + string(filepath.Separator)
	for f := range b.touched {
		if strings.HasPrefix(f, prefix) {
			n++
		}
	}
	return n
}

// render draws a row's badges, "✳3 M", on the row's background; "" when it
// has none.
func (b fileBadges) render(path string, isDir bool, bg lipgloss.Color) string {
	var parts []string
	if n := b.touchedCount(path, isDir); n > 0 {
		text := "✳"
		if isDir {
			text += strconv.Itoa(n)
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color(common.ClaudeAgentColor)).
			Background(bg).Render(text))
	}
	if letter := b.git.Letter(path, isDir); letter != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(gitLetterColor(letter)).Background(bg).Render(letter))
	}
	return strings.Join(parts, lipgloss.NewStyle().Background(bg).Render(" "))
}

// gitLetterColor colors git's letters as git tools do: new files green,
// deleted ones red, changed ones yellow and untracked ones dim.
func gitLetterColor(letter string) lipgloss.Color {
	switch letter {
	case "A":
		return lipgloss.Color(common.Theme.Correct)
	case "D", "U":
		return lipgloss.Color(common.Theme.Error)
	case "?":
		return lipgloss.Color(common.GitUntrackedColor)
	default:
		return lipgloss.Color(common.ClaudeNeedsYouColor)
	}
}
