package rendering

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// A title at the foot is at the left of the box's bottom edge, the info
// items at its right, and the top edge is a line.
func TestATitleAtTheFootOfTheBorder(t *testing.T) {
	box := func(width int, info ...string) *Renderer {
		r := getDefaultTestRenderer(3, width, true)
		r.AddLines("L1")
		r.SetBorderTitle("asset")
		r.SetBorderTitleSuffix("9/15")
		r.SetBorderTitleAtFoot()
		if len(info) > 0 {
			r.SetBorderInfoItems(info...)
		}
		return r
	}

	assert.Equal(t, ""+
		"╭──────────────────╮\n"+
		"│L1                │\n"+
		"╰─ asset 9/15 ─────╯", box(20).Render())

	assert.Equal(t, ""+
		"╭──────────────────╮\n"+
		"│L1                │\n"+
		"╰ a… 9/15  name ↑ ─╯", box(20, "name ↑").Render(),
		"the info items stay whole, and the suffix: the title is cut")

	assert.Equal(t, ""+
		"╭──────────────╮\n"+
		"│L1            │\n"+
		"╰ as…  name ↑ ─╯", box(16, "name ↑").Render(),
		"with no room for two cells of the title before it, the suffix goes")

	assert.Equal(t, ""+
		"╭───────────╮\n"+
		"│L1         │\n"+
		"╰── name ↑ ─╯", box(13, "name ↑").Render(),
		"and with no room for a title, the edge has the info items alone")

	r := box(20)
	r.SetBorderTitleGlyph("✳", nil)
	r.SetBorderTitleReverse()
	assert.Equal(t, ""+
		"╭──────────────────╮\n"+
		"│L1                │\n"+
		"╰─ ✳ asset 9/15 ───╯", ansi.Strip(r.Render()), "on a band, the same cells")
}
