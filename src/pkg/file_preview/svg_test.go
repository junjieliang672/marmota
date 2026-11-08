package filepreview

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- isSVGData tests ---

func TestSVGData_WithXMLProlog(t *testing.T) {
	data := []byte(
		`<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100"/></svg>`,
	)
	if !isSVGData(data) {
		t.Error("expected isSVGData to return true for SVG with XML prolog")
	}
}

func TestSVGData_BareSVG(t *testing.T) {
	data := []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100"/></svg>`,
	)
	if !isSVGData(data) {
		t.Error("expected isSVGData to return true for bare <svg>")
	}
}

func TestSVGData_BOMPrefixed(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`)
	data := slices.Concat(bom, svg)
	if !isSVGData(data) {
		t.Error("expected isSVGData to return true for BOM-prefixed SVG")
	}
}

func TestSVGData_PNGMagicBytes(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if isSVGData(data) {
		t.Error("expected isSVGData to return false for PNG magic bytes")
	}
}

func TestSVGData_EmptyInput(t *testing.T) {
	if isSVGData(nil) {
		t.Error("expected isSVGData to return false for nil input")
	}
	if isSVGData([]byte{}) {
		t.Error("expected isSVGData to return false for empty input")
	}
}

func TestSVGData_NonSVGXML(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><html><body>hello</body></html>`)
	if isSVGData(data) {
		t.Error("expected isSVGData to return false for non-SVG XML (HTML)")
	}
}

func TestSVGData_CaseInsensitive(t *testing.T) {
	data := []byte(`<SVG xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></SVG>`)
	if !isSVGData(data) {
		t.Error("expected isSVGData to return true for uppercase <SVG>")
	}
}

func TestSVGData_LeadingWhitespace(t *testing.T) {
	data := []byte(`
	<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`)
	if !isSVGData(data) {
		t.Error("expected isSVGData to return true for SVG with leading whitespace")
	}
}

// --- renderSVGWithRsvg tests ---

func TestRenderSVGWithRsvg_ValidSVG(t *testing.T) {
	if _, err := exec.LookPath("rsvg-convert"); err != nil {
		t.Skip("rsvg-convert not installed")
	}

	svgContent := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40" fill="red"/></svg>`
	tmpFile := filepath.Join(t.TempDir(), "test.svg")
	os.WriteFile(tmpFile, []byte(svgContent), 0644)

	img, err := renderSVGWithRsvg(tmpFile, 400)
	require.NoError(t, err)
	assert.NotNil(t, img)
	assert.Positive(t, img.Bounds().Dx())
	assert.LessOrEqual(t, img.Bounds().Dx(), 400)
}

func TestRenderSVGWithRsvg_PreservesAspectRatio(t *testing.T) {
	if _, err := exec.LookPath("rsvg-convert"); err != nil {
		t.Skip("rsvg-convert not installed")
	}

	// Landscape SVG: 200x100
	svgContent := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><rect width="200" height="100" fill="blue"/></svg>`
	tmpFile := filepath.Join(t.TempDir(), "landscape.svg")
	os.WriteFile(tmpFile, []byte(svgContent), 0644)

	img, err := renderSVGWithRsvg(tmpFile, 400)
	require.NoError(t, err)
	ratio := float64(img.Bounds().Dx()) / float64(img.Bounds().Dy())
	assert.InDelta(t, 2.0, ratio, 0.2) // ~2:1 aspect ratio
}

func TestRenderSVGWithRsvg_NonExistentFile(t *testing.T) {
	if _, err := exec.LookPath("rsvg-convert"); err != nil {
		t.Skip("rsvg-convert not installed")
	}

	_, err := renderSVGWithRsvg("/nonexistent/file.svg", 400)
	assert.Error(t, err)
}

func TestRenderSVGWithRsvg_MalformedSVG(t *testing.T) {
	if _, err := exec.LookPath("rsvg-convert"); err != nil {
		t.Skip("rsvg-convert not installed")
	}

	tmpFile := filepath.Join(t.TempDir(), "bad.svg")
	os.WriteFile(tmpFile, []byte("not an svg at all"), 0644)

	_, err := renderSVGWithRsvg(tmpFile, 400)
	assert.Error(t, err)
}

func TestRenderSVGWithRsvg_RendersText(t *testing.T) {
	if _, err := exec.LookPath("rsvg-convert"); err != nil {
		t.Skip("rsvg-convert not installed")
	}

	svgContent := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><rect width="200" height="100" fill="white"/><text x="10" y="50" font-size="40" fill="black">Hello</text></svg>`
	tmpFile := filepath.Join(t.TempDir(), "text.svg")
	os.WriteFile(tmpFile, []byte(svgContent), 0644)

	img, err := renderSVGWithRsvg(tmpFile, 400)
	require.NoError(t, err)

	// Verify text was rendered: check for non-white pixels
	bounds := img.Bounds()
	hasNonWhite := false
	for y := bounds.Min.Y; y < bounds.Max.Y && !hasNonWhite; y++ {
		for x := bounds.Min.X; x < bounds.Max.X && !hasNonWhite; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r < 0xF000 || g < 0xF000 || b < 0xF000 {
				hasNonWhite = true
			}
		}
	}
	assert.True(t, hasNonWhite, "Expected rendered text to produce non-white pixels")
}

// --- prepareImageForPreview integration tests ---

func TestPrepareImageForPreview_SVGBytesNowFail(t *testing.T) {
	// After removing SVG branch, SVG bytes should fail in prepareImageForPreview
	svgBytes := []byte(
		`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><circle cx="50" cy="50" r="40"/></svg>`,
	)
	_, _, _, err := prepareImageForPreview(svgBytes)
	assert.Error(t, err, "SVG bytes should no longer be handled by prepareImageForPreview")
}

func TestSVG_PrepareImageForPreview_PNGStillWorks(t *testing.T) {
	// Create a minimal valid PNG in memory
	pngImg := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pngImg.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, pngImg); err != nil {
		t.Fatalf("failed to encode test PNG: %v", err)
	}

	img, w, h, err := prepareImageForPreview(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error for PNG: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image for PNG")
	}
	if w != 2 || h != 2 {
		t.Errorf("expected 2x2 PNG dimensions, got %dx%d", w, h)
	}
}

func TestSVG_PrepareImageForPreview_ZeroBytes(t *testing.T) {
	// Should not panic, should return error
	_, _, _, err := prepareImageForPreview([]byte{})
	if err == nil {
		t.Error("expected error for zero-byte input")
	}
}
