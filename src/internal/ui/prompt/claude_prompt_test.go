package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func TestGetPromptAction_Claude_NoArgs(t *testing.T) {
	action, err := getPromptAction(false, "claude", defaultTestCwd)

	require.NoError(t, err)
	assert.Equal(t, common.ClaudeAction{}, action)
}

func TestGetPromptAction_Claude_RejectsArguments(t *testing.T) {
	tests := []string{
		"claude -h",
		"claude -v",
		"claude foo",
		"claude 2",
		"claude 3",
		"claude 2 extra",
		"claude rc extra",
		"claude RC",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			action, err := getPromptAction(false, input, defaultTestCwd)

			var cmdErr invalidCmdError
			require.ErrorAs(t, err, &cmdErr)
			assert.Contains(t, cmdErr.uiMessage(), "accepts only 'rc'")
			assert.Equal(t, common.NoAction{}, action)
		})
	}
}

func TestGetPromptAction_Claude_RemoteControl(t *testing.T) {
	action, err := getPromptAction(false, "claude rc", defaultTestCwd)

	require.NoError(t, err)
	assert.Equal(t, common.ClaudeAction{RemoteControl: true}, action)
}

func TestClaudeAction_String(t *testing.T) {
	a := common.ClaudeAction{}

	assert.Equal(t, "ClaudeAction (smart-layout, skip-permissions)", a.String())
	assert.Equal(t, "ClaudeAction (smart-layout, skip-permissions, remote-control)",
		common.ClaudeAction{RemoteControl: true}.String())
}

func TestClaudeAction_ImplementsModelAction(t *testing.T) {
	var _ common.ModelAction = common.ClaudeAction{}
}

func TestDefaultCommandSlice_ClaudeEntry(t *testing.T) {
	cmds := defaultCommandSlice()

	var claudeCmd *promptCommand
	for i := range cmds {
		if cmds[i].command == ClaudeCommand {
			claudeCmd = &cmds[i]
			break
		}
	}

	require.NotNil(t, claudeCmd)
	assert.Equal(t, "claude", claudeCmd.usage)
	assert.Contains(t, claudeCmd.description, "--dangerously-skip-permissions")
	assert.Contains(t, claudeCmd.description, "claude:")
	assert.Contains(t, claudeCmd.description, "claude rc:")
	assert.Contains(t, claudeCmd.description, "--remote-control")
	assert.NotContains(t, claudeCmd.description, "claude 2:")
	assert.NotContains(t, claudeCmd.description, "claude 3:")
	assert.NotContains(t, claudeCmd.usage, "[2|3]")
}

func TestGetPromptAction_Claude_ShellModePassthrough(t *testing.T) {
	action, err := getPromptAction(true, "claude 2", defaultTestCwd)

	require.NoError(t, err)
	assert.Equal(t, common.ShellCommandAction{Command: "claude 2"}, action)
}
