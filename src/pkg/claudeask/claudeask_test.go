package claudeask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

func TestChoiceArgs(t *testing.T) {
	assert.Equal(t, []string{"--model", "opus", "--effort", "high"}, ChoiceArgs("opus", "high"))
	assert.Empty(t, ChoiceArgs("", ""), "Claude Code's own choices")
}

func TestContextBlock(t *testing.T) {
	c := Context{
		Folder: "/home/me/repo/asset", Cursor: "logo.png",
		Selected: []string{"logo.png", "logo-white.png"}, Sort: "name ↑", ShowHidden: true,
		InGit: true, GitBranch: "main", GitChanged: 1, Notes: "/home/me/repo/CLAUDE.md",
	}
	block := c.Block("/home/me")
	assert.Equal(t, `<marmota-context>
cwd:      ~/repo/asset
cursor:   logo.png
selected: 2 items: logo.png, logo-white.png
view:     sorted by name ↑, hidden files shown
git:      main · 1 changed
notes:    ~/repo/CLAUDE.md
</marmota-context>`, block)

	assert.Equal(
		t,
		"<marmota-context>\ncwd:      /tmp\n</marmota-context>",
		Context{Folder: "/tmp"}.Block("/home/me"),
	)
}

func TestContextBlockCountsALargeSelection(t *testing.T) {
	c := Context{Folder: "/f"}
	for i := range 25 {
		c.Selected = append(c.Selected, "f"+strings.Repeat("x", i))
	}
	assert.Contains(t, c.Block(""), "selected: 25 items: f, fx, ")
	assert.Contains(t, c.Block(""), ", … and 5 more\n")
}

func TestPromptLeavesOutTheContextWhenShown(t *testing.T) {
	prompt := Prompt("  group the logos  ", Context{Folder: "/f", Cursor: "a"}, "")
	assert.True(t, strings.HasPrefix(prompt, "group the logos\n\n<marmota-context>"))
	assert.Equal(t, "group the logos", strings.TrimSpace(claudesession.StripContext(prompt)))
}

func TestPermissionModes(t *testing.T) {
	assert.Equal(t, "bypassPermissions", PermissionModes[0], "superfile's default comes first")
	assert.Equal(t, []string{"--dangerously-skip-permissions"}, PermissionArgs(""))
	assert.Equal(t, []string{"--dangerously-skip-permissions"}, PermissionArgs("bypassPermissions"))
	assert.Equal(t, []string{"--permission-mode", "plan"}, PermissionArgs("plan"))
	assert.Equal(t, "accept edits", PermissionModeLabel("acceptEdits"))
	assert.Equal(t, "bypass", PermissionModeLabel("bypassPermissions"))
	assert.Equal(t, "manual", PermissionModeLabel("manual"))
	assert.Equal(t, "manual", PermissionModeLabel("default"), "as a transcript records manual")
	assert.Equal(t, "dontAsk", PermissionModeLabel("dontAsk"), "a mode it doesn't know keeps its name")
}

func TestModelLabel(t *testing.T) {
	for id, want := range map[string]string{
		"claude-opus-5-5":            "opus 5.5",
		"claude-sonnet-4-5-20250929": "sonnet 4.5",
		"claude-haiku-4-5":           "haiku 4.5",
		"opus":                       "opus",
		"claude-fable-5-1":           "fable 5.1",
	} {
		assert.Equal(t, want, ModelLabel(id), id)
	}
}

func TestDefaultsAreClaudeCodesSettings(t *testing.T) {
	dir := t.TempDir()
	model, effort := Defaults(dir)
	assert.Empty(t, model+effort, "no settings: none")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"),
		[]byte(`{"effortLevel":"medium","alwaysThinkingEnabled":true}`), 0o600))
	model, effort = Defaults(dir)
	assert.Empty(t, model)
	assert.Equal(t, "medium", effort)
}
