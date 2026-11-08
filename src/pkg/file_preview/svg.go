package filepreview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// isSVGData checks whether the given byte slice looks like SVG content.
// It trims leading whitespace and UTF-8 BOM, then checks for <svg or <?xml ... <svg.
func isSVGData(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// Strip UTF-8 BOM if present
	trimmed := data
	if len(trimmed) >= 3 && trimmed[0] == 0xEF && trimmed[1] == 0xBB && trimmed[2] == 0xBF {
		trimmed = trimmed[3:]
	}

	s := strings.TrimSpace(string(trimmed))
	lower := strings.ToLower(s)

	if strings.HasPrefix(lower, "<svg") {
		return true
	}

	if strings.HasPrefix(lower, "<?xml") {
		// Look for <svg after the XML prolog
		idx := strings.Index(lower, "<svg")
		return idx > 0
	}

	return false
}

// renderSVGWithRsvg uses rsvg-convert to rasterize an SVG file to a PNG image.
// It follows the same pattern as PDF preview's use of mutool.
func renderSVGWithRsvg(svgPath string, maxWidth int) (image.Image, error) {
	rsvgPath, err := exec.LookPath("rsvg-convert")
	if err != nil {
		slog.Warn("rsvg-convert not found, cannot render SVG preview", "error", err)
		return nil, fmt.Errorf("rsvg-convert not available: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:mnd // 5s timeout for SVG rendering
	defer cancel()

	// rsvg-convert renders SVG to PNG on stdout (default when no -o is specified)
	cmd := exec.CommandContext(ctx, rsvgPath,
		"-f", "png",
		"-w", strconv.Itoa(maxWidth),
		"-a",    // keep aspect ratio
		svgPath, // input file
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		slog.Error("rsvg-convert command failed",
			"error", err,
			"stderr", stderr.String(),
			"path", svgPath)
		return nil, fmt.Errorf("rsvg-convert failed: %w", err)
	}

	img, err := png.Decode(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		slog.Error("Failed to decode PNG from rsvg-convert output", "error", err)
		return nil, fmt.Errorf("failed to decode rsvg-convert PNG output: %w", err)
	}

	slog.Debug("SVG rasterized successfully via rsvg-convert",
		"path", svgPath,
		"width", img.Bounds().Dx(),
		"height", img.Bounds().Dy())

	return img, nil
}
