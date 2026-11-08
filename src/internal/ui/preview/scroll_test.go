package preview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLines(t *testing.T, name string, n int) string {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "row %04d\n", i)
	}
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	return path
}

// The wheel scrolls through the document no further than it goes; until it
// is drawn, scrolling waits for it, and a document for what the preview no
// longer shows is dropped.
func TestScrollingStaysWithinTheDocument(t *testing.T) {
	m := New()
	m.SetWidth(30)
	m.SetHeight(12) // 10 rows inside the box
	m.SetLocation("a.txt")
	doc := make([]string, 25)
	for i := range doc {
		doc[i] = fmt.Sprintf("row %02d", i)
	}

	assert.False(t, m.ScrollBy(3, 0), "not drawn yet")
	_, _, ok := m.RequestDoc(-3, 0)
	assert.False(t, ok, "up at the top has nothing to draw")
	location, width, ok := m.RequestDoc(3, 0)
	require.True(t, ok)
	assert.Equal(t, "a.txt", location)
	assert.Equal(t, 28, width)
	_, _, ok = m.RequestDoc(3, 0)
	assert.False(t, ok, "drawn once")

	m.SetDoc("a.txt", 28, doc, 28)
	assert.Equal(t, 6, m.scroll.top, "both scrolls once drawn")
	assert.Contains(t, ansi.Strip(m.GetContent()), "row 06")
	assert.NotContains(t, ansi.Strip(m.GetContent()), "row 05")
	assert.True(t, m.ScrollBy(100, 0))
	assert.Equal(t, 15, m.scroll.top, "the last rows fill the box")
	assert.True(t, m.ScrollBy(0, 10))
	assert.Equal(t, 0, m.scroll.left, "nothing to see sideways in rows that fit")
	assert.True(t, m.ScrollBy(-100, 0))
	assert.Equal(t, 0, m.scroll.top)

	m.SetLocation("b.txt")
	_, _, ok = m.RequestDoc(3, 0)
	require.True(t, ok)
	m.SetDoc("a.txt", 28, doc, 28)
	assert.False(t, m.scroll.drawn, "a document for another file is dropped")
	m.SetContent("new")
	assert.False(t, m.scroll.drawing, "what is drawn anew starts from its top")
}

// A text file's document is what the preview shows of it, down to DocLines
// lines and a row that says it stops there; a folder's is its listing; an
// image has none.
func TestTheDocumentOfAFileAFolderAndAnImage(t *testing.T) {
	m := New()
	short := writeLines(t, "short.txt", 30)
	doc, docWidth := m.RenderDoc(short, 40)
	require.Len(t, doc, 30)
	assert.Equal(t, 40, docWidth, "lines that fit: as wide as the box")
	assert.Equal(t, "row 0030", strings.TrimSpace(ansi.Strip(doc[29])))
	for _, row := range doc {
		assert.Equal(t, 40, ansi.StringWidth(row))
	}

	long, _ := m.RenderDoc(writeLines(t, "long.txt", DocLines+50), 40)
	require.Len(t, long, DocLines+1)
	assert.Equal(t, fmt.Sprintf("row %04d", DocLines), strings.TrimSpace(ansi.Strip(long[DocLines-1])))
	assert.Contains(t, ansi.Strip(long[DocLines]), "the preview stops here")

	dir := t.TempDir()
	for i := range 40 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), nil, 0o600))
	}
	rows, _ := m.RenderDoc(dir, 40)
	listing := strings.Join(rows, "\n")
	assert.Contains(t, listing, "f00.txt")
	assert.Contains(t, listing, "f39.txt", "the whole listing, not what fits")

	image := filepath.Join(t.TempDir(), "a.png")
	require.NoError(t, os.WriteFile(image, []byte("png"), 0o600))
	rows, _ = m.RenderDoc(image, 40)
	assert.Nil(t, rows)
}

// A file's long lines scroll sideways, as far as the widest one goes; a tab
// counts as the spaces it is drawn as.
func TestLongLinesScrollSideways(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.txt")
	text := "start " + strings.Repeat("-", 80) + " end\nshort\n\tindented\n"
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	m := New()
	m.SetWidth(32)
	m.SetHeight(12) // 30 columns inside the box
	m.SetLocation(path)

	doc, docWidth := m.RenderDoc(path, 30)
	assert.Equal(t, 90, docWidth, "as wide as the widest line")
	for _, row := range doc {
		assert.Equal(t, 90, ansi.StringWidth(row))
	}
	assert.Equal(t, "    indented", strings.TrimRight(ansi.Strip(doc[2]), " "))

	_, _, ok := m.RequestDoc(0, 6)
	require.True(t, ok, "a scroll right draws the document")
	m.SetDoc(path, 30, doc, docWidth)
	assert.Equal(t, 6, m.scroll.left)
	assert.NotContains(t, ansi.Strip(m.GetContent()), "start")
	assert.True(t, m.ScrollBy(0, 1000))
	assert.Equal(t, 60, m.scroll.left, "no further than the widest line")
	assert.Contains(t, ansi.Strip(m.GetContent()), " end")
	assert.True(t, m.ScrollBy(0, -1000))
	assert.Contains(t, ansi.Strip(m.GetContent()), "start")
}
