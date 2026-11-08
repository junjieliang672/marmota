package preview

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui"
)

// The mouse wheel scrolls the preview through its document: what it shows of
// a text file or a folder's listing, drawn past what fits, down and, for a
// file's long lines, sideways. The document is drawn in the background the
// first time you scroll down or right; until then the preview holds what
// fits, drawn as fast as before.

// DocLines is how much of a file, or of a folder's listing, the preview
// scrolls through. Highlighting more would keep you waiting: 2000 lines of
// Go take about 0.1 s.
const DocLines = 2000

// DocWidth is how wide a file's lines may be in the document: the preview
// scrolls sideways as far as its widest line, and no further than this.
const DocWidth = 1024

type scrollState struct {
	// top is the document's first line shown, left its first column.
	top, left int
	doc       []string
	// width is how wide the document's rows are: its widest line, and at
	// least the inside of the box.
	width int
	// drawn is set once the document is drawn, empty when there is nothing
	// to scroll; drawing while it is drawn, and pendingLines and
	// pendingCols are how far you scrolled meanwhile.
	drawn, drawing            bool
	pendingLines, pendingCols int
	// header is rows that stay at the top while the document scrolls, and
	// gutter how many columns on the left stay when it scrolls sideways,
	// as a diff's title and line numbers do.
	header []string
	gutter int
}

// ScrollBy moves the preview through its document by lines, down when
// positive, and by cols, right when positive, no further than it goes. It
// reports whether the document is drawn: when it isn't, RequestDoc has it
// drawn.
func (m *Model) ScrollBy(lines, cols int) bool {
	s := &m.scroll
	if !s.drawn {
		return false
	}
	top := min(max(0, s.top+lines), max(0, len(s.doc)-m.docHeight()))
	left := min(max(0, s.left+cols), max(0, s.width-m.InnerWidth()))
	if top != s.top || left != s.left {
		s.top, s.left = top, left
		m.showDoc()
	}
	return true
}

// docHeight is how many of the document's rows the box shows under its
// header.
func (m *Model) docHeight() int {
	return max(1, m.InnerHeight()-len(m.scroll.header))
}

// showDoc draws the part of the document the box shows.
func (m *Model) showDoc() {
	s := &m.scroll
	width := m.InnerWidth()
	rows := s.doc[s.top:min(len(s.doc), s.top+m.docHeight())]
	if s.width > width {
		cut := make([]string, len(rows))
		for i, row := range rows {
			cut[i] = ansi.Cut(row, 0, s.gutter) + ansi.Cut(row, s.gutter+s.left, s.left+width)
		}
		rows = cut
	}
	m.content = m.RenderLines(append(slices.Clone(s.header), rows...))
}

// ShowFixedDoc shows a document drawn elsewhere, such as a diff, ready to
// scroll: header stays at the top, and its rows' first gutter columns stay
// when it scrolls sideways. docWidth is how wide its widest row is.
func (m *Model) ShowFixedDoc(header, doc []string, docWidth, gutter int) {
	m.scroll = scrollState{drawn: true, doc: doc, width: max(docWidth, m.InnerWidth()),
		header: header, gutter: gutter}
	m.showDoc()
}

// DocTop is the document's first row shown.
func (m *Model) DocTop() int {
	return m.scroll.top
}

// RequestDoc notes a scroll that waits for the document. The first scroll
// down or right returns what to draw it for: the location, and the width
// inside the box.
func (m *Model) RequestDoc(lines, cols int) (string, int, bool) {
	s := &m.scroll
	switch {
	case s.drawing:
		s.pendingLines += lines
		s.pendingCols += cols
		return "", 0, false
	case s.drawn, lines <= 0 && cols <= 0, m.location == "":
		return "", 0, false
	}
	s.drawing, s.pendingLines, s.pendingCols = true, lines, cols
	return m.location, m.InnerWidth(), true
}

// SetDoc takes the document drawn for location at width, its rows docWidth
// wide, and scrolls as far as you did meanwhile. A document for what the
// preview no longer shows is dropped.
func (m *Model) SetDoc(location string, width int, doc []string, docWidth int) {
	s := &m.scroll
	if !s.drawing || location != m.location || width != m.InnerWidth() {
		return
	}
	lines, cols := s.pendingLines, s.pendingCols
	m.scroll = scrollState{drawn: true, doc: doc, width: max(docWidth, width)}
	m.ScrollBy(lines, cols)
}

// RenderDoc draws the document of itemPath for a box width wide, and says
// how wide its rows are: up to DocLines lines of a text file, as wide as its
// widest line up to DocWidth, or a folder's listing, cut to the box as the
// preview cuts it. It is nil for what doesn't scroll, such as an image. It
// runs in the background, so it reads nothing of the preview that changes.
func (m *Model) RenderDoc(itemPath string, width int) ([]string, int) {
	info, err := os.Stat(itemPath)
	switch {
	case err != nil, slices.Contains(common.UnsupportedPreviewFormats, filepath.Ext(itemPath)):
		return nil, width
	case info.IsDir():
		rows := docRows(m.renderDirectoryPreview(ui.FilePreviewPanelRenderer(DocLines, width), itemPath, DocLines))
		return rows, width
	case !info.Mode().IsRegular(), isVideoFile(itemPath), isImageFile(itemPath), isPDFFile(itemPath):
		return nil, width
	}
	// A line more than the document holds says whether the file goes on.
	content, problem := m.textContent(itemPath, DocWidth, DocLines+1)
	if problem != "" {
		return nil, width
	}
	// As wide as the widest line once drawn, its tabs spaces.
	docWidth := width
	for line := range strings.SplitSeq(content, "\n") {
		docWidth = max(docWidth, ansi.StringWidth(common.MakePrintableWithEscCheck(line, true)))
	}
	docWidth = min(docWidth, max(width, DocWidth))
	r := ui.FilePreviewPanelRenderer(DocLines+1, docWidth)
	r.AddLines(content)
	rows := docRows(r.Render())
	if len(rows) > DocLines {
		rows = append(rows[:DocLines], moreRow(docWidth))
	}
	return rows, docWidth
}

// docRows splits a drawn document into its rows, without the blank rows the
// renderer pads it with.
func docRows(drawn string) []string {
	rows := strings.Split(drawn, "\n")
	for len(rows) > 0 && strings.TrimSpace(ansi.Strip(rows[len(rows)-1])) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// moreRow ends the document of a file longer than it.
func moreRow(width int) string {
	return lipgloss.NewStyle().Foreground(common.FilePanelBorderColor).Background(common.FilePanelBGColor).
		Width(width).Render(ansi.Truncate("⋯ the preview stops here", width, "…"))
}
