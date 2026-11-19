package ask

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

var linkAt = regexp.MustCompile("\x1b]8;;([^\x1b]*)\x1b\\\\")

// linksIn is what each line links to, in order, without the ends of links.
func linksIn(lines []string) [][]string {
	out := make([][]string, len(lines))
	for i, l := range lines {
		for _, m := range linkAt.FindAllStringSubmatch(l, -1) {
			if m[1] != "" {
				out[i] = append(out[i], m[1])
			}
		}
	}
	return out
}

func allLinks(lines []string) []string {
	var out []string
	for _, l := range linksIn(lines) {
		out = append(out, l...)
	}
	return out
}

func TestALinkShowsItsTextAndKeepsWhereItGoes(t *testing.T) {
	lines := markdownLines("see [PR #12](https://github.com/x/y/pull/12) now", 60, newAnswerStyles("").textStyles)
	require.Len(t, lines, 1)
	assert.Equal(t, "see PR #12 now", ansi.Strip(lines[0]))
	assert.Equal(t, []string{"https://github.com/x/y/pull/12"}, allLinks(lines))
}

func TestAnAddressWrittenOutIsALinkOnEveryLineItTakes(t *testing.T) {
	address := "https://example.com/" + strings.Repeat("a", 50)
	lines := markdownLines("look at "+address+".", 30, newAnswerStyles("").textStyles)
	require.Greater(t, len(lines), 1)
	for i, l := range linksIn(lines) {
		if !strings.Contains(ansi.Strip(lines[i]), "a") || strings.TrimSpace(ansi.Strip(lines[i])) == "look at" {
			continue
		}
		assert.Equal(t, []string{address}, l, "line %d: %q", i, ansi.Strip(lines[i]))
	}
}

func TestPunctuationAfterAnAddressIsNotPartOfIt(t *testing.T) {
	assert.Equal(t, []string{"https://a.com/x"},
		allLinks(markdownLines("（见 https://a.com/x）。", 60, newAnswerStyles("").textStyles)))
	assert.Equal(t, []string{"https://a.com/x"},
		allLinks(markdownLines("(see https://a.com/x).", 60, newAnswerStyles("").textStyles)))
	assert.Equal(t, []string{"https://en.wikipedia.org/wiki/Go_(game)"},
		allLinks(markdownLines("https://en.wikipedia.org/wiki/Go_(game)", 60, newAnswerStyles("").textStyles)))
}

func TestCodeIsNoLinkButAFileItNamesIs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "app.go"), nil, 0o600))
	st := newAnswerStyles("").textStyles
	st.dir = dir
	assert.Empty(t, allLinks(markdownLines("```\ncurl https://a.com/x\n```", 60, st)), "a code block")
	assert.Empty(t, allLinks(markdownLines("`curl https://a.com/x`", 60, st)), "code in a line")
	assert.Equal(t, []string{"file://" + dir + "/src/app.go#L12"},
		allLinks(markdownLines("changed `src/app.go:12`", 60, st)))
	assert.Equal(t, []string{"file://" + dir + "/src/app.go"},
		allLinks(markdownLines("see src/app.go for it", 60, st)))
	assert.Empty(t, allLinks(markdownLines("see src/gone.go for it", 60, st)), "no such file")
}

func TestALinkToASessionOnClaudeAIOpensInTheClaudeApp(t *testing.T) {
	assert.Equal(t, []string{"claude://claude.ai/code/session_01abc"},
		allLinks(markdownLines("https://claude.ai/code/session_01abc", 60, newAnswerStyles("").textStyles)))
}

func TestWhatYouAskedIsOnABandAcrossThePreview(t *testing.T) {
	st := newPaneStyles()
	lines := promptLines("what changed?", "", 40, st)
	require.Len(t, lines, 1)
	assert.Equal(t, 40, ansi.StringWidth(lines[0]), "the band goes across")
	assert.Equal(t, "› what changed?", strings.TrimRight(ansi.Strip(lines[0]), " "))
}

func TestThePreviewKeepsWhereALinkGoesAndPutsALineBetweenTurns(t *testing.T) {
	s := Session{Title: "x", Turns: []claudesession.Turn{
		{Prompt: "first", Reply: "done"},
		{Prompt: "where?", Reply: "in [PR #12](https://github.com/x/y/pull/12)"},
	}}
	c := NewComposer()
	out := RenderSession(s, c.Render(58, Sending{}), 60, 20, 0, false)
	assert.Contains(t, allLinks(strings.Split(out, "\n")), "https://github.com/x/y/pull/12",
		"through the panel's renderer, which takes out control characters")
	assert.Contains(t, ansi.Strip(out), "in PR #12")
	assert.Contains(t, ansi.Strip(out), "────────", "a line between the two turns")
}
