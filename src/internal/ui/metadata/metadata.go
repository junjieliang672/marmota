package metadata

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/barasher/go-exiftool"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

type Metadata struct {
	// Stores key value pairs
	data     [][2]string
	infoMsg  string
	filepath string

	// For the Details box: what data holds as text, as values.
	isDir   bool
	items   int // entries in a folder; -1 when not counted
	modTime time.Time
}

func NewMetadata(data [][2]string, filepath string, infoMsg string) Metadata {
	return Metadata{
		data:     data,
		filepath: filepath,
		infoMsg:  infoMsg,
		items:    -1,
	}
}

// Facts is the metadata as the Details box uses it. Size, Resolution and
// Page are "" when not known.
type Facts struct {
	Path        string
	Info        string
	IsDir       bool
	Items       int
	Size        string
	Modified    time.Time
	Permissions string
	Resolution  string
	Page        string
}

func (m Metadata) Facts() Facts {
	get := func(key string) string {
		v, _ := m.GetValue(key)
		return v
	}
	return Facts{
		Path:        m.filepath,
		Info:        m.infoMsg,
		IsDir:       m.isDir,
		Items:       m.items,
		Size:        get(keySize),
		Modified:    m.modTime,
		Permissions: get(keyPermissions),
		Resolution:  get(keyResolution),
		Page:        get(keyPage),
	}
}

func (m Metadata) GetPath() string {
	return m.filepath
}

func (m Metadata) GetData() [][2]string {
	return m.data
}

func (m Metadata) GetValue(key string) (string, error) {
	for _, pair := range m.data {
		if pair[0] == key {
			return pair[1], nil
		}
	}

	return "", fmt.Errorf("key %s not found", key)
}

// Note : We dont use map[string]string, as metadata
// 1 -> We dont need to support get(key) yet. Only usage is via iterating the whole list
// 2 -> We need custom ordering

func sortMetadata(meta [][2]string) {
	sort.SliceStable(meta, func(i, j int) bool {
		pi, iOkay := sortPriority[meta[i][0]]
		pj, jOkay := sortPriority[meta[j][0]]

		// Both are priority fields
		if iOkay && jOkay {
			return pi < pj
		}
		// i is a priority field, and j is not
		if iOkay {
			return true
		}

		// j is a priority field, and i is not
		if jOkay {
			return false
		}

		// None of them are priority fields, sort with name
		return meta[i][0] < meta[j][0]
	})
}

func GetMetadata(
	filePath string,
	metadataFocussed bool,
	et *exiftool.Exiftool,
	pdfCurrentPage, pdfTotalPages int,
) Metadata {
	meta := getMetaDataUnsorted(filePath, metadataFocussed, et, pdfCurrentPage, pdfTotalPages)
	sortMetadata(meta.data)
	return meta
}

func getSymLinkMetaData(filePath string) Metadata {
	res := Metadata{
		filepath: filePath,
		items:    -1,
	}
	linkPath, symlinkErr := filepath.EvalSymlinks(filePath)
	if symlinkErr != nil {
		res.infoMsg = linkFileBrokenMsg
	} else {
		path := [2]string{keyPath, linkPath}
		res.data = append(res.data, path)
	}
	return res
}

func getMetaDataUnsorted(
	filePath string,
	metadataFocussed bool,
	et *exiftool.Exiftool,
	pdfCurrentPage, pdfTotalPages int,
) Metadata {
	res := Metadata{
		filepath: filePath,
		items:    -1,
	}

	fileInfo, err := os.Lstat(filePath)
	if err != nil {
		res.infoMsg = fileStatErrorMsg
		return res
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return getSymLinkMetaData(filePath)
	}
	// Add basic metadata information irrespective of what is fetched from exiftool
	// Note : we prioritize these while sorting Metadata
	name := [2]string{keyName, filePath} // Use full file path instead of just the name
	size := [2]string{keySize, common.FormatFileSize(fileInfo.Size())}
	modifyDate := [2]string{keyDataModified, fileInfo.ModTime().Format("Jan 2 06 15:04")}
	permissions := [2]string{keyPermissions, fileInfo.Mode().String()}

	if fileInfo.IsDir() && metadataFocussed {
		// TODO : Calling dirSize() could be expensive for large directories, as it recursively
		// walks the entire tree. For now we have async approach of loading metadata,
		// and its only loaded when metadata panel is focussed.
		size = [2]string{keySize, common.FormatFileSize(utils.DirSize(filePath))}
	}
	res.data = append(res.data, name, size, modifyDate, permissions)
	res.isDir = fileInfo.IsDir()
	res.modTime = fileInfo.ModTime()
	if res.isDir {
		res.items = countEntries(filePath)
	}

	// For images, add resolution if available (extract from exiftool without adding all fields)
	if common.Config.Metadata && et != nil {
		addImageResolution(filePath, et, &res)
	}

	// For PDFs, add page information
	addPDFPageInfo(filePath, &res, pdfCurrentPage, pdfTotalPages)

	return res
}

// countEntries is how many entries a folder has, hidden ones included; -1
// when it can't be read.
func countEntries(dir string) int {
	f, err := os.Open(dir)
	if err != nil {
		return -1
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return -1
	}
	return len(names)
}

// addImageResolution detects and adds image resolution from exiftool metadata
// This extracts ONLY width and height, without adding all other exiftool fields
func addImageResolution(filePath string, et *exiftool.Exiftool, res *Metadata) {
	if et == nil {
		return
	}

	// Extract metadata from file using exiftool
	fileInfos := et.ExtractMetadata(filePath)
	if len(fileInfos) == 0 {
		return
	}

	// Create map for quick lookup
	metaMap := make(map[string]string)
	for _, fileInfo := range fileInfos {
		if fileInfo.Err != nil {
			continue
		}
		for k, v := range fileInfo.Fields {
			metaMap[k] = fmt.Sprintf("%v", v)
		}
	}

	// Try different exiftool field name variations for width and height
	widthKeys := []string{
		"ImageWidth",      // Camel case (rare)
		"Image Width",     // Common exiftool format with space
		"Width",           // Simple format
		"ExifImageWidth",  // EXIF specific
		"PixelXDimension", // Alternative EXIF
	}
	heightKeys := []string{
		"ImageHeight",     // Camel case (rare)
		"Image Height",    // Common exiftool format with space
		"Height",          // Simple format
		"ExifImageHeight", // EXIF specific
		"PixelYDimension", // Alternative EXIF
	}

	var width, height string
	for _, wKey := range widthKeys {
		if w, ok := metaMap[wKey]; ok {
			width = w
			break
		}
	}

	for _, hKey := range heightKeys {
		if h, ok := metaMap[hKey]; ok {
			height = h
			break
		}
	}

	// If both width and height are found, add resolution field
	if width != "" && height != "" {
		resolution := [2]string{keyResolution, fmt.Sprintf("%sx%s", width, height)}
		res.data = append(res.data, resolution)
	}
}

// pdfPageCountTimeout bounds mutool's count of a PDF's pages.
const pdfPageCountTimeout = 2 * time.Second

// getPDFPageCount gets the total number of pages in a PDF file using mutool
func getPDFPageCount(pdfPath string) (int, error) {
	mutoolPath, err := exec.LookPath("mutool")
	if err != nil {
		return 0, fmt.Errorf("mutool not available: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pdfPageCountTimeout)
	defer cancel()

	// Use mutool show to extract page count
	// Output format: "page 1 = 6 0 R\npage 2 = 9 0 R\n..."
	cmd := exec.CommandContext(ctx, mutoolPath, "show", pdfPath, "pages")
	output, err := cmd.Output()
	if err != nil {
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

// addPDFPageInfo adds PDF page information to metadata
func addPDFPageInfo(filePath string, res *Metadata, currentPage, totalPages int) {
	// Check if it's a PDF file
	if !strings.HasSuffix(strings.ToLower(filePath), ".pdf") {
		return
	}

	// If page info was provided from the previewer, use it
	if currentPage > 0 && totalPages > 0 {
		pageInfo := [2]string{keyPage, fmt.Sprintf("%d / %d", currentPage, totalPages)}
		res.data = append(res.data, pageInfo)
		return
	}

	// Otherwise, get total page count and default to page 1
	totalPagesFromFile, err := getPDFPageCount(filePath)
	if err != nil {
		slog.Debug("Could not get PDF page count", "error", err, "path", filePath)
		return
	}

	// Add page info as "1 / N" (default when not in preview)
	pageInfo := [2]string{keyPage, fmt.Sprintf("1 / %d", totalPagesFromFile)}
	res.data = append(res.data, pageInfo)
}
