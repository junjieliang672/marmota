package preview

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// TreeConfig holds configuration for tree preview rendering
type TreeConfig struct {
	MaxDepth         int  // Maximum depth to traverse (0 = list only, -1 = unlimited)
	MaxFiles         int  // Maximum number of files to show per directory
	MaxFilesPerLevel int  // Maximum files to show at each level (for nested dirs)
	ShowHidden       bool // Whether to show hidden files
	MaxTotalLines    int  // Maximum total lines to render
	CompactMode      bool // Whether to use compact tree symbols
	ShowFileCount    bool // Whether to show file counts for directories
	UseColors        bool // Whether to use colors for different file types
	IndentSize       int  // Size of indentation (in spaces)
}

// The tree's defaults.
const (
	defaultTreeMaxDepth         = 2
	defaultTreeMaxFiles         = 20
	defaultTreeMaxFilesPerLevel = 5 // Limit files shown at each nesting level
	defaultTreeMaxTotalLines    = 100
	// treeIndentSize is the spaces each nesting level indents by.
	treeIndentSize = 4
)

// DefaultTreeConfig returns the default tree configuration
func DefaultTreeConfig() TreeConfig {
	return TreeConfig{
		MaxDepth:         defaultTreeMaxDepth,
		MaxFiles:         defaultTreeMaxFiles,
		MaxFilesPerLevel: defaultTreeMaxFilesPerLevel,
		ShowHidden:       false,
		MaxTotalLines:    defaultTreeMaxTotalLines,
		CompactMode:      false,
		ShowFileCount:    true,
		UseColors:        true,
		IndentSize:       treeIndentSize,
	}
}

// TreeNode represents a node in the directory tree
type TreeNode struct {
	Name      string
	Path      string
	IsDir     bool
	IsLink    bool
	Children  []*TreeNode
	FileCount int // Number of files in directory
	DirCount  int // Number of subdirectories
	Truncated bool
}

type dirEntryWithInfo struct {
	entry os.DirEntry
	info  os.FileInfo
}

// buildTree builds a tree structure from a directory
func buildTree(path string, config TreeConfig, currentDepth int) (*TreeNode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	node := &TreeNode{
		Name:   filepath.Base(path),
		Path:   path,
		IsDir:  info.IsDir(),
		IsLink: info.Mode()&os.ModeSymlink != 0,
	}

	// If it's a symlink, we don't traverse further; if it's not a
	// directory, or we've reached max depth, return
	if node.IsLink || !node.IsDir || (config.MaxDepth >= 0 && currentDepth >= config.MaxDepth) {
		return node, nil
	}

	// Read directory contents
	entries, err := os.ReadDir(path)
	if err != nil {
		return node, nil //nolint:nilerr // a folder it can't read is shown without children
	}

	filteredEntries := visibleEntries(entries, config.ShowHidden)
	// Separate directories and files for better display
	dirs, files := splitDirsAndFiles(filteredEntries)
	node.DirCount, node.FileCount = len(dirs), len(files)

	childCount := addChildren(node, config, currentDepth, dirs, files,
		childLimit(config, currentDepth, len(filteredEntries)))
	// Mark as truncated if we didn't show everything
	node.Truncated = childCount < len(filteredEntries)

	return node, nil
}

// visibleEntries are a folder's entries the tree shows, hidden ones only
// with showHidden, sorted by modification time descending (most recent
// first).
func visibleEntries(entries []os.DirEntry, showHidden bool) []dirEntryWithInfo {
	var visible []dirEntryWithInfo
	for _, entry := range entries {
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		visible = append(visible, dirEntryWithInfo{entry: entry, info: entryInfo})
	}
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].info.ModTime().After(visible[j].info.ModTime())
	})
	return visible
}

func splitDirsAndFiles(entries []dirEntryWithInfo) ([]dirEntryWithInfo, []dirEntryWithInfo) {
	var dirs, files []dirEntryWithInfo
	for _, entry := range entries {
		if entry.entry.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}
	return dirs, files
}

// childLimit is how many of a folder's count entries the tree shows at
// depth: more at the root level (MaxFiles), fewer at nested levels so as
// not to overwhelm (MaxFilesPerLevel, else MaxFiles).
func childLimit(config TreeConfig, depth, count int) int {
	limit := config.MaxFiles
	if depth > 0 && config.MaxFilesPerLevel > 0 {
		limit = config.MaxFilesPerLevel
	}
	if limit > 0 && count > limit {
		return limit
	}
	return count
}

// addChildren adds the trees of dirs, then of files, to node, up to limit
// in all, and returns how many it added. A folder that can't be read takes
// no place; a file takes one either way.
func addChildren(node *TreeNode, config TreeConfig, depth int, dirs, files []dirEntryWithInfo, limit int) int {
	childCount := 0
	for i := 0; i < len(dirs) && childCount < limit; i++ {
		if child, err := buildTree(filepath.Join(node.Path, dirs[i].entry.Name()), config, depth+1); err == nil {
			node.Children = append(node.Children, child)
			childCount++
		}
	}
	remainingSlots := limit - childCount
	for i := 0; i < len(files) && i < remainingSlots; i++ {
		if child, err := buildTree(filepath.Join(node.Path, files[i].entry.Name()), config, depth+1); err == nil {
			node.Children = append(node.Children, child)
			childCount++
		}
	}
	return childCount
}

// treeGlyphs are the characters a tree's lines are drawn with.
type treeGlyphs struct {
	connector, branch, trunk, empty string
}

// treeGlyphsFor are the tree characters: balanced clarity without
// redundancy, narrower in compact mode.
func treeGlyphsFor(compact bool) treeGlyphs {
	if compact {
		return treeGlyphs{connector: "├─ ", branch: "└─ ", trunk: "│  ", empty: "   "}
	}
	// Use clear symbols with balanced spacing
	return treeGlyphs{connector: "├── ", branch: "└── ", trunk: "│   ", empty: "    "}
}

// renderTreeWithDepth renders a tree node with depth tracking for visual effects
func renderTreeWithDepth(
	node *TreeNode,
	prefix string,
	isLast bool,
	config TreeConfig,
	linesRendered *int,
	depth int,
) []string {
	if *linesRendered >= config.MaxTotalLines {
		return []string{}
	}
	glyphs := treeGlyphsFor(config.CompactMode)

	// Calculate opacity/dimming based on depth for visual hierarchy
	// Deeper items are slightly dimmer to show hierarchy: bright, slightly
	// dimmer, more dim, then dimmer for all deeper nesting.
	treeLineColors := []lipgloss.Color{"#cdd6f4", "#bac2de", "#a6adc8", "#9399b2"}
	treeLineColor := treeLineColors[min(depth, len(treeLineColors)-1)]

	treeLineStyle := lipgloss.NewStyle().Foreground(treeLineColor)

	// Only show tree characters for nested items (depth > 0)
	// First level items don't need tree chars since they're obviously in the parent
	line := ""
	if depth > 0 {
		glyph := glyphs.connector
		if isLast {
			glyph = glyphs.branch
		}
		line = prefix + treeLineStyle.Render(glyph)
	}
	lines := []string{line + nodeLabel(node, config)}
	*linesRendered++

	// The children's prefix: proper spacing, with the trunk line styled for
	// visual continuity
	childPrefix := prefix + treeLineStyle.Render(glyphs.trunk)
	if isLast {
		childPrefix = prefix + glyphs.empty
	}
	for i, child := range node.Children {
		if *linesRendered >= config.MaxTotalLines {
			break
		}
		isLastChild := i == len(node.Children)-1
		lines = append(lines, renderTreeWithDepth(child, childPrefix, isLastChild, config, linesRendered, depth+1)...)
	}

	return lines
}

// nodeLabel is a tree node's icon and name, bold for directories, with a
// directory's counts, and a mark when its children weren't all shown.
func nodeLabel(node *TreeNode, config TreeConfig) string {
	style := common.GetElementIcon(node.Name, node.IsDir, node.IsLink, common.Config.Nerdfont)
	iconColor := lipgloss.Color(style.Color)

	// Icon with color
	label := style.Icon + " "
	if config.UseColors {
		label = lipgloss.NewStyle().Foreground(iconColor).Bold(node.IsDir).Render(style.Icon + " ")
	}

	// Name - bold for directories, normal for files
	if node.IsDir {
		nameStyle := lipgloss.NewStyle().Bold(true)
		if config.UseColors {
			nameStyle = nameStyle.Foreground(iconColor)
		}
		label += nameStyle.Render(node.Name)
	} else {
		label += node.Name
	}

	if node.IsDir && config.ShowFileCount {
		label += countLabel(node)
	}
	if node.Truncated {
		label += truncationLabel(node)
	}
	return label
}

// countLabel says how many directories and files a directory holds, or
// nothing when it holds none.
func countLabel(node *TreeNode) string {
	var countStr string
	switch {
	case node.DirCount > 0 && node.FileCount > 0:
		countStr = fmt.Sprintf(" (%d dirs, %d files)", node.DirCount, node.FileCount)
	case node.DirCount > 0:
		countStr = fmt.Sprintf(" (%d dirs)", node.DirCount)
	case node.FileCount > 0:
		countStr = fmt.Sprintf(" (%d files)", node.FileCount)
	default:
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#6c7086")).Italic(true).Render(countStr)
}

// truncationLabel marks a node whose children weren't all shown, with how
// many were left out when that's known.
func truncationLabel(node *TreeNode) string {
	truncStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f9e2af")).Bold(true)
	if remaining := (node.FileCount + node.DirCount) - len(node.Children); remaining > 0 {
		return truncStyle.Render(fmt.Sprintf(" [+%d more]", remaining))
	}
	return truncStyle.Render(" [...]")
}

// RenderDirectoryTree renders a directory as a tree structure
func RenderDirectoryTree(path string, config TreeConfig) ([]string, error) {
	tree, err := buildTree(path, config, 0)
	if err != nil {
		return nil, err
	}

	linesRendered := 0
	var lines []string

	// Don't render the root node itself, just render its children with tree structure
	// This way we see the tree lines starting from the first level
	if len(tree.Children) == 0 {
		return []string{}, nil
	}

	// Render each child of the root with tree structure
	for i, child := range tree.Children {
		if linesRendered >= config.MaxTotalLines {
			break
		}
		isLast := i == len(tree.Children)-1
		childLines := renderTreeWithDepth(child, "", isLast, config, &linesRendered, 0)
		lines = append(lines, childLines...)
	}

	// Add indication if we hit the line limit
	if linesRendered >= config.MaxTotalLines {
		truncStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f9e2af"))
		lines = append(
			lines,
			truncStyle.Render(fmt.Sprintf("... (output truncated at %d lines)", config.MaxTotalLines)),
		)
	}

	return lines, nil
}
