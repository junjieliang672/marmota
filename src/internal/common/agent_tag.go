package common

import "github.com/charmbracelet/lipgloss"

// AgentColor is the color of the program a session runs in: Claude Code's
// orange, or Codex's blue.
func AgentColor(codex bool) lipgloss.Color {
	if codex {
		return lipgloss.Color(CodexAgentColor)
	}
	return lipgloss.Color(ClaudeAgentColor)
}

// AgentTag names the program a session runs in, in its color on bg: cc for
// Claude Code, co for Codex.
func AgentTag(codex bool, bg lipgloss.TerminalColor) string {
	tag := "cc"
	if codex {
		tag = "co"
	}
	return lipgloss.NewStyle().Foreground(AgentColor(codex)).Background(bg).Bold(true).Render(tag)
}

// RemoteControlMark marks a session you can follow and answer from the Claude
// or ChatGPT app (Remote Control), on bg: a phone, or rc without a Nerd Font.
func RemoteControlMark(bg lipgloss.TerminalColor) string {
	mark := "rc"
	if Config.Nerdfont {
		mark = "\U000F011C" // nf-md-cellphone
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(Theme.Correct)).Background(bg).Render(mark)
}
