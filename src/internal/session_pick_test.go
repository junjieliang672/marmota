package internal

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fzf starts each color code with an empty parameter, which micro's
// terminal pane ignores, colors and all: each empty one is given its value.
func TestColorCodesWithAnEmptyParameterAreWrittenInFull(t *testing.T) {
	for _, c := range []struct{ name, in, out string }{
		{"as fzf writes a color", "\x1b[;1;38;5;110m>\x1b[0m", "\x1b[0;1;38;5;110m>\x1b[0m"},
		{"a reset written short", "a\x1b[mb\x1b[;mc", "a\x1b[0mb\x1b[0;0mc"},
		{"one written in full passes", "\x1b[38;5;209mClaude\x1b[0m", "\x1b[38;5;209mClaude\x1b[0m"},
		{"other sequences pass", "\x1b[?1049h\x1b[7A\x1b[G\x1b[K\x1b[;H", "\x1b[?1049h\x1b[7A\x1b[G\x1b[K\x1b[;H"},
		{"and text with no sequence", "plain ✳ 中文", "plain ✳ 中文"},
	} {
		out, rest := rewriteColors([]byte(c.in))
		assert.Equal(t, c.out, string(out), c.name)
		assert.Empty(t, rest, c.name)
	}
}

// A read may end in the middle of a sequence: its start is kept for the
// next one, so no sequence is passed on in halves.
func TestAColorCodeCutByAReadIsFinishedByTheNext(t *testing.T) {
	whole := "row \x1b[;38;5;209mClaude\x1b[0m end"
	want, _ := rewriteColors([]byte(whole))
	for cut := 1; cut < len(whole); cut++ {
		first, rest := rewriteColors([]byte(whole[:cut]))
		second, left := rewriteColors(append(rest, whole[cut:]...))
		assert.Equal(t, string(want), string(first)+string(second), "cut at %d", cut)
		assert.Empty(t, left)
	}

	var out bytes.Buffer
	copyDrawn(&out, strings.NewReader(whole))
	assert.Equal(t, string(want), out.String())
}

// A picker's rows: the fields a program reads, then the row to show, where
// Claude Code's sessions and Codex's each have a mark and a color.
func TestThePickersRowsTellClaudeCodeFromCodex(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, WriteSessionRows(&out, []ListedSession{
		{ID: "a1", Agent: AgentClaudeCode, Status: "working", Title: "Fix the  login\tbug", Cwd: "/w/repo"},
		{ID: "b2", Agent: AgentCodex, Status: "waiting", Title: "给登录页写文档，要覆盖所有的错误提示和重试流程以及超时的处理方式", Cwd: "/w/docs"},
		{ID: "c3", Agent: AgentClaudeCode, Host: "kingjj-air", Status: "idle", Title: "nova", Cwd: "/Users/kingjj/proj"},
	}, "/w/repo"))
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Len(t, lines, 5)
	fields := func(i int) []string { return strings.SplitN(lines[i], "\t", 5) }
	shown := func(i int) string { return ansi.Strip(fields(i)[4]) }

	// first a row for each agent that starts a session in the folder
	assert.Equal(t, []string{"new", "cc", "here", "new · repo"}, fields(0)[:4])
	assert.Equal(t, []string{"new", "co", "here", "new · repo"}, fields(1)[:4])
	assert.Contains(t, shown(0), "✳ Claude Code  + New session in repo")
	assert.Contains(t, shown(1), "◆ Codex        + New session in repo")

	assert.Equal(t, []string{"a1", "cc", "here", "Fix the login bug"}, fields(2)[:4], "a title stays one field")
	assert.Regexp(t, `^✳ Claude Code  ● working    Fix the login bug\s+repo$`, shown(2))
	assert.Equal(t, []string{"b2", "co", "here"}, fields(3)[:3])
	assert.Regexp(t, `^◆ Codex        ! needs you  给登录页写文档.*…\s+docs$`, shown(3), "a long title is cut")
	assert.Regexp(t, `^✳ Claude Code  ○ idle       nova\s+☁ kingjj-air · proj$`, shown(4))
	// the columns line up, by the cells Chinese characters take
	assert.Equal(t, ansi.StringWidth(shown(2)[:strings.Index(shown(2), "repo")]),
		ansi.StringWidth(shown(3)[:strings.Index(shown(3), "docs")]))

	// each agent in its color, and what a session does in its own
	assert.Contains(t, fields(2)[4], rowClaude+"✳ Claude Code")
	assert.Contains(t, fields(3)[4], rowCodex+"◆ Codex")
	assert.Contains(t, fields(2)[4], rowWorks+"● working")
	assert.Contains(t, fields(3)[4], rowNeeds+"! needs you")
	assert.NotEqual(t, rowClaude, rowCodex)
}
