package ask

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/internal/common"
)

type answerStyles struct {
	textStyles

	agent, ok, bad, title lipgloss.Style
}

func newAnswerStyles(bg lipgloss.Color) answerStyles {
	base := lipgloss.NewStyle().Foreground(common.FilePanelFGColor).Background(bg)
	dim := base.Foreground(common.FilePanelBorderColor)
	code := base.Foreground(lipgloss.Color(common.Theme.Hint))
	st := textStyles{
		base:      base,
		bold:      base.Bold(true),
		code:      code,
		dim:       dim,
		italic:    base.Italic(true),
		link:      base.Foreground(common.FilePanelDirectoryColor).Underline(true),
		head:      base.Foreground(common.FilePanelBorderActiveColor).Bold(true),
		codeBlock: code,
		border:    dim,
	}
	if common.FilePanelCursorRowBGColor != "" {
		st.codeBlock, st.codeBG = code.Background(common.FilePanelCursorRowBGColor), true
	}
	return answerStyles{
		textStyles: st,
		agent:      base.Foreground(lipgloss.Color(common.ClaudeAgentColor)),
		ok:         base.Foreground(lipgloss.Color(common.Theme.Correct)),
		bad:        base.Foreground(lipgloss.Color(common.Theme.Error)),
		title:      base.Bold(true),
	}
}
