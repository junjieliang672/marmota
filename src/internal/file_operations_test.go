package internal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

// TestCopyElement tests the copyElement function which handles copying both files and directories.
func TestCopyElement(t *testing.T) {
	t.Run("Copy single file to destination directory", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "hello.txt")
		utils.SetupFilesWithData(t, []byte("hello world"), srcFile)

		dstFile := filepath.Join(dstDir, "hello.txt")
		err := copyElement(srcFile, dstFile)
		require.NoError(t, err)

		assert.FileExists(t, dstFile)

		// Verify content is identical
		srcContent, err := os.ReadFile(srcFile)
		require.NoError(t, err)
		dstContent, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, srcContent, dstContent)

		// Original should still exist
		assert.FileExists(t, srcFile)
	})

	t.Run("Copy directory recursively", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		subDir := filepath.Join(srcDir, "subdir")
		utils.SetupDirectories(t, subDir)
		utils.SetupFilesWithData(t, []byte("top level"), filepath.Join(srcDir, "top.txt"))
		utils.SetupFilesWithData(t, []byte("nested file"), filepath.Join(subDir, "nested.txt"))

		dstCopy := filepath.Join(dstDir, "copy_of_src")
		err := copyElement(srcDir, dstCopy)
		require.NoError(t, err)

		assert.DirExists(t, dstCopy)
		assert.FileExists(t, filepath.Join(dstCopy, "top.txt"))
		assert.DirExists(t, filepath.Join(dstCopy, "subdir"))
		assert.FileExists(t, filepath.Join(dstCopy, "subdir", "nested.txt"))

		// Verify nested file content
		content, err := os.ReadFile(filepath.Join(dstCopy, "subdir", "nested.txt"))
		require.NoError(t, err)
		assert.Equal(t, []byte("nested file"), content)
	})

	t.Run("Copy file preserves file permissions", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "script.sh")
		err := os.WriteFile(srcFile, []byte("#!/bin/sh\necho hello"), 0o755)
		require.NoError(t, err)

		dstFile := filepath.Join(dstDir, "script.sh")
		err = copyElement(srcFile, dstFile)
		require.NoError(t, err)

		srcInfo, err := os.Stat(srcFile)
		require.NoError(t, err)
		dstInfo, err := os.Stat(dstFile)
		require.NoError(t, err)

		// Mode bits should match (mask to just permission bits)
		assert.Equal(t, srcInfo.Mode().Perm(), dstInfo.Mode().Perm())
	})

	t.Run("Copy nonexistent source returns error", func(t *testing.T) {
		dstDir := t.TempDir()
		nonExistentSrc := filepath.Join(dstDir, "does_not_exist.txt")
		dstFile := filepath.Join(dstDir, "output.txt")

		err := copyElement(nonExistentSrc, dstFile)
		require.Error(t, err)
		assert.NoFileExists(t, dstFile)
	})

	t.Run("Copy empty directory", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		emptyDir := filepath.Join(srcDir, "empty")
		utils.SetupDirectories(t, emptyDir)

		dstCopy := filepath.Join(dstDir, "empty_copy")
		err := copyElement(emptyDir, dstCopy)
		require.NoError(t, err)

		assert.DirExists(t, dstCopy)

		entries, err := os.ReadDir(dstCopy)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("Copy file with content of multiple lines", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		content := []byte("line1\nline2\nline3\nsome special chars: !@#$%^&*()")
		srcFile := filepath.Join(srcDir, "multiline.txt")
		utils.SetupFilesWithData(t, content, srcFile)

		dstFile := filepath.Join(dstDir, "multiline.txt")
		err := copyElement(srcFile, dstFile)
		require.NoError(t, err)

		dstContent, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, content, dstContent)
	})
}

// TestMoveElement tests the moveElement function which handles moving both files and directories.
func TestMoveElement(t *testing.T) {
	t.Run("Move file to different directory", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "move_me.txt")
		content := []byte("content to move")
		utils.SetupFilesWithData(t, content, srcFile)

		dstFile := filepath.Join(dstDir, "move_me.txt")
		err := moveElement(srcFile, dstFile)
		require.NoError(t, err)

		// Destination should exist with correct content
		assert.FileExists(t, dstFile)
		dstContent, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, content, dstContent)

		// Source should no longer exist
		assert.NoFileExists(t, srcFile)
	})

	t.Run("Move directory to different location", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		dirToMove := filepath.Join(srcDir, "mydir")
		fileInDir := filepath.Join(dirToMove, "file.txt")
		utils.SetupDirectories(t, dirToMove)
		utils.SetupFilesWithData(t, []byte("file inside dir"), fileInDir)

		dstCopy := filepath.Join(dstDir, "mydir_moved")
		err := moveElement(dirToMove, dstCopy)
		require.NoError(t, err)

		// Destination should exist
		assert.DirExists(t, dstCopy)
		assert.FileExists(t, filepath.Join(dstCopy, "file.txt"))

		// Source should no longer exist
		assert.NoDirExists(t, dirToMove)
	})

	t.Run("Move nonexistent source returns error", func(t *testing.T) {
		dstDir := t.TempDir()
		nonExistentSrc := filepath.Join(dstDir, "does_not_exist.txt")
		dstFile := filepath.Join(dstDir, "output.txt")

		err := moveElement(nonExistentSrc, dstFile)
		require.Error(t, err)
		assert.NoFileExists(t, dstFile)
	})

	t.Run("Move file within same directory (rename)", func(t *testing.T) {
		srcDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "original.txt")
		content := []byte("rename me")
		utils.SetupFilesWithData(t, content, srcFile)

		dstFile := filepath.Join(srcDir, "renamed.txt")
		err := moveElement(srcFile, dstFile)
		require.NoError(t, err)

		assert.FileExists(t, dstFile)
		assert.NoFileExists(t, srcFile)

		dstContent, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, content, dstContent)
	})
}

// TestIsSamePartition tests the isSamePartition helper function.
func TestIsSamePartition(t *testing.T) {
	t.Run("Two paths in same temp dir are on same partition", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		// Both temp dirs should be on the same filesystem
		same, err := isSamePartition(dir1, dir2)
		require.NoError(t, err)
		assert.True(t, same, "two temp dirs created via t.TempDir() should be on the same partition")
	})

	t.Run("Same path is on same partition as itself", func(t *testing.T) {
		dir := t.TempDir()

		same, err := isSamePartition(dir, dir)
		require.NoError(t, err)
		assert.True(t, same)
	})

	t.Run("Subdirectory is on same partition as parent", func(t *testing.T) {
		parentDir := t.TempDir()
		subDir := filepath.Join(parentDir, "sub")
		utils.SetupDirectories(t, subDir)

		same, err := isSamePartition(parentDir, subDir)
		require.NoError(t, err)
		assert.True(t, same)
	})
}

// TestIsAncestor tests the isAncestor function which checks directory containment.
func TestIsAncestor(t *testing.T) {
	t.Run("Same directory is ancestor of itself", func(t *testing.T) {
		dir := t.TempDir()
		assert.True(t, isAncestor(dir, dir))
	})

	t.Run("Parent is ancestor of child directory", func(t *testing.T) {
		parentDir := t.TempDir()
		childDir := filepath.Join(parentDir, "child")
		utils.SetupDirectories(t, childDir)

		assert.True(t, isAncestor(parentDir, childDir))
	})

	t.Run("Grandparent is ancestor of grandchild", func(t *testing.T) {
		parentDir := t.TempDir()
		childDir := filepath.Join(parentDir, "child")
		grandchildDir := filepath.Join(childDir, "grandchild")
		utils.SetupDirectories(t, childDir, grandchildDir)

		assert.True(t, isAncestor(parentDir, grandchildDir))
	})

	t.Run("Child is not ancestor of parent", func(t *testing.T) {
		parentDir := t.TempDir()
		childDir := filepath.Join(parentDir, "child")
		utils.SetupDirectories(t, childDir)

		assert.False(t, isAncestor(childDir, parentDir))
	})

	t.Run("Sibling directories are not ancestors of each other", func(t *testing.T) {
		parentDir := t.TempDir()
		sibling1 := filepath.Join(parentDir, "sibling1")
		sibling2 := filepath.Join(parentDir, "sibling2")
		utils.SetupDirectories(t, sibling1, sibling2)

		assert.False(t, isAncestor(sibling1, sibling2))
		assert.False(t, isAncestor(sibling2, sibling1))
	})

	t.Run("Path with similar prefix is not considered ancestor", func(t *testing.T) {
		// Ensure 'foo' is not considered ancestor of 'foobar'
		parentDir := t.TempDir()
		fooDir := filepath.Join(parentDir, "foo")
		foobarDir := filepath.Join(parentDir, "foobar")
		utils.SetupDirectories(t, fooDir, foobarDir)

		assert.False(t, isAncestor(fooDir, foobarDir))
	})
}

// TestCopyFile tests the internal copyFile helper function directly.
func TestCopyFile(t *testing.T) {
	t.Run("Copy file content correctly", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		content := []byte("test file content for copyFile")
		srcFile := filepath.Join(srcDir, "src.txt")
		err := os.WriteFile(srcFile, content, 0o644)
		require.NoError(t, err)

		srcInfo, err := os.Stat(srcFile)
		require.NoError(t, err)

		dstFile := filepath.Join(dstDir, "dst.txt")
		err = copyFile(srcFile, dstFile, srcInfo)
		require.NoError(t, err)

		dstContent, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Equal(t, content, dstContent)
	})

	t.Run("Copy empty file", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "empty.txt")
		err := os.WriteFile(srcFile, []byte{}, 0o644)
		require.NoError(t, err)

		srcInfo, err := os.Stat(srcFile)
		require.NoError(t, err)

		dstFile := filepath.Join(dstDir, "empty_copy.txt")
		err = copyFile(srcFile, dstFile, srcInfo)
		require.NoError(t, err)

		assert.FileExists(t, dstFile)
		dstContent, err := os.ReadFile(dstFile)
		require.NoError(t, err)
		assert.Empty(t, dstContent)
	})
}

func TestPasteDir_ContinuesAfterEntryFailure(t *testing.T) {
	if runtime.GOOS == utils.OsWindows {
		t.Skip("symlink-based failure fixture is not reliable on windows")
	}

	srcDir := t.TempDir()
	dstRoot := t.TempDir()
	dstDir := filepath.Join(dstRoot, "copied")

	badLink := filepath.Join(srcDir, "aaa_badlink")
	goodFile := filepath.Join(srcDir, "zzz_good.txt")
	require.NoError(t, os.Symlink(filepath.Join(srcDir, "missing.txt"), badLink))
	require.NoError(t, os.WriteFile(goodFile, []byte("ok"), 0o644))

	processBar := processbar.New()
	processBar.ListenForChannelUpdates()
	t.Cleanup(processBar.SendStopListeningMsgBlocking)

	p, err := processBar.SendAddProcessMsg("paste", 2, true)
	require.NoError(t, err)

	err = pasteDir(srcDir, dstDir, &p, false, &processBar)
	require.Error(t, err)

	require.FileExists(t, filepath.Join(dstDir, "zzz_good.txt"))
	_, statErr := os.Lstat(filepath.Join(dstDir, "aaa_badlink"))
	assert.Error(t, statErr)
}

// TestValidatePasteOperation tests the validatePasteOperation function for correct validation logic.
func TestValidatePasteOperation(t *testing.T) {
	t.Run("Paste into different directory is valid for cut", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "file.txt")
		utils.SetupFiles(t, srcFile)

		err := validatePasteOperation(dstDir, []string{srcFile}, true /* cut */)
		require.NoError(t, err)
	})

	t.Run("Paste into different directory is valid for copy", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "file.txt")
		utils.SetupFiles(t, srcFile)

		err := validatePasteOperation(dstDir, []string{srcFile}, false /* copy */)
		require.NoError(t, err)
	})

	t.Run("Cut and paste into same parent directory is prevented", func(t *testing.T) {
		srcDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "file.txt")
		utils.SetupFiles(t, srcFile)

		// Cutting a file and pasting into the same directory should be prevented
		err := validatePasteOperation(srcDir, []string{srcFile}, true /* cut */)
		require.Error(t, err)
	})

	t.Run("Copy and paste into same parent directory is allowed", func(t *testing.T) {
		srcDir := t.TempDir()

		srcFile := filepath.Join(srcDir, "file.txt")
		utils.SetupFiles(t, srcFile)

		// Copying a file into the same directory is allowed (duplicate will be created)
		err := validatePasteOperation(srcDir, []string{srcFile}, false /* copy */)
		require.NoError(t, err)
	})

	t.Run("Paste directory into itself is prevented", func(t *testing.T) {
		srcDir := t.TempDir()

		err := validatePasteOperation(srcDir, []string{srcDir}, true /* cut */)
		require.Error(t, err)
	})

	t.Run("Paste directory into its subdirectory is prevented", func(t *testing.T) {
		srcDir := t.TempDir()
		subDir := filepath.Join(srcDir, "sub")
		utils.SetupDirectories(t, subDir)

		err := validatePasteOperation(subDir, []string{srcDir}, true /* cut */)
		require.Error(t, err)
	})
}
