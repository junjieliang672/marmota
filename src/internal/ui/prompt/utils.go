package prompt

import (
	"fmt"
	"strings"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func getPromptAction(shellMode bool, value string, cwdLocation string) (common.ModelAction, error) {
	noAction := common.NoAction{}
	if value == "" {
		return noAction, nil
	}
	if shellMode {
		return common.ShellCommandAction{
			Command: value,
		}, nil
	}

	promptArgs, err := tokenizePromptCommand(value, cwdLocation)
	if err != nil {
		return noAction, invalidCmdError{
			uiMsg:        tokenizationError + " : " + err.Error(),
			wrappedError: fmt.Errorf("error during tokenization : %w", err),
		}
	}

	switch promptArgs[0] {
	case ClaudeCommand:
		switch {
		case len(promptArgs) == 1:
			return common.ClaudeAction{}, nil
		case len(promptArgs) == 2 && promptArgs[1] == ClaudeRemoteControlArg:
			return common.ClaudeAction{RemoteControl: true}, nil
		default:
			return noAction, invalidCmdError{
				uiMsg: "claude command accepts only 'rc'; usage: claude [rc]",
			}
		}

	case CodexCommand:
		if len(promptArgs) != 1 {
			return noAction, invalidCmdError{
				uiMsg: "codex command accepts no arguments; usage: codex",
			}
		}
		return common.CodexAction{}, nil

	default:
		return noAction, invalidCmdError{
			uiMsg: "Invalid spf command : " + promptArgs[0] + " (use claude or codex)",
		}
	}
}

// Only allocates memory proportional to first token's size
// Only works for space right now. Does not splits command based on
// \n or \t , etc
func getFirstToken(command string) string {
	command = strings.TrimSpace(command)
	spaceIndex := strings.IndexByte(command, ' ')
	if spaceIndex == -1 {
		return command
	}
	return command[:spaceIndex]
}
