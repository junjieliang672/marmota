package preview

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/junjieliang672/marmota/src/internal/ui"
	"github.com/junjieliang672/marmota/src/internal/ui/rendering"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/config/icon"
	filepreview "github.com/junjieliang672/marmota/src/pkg/file_preview"
)

type Model struct {
	open               bool
	width              int
	height             int
	location           string
	content            string
	imagePreviewer     *filepreview.ImagePreviewer
	pdfPreviewer       *filepreview.PDFPreviewer
	batCmd             string
	thumbnailGenerator *filepreview.ThumbnailGenerator
	dirPreviewCache    map[string]cachedDirectoryPreview
	dirPreviewCacheMu  sync.Mutex
	// scroll is where the wheel has scrolled the preview to (scroll.go).
	scroll scrollState
}

type cachedDirectoryPreview struct {
	rendered  string
	expiresAt time.Time
}

const (
	dirPreviewCacheTTL     = 5 * time.Second
	dirPreviewCacheMaxSize = 32
	// The preview is drawn in a box: a cell of border on each side.
	previewFrameCells = 2
	// Images are drawn from the first row inside the box; the box starts on
	// the screen's first row.
	previewContentTop = 2
	// A box needs this many rows and columns, borders included.
	minFrameSize = 3
)

func New() Model {
	generator, err := filepreview.NewThumbnailGenerator()
	if err != nil {
		slog.Error("Could not NewThumbnailGenerator object", "error", err)
	}

	imagePreviewer := filepreview.NewImagePreviewer()
	imagePreviewer.SetContentTop(previewContentTop)

	return Model{
		open:               common.Config.DefaultOpenFilePreview,
		imagePreviewer:     imagePreviewer,
		pdfPreviewer:       filepreview.NewPDFPreviewer(imagePreviewer),
		thumbnailGenerator: generator,
		dirPreviewCache:    make(map[string]cachedDirectoryPreview),
		// TODO:  This is an IO operation, move to async ?
		batCmd: checkBatCmd(),
	}
}

func (m *Model) SetWidth(width int) {
	if width != m.width {
		m.scroll = scrollState{}
	}
	m.width = width
}

func (m *Model) SetHeight(height int) {
	if height != m.height {
		m.scroll = scrollState{}
	}
	m.height = height
}

func (m *Model) IsOpen() bool {
	return m.open
}

func (m *Model) Open() {
	m.open = true
}

func (m *Model) Close() {
	m.open = false
}

// InnerWidth is the width inside the preview's box.
func (m *Model) InnerWidth() int {
	return max(0, m.width-previewFrameCells)
}

// InnerHeight is the height inside the preview's box.
func (m *Model) InnerHeight() int {
	return max(0, m.height-previewFrameCells)
}

// imageColumn is the screen column, from 1, where the inside of the box
// starts: the preview is the rightmost pane, and its border takes a column.
// Images are drawn there, on the row SetContentTop gives.
func (m *Model) imageColumn(fullModelWidth int) int {
	return fullModelWidth - m.width + previewFrameCells
}

// frame draws content, sized to the inside of the box, in the box titled
// "Preview".
func (m *Model) frame(content string) string {
	if m.width < minFrameSize || m.height < minFrameSize {
		return content
	}
	r := ui.FilePreviewFrameRenderer(m.height, m.width, false)
	r.AddLines(content)
	return r.Render()
}

// Simple rendered string with given text
func (m *Model) RenderText(text string) string {
	return m.frame(ui.FilePreviewPanelRenderer(m.InnerHeight(), m.InnerWidth()).
		AddLines(text).
		Render()) + m.imagePreviewer.ClearKittyImages()
}

// RenderLines draws lines that are already laid out to fit the pane, such
// as an answer from the Ask bar.
func (m *Model) RenderLines(lines []string) string {
	r := ui.FilePreviewPanelRenderer(m.InnerHeight(), m.InnerWidth())
	for _, line := range lines {
		r.AddLineWithCustomTruncate(line, rendering.TailsTruncateRight)
	}
	return m.frame(r.Render()) + m.imagePreviewer.ClearKittyImages()
}

func (m *Model) SetContentWithRenderText(text string) {
	m.scroll = scrollState{}
	m.content = m.RenderText(text)
}

func (m *Model) GetContent() string {
	return m.content
}

func (m *Model) GetWidth() int {
	return m.width
}

func (m *Model) GetHeight() int {
	return m.height
}

func (m *Model) GetLocation() string {
	return m.location
}

func (m *Model) SetOpen(open bool) {
	m.open = open
}

func (m *Model) SetLocation(location string) {
	m.scroll = scrollState{}
	m.location = location
}

// SetContent shows what was drawn for the preview's location, from its top.
func (m *Model) SetContent(content string) {
	m.scroll = scrollState{}
	m.content = content
}

func (m *Model) ToggleOpen() {
	m.open = !m.open
}

func (m *Model) GetPdfPreviewer() *filepreview.PDFPreviewer {
	return m.pdfPreviewer
}

func (m *Model) CleanUp() {
	if m.thumbnailGenerator != nil {
		err := m.thumbnailGenerator.CleanUp()
		if err != nil {
			slog.Error("Error While cleaning up TempDirectory", "Error:", err)
		}
	}
}

func renderFileInfoError(r *rendering.Renderer, err error) string {
	slog.Error("Error get file info", "error", err)
	return r.Render()
}

func renderUnsupportedFormat(box lipgloss.Style) string {
	return box.Render(common.FilePreviewUnsupportedFormatText)
}

func renderUnsupportedFileMode(r *rendering.Renderer) string {
	r.AddLines(common.FilePreviewUnsupportedFileMode)
	return r.Render()
}

func (m *Model) renderDirectoryPreview(r *rendering.Renderer, itemPath string, previewHeight int) string {
	if rendered, ok := m.getCachedDirectoryPreview(itemPath, previewHeight); ok {
		return rendered
	}

	var rendered string
	// Check if tree view is enabled
	if common.Config.DirPreviewTreeView {
		rendered = renderDirectoryPreviewTree(r, itemPath, previewHeight)
	} else {
		// Fallback to simple list view
		rendered = renderDirectoryPreviewList(r, itemPath, previewHeight)
	}

	m.setCachedDirectoryPreview(itemPath, previewHeight, rendered)
	return rendered
}

// treeHeaderLines are the folder name and the separator above the tree.
const treeHeaderLines = 2

func renderDirectoryPreviewTree(r *rendering.Renderer, itemPath string, previewHeight int) string {
	// Configure tree rendering with improved settings
	config := TreeConfig{
		MaxDepth:         common.Config.DirPreviewMaxDepth,
		MaxFiles:         common.Config.DirPreviewMaxFiles,
		MaxFilesPerLevel: common.Config.DirPreviewMaxFilesNested,
		ShowHidden:       common.Config.DirPreviewShowHidden,
		MaxTotalLines:    max(1, previewHeight-treeHeaderLines),
		CompactMode:      common.Config.DirPreviewCompactTree,
		ShowFileCount:    common.Config.DirPreviewShowCount,
		UseColors:        true, // Always use colors for better visual hierarchy
		IndentSize:       treeIndentSize,
	}

	lines, err := RenderDirectoryTree(itemPath, config)
	if err != nil {
		slog.Error("Error render directory tree preview", "error", err)
		r.AddLines(common.FilePreviewDirectoryUnreadableText)
		return r.Render()
	}

	if len(lines) == 0 {
		r.AddLines(common.FilePreviewEmptyText)
		return r.Render()
	}

	// Add summary header for better context
	dirName := filepath.Base(itemPath)
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#89b4fa")).
		Bold(true)
	r.AddLines(headerStyle.Render("📁 " + dirName))

	// Add a subtle separator for visual clarity
	separatorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#45475a"))
	r.AddLines(separatorStyle.Render("─────────────────────"))

	// Add all tree lines
	for _, line := range lines {
		r.AddLines(line)
	}

	return r.Render()
}

func renderDirectoryPreviewList(r *rendering.Renderer, itemPath string, previewHeight int) string {
	files, err := os.ReadDir(itemPath)
	if err != nil {
		slog.Error("Error render directory preview", "error", err)
		r.AddLines(common.FilePreviewDirectoryUnreadableText)
		return r.Render()
	}

	if len(files) == 0 {
		r.AddLines(common.FilePreviewEmptyText)
		return r.Render()
	}

	sort.Slice(files, func(i, j int) bool {
		iInfo, iErr := files[i].Info()
		jInfo, jErr := files[j].Info()
		if iErr != nil && jErr != nil {
			return false
		}
		if iErr != nil {
			return false
		}
		if jErr != nil {
			return true
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	maxFiles := previewHeight
	if common.Config.DirPreviewMaxFiles > 0 && common.Config.DirPreviewMaxFiles < previewHeight {
		maxFiles = common.Config.DirPreviewMaxFiles
	}

	totalFiles := len(files)
	filesToShow := min(maxFiles, totalFiles)

	for i := range filesToShow {
		file := files[i]
		isLink := false
		if info, err := file.Info(); err == nil {
			isLink = info.Mode()&os.ModeSymlink != 0
		}
		style := common.GetElementIcon(file.Name(), file.IsDir(), isLink, common.Config.Nerdfont)
		res := lipgloss.NewStyle().Foreground(lipgloss.Color(style.Color)).Background(common.FilePanelBGColor).
			Render(style.Icon+" ") + common.FilePanelStyle.Render(file.Name())
		r.AddLines(res)
	}

	// Show truncation indicator if there are more files
	if totalFiles > filesToShow {
		truncStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f9e2af")).
			Background(common.FilePanelBGColor)
		remaining := totalFiles - filesToShow
		var unit string
		if remaining == 1 {
			unit = "item"
		} else {
			unit = "items"
		}
		r.AddLines(truncStyle.Render(fmt.Sprintf("... and %d more %s", remaining, unit)))
	}

	return r.Render()
}

func (m *Model) renderImagePreview(box lipgloss.Style, itemPath string, previewWidth,
	previewHeight int, sideAreaWidth int,
) string {
	if !m.open {
		return box.Render("\n --- Preview panel is closed ---")
	}

	if !common.Config.ShowImagePreview {
		return box.Render("\n --- Image preview is disabled ---")
	}

	// Use the new auto-detection function to choose the best renderer
	imageRender, err := m.imagePreviewer.ImagePreview(itemPath, previewWidth, previewHeight,
		common.Theme.FilePanelBG, sideAreaWidth)
	if errors.Is(err, image.ErrFormat) {
		return box.Render("\n --- " + icon.Error + " Unsupported image formats ---")
	}

	if err != nil {
		slog.Error("Error convert image to ansi", "error", err)
		return box.Render("\n --- " + icon.Error + " Error convert image to ansi ---")
	}

	// Check if this looks like Kitty protocol output (starts with escape sequences)
	// For Kitty protocol, avoid using lipgloss alignment to prevent layout drift
	if strings.HasPrefix(imageRender, "\x1b_G") {
		rendered := common.FilePreviewBox(previewHeight, previewWidth).Render(imageRender)
		return rendered
	}

	// For ANSI output, we can safely use vertical alignment
	return box.AlignVertical(lipgloss.Center).AlignHorizontal(lipgloss.Center).Render(imageRender)
}

func (m *Model) renderPDFPreview(box lipgloss.Style, itemPath string, previewWidth,
	previewHeight int, sideAreaWidth int,
) string {
	if !m.open {
		return box.Render("\n --- Preview panel is closed ---")
	}

	if !common.Config.ShowImagePreview {
		return box.Render("\n --- PDF preview is disabled (requires image preview) ---")
	}

	if m.pdfPreviewer == nil {
		return box.Render("\n --- " + icon.Error + " PDF previewer not initialized ---")
	}

	// Render the first page of the PDF
	pdfRender, err := m.pdfPreviewer.PDFPreview(itemPath, previewWidth, previewHeight,
		common.Theme.FilePanelBG, sideAreaWidth)
	if err != nil {
		slog.Error("Error rendering PDF preview", "error", err, "path", itemPath)
		return box.Render("\n --- " + icon.Error + " Error rendering PDF preview ---")
	}

	// Check if this looks like Kitty protocol output (starts with escape sequences)
	// For Kitty protocol, avoid using lipgloss alignment to prevent layout drift
	if strings.HasPrefix(pdfRender, "\x1b_G") {
		rendered := common.FilePreviewBox(previewHeight, previewWidth).Render(pdfRender)
		return rendered
	}

	// For ANSI output, we can safely use vertical alignment
	return box.AlignVertical(lipgloss.Center).AlignHorizontal(lipgloss.Center).Render(pdfRender)
}

func (m *Model) renderTextPreview(r *rendering.Renderer, box lipgloss.Style, itemPath string,
	previewWidth, previewHeight int,
) string {
	fileContent, problem := m.textContent(itemPath, previewWidth, previewHeight)
	if problem != "" {
		return box.Render(problem)
	}
	r.AddLines(fileContent)
	return r.Render()
}

// textContent is up to lines lines of a text file, each cut to width, and
// highlighted when it is code. problem says what to show instead when there
// is nothing to show.
func (m *Model) textContent(itemPath string, width, lines int) (string, string) {
	format := lexers.Match(filepath.Base(itemPath))
	if format == nil {
		isText, err := common.IsTextFile(itemPath)
		if err != nil {
			slog.Error("Error while checking text file", "error", err)
			return "", common.FilePreviewError
		} else if !isText {
			return "", common.FilePreviewUnsupportedFormatText
		}
	}

	fileContent, err := utils.ReadFileContent(itemPath, width, lines)
	if err != nil {
		slog.Error("Error open file", "error", err)
		return "", common.FilePreviewError
	}

	if fileContent == "" {
		return "", common.FilePreviewEmptyText
	}

	if format == nil {
		return fileContent, ""
	}
	background := ""
	if !common.Config.TransparentBackground {
		background = common.Theme.FilePanelBG
	}
	if common.Config.CodePreviewer == "bat" {
		if m.batCmd == "" {
			return "", "\n --- " + icon.Error +
				" 'bat' is not installed or not found. ---\n --- Cannot render file preview. ---"
		}
		fileContent, err = getBatSyntaxHighlightedContent(itemPath, lines, background, m.batCmd)
	} else {
		fileContent, err = highlightCode(fileContent, format.Config().Name,
			common.Theme.CodeSyntaxHighlightTheme, background)
	}
	if err != nil {
		slog.Error("Error render code highlight", "error", err)
		return "", "\n" + common.FilePreviewError
	}
	return fileContent, ""
}

func (m *Model) RenderWithPath(itemPath string, fullModelWidth int) string {
	return m.frame(m.renderContent(itemPath, fullModelWidth))
}

// renderContent is what the preview shows of itemPath, sized to the inside of
// its box.
func (m *Model) renderContent(itemPath string, fullModelWidth int) string {
	previewHeight := m.InnerHeight()
	previewWidth := m.InnerWidth()
	sideAreaWidth := m.imageColumn(fullModelWidth)

	box := common.FilePreviewBox(previewHeight, previewWidth)
	r := ui.FilePreviewPanelRenderer(previewHeight, previewWidth)
	clearCmd := m.imagePreviewer.ClearKittyImages()

	fileInfo, infoErr := os.Stat(itemPath)
	if infoErr != nil {
		return renderFileInfoError(r, infoErr) + clearCmd
	}
	slog.Debug("Attempting to render preview", "itemPath", itemPath,
		"mode", fileInfo.Mode().String(), "isRegular", fileInfo.Mode().IsRegular())

	// For non regular files which are not directories Dont try to read them
	// See Issue #876
	if !fileInfo.Mode().IsRegular() && (fileInfo.Mode()&fs.ModeDir) == 0 {
		return renderUnsupportedFileMode(r) + clearCmd
	}

	ext := filepath.Ext(itemPath)
	if slices.Contains(common.UnsupportedPreviewFormats, ext) {
		return renderUnsupportedFormat(box) + clearCmd
	}

	if fileInfo.IsDir() {
		return m.renderDirectoryPreview(r, itemPath, previewHeight) + clearCmd
	}

	if isVideoFile(itemPath) {
		if m.thumbnailGenerator == nil {
			return renderUnsupportedFormat(box) + clearCmd
		}
		thumbnailPath, err := m.thumbnailGenerator.GetThumbnailOrGenerate(itemPath)
		if err != nil {
			slog.Error("Error generating thumbnail", "error", err)
			return renderUnsupportedFormat(box) + clearCmd
		}
		return m.renderImagePreview(box, thumbnailPath, previewWidth, previewHeight, sideAreaWidth)
	}

	if isImageFile(itemPath) {
		return m.renderImagePreview(box, itemPath, previewWidth, previewHeight, sideAreaWidth)
	}

	if isPDFFile(itemPath) {
		return m.renderPDFPreview(box, itemPath, previewWidth, previewHeight, sideAreaWidth)
	}

	return m.renderTextPreview(r, box, itemPath, previewWidth, previewHeight) + clearCmd
}

func (m *Model) getCachedDirectoryPreview(itemPath string, previewHeight int) (string, bool) {
	cacheKey := directoryPreviewCacheKey(itemPath, previewHeight)
	now := time.Now()

	m.dirPreviewCacheMu.Lock()
	defer m.dirPreviewCacheMu.Unlock()

	entry, ok := m.dirPreviewCache[cacheKey]
	if !ok {
		return "", false
	}
	if now.After(entry.expiresAt) {
		delete(m.dirPreviewCache, cacheKey)
		return "", false
	}
	return entry.rendered, true
}

func (m *Model) setCachedDirectoryPreview(itemPath string, previewHeight int, rendered string) {
	cacheKey := directoryPreviewCacheKey(itemPath, previewHeight)
	now := time.Now()

	m.dirPreviewCacheMu.Lock()
	defer m.dirPreviewCacheMu.Unlock()

	for key, entry := range m.dirPreviewCache {
		if now.After(entry.expiresAt) {
			delete(m.dirPreviewCache, key)
		}
	}
	if len(m.dirPreviewCache) >= dirPreviewCacheMaxSize {
		for key := range m.dirPreviewCache {
			delete(m.dirPreviewCache, key)
			break
		}
	}

	m.dirPreviewCache[cacheKey] = cachedDirectoryPreview{
		rendered:  rendered,
		expiresAt: now.Add(dirPreviewCacheTTL),
	}
}

func directoryPreviewCacheKey(itemPath string, previewHeight int) string {
	return fmt.Sprintf("%s|%d|%t|%d|%d|%d|%t|%t|%t",
		itemPath,
		previewHeight,
		common.Config.DirPreviewTreeView,
		common.Config.DirPreviewMaxDepth,
		common.Config.DirPreviewMaxFiles,
		common.Config.DirPreviewMaxFilesNested,
		common.Config.DirPreviewShowHidden,
		common.Config.DirPreviewCompactTree,
		common.Config.DirPreviewShowCount,
	)
}

func getBatSyntaxHighlightedContent(
	itemPath string,
	previewLine int,
	background string,
	batCmd string,
) (string, error) {
	// --plain: use the plain style without line numbers and decorations
	// --force-colorization: force colorization for non-interactive shell output
	// --line-range <:m>: only read from line 1 to line "m"
	batArgs := []string{itemPath, "--plain", "--force-colorization", "--line-range", fmt.Sprintf(":%d", previewLine-1)}

	// set timeout for the external command execution to 500ms max
	ctx, cancel := context.WithTimeout(context.Background(), common.DefaultPreviewTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, batCmd, batArgs...)

	fileContentBytes, err := cmd.Output()
	if err != nil {
		slog.Error("Error render code highlight", "error", err)
		return "", err
	}

	fileContent := string(fileContentBytes)
	if !common.Config.TransparentBackground {
		fileContent = setBatBackground(fileContent, background)
	}
	return fileContent, nil
}

func setBatBackground(input string, background string) string {
	tokens := strings.Split(input, "\x1b[0m")
	backgroundStyle := lipgloss.NewStyle().Background(lipgloss.Color(background))
	for idx, token := range tokens {
		tokens[idx] = backgroundStyle.Render(token)
	}
	return strings.Join(tokens, "\x1b[0m")
}

// Check if bat is an executable in PATH and whether to use bat or batcat as command
func checkBatCmd() string {
	if _, err := exec.LookPath("bat"); err == nil {
		return "bat"
	}
	// on ubuntu bat executable is called batcat
	if _, err := exec.LookPath("batcat"); err == nil {
		return "batcat"
	}
	return ""
}

func isImageFile(filename string) bool {
	return common.ImageExtensions[strings.ToLower(filepath.Ext(filename))]
}

func isVideoFile(filename string) bool {
	return common.VideoExtensions[strings.ToLower(filepath.Ext(filename))]
}

func isPDFFile(filename string) bool {
	return strings.ToLower(filepath.Ext(filename)) == ".pdf"
}
