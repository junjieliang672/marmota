package preview

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTreeConfig(t *testing.T) {
	config := DefaultTreeConfig()

	assert.Equal(t, 2, config.MaxDepth, "Default max depth should be 2")
	assert.Equal(t, 20, config.MaxFiles, "Default max files should be 20")
	assert.False(t, config.ShowHidden, "Default should not show hidden files")
	assert.Equal(t, 100, config.MaxTotalLines, "Default max total lines should be 100")
	assert.False(t, config.CompactMode, "Default should not use compact mode")
	assert.True(t, config.ShowFileCount, "Default should show file count")
}

func TestBuildTree_SimpleDirectory(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create some files and directories
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "file3.txt"), []byte("content"), 0644))

	config := DefaultTreeConfig()
	config.MaxDepth = 2

	tree, err := buildTree(tmpDir, config, 0)
	require.NoError(t, err)
	require.NotNil(t, tree)

	assert.True(t, tree.IsDir, "Root should be a directory")
	assert.Equal(t, 2, tree.FileCount, "Should have 2 files in root")
	assert.Equal(t, 1, tree.DirCount, "Should have 1 subdirectory")
	assert.Len(t, tree.Children, 3, "Should have 3 children (2 files + 1 dir)")
}

func TestBuildTree_MaxDepthLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested directories
	subdir1 := filepath.Join(tmpDir, "level1")
	subdir2 := filepath.Join(subdir1, "level2")
	subdir3 := filepath.Join(subdir2, "level3")

	require.NoError(t, os.MkdirAll(subdir3, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(subdir3, "deep.txt"), []byte("content"), 0644))

	config := DefaultTreeConfig()
	config.MaxDepth = 2

	tree, err := buildTree(tmpDir, config, 0)
	require.NoError(t, err)

	// At max depth 2, we should traverse: root (0) -> level1 (1) -> level2 (2)
	// level3 should not be traversed
	assert.NotNil(t, tree)
	require.Len(t, tree.Children, 1, "Should have level1")

	level1 := tree.Children[0]
	require.Len(t, level1.Children, 1, "level1 should have level2")

	level2 := level1.Children[0]
	// level2 should not have children because we hit max depth
	assert.Empty(t, level2.Children, "level2 should not have children (max depth reached)")
}

func TestBuildTree_HiddenFiles(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "visible.txt"), []byte("content"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hidden.txt"), []byte("content"), 0644))

	// Test with ShowHidden = false
	config := DefaultTreeConfig()
	config.ShowHidden = false

	tree, err := buildTree(tmpDir, config, 0)
	require.NoError(t, err)
	assert.Len(t, tree.Children, 1, "Should only have 1 visible file")
	assert.Equal(t, "visible.txt", tree.Children[0].Name)

	// Test with ShowHidden = true
	config.ShowHidden = true
	tree, err = buildTree(tmpDir, config, 0)
	require.NoError(t, err)
	assert.Len(t, tree.Children, 2, "Should have both visible and hidden files")
}

func TestBuildTree_MaxFilesLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create more files than the limit
	for i := range 10 {
		filename := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".txt")
		require.NoError(t, os.WriteFile(filename, []byte("content"), 0644))
	}

	config := DefaultTreeConfig()
	config.MaxFiles = 5

	tree, err := buildTree(tmpDir, config, 0)
	require.NoError(t, err)

	assert.Len(t, tree.Children, 5, "Should only have MaxFiles children")
	assert.True(t, tree.Truncated, "Should be marked as truncated")
}

func TestBuildTree_Symlink(t *testing.T) {
	tmpDir := t.TempDir()

	targetFile := filepath.Join(tmpDir, "target.txt")
	require.NoError(t, os.WriteFile(targetFile, []byte("content"), 0644))

	symlinkPath := filepath.Join(tmpDir, "link.txt")
	require.NoError(t, os.Symlink(targetFile, symlinkPath))

	config := DefaultTreeConfig()
	tree, err := buildTree(tmpDir, config, 0)
	require.NoError(t, err)

	// Find the symlink child
	var linkNode *TreeNode
	for _, child := range tree.Children {
		if child.Name == "link.txt" {
			linkNode = child
			break
		}
	}

	require.NotNil(t, linkNode, "Should find the symlink node")
	assert.True(t, linkNode.IsLink, "Should be marked as a symlink")
}

func TestRenderDirectoryTree_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	config := DefaultTreeConfig()
	lines, err := RenderDirectoryTree(tmpDir, config)

	require.NoError(t, err)
	assert.Empty(t, lines, "Empty directory should return empty lines (no children to display)")
}

func TestRenderDirectoryTree_WithContent(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "file2.txt"), []byte("content"), 0644))

	config := DefaultTreeConfig()
	lines, err := RenderDirectoryTree(tmpDir, config)

	require.NoError(t, err)
	assert.Greater(t, len(lines), 1, "Should have multiple lines")
}

func TestRenderDirectoryTree_MaxTotalLines(t *testing.T) {
	tmpDir := t.TempDir()

	// Create many nested directories
	currentDir := tmpDir
	for range 20 {
		currentDir = filepath.Join(currentDir, "subdir")
		require.NoError(t, os.MkdirAll(currentDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(currentDir, "file.txt"), []byte("content"), 0644))
	}

	config := DefaultTreeConfig()
	config.MaxDepth = -1 // Unlimited depth
	config.MaxTotalLines = 10

	lines, err := RenderDirectoryTree(tmpDir, config)

	require.NoError(t, err)
	// Should respect MaxTotalLines limit (may be <= because of truncation message)
	assert.LessOrEqual(
		t,
		len(lines),
		config.MaxTotalLines+1,
		"Should not exceed MaxTotalLines (+1 for truncation message)",
	)
}

func TestRenderDirectoryTree_CompactMode(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))

	// Test compact mode
	config := DefaultTreeConfig()
	config.CompactMode = true

	lines, err := RenderDirectoryTree(tmpDir, config)
	require.NoError(t, err)
	assert.NotEmpty(t, lines, "Should render in compact mode")

	// Test normal mode
	config.CompactMode = false
	linesNormal, err := RenderDirectoryTree(tmpDir, config)
	require.NoError(t, err)
	assert.NotEmpty(t, linesNormal, "Should render in normal mode")
}

func TestRenderDirectoryTree_InvalidPath(t *testing.T) {
	config := DefaultTreeConfig()
	_, err := RenderDirectoryTree("/nonexistent/path/that/does/not/exist", config)

	assert.Error(t, err, "Should return error for nonexistent path")
}

func TestRenderTree_Sorting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files and directories with distinct modification times
	baseTime := time.Now()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "zebra.txt"), []byte("content"), 0644))
	require.NoError(t, os.Chtimes(filepath.Join(tmpDir, "zebra.txt"), baseTime, baseTime.Add(-3*time.Hour)))

	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "alpha"), 0755))
	require.NoError(t, os.Chtimes(filepath.Join(tmpDir, "alpha"), baseTime, baseTime.Add(-1*time.Hour)))

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "beta.txt"), []byte("content"), 0644))
	require.NoError(t, os.Chtimes(filepath.Join(tmpDir, "beta.txt"), baseTime, baseTime.Add(-2*time.Hour)))

	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "charlie"), 0755))
	require.NoError(t, os.Chtimes(filepath.Join(tmpDir, "charlie"), baseTime, baseTime))

	config := DefaultTreeConfig()
	tree, err := buildTree(tmpDir, config, 0)
	require.NoError(t, err)

	// Should be sorted by modification time descending (most recent first)
	require.Len(t, tree.Children, 4)
	assert.Equal(t, "charlie", tree.Children[0].Name)
	assert.Equal(t, "alpha", tree.Children[1].Name)
	assert.Equal(t, "beta.txt", tree.Children[2].Name)
	assert.Equal(t, "zebra.txt", tree.Children[3].Name)
}
