package rendering

import (
	"log/slog"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Add lines as much as the remaining capacity allows
func (r *Renderer) AddLines(lines ...string) *Renderer {
	r.contentSections[r.curSectionIdx].AddLines(lines...)
	return r
}

// Lines until now will belong to current section, and
// Any new lines will belong to a new section
func (r *Renderer) AddSection() {
	// r.actualContentHeight before this point only includes sections
	// before r.curSectionIdx
	r.actualContentHeight += r.contentSections[r.curSectionIdx].CntLines()

	// Silently Fail if cannot add
	if r.contentHeight <= r.actualContentHeight {
		slog.Error("Cannot add any more sections", "actualHeight", r.actualContentHeight,
			"contentHeight", r.contentHeight)
		return
	}

	// Add divider
	r.border.AddDivider(r.actualContentHeight)
	// sectionDivider should be of borderstyle
	r.sectionDividers = append(r.sectionDividers, lipgloss.NewStyle().
		Foreground(r.borderFGColor).
		Background(r.borderBGColor).
		Render(strings.Repeat(r.borderStrings.Top, r.contentWidth)))
	r.actualContentHeight++

	remainingHeight := r.contentHeight - r.actualContentHeight
	r.contentSections = append(r.contentSections,
		NewContentRenderer(remainingHeight, r.contentWidth, r.defTruncateStyle))
	// Adjust index
	r.curSectionIdx++
}

// Truncate would always preserve ansi codes.
func (r *Renderer) AddLineWithCustomTruncate(line string, truncateStyle TruncateStyle) {
	r.contentSections[r.curSectionIdx].AddLineWithCustomTruncate(line, truncateStyle)
}

func (r *Renderer) SetBorderTitle(title string) {
	r.border.SetTitle(title)
}

// SetBorderTitleSuffix sets text drawn after the title in the suffix color,
// such as a panel's "9/15".
func (r *Renderer) SetBorderTitleSuffix(suffix string) {
	r.border.SetTitleSuffix(suffix)
}

// SetBorderTitleGlyph sets a symbol drawn before the title in color, such as
// a session's state.
func (r *Renderer) SetBorderTitleGlyph(glyph string, color lipgloss.TerminalColor) {
	r.border.SetTitleGlyph(glyph)
	r.titleGlyphFGColor = color
}

// styledTopLine is the top border drawn with the title and its suffix in
// their own colors.
func (r *Renderer) styledTopLine() (string, bool) {
	l, ok := r.border.layoutTitle()
	if !ok {
		return "", false
	}
	border := lipgloss.NewStyle().Foreground(r.borderFGColor).Background(r.borderBGColor)
	line := border.Render(r.borderStrings.TopLeft + strings.Repeat(r.borderStrings.Top, l.margin) + " ")
	if l.glyph != "" {
		glyphStyle := border
		if r.titleGlyphFGColor != nil {
			glyphStyle = border.Foreground(r.titleGlyphFGColor)
		}
		line += glyphStyle.Render(l.glyph) + border.Render(" ")
	}
	line += border.Foreground(r.titleFGColor).Render(l.title)
	if l.suffix != "" {
		line += border.Render(" ") + border.Foreground(r.titleSuffixFGColor).Render(l.suffix)
	}
	return line + border.Render(" "+strings.Repeat(r.borderStrings.Top, l.rest)+r.borderStrings.TopRight), true
}

func (r *Renderer) SetBorderInfoItems(infoItems ...string) {
	r.border.SetInfoItems(infoItems...)
}

func (r *Renderer) AreInfoItemsTruncated() bool {
	return r.border.AreInfoItemsTruncated()
}

// Should not do any updates on 'r'
func (r *Renderer) Render() string {
	res := r.Style().Render(r.joinedSections())
	if r.borderRequired {
		res = r.withStyledTopLine(res)
	}
	return r.fitToSize(res)
}

// joinedSections is the content sections one after another, with the
// dividers between them.
func (r *Renderer) joinedSections() string {
	content := strings.Builder{}
	for i := range r.contentSections {
		// After every iteration, current cursor will be on next newline
		curContent := r.contentSections[i].Render()
		content.WriteString(curContent)
		// == "" check cant differentiate between no data, vs empty line
		if r.contentSections[i].CntLines() > 0 {
			content.WriteString("\n")
		}

		if i < len(r.contentSections)-1 {
			// True for all except last section
			content.WriteString(r.sectionDividers[i])
			content.WriteString("\n")
		}
	}
	return strings.TrimSuffix(content.String(), "\n")
}

// withStyledTopLine puts the styled top border line in place of res's
// first line.
func (r *Renderer) withStyledTopLine(res string) string {
	if top, ok := r.styledTopLine(); ok {
		if i := strings.IndexByte(res, '\n'); i >= 0 {
			return top + res[i:]
		}
	}
	return res
}

// fitToSize cuts res to the renderer's total size: lipgloss Render()
// doesn't always respect the "height" value, so res can be taller than
// intended. Post rendering validations - Maybe we can return an error
// instead of logging
func (r *Renderer) fitToSize(res string) string {
	maxW := 0
	for line := range strings.Lines(res) {
		maxW = max(maxW, ansi.StringWidth(line))
	}

	lineCnt := strings.Count(res, "\n") + 1
	if maxW <= r.totalWidth && lineCnt <= r.totalHeight {
		return res
	}
	slog.Error("Rendered output data", "lineCnt", lineCnt, "totalHeight", r.totalHeight,
		"totalWidth", r.totalWidth, "maxW", maxW)
	newRes := strings.Builder{}
	curCnt := 0
	// Dont use strings.Lines(), that wont allow us to have empty lines
	for line := range strings.SplitSeq(res, "\n") {
		if curCnt == r.totalHeight {
			break
		}
		newRes.WriteString(ansi.Truncate(line, r.totalWidth, ""))
		curCnt++
		if curCnt < r.totalHeight {
			newRes.WriteByte('\n')
		}
	}
	return newRes.String()
}

func (r *Renderer) Style() lipgloss.Style {
	contentHeight := r.contentHeight
	if r.truncateHeight {
		contentHeight = r.actualContentHeight
	}
	s := lipgloss.NewStyle().
		Width(r.contentWidth).
		Height(contentHeight).
		Background(r.contentBGColor).
		Foreground(r.contentFGColor)

	if r.borderRequired {
		s = s.Border(r.border.GetBorder(r.borderStrings))
		s = s.BorderForeground(r.borderFGColor).
			BorderBackground(r.borderBGColor)
	}
	return s
}

// ContentLines is how many lines were added so far, dividers included.
func (r *Renderer) ContentLines() int {
	return r.actualContentHeight + r.contentSections[r.curSectionIdx].CntLines()
}
