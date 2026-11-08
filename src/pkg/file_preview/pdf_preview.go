package filepreview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PDFPreviewer handles PDF preview generation using external tools
type PDFPreviewer struct {
	imagePreviewer  *ImagePreviewer
	tempDir         string
	currentPage     int
	totalPages      int
	currentFilePath string
}

// NewPDFPreviewer creates a new PDF previewer
func NewPDFPreviewer(imagePreviewer *ImagePreviewer) *PDFPreviewer {
	// Create temp directory for PDF page images
	tempDir := filepath.Join(os.TempDir(), "superfile-pdf-preview")
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		slog.Error("Failed to create PDF preview temp directory", "error", err)
	}

	return &PDFPreviewer{
		imagePreviewer: imagePreviewer,
		tempDir:        tempDir,
	}
}

// PDFPreview generates a preview of a PDF file page
// It uses mutool (from MuPDF) to convert the current page to PNG
func (p *PDFPreviewer) PDFPreview(path string, maxWidth int, maxHeight int,
	defaultBGColor string, sideAreaWidth int) (string, error) {
	// Validate dimensions
	if maxWidth <= 0 || maxHeight <= 0 {
		return "", fmt.Errorf("dimensions must be positive (maxWidth=%d, maxHeight=%d)", maxWidth, maxHeight)
	}

	// Check if mutool is available
	mutoolPath, err := exec.LookPath("mutool")
	if err != nil {
		slog.Warn("mutool not found, cannot render PDF preview", "error", err)
		return "", fmt.Errorf("mutool not available: %w", err)
	}

	// Check if we're viewing a new PDF file
	if p.currentFilePath != path {
		p.currentFilePath = path
		p.currentPage = 1

		// Get total page count
		totalPages, pagesErr := p.getTotalPages(path)
		if pagesErr != nil {
			slog.Warn("Could not determine PDF page count, defaulting to 1", "error", pagesErr)
			p.totalPages = 1
		} else {
			p.totalPages = totalPages
		}
	}

	// Ensure current page is within valid range
	if p.currentPage < 1 {
		p.currentPage = 1
	}
	if p.currentPage > p.totalPages {
		p.currentPage = p.totalPages
	}

	// Render current page to PNG using mutool
	img, err := p.renderPDFPageWithMutool(mutoolPath, path, p.currentPage)
	if err != nil {
		return "", err
	}

	// Use the image previewer to display the rendered page
	return p.renderPDFImage(img, path, maxWidth, maxHeight, defaultBGColor, sideAreaWidth)
}

// How long mutool may take to draw a page, and to count the pages.
const (
	pdfRenderTimeout    = 5 * time.Second
	pdfPageCountTimeout = 2 * time.Second
)

// renderPDFPageWithMutool uses mutool to convert a PDF page to image
func (p *PDFPreviewer) renderPDFPageWithMutool(mutoolPath, pdfPath string, pageNum int) (image.Image, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), pdfRenderTimeout)
	defer cancel()

	// mutool draw command: renders PDF page to PNG
	// -o - : output to stdout
	// -F png : format PNG
	// -r 150 : resolution 150 DPI (good quality)
	// -c rgba : color space with alpha
	// pdfPath pageNum : file path and page number (1-indexed)
	pageStr := strconv.Itoa(pageNum)
	cmd := exec.CommandContext(ctx, mutoolPath, "draw",
		"-o", "-", // Output to stdout
		"-F", "png", // PNG format
		"-r", "150", // 150 DPI resolution
		"-c", "rgba", // RGBA color space
		pdfPath, pageStr) // PDF file and page number

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		slog.Error("mutool command failed",
			"error", err,
			"stderr", stderr.String(),
			"path", pdfPath,
			"page", pageNum)
		return nil, fmt.Errorf("mutool failed: %w", err)
	}

	// Decode PNG from stdout
	img, err := png.Decode(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		slog.Error("Failed to decode PNG from mutool output", "error", err)
		return nil, fmt.Errorf("failed to decode PNG: %w", err)
	}

	return img, nil
}

// renderPDFImage renders a PDF page image using the existing image preview infrastructure
func (p *PDFPreviewer) renderPDFImage(img image.Image, path string, maxWidth int, maxHeight int,
	defaultBGColor string, sideAreaWidth int) (string, error) {
	originalWidth := img.Bounds().Dx()
	originalHeight := img.Bounds().Dy()

	// Try Kitty first if supported
	if p.imagePreviewer.IsKittyCapable() {
		result, err := p.imagePreviewer.renderWithKittyUsingTermCap(
			img, path+"_pdf_page0",
			originalWidth, originalHeight,
			maxWidth, maxHeight,
			sideAreaWidth,
		)
		if err == nil {
			return result, nil
		}
		// Fall through to ANSI if Kitty fails
		slog.Warn("Kitty renderer failed for PDF, falling back to ANSI", "error", err)
	}

	// Fall back to ANSI rendering
	return p.imagePreviewer.ANSIRenderer(img, defaultBGColor, maxWidth, maxHeight)
}

// getTotalPages gets the total number of pages in a PDF file using mutool
func (p *PDFPreviewer) getTotalPages(pdfPath string) (int, error) {
	mutoolPath, err := exec.LookPath("mutool")
	if err != nil {
		return 0, fmt.Errorf("mutool not available: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pdfPageCountTimeout)
	defer cancel()

	// Use mutool show to extract page count
	// mutool show file.pdf pages
	// Output format: "page 1 = 6 0 R\npage 2 = 9 0 R\n..."
	cmd := exec.CommandContext(ctx, mutoolPath, "show", pdfPath, "pages")
	output, err := cmd.Output()
	if err != nil {
		slog.Error("Failed to get PDF page count", "error", err, "path", pdfPath)
		return 0, fmt.Errorf("failed to get page count: %w", err)
	}

	// Count lines that start with "page "
	lines := strings.Split(string(output), "\n")
	pageCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "page ") {
			pageCount++
		}
	}

	if pageCount == 0 {
		return 1, nil // Default to 1 page if we can't determine
	}

	return pageCount, nil
}

// GetCurrentPage returns the current page number
func (p *PDFPreviewer) GetCurrentPage() int {
	return p.currentPage
}

// GetTotalPages returns the total number of pages
func (p *PDFPreviewer) GetTotalPages() int {
	return p.totalPages
}

// GetCurrentFilePath returns the current PDF file path
func (p *PDFPreviewer) GetCurrentFilePath() string {
	return p.currentFilePath
}

// NextPage advances to the next page if available
func (p *PDFPreviewer) NextPage() bool {
	if p.currentPage < p.totalPages {
		p.currentPage++
		return true
	}
	return false
}

// PreviousPage goes back to the previous page if available
func (p *PDFPreviewer) PreviousPage() bool {
	if p.currentPage > 1 {
		p.currentPage--
		return true
	}
	return false
}

// FirstPage jumps to the first page
func (p *PDFPreviewer) FirstPage() bool {
	if p.currentPage != 1 {
		p.currentPage = 1
		return true
	}
	return false
}

// CleanUp removes temporary PDF preview files
func (p *PDFPreviewer) CleanUp() error {
	if p.tempDir != "" {
		return os.RemoveAll(p.tempDir)
	}
	return nil
}
