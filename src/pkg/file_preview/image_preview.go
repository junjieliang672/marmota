package filepreview

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // Register GIF decoder
	_ "image/jpeg" // Register JPEG decoder
	_ "image/png"  // Register PNG decoder
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/muesli/termenv"
	_ "golang.org/x/image/webp" // Register WebP decoder
)

type ImageRenderer int

const (
	RendererANSI ImageRenderer = iota
	RendererKitty
)

// ImagePreviewCache stores cached image previews
type ImagePreviewCache struct {
	cache      map[string]*CachedPreview
	mutex      sync.RWMutex
	maxEntries int
	expiration time.Duration
}

// CachedPreview represents a cached image preview
type CachedPreview struct {
	Preview    string
	Timestamp  time.Time
	Renderer   ImageRenderer
	Dimensions string // "width,height,bgColor,sideAreaWidth"
}

// ImagePreviewer encapsulates image preview functionality with caching
type ImagePreviewer struct {
	cache       *ImagePreviewCache
	terminalCap *TerminalCapabilities
	// contentTop is the screen row an image is drawn on, from 1: the first
	// row inside the preview's box.
	contentTop int
}

// NewImagePreviewer creates a new ImagePreviewer with default cache settings
func NewImagePreviewer() *ImagePreviewer {
	return NewImagePreviewerWithConfig(defaultThumbnailCacheSize, defaultCacheExpiration)
}

// NewImagePreviewerWithConfig creates a new ImagePreviewer with custom cache configuration
func NewImagePreviewerWithConfig(maxEntries int, expiration time.Duration) *ImagePreviewer {
	previewer := &ImagePreviewer{
		cache:       NewImagePreviewCache(maxEntries, expiration),
		terminalCap: NewTerminalCapabilities(),
	}

	// Initialize terminal capabilities
	previewer.terminalCap.InitTerminalCapabilities()

	return previewer
}

// SetContentTop sets the screen row, from 1, that images are drawn on.
func (p *ImagePreviewer) SetContentTop(row int) {
	p.contentTop = row
}

// NewImagePreviewCache creates a new image preview cache
func NewImagePreviewCache(maxEntries int, expiration time.Duration) *ImagePreviewCache {
	cache := &ImagePreviewCache{
		cache:      make(map[string]*CachedPreview),
		maxEntries: maxEntries,
		expiration: expiration,
	}

	// Start a cleanup goroutine
	go cache.periodicCleanup()

	return cache
}

// periodicCleanup removes expired entries periodically
func (c *ImagePreviewCache) periodicCleanup() {
	//nolint:mnd // half of expiration for cleanup interval
	ticker := time.NewTicker(c.expiration / 2)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanupExpired()
	}
}

// cleanupExpired removes expired cache entries
func (c *ImagePreviewCache) cleanupExpired() {
	now := time.Now()
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for key, entry := range c.cache {
		if now.Sub(entry.Timestamp) > c.expiration {
			delete(c.cache, key)
		}
	}
}

// Get retrieves a cached preview if available
func (c *ImagePreviewCache) Get(path, dimensions string, renderer ImageRenderer) (string, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	cacheKey := path + ":" + dimensions

	if entry, exists := c.cache[cacheKey]; exists {
		if entry.Renderer == renderer && time.Since(entry.Timestamp) < c.expiration {
			return entry.Preview, true
		}
	}

	return "", false
}

// Set stores a preview in the cache
func (c *ImagePreviewCache) Set(path, dimensions, preview string, renderer ImageRenderer) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if we need to evict entries
	if len(c.cache) >= c.maxEntries {
		c.evictOldest()
	}

	cacheKey := path + ":" + dimensions
	c.cache[cacheKey] = &CachedPreview{
		Preview:    preview,
		Timestamp:  time.Now(),
		Renderer:   renderer,
		Dimensions: dimensions,
	}
}

// evictOldest removes the oldest entry from the cache
func (c *ImagePreviewCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	// Find the oldest entry
	for key, entry := range c.cache {
		if oldestKey == "" || entry.Timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Timestamp
		}
	}

	// Remove the oldest entry
	if oldestKey != "" {
		delete(c.cache, oldestKey)
	}
}

type colorCache struct {
	rgbaToTermenv map[color.RGBA]termenv.RGBColor
}

func newColorCache() *colorCache {
	return &colorCache{
		rgbaToTermenv: make(map[color.RGBA]termenv.RGBColor),
	}
}

func (c *colorCache) getTermenvColor(col color.Color, fallbackColor string) termenv.RGBColor {
	rgba, ok := color.RGBAModel.Convert(col).(color.RGBA)
	if !ok || rgba.A == 0 {
		return termenv.RGBColor(fallbackColor)
	}

	if termenvColor, exists := c.rgbaToTermenv[rgba]; exists {
		return termenvColor
	}

	termenvColor := termenv.RGBColor(fmt.Sprintf("#%02x%02x%02x", rgba.R, rgba.G, rgba.B))
	c.rgbaToTermenv[rgba] = termenvColor
	return termenvColor
}

// ConvertImageToANSI converts an image to ANSI escape codes with proper aspect ratio
func ConvertImageToANSI(img image.Image, defaultBGColor color.Color) string {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	var output strings.Builder
	cache := newColorCache()
	defaultBGHex := colorToHex(defaultBGColor)

	for y := 0; y < height; y += 2 {
		for x := range width {
			upperColor := cache.getTermenvColor(img.At(x, y), defaultBGHex)
			lowerColor := cache.getTermenvColor(defaultBGColor, "")

			if y+1 < height {
				lowerColor = cache.getTermenvColor(img.At(x, y+1), defaultBGHex)
			}

			// Using the "▄" character which fills the lower half
			cell := termenv.String("▄").Foreground(lowerColor).Background(upperColor)
			output.WriteString(cell.String())
		}
		// Only add newline if this is not the last row
		if y+2 < height {
			output.WriteString("\n")
		}
	}

	return output.String()
}

// ImagePreview generates a preview of an image file
func (p *ImagePreviewer) ImagePreview(path string, maxWidth int, maxHeight int,
	defaultBGColor string, sideAreaWidth int) (string, error) {
	// Validate dimensions
	if maxWidth <= 0 || maxHeight <= 0 {
		return "", fmt.Errorf("dimensions must be positive (maxWidth=%d, maxHeight=%d)", maxWidth, maxHeight)
	}

	// Create dimensions string for cache key
	dimensions := fmt.Sprintf("%d,%d,%s,%d", maxWidth, maxHeight, defaultBGColor, sideAreaWidth)

	// Try Kitty first as it's more modern
	if p.IsKittyCapable() {
		// Check cache for Kitty renderer
		if preview, found := p.cache.Get(path, dimensions, RendererKitty); found {
			return preview, nil
		}

		preview, err := p.ImagePreviewWithRenderer(
			path,
			maxWidth,
			maxHeight,
			defaultBGColor,
			RendererKitty,
			sideAreaWidth,
		)
		if err == nil {
			// Cache the successful result
			p.cache.Set(path, dimensions, preview, RendererKitty)
			return preview, nil
		}

		// Fall through to ANSI if Kitty fails
		slog.Error("Kitty renderer failed, falling back to ANSI", "error", err)
	}

	// Check cache for ANSI renderer
	if preview, found := p.cache.Get(path, dimensions, RendererANSI); found {
		return preview, nil
	}

	// Fall back to ANSI
	preview, err := p.ImagePreviewWithRenderer(path, maxWidth, maxHeight, defaultBGColor, RendererANSI, sideAreaWidth)
	if err == nil {
		// Cache the successful result
		p.cache.Set(path, dimensions, preview, RendererANSI)
	}
	return preview, err
}

// ImagePreviewWithRenderer generates an image preview using the specified renderer
func (p *ImagePreviewer) ImagePreviewWithRenderer(path string, maxWidth int, maxHeight int,
	defaultBGColor string, renderer ImageRenderer, sideAreaWidth int) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	const maxFileSize = 100 * 1024 * 1024 // 100MB limit
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("image file too large: %d bytes", info.Size())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// SVG detection: route to rsvg-convert before standard image pipeline
	if isSVGData(data) {
		img, renderErr := renderSVGWithRsvg(path, 1920) //nolint:mnd // 1920px max width for SVG rendering
		if renderErr != nil {
			return "", fmt.Errorf("SVG preview failed: %w", renderErr)
		}
		originalWidth := img.Bounds().Dx()
		originalHeight := img.Bounds().Dy()

		// Use Kitty if capable, otherwise ANSI (same as the existing renderer switch below)
		if p.IsKittyCapable() {
			result, kittyErr := p.renderWithKittyUsingTermCap(
				img,
				path,
				originalWidth,
				originalHeight,
				maxWidth,
				maxHeight,
				sideAreaWidth,
			)
			if kittyErr == nil {
				return result, nil
			}
			slog.Warn("Kitty renderer failed for SVG, falling back to ANSI", "error", kittyErr)
		}
		return p.ANSIRenderer(img, defaultBGColor, maxWidth, maxHeight)
	}

	// Use the new image preparation pipeline
	img, originalWidth, originalHeight, err := prepareImageForPreview(data)
	if err != nil {
		return "", err
	}

	switch renderer {
	case RendererKitty:
		result, err := p.renderWithKittyUsingTermCap(img, path, originalWidth,
			originalHeight, maxWidth, maxHeight, sideAreaWidth)
		if err != nil {
			// If kitty fails, fall back to ANSI renderer
			slog.Error("Kitty renderer failed, falling back to ANSI", "error", err)
			return p.ANSIRenderer(img, defaultBGColor, maxWidth, maxHeight)
		}
		return result, nil

	case RendererANSI:
		return p.ANSIRenderer(img, defaultBGColor, maxWidth, maxHeight)
	default:
		return "", fmt.Errorf("invalid renderer : %v", renderer)
	}
}

// Convert image to ansi
func (p *ImagePreviewer) ANSIRenderer(img image.Image, defaultBGColor string,
	maxWidth int, maxHeight int) (string, error) {
	bgColor, err := hexToColor(defaultBGColor)
	if err != nil {
		return "", fmt.Errorf("invalid background color: %w", err)
	}

	// For ANSI rendering, resize image appropriately
	fittedImg := resizeForANSI(img, maxWidth, maxHeight)
	return ConvertImageToANSI(fittedImg, bgColor), nil
}

func hexToColor(hex string) (color.RGBA, error) {
	if len(hex) != 7 || hex[0] != '#' {
		return color.RGBA{}, errors.New("invalid hex color format")
	}
	values, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{
		R: uint8(values >> rgbShift16), //nolint:gosec // RGB values are masked to 8-bit range
		G: uint8((values >> rgbShift8) & rgbMask),
		B: uint8(values & rgbMask),
		A: alphaOpaque,
	}, nil
}

func colorToHex(color color.Color) string {
	r, g, b, _ := color.RGBA()
	return fmt.Sprintf(
		"#%02x%02x%02x",
		uint8(r>>rgbShift8), //nolint:gosec // RGBA() returns 16-bit values, shifting by 8 gives 8-bit
		uint8(g>>rgbShift8), //nolint:gosec // RGBA() returns 16-bit values, shifting by 8 gives 8-bit
		uint8(b>>rgbShift8), //nolint:gosec // RGBA() returns 16-bit values, shifting by 8 gives 8-bit
	)
}
