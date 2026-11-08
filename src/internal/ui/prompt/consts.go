package prompt

import "time"

// These could as well be property of prompt Model vs being global consts
// But its fine
const (
	promptHeadlineText = "Marmota Prompt"

	// Agent mode only starts agent sessions: Claude Code or Codex.
	ClaudeCommand = "claude"
	CodexCommand  = "codex"
	// ClaudeRemoteControlArg is the `claude rc` sub-argument.
	ClaudeRemoteControlArg = "rc"

	// We could later make this configurable. But, not needed now.
	agentPromptChar = ">"
	shellPromptChar = ":"

	successMessagePrefix = "Success"
	failureMessagePrefix = "Error"

	shellModeString = "(Shell Mode)"
	agentModeString = "(Agent Mode)"

	// Error message string
	tokenizationError = "Failed during tokenization"

	// Timeout for command executed for shell substitution
	shellSubTimeout        = 1000 * time.Millisecond
	shellSubTimeoutInTests = 100 * time.Millisecond

	defaultTestCwd = "/"

	PromptMinWidth  = 10
	PromptMinHeight = 3

	defaultTestWidth     = 100
	defaultTestMaxHeight = 100

	// UI dimension constants for prompt modal
	// promptInputPadding is total padding for prompt input fields
	promptInputPadding = 6 // 2 + 1 + 2 + 1 (borders and spacing)
)

func modeString(shellMode bool) string {
	if shellMode {
		return shellModeString
	}
	return agentModeString
}

func shellPrompt(shellMode bool) string {
	if shellMode {
		return shellPromptChar
	}
	return agentPromptChar
}

func defaultCommandSlice() []promptCommand {
	return []promptCommand{
		{
			command: ClaudeCommand,
			usage:   ClaudeCommand,
			description: "Open Claude Code in a smart WezTerm layout (--dangerously-skip-permissions)\n" +
				"  claude:   Smart split, single session in current directory\n" +
				"  claude rc: Same, with Remote Control enabled (--remote-control)",
		},
		{
			command:     CodexCommand,
			usage:       CodexCommand,
			description: "Open OpenAI Codex CLI in a smart WezTerm layout (on-request approvals)",
		},
	}
}
