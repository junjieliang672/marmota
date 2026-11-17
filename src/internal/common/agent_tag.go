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

// HostMark says which computer a session runs on, on bg: a line-art monitor
// for this one, a line-art cloud and the computer's name for another. Each
// icon is followed by a space, so WezTerm draws it two cells wide. Without a
// Nerd Font only another computer is marked, "@name".
func HostMark(host string, bg lipgloss.TerminalColor) string {
	style := lipgloss.NewStyle().Background(bg)
	if !Config.Nerdfont {
		if host == "" {
			return ""
		}
		return style.Foreground(lipgloss.Color(Theme.Hint)).Render("@" + host)
	}
	if host == "" {
		return style.Foreground(lipgloss.Color(Theme.FooterBorder)).Render("\U000F0379") // nf-md-monitor
	}
	return style.Foreground(lipgloss.Color(Theme.Hint)).Render("\U000F0163 " + host) // nf-md-cloud_outline
}
