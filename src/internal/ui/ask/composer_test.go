package ask

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

func TestMain(m *testing.M) {
	if err := common.PopulateGlobalConfigs(); err != nil {
		panic(err)
	}
	m.Run()
}

func typeText(c *Composer, text string) {
	for _, r := range text {
		c.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	}
	panic(k)
}

func TestMentionsAreAddedOnceAndKeptWithTheirPaths(t *testing.T) {
	c := NewComposer()
	c.Focus()
	typeText(&c, "look at")
	assert.True(t, c.AddMention("@a.go", "/r/a.go"))
	assert.False(t, c.AddMention("@a.go", "/r/a.go"), "once is enough")
	assert.True(t, c.AddMention(`@"my notes.md"`, "/r/my notes.md"))
	assert.True(t, c.AddMention("@proj-1", ""), "a session's mention has no path")
	assert.Equal(t, `look at @a.go @"my notes.md" @proj-1 `, c.Value())
	assert.Equal(t, map[string]string{"@a.go": "/r/a.go", `@"my notes.md"`: "/r/my notes.md"}, c.Mentions())

	c.SetValue(`look at @"my notes.md"`)
	assert.Equal(t, map[string]string{`@"my notes.md"`: "/r/my notes.md"}, c.Mentions(),
		"a mention taken out of the question is let go")

	c.HandleKey(key("ctrl+u"))
	assert.Empty(t, c.Value())
	assert.Empty(t, c.Mentions())
}

func TestTheBarsKeys(t *testing.T) {
	c := NewComposer()
	c.Focus()
	c.HandleKey(key("shift+tab"))
	assert.Equal(t, "acceptEdits", c.PermissionMode())
	c.HandleKey(key("tab"))
	assert.Empty(t, c.Value(), "tab types nothing")
	assert.True(t, c.StartsCodex(), "it chooses Codex for a new session")
	c.HandleKey(key("tab"))
	assert.False(t, c.StartsCodex(), "and Claude Code again")
	c.HandleKey(key("ctrl+o"))
	model, _ := c.Choices()
	assert.Equal(t, "opus", model)

	c.SetHistory([]string{"first", "second"})
	typeText(&c, "draft")
	c.HandleKey(key("ctrl+p"))
	assert.Equal(t, "second", c.Value())
	c.HandleKey(key("ctrl+p"))
	assert.Equal(t, "first", c.Value())
	c.HandleKey(key("ctrl+p"))
	assert.Equal(t, "first", c.Value(), "it stops at the oldest")
	c.HandleKey(key("ctrl+n"))
	c.HandleKey(key("ctrl+n"))
	assert.Equal(t, "draft", c.Value(), "back to what you were writing")

	c.Sent("draft")
	assert.Empty(t, c.Value())
	assert.Equal(t, []string{"first", "second", "draft"}, c.History())
}

// With Codex chosen, the model and effort keys step through Codex's models,
// and the efforts the chosen one takes; Claude Code's choices stay.
func TestCodexHasItsOwnModelsAndEfforts(t *testing.T) {
	c := NewComposer()
	c.Focus()
	c.SetChoices("opus", "max")
	c.SetCodexModels([]codexsession.Model{
		{ID: "sol", Efforts: []string{"low", "ultra"}},
		{ID: "luna", Efforts: []string{"low", "high"}},
	}, "sol")
	c.HandleKey(key("tab"))
	choices := func() [2]string {
		model, effort := c.Choices()
		return [2]string{model, effort}
	}
	assert.Equal(t, [2]string{"", ""}, choices(), "Codex's own")
	c.HandleKey(key("ctrl+t"))
	c.HandleKey(key("ctrl+t"))
	assert.Equal(t, [2]string{"", "ultra"}, choices(), "Codex's own model, sol, takes ultra")
	c.HandleKey(key("ctrl+o"))
	assert.Equal(t, [2]string{"sol", "ultra"}, choices())
	c.HandleKey(key("ctrl+o"))
	assert.Equal(t, [2]string{"luna", ""}, choices(), "luna doesn't take ultra")
	c.HandleKey(key("ctrl+t"))
	c.HandleKey(key("ctrl+t"))
	assert.Equal(t, [2]string{"luna", "high"}, choices())
	c.HandleKey(key("ctrl+o"))
	assert.Equal(t, [2]string{"", ""}, choices(), "round to Codex's own, sol, which doesn't take high")
	c.HandleKey(key("ctrl+t"))

	c.HandleKey(key("tab"))
	assert.Equal(t, [2]string{"opus", "max"}, choices(), "Claude Code's choices stayed")
	model, effort := c.CodexChoices()
	assert.Equal(t, [2]string{"", "low"}, [2]string{model, effort}, "and so did Codex's")

	c.SetCodexChoices("retired", "ultra")
	c.SetCodexModels([]codexsession.Model{{ID: "sol", Efforts: []string{"low"}}}, "sol")
	model, effort = c.CodexChoices()
	assert.Equal(t, [2]string{"", ""}, [2]string{model, effort}, "what Codex no longer offers goes back to its own")
}

// A Codex session starts at medium unless another effort is chosen, or the
// model doesn't take medium.
func TestCodexSessionsStartAtMedium(t *testing.T) {
	c := NewComposer()
	assert.Equal(t, "medium", c.CodexStartEffort(), "Codex's models unknown")
	c.SetCodexModels([]codexsession.Model{
		{ID: "sol", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "low"},
		{ID: "mini", Efforts: []string{"low", "high"}, DefaultEffort: "low"},
	}, "sol")
	assert.Equal(t, "medium", c.CodexStartEffort(), "not Codex's own low")
	c.SetCodexChoices("mini", "")
	assert.Equal(t, "low", c.CodexStartEffort(), "mini doesn't take medium: its own")
	c.SetCodexChoices("sol", "high")
	assert.Equal(t, "high", c.CodexStartEffort())
}

// For a new session the keys line says what tab switches to; writing to a
// running one, whose agent is its own, it doesn't offer it.
func TestTheBarOffersTheOtherAgent(t *testing.T) {
	c := NewComposer()
	c.Focus()
	assert.Contains(t, ansi.Strip(c.Render(120, Sending{Text: "starts", New: true})), "tab Codex")
	c.SetStartsCodex(true)
	assert.Contains(t, ansi.Strip(c.Render(120, Sending{Text: "starts", New: true})), "tab Claude Code")
	assert.NotContains(t, ansi.Strip(c.Render(120, Sending{Text: "typed into its pane", Fixed: true})), "tab ")
}

func TestTheBarSaysWhereItSends(t *testing.T) {
	c := NewComposer()
	out := ansi.Strip(c.Render(60, Sending{Text: "typed into its pane (hidden)", Fixed: true}))
	assert.Contains(t, out, "K to write to the selected session", "without the keyboard it says how to get it")
	assert.Contains(t, out, "→ typed into its pane (hidden)")
	assert.Contains(t, out, "K write")
	assert.NotContains(t, out, "Ask")
	assert.NotContains(t, out, "Work")

	c.Focus()
	out = ansi.Strip(c.Render(60, Sending{Text: "starts a background session in project, ⏵⏵ bypass"}))
	assert.Contains(t, out, "› ", "the question starts after ›")
	assert.Contains(t, out, "Write to this session, or describe a task")
	assert.Contains(t, out, "enter send · ↑↓ history · ctrl+u clear")
	assert.Contains(t, out, "shift+tab permissions · esc back to the list", "wrapped, not cut")
	assert.NotContains(t, out, "…")
	assert.Contains(t, out, "ctrl+o model default", "Claude Code's own model, when its settings name none")
	for _, line := range strings.Split(out, "\n") {
		assert.Equal(t, 60, ansi.StringWidth(line))
	}
	c.HandleKey(key("ctrl+o"))
	assert.Contains(t, ansi.Strip(c.Render(60, Sending{})), "opus")

	c.SetNotice("couldn't start", true)
	assert.Contains(t, ansi.Strip(c.Render(60, Sending{})), "couldn't start")
	typeText(&c, "x")
	assert.NotContains(t, ansi.Strip(c.Render(60, Sending{})), "couldn't start", "typing clears it")
}

// The bar always says what a request goes with: for a new session the
// bar's choices, else Claude Code's settings; for a running one, what it
// answers with, which the bar's choices don't change.
func TestTheBarSaysTheModelAndEffort(t *testing.T) {
	c := NewComposer()
	c.Focus()
	out := ansi.Strip(c.Render(70, Sending{Text: "starts in the background", DefaultEffort: "medium"}))
	assert.Contains(t, out, "ctrl+o model default · ctrl+t effort medium", "on a line of their own, with their keys")
	assert.Contains(t, out, "→ starts in the background  ", "the line above keeps its room")

	out = ansi.Strip(c.Render(70, Sending{Text: "starts in the background",
		DefaultModel: "claude-opus-5-5", DefaultEffort: "medium"}))
	assert.Contains(t, out, "model opus 5.5 · ctrl+t effort medium", "the model Claude Code's settings name")

	c.HandleKey(key("ctrl+o"))
	c.HandleKey(key("ctrl+t"))
	out = ansi.Strip(c.Render(70, Sending{Text: "starts in the background", DefaultEffort: "medium"}))
	assert.Contains(t, out, "model opus · ctrl+t effort high", "what the bar chose, from medium")

	out = ansi.Strip(c.Render(70, Sending{Text: "typed into its pane", Fixed: true,
		Model: "claude-sonnet-4-5-20250929", Effort: "high"}))
	assert.Contains(t, out, "model sonnet 4.5 · effort high", "a running session's own, not the bar's")
	assert.NotContains(t, out, "ctrl+o", "which the bar can't change")
}

func TestALongQuestionShowsItsLastRows(t *testing.T) {
	c := NewComposer()
	c.Focus()
	typeText(&c, strings.Repeat("word ", 40)+"end")
	out := ansi.Strip(c.Render(40, Sending{}))
	assert.Contains(t, out, "end", "the rows with the cursor show")
	rows := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "word") {
			rows++
		}
	}
	assert.Equal(t, maxInputRows, rows, "at most three rows of question")

	lines := c.RenderLines(36, 5, Sending{})
	assert.Len(t, lines, 5, "within the rows there are")
	assert.Contains(t, ansi.Strip(lines[0]), "end", "the question keeps the row with the cursor")
}

func TestSessionMentionsHaveTheirOwnColor(t *testing.T) {
	c := NewComposer()
	c.AddMention("@a.go", "/r/a.go")
	c.AddMention("@proj-1", "")
	c.AddMention(`@"my session"`, "")
	kinds := c.mentionKinds([]rune(c.Value()))
	value := c.Value()
	at := func(s string) mentionKind {
		i := strings.Index(value, s)
		require.GreaterOrEqual(t, i, 0, s)
		return kinds[len([]rune(value[:i]))]
	}
	assert.Equal(t, fileMention, at("@a.go"))
	assert.Equal(t, sessionMention, at("@proj-1"))
	assert.Equal(t, sessionMention, at(`@"my session"`))
}

func TestRenderSessionLaysOutTheSession(t *testing.T) {
	c := NewComposer()
	s := Session{
		Glyph: "✳", Title: "proj-1 · fix the login",
		State: "working", Place: "beside Marmota", Mode: "bypass",
		Folder: "~/w/repo", Branch: "main",
		Now: "Bash(go test ./...)",
		Turns: []claudesession.Turn{
			{Prompt: "why does login fail?", Reply: "The **token** expires.", Tools: []string{"Read(auth.go)"},
				At: time.Now()},
		},
		Tasks: []claudesession.Todo{{Content: "fix it", Status: "in_progress"}, {Content: "test it"}},
		Files: []string{"auth.go"},
		Mark:  "token",
	}
	out := ansi.Strip(RenderSession(s, c.Render(58, Sending{Text: "typed into its pane"}), 60, 30, 0, false))
	lines := strings.Split(out, "\n")
	assert.Len(t, lines, 30)
	for _, want := range []string{
		"✳ proj-1 · fix the login", "working · beside Marmota · ⏵⏵ bypass", "~/w/repo · main",
		"Conversation", "› why does login fail?", "  ⎿ Read(auth.go)",
		"▌ The token expires.", "Tasks", "◐ fix it", "☐ test it", "Changed files", "auth.go",
		"→ typed into its pane",
	} {
		assert.Contains(t, out, want)
	}
	reply := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "The token expires.") })
	require.GreaterOrEqual(t, reply, 0)
	assert.Equal(t, "✳ Bash(go test ./...)", strings.Trim(lines[reply+2], "│ "), "under the reply, a line apart")

	// The conversation stays where it is whether a tool runs or not.
	s.Now = "working"
	between := strings.Split(ansi.Strip(RenderSession(s, c.Render(58, Sending{}), 60, 30, 0, false)), "\n")
	assert.Equal(t, reply, slices.IndexFunc(between, func(l string) bool { return strings.Contains(l, "The token expires.") }))
	assert.Equal(t, "✳ working", strings.Trim(between[reply+2], "│ "))

	s.Question = []string{"☐ Color", "Do you prefer red or blue?", "❯ 1. Red", "  2. Blue"}
	out = ansi.Strip(RenderSession(s, c.Render(58, Sending{}), 60, 30, 0, true))
	assert.Contains(t, out, "K to answer")
	assert.Contains(t, out, "▎ ❯ 1. Red", "behind the question's bar")
	s.Answering = true
	assert.Contains(t, ansi.Strip(RenderSession(s, c.Render(58, Sending{}), 60, 30, 0, true)),
		"keys go to it · ctrl+k back")
}

func TestRenderSessionScrollsTheConversation(t *testing.T) {
	var turns []claudesession.Turn
	for i := range 20 {
		turns = append(turns, claudesession.Turn{Prompt: "question " + string(rune('a'+i)), Reply: "answer"})
	}
	c := NewComposer()
	s := Session{Title: "x", Turns: turns}
	out := ansi.Strip(RenderSession(s, c.Render(58, Sending{}), 60, 20, 0, false))
	assert.Contains(t, out, "question t", "the newest turn shows")
	assert.NotContains(t, out, "question a")
	out = ansi.Strip(RenderSession(s, c.Render(58, Sending{}), 60, 20, 100, false))
	assert.Contains(t, out, "question a", "scrolled back, the oldest")
	require.NotContains(t, out, "question t")
}

// A reply's Markdown is drawn: a table in a box, a heading, lists, code.
func TestARepliesMarkdownIsDrawn(t *testing.T) {
	reply := "## Go vs Rust\n\n| Language | Typing | Note |\n|---|---|---|\n" +
		"| Go | Static, strong | Simple syntax and goroutines |\n| Rust | Static | Memory safety |\n\n" +
		"- uses `go build`\n- [x] done\n\n```python\nprint(1)\n```\n\n> a quote and a [link](https://x.y)"
	c := NewComposer()
	out := ansi.Strip(RenderSession(Session{Title: "x", Turns: []claudesession.Turn{{Prompt: "compare", Reply: reply}}},
		c.Render(58, Sending{}), 60, 40, 0, false))
	for _, want := range []string{
		"⏺ Go vs Rust", "┌──────────┬", "│ Language │", "├──────────┼", "│ Go       │", "└──────────┴",
		"• uses go build", "☑ done", "print(1)", "▎ a quote and a link",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "|---|", "no raw table syntax")
	assert.NotContains(t, out, "```")
}

func TestAPreviewTooWideGoesUnderTheOptions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "preview-question.txt"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	require.Greater(t, ansi.StringWidth(lines[4]), 48)

	out := stackColumns(lines, 48)

	for _, l := range out {
		if strings.ContainsAny(l, "┌│└❯") {
			assert.LessOrEqual(t, ansi.StringWidth(l), 48, "%q", l)
		}
	}
	joined := strings.Join(out, "\n")
	options := strings.Index(joined, "2. Top bar")
	box := strings.Index(joined, "┌")
	assert.Positive(t, options)
	assert.Greater(t, box, options, "the preview comes after the options")
	assert.Contains(t, joined, "│ | Nav    | Content          |            │", "and is whole")
	assert.Contains(t, joined, "Notes: press n to add notes")
	assert.Less(t, strings.Index(joined, "Notes"), strings.Index(joined, "Chat about this"))

	assert.Equal(t, lines, stackColumns(lines, 120), "side by side when it fits")
}

func TestTablesFitThePane(t *testing.T) {
	st := newPaneStyles().textStyles
	md := "| Language | Typing | Note |\n|---|---|---|\n" +
		"| Go | Static, strong | Simple syntax, and goroutines make concurrency easy. |\n" +
		"| Rust | Static, strong | Memory safety without a garbage collector. |\n"

	wide := markdownLines(md, 80, st)
	plain := ansi.Strip(strings.Join(wide, "\n"))
	assert.Contains(t, plain, "│ Language │ Typing         │", "short columns keep their width")
	for _, l := range wide {
		assert.LessOrEqual(t, ansi.StringWidth(l), 80)
	}

	narrow := ansi.Strip(strings.Join(markdownLines(md, 30, st), "\n"))
	assert.NotContains(t, narrow, "┌", "too narrow for the rows: a list")
	assert.Contains(t, narrow, "• Go")
	assert.Contains(t, narrow, "Typing: Static, strong")
	assert.Contains(t, narrow, "Note: Simple syntax")

	assert.Equal(t, []int{8, 15}, fitColumns([]int{8, 15}, []int{8, 6}, 40), "both fit")
	widths := fitColumns([]int{8, 15, 58}, []int{8, 7, 11}, 50)
	assert.Equal(t, 50, widths[0]+widths[1]+widths[2])
	assert.Equal(t, 8, widths[0], "a column at its natural width stays")
	assert.Greater(t, widths[2], widths[1], "the room goes where it's wanted")
}

// Up and down step through earlier questions, as in Claude Code; in a
// question over several rows they move the cursor a row first, and from
// the first row up recalls the question before.
func TestUpAndDownMoveARowThenRecall(t *testing.T) {
	up, down := tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyDown}
	c := NewComposer()
	c.Focus()
	c.Sent("an earlier question")
	long := strings.TrimSpace(strings.Repeat("word ", 30))
	c.SetValue(long)
	c.Render(40, Sending{Text: "x"})
	rows, row := c.rows([]rune(long), c.textWidth)
	require.GreaterOrEqual(t, len(rows), 3, "the question takes several rows")
	require.Equal(t, len(rows)-1, row, "the cursor is at its end")

	c.HandleKey(up)
	assert.Equal(t, long, c.Value(), "a row up, not the question before")
	_, at := c.rows([]rune(long), c.textWidth)
	assert.Equal(t, row-1, at)
	c.HandleKey(down)
	_, at = c.rows([]rune(long), c.textWidth)
	assert.Equal(t, row, at, "and back down")

	for range len(rows) - 1 {
		c.HandleKey(up)
	}
	_, at = c.rows([]rune(long), c.textWidth)
	require.Equal(t, 0, at, "on the first row")
	c.HandleKey(up)
	assert.Equal(t, "an earlier question", c.Value(), "from the first row, the question before")
	c.HandleKey(down)
	assert.Equal(t, long, c.Value(), "past the latest, what you were writing")

	c.SetValue("short")
	c.input.SetCursor(0)
	c.HandleKey(down)
	assert.Equal(t, 5, c.input.Position(), "down on the last row goes to its end")
	c.HandleKey(up)
	c.HandleKey(up)
	assert.Equal(t, "an earlier question", c.Value(), "while stepping through them, up goes on back")
}
