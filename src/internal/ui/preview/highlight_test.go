package preview

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inColor draws with colors for the test, as in a terminal.
func inColor(t *testing.T) {
	t.Helper()
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
}

// Colored, a file reads as it is: nothing put in the middle of a line or
// padded at its end, even where one token runs on from one line into the
// next.
func TestHighlightingKeepsTheText(t *testing.T) {
	inColor(t)
	for name, c := range map[string]struct{ lexer, text string }{
		// The README that showed the gaps: "<station>" pushed right in the
		// code block, and the inline code at a line's end to the edge.
		"markdown": {"Markdown", "## Usage\n\n" +
			"    tidepool today <station>     today's highs and lows\n" +
			"    tidepool week <station>      the next seven days\n\n" +
			"Stations live in `data/stations.csv`.\n"},
		"a comment ending mid-line": {"Go", "package a\n\n/* one\n   a longer line */ var x = 1\n"},
	} {
		for _, background := range []string{"#1a1b26", ""} {
			out, err := highlightCode(c.text, c.lexer, "monokai", background)
			require.NoError(t, err, name)
			assert.Equal(t, c.text, ansi.Strip(out), "%s, background %q", name, background)
			assert.NotEqual(t, c.text, out, "%s: colored", name)
		}
	}
}

func TestHighlightingAnUnknownLanguageStillShowsTheText(t *testing.T) {
	out, err := highlightCode("plain\ntext\n", "no such language", "no such style", "")
	require.NoError(t, err)
	assert.Equal(t, "plain\ntext\n", ansi.Strip(out))
}
