package preview

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

// highlightCode colors content as code in the language lexerName names,
// with the chroma style styleName on background ("" for none).
//
// Each line of a token is colored on its own. A token can span lines (in
// Markdown, a run of text from the end of one line into the next), and
// lipgloss pads every line of what it renders to the widest, which put gaps
// in the middle of lines.
func highlightCode(content, lexerName, styleName, background string) (string, error) {
	lexer := lexers.Get(lexerName)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	tokens, err := chroma.Coalesce(lexer).Tokenise(nil, content)
	if err != nil {
		return "", err
	}
	style := styles.Get(styleName)
	var b strings.Builder
	for _, token := range tokens.Tokens() {
		entry := style.Get(token.Type)
		color := lipgloss.NewStyle().
			Background(lipgloss.Color(background)).
			Bold(entry.Bold == chroma.Yes).
			Italic(entry.Italic == chroma.Yes).
			Underline(entry.Underline == chroma.Yes)
		if entry.Colour.IsSet() {
			color = color.Foreground(lipgloss.Color(entry.Colour.String()))
		}
		for i, line := range strings.Split(token.Value, "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			if line != "" {
				b.WriteString(color.Render(line))
			}
		}
	}
	return b.String(), nil
}
