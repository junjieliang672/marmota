package filepreview

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// Each line of the preview is two rows of the image, drawn as half blocks,
// and the lines come in order, one newline between each.
func TestConvertImageToANSIDrawsTwoRowsALine(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 5))
	for y := range 5 {
		for x := range 3 {
			img.Set(x, y, color.RGBA{R: uint8(50 * y), G: 10, B: 10, A: 255})
		}
	}

	lines := strings.Split(ansi.Strip(ConvertImageToANSI(img, color.Black)), "\n")

	assert.Equal(t, []string{"▄▄▄", "▄▄▄", "▄▄▄"}, lines, "5 rows make 3 lines, the last half filled")
}
