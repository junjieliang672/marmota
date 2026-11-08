package internal

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/utils"

	trash_win "github.com/hymkor/trash-go"
	"github.com/rkoesters/xdg/trash"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/config/icon"
)

// isSamePartition checks if two paths are on the same filesystem partition
func isSamePartition(path1, path2 string) (bool, error) {
	// Get the absolute path to handle relative paths
	absPath1, err := filepath.Abs(path1)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute path of the first path: %w", err)
	}

	absPath2, err := filepath.Abs(path2)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute path of the second path: %w", err)
	}

	if runtime.GOOS == utils.OsWindows {
		// On Windows, we can check if both paths are on the same drive (same letter)
		drive1 := getDriveLetter(absPath1)
		drive2 := getDriveLetter(absPath2)
		return drive1 == drive2, nil
	}

	// For Unix-like systems, we use the same path to check the root partition
	return filepath.VolumeName(absPath1) == filepath.VolumeName(absPath2), nil
}

// getDriveLetter extracts the drive letter from a Windows path
func getDriveLetter(path string) string {
	// Windows paths are usually like "C:\path\to\file"
	// So we need to extract the drive letter (e.g., "C")
	return strings.ToUpper(string(path[0]))
}

// moveElement moves a file or directory efficiently
func moveElement(src, dst string) error {
	// Check if source and destination are on the same partition
	sameDev, err := isSamePartition(src, dst)
	if err != nil {
		return fmt.Errorf("failed to check partitions: %w", err)
	}

	// If on the same partition, attempt to rename (which will use the same inode)
	if sameDev {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		// If rename fails, fall back to copy+delete
	}

	// If on different partitions or rename failed, fall back to copy+delete
	err = copyElement(src, dst)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	err = os.RemoveAll(src)
	if err != nil {
		return fmt.Errorf("failed to remove source after copy: %w", err)
	}

	return nil
}

// copyElement handles copying of both files and directories
func copyElement(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	if srcInfo.IsDir() {
		return copyDir(src, dst, srcInfo)
	}
	return copyFile(src, dst, srcInfo)
}

// copyDir recursively copies a directory
func copyDir(src, dst string, srcInfo os.FileInfo) error {
	err := os.MkdirAll(dst, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	var failedCount int
	var firstErr error
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		entryInfo, err := entry.Info()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to get entry info for %s: %w", srcPath, err)
			}
			failedCount++
			continue
		}

		if entryInfo.IsDir() {
			err = copyDir(srcPath, dstPath, entryInfo)
		} else {
			err = copyFile(srcPath, dstPath, entryInfo)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failedCount++
		}
	}

	if failedCount > 0 {
		return fmt.Errorf("failed to copy %d entries from %s: %w", failedCount, src, firstErr)
	}
	return nil
}

// copyFile copies a single file
func copyFile(src, dst string, srcInfo os.FileInfo) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	return nil
}

func moveToTrash(src string) error {
	var err error
	switch runtime.GOOS {
	case utils.OsDarwin:
		err = moveToTrashDarwin(src)
	case utils.OsWindows:
		err = trash_win.Throw(src)
	default:
		// TODO: We should consider moving away from this package. Its not well written.
		// It uses package globals, It doesn't initializes trash directory, and we have to do it
		// separately outside of the this package. There is not documentation about this
		// It also uses deprecated libraries, and isn't well maintained.
		err = trash.Trash(src)
	}
	if err != nil {
		slog.Error("Error while deleting single item, in function to move file to trash can", "error", err)
	}
	return err
}

// moveToTrashDarwin handles trash operations on macOS.
// It first tries a direct rename to ~/.Trash (fast path), and if that fails
// due to permissions, it falls back to using Finder via osascript which can
// prompt for admin credentials.
func moveToTrashDarwin(src string) error {
	trashPath := filepath.Join(variable.DarwinTrashDirectory, filepath.Base(src))
	trashPath, err := renameIfDuplicate(trashPath)
	if err != nil {
		return err
	}

	// Try direct rename first - this is the fast path for user-owned files
	err = os.Rename(src, trashPath)
	if err == nil {
		return nil
	}

	// Check the underlying error to decide how to proceed
	// We need to unwrap to get the syscall error
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// A cross-device link (EXDEV), source and trash on different
		// partitions, falls through to copy+delete. Other errors go to
		// Finder: it can ask for admin credentials when permission is
		// denied, and may handle the rest better.
		if errno != syscall.EXDEV {
			return moveToTrashViaFinder(src)
		}
	} else if os.IsPermission(err) {
		// Fallback permission check for wrapped errors
		return moveToTrashViaFinder(src)
	}

	// For cross-device errors or unknown cases, try copy+delete
	return moveElement(src, trashPath)
}

// moveToTrashViaFinder uses AppleScript to ask Finder to move a file to trash.
// This allows deletion of files that require elevated permissions, as Finder
// will prompt the user for their password if needed.
func moveToTrashViaFinder(src string) error {
	// Escape backslashes and double quotes for AppleScript string
	escapedPath := strings.ReplaceAll(src, "\\", "\\\\")
	escapedPath = strings.ReplaceAll(escapedPath, "\"", "\\\"")
	script := fmt.Sprintf(`tell application "Finder" to delete POSIX file "%s"`, escapedPath)
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("finder trash failed: %w (output: %s)", err, string(output))
	}
	return nil
}

// pasteDir handles directory copying with progress tracking
func pasteDir(src, dst string, p *processbar.Process, cut bool, processBarModel *processbar.Model) error {
	dst, err := renameIfDuplicate(dst)
	if err != nil {
		return err
	}

	// Check if we can do a fast move within the same partition. For cut
	// operations on same partition, try fast rename first; if rename fails,
	// fall back to manual copy
	sameDev, err := isSamePartition(src, dst)
	if err == nil && sameDev && cut && os.Rename(src, dst) == nil {
		return nil
	}

	paste := &dirPaste{src: src, dst: dst, cut: cut, sameDev: sameDev, p: p, processBarModel: processBarModel}
	if err = filepath.Walk(src, paste.visit); err != nil {
		return err
	}
	if paste.failures.count > 0 {
		return fmt.Errorf("failed to paste %d entries from %s: %w", paste.failures.count, src, paste.failures.first)
	}

	// If this was a cut operation and we had to do a manual copy, remove the source
	if cut && !sameDev {
		if err = os.RemoveAll(src); err != nil {
			return fmt.Errorf("failed to remove source after move: %w", err)
		}
	}

	return nil
}

// failureTally counts the failures of an operation on many entries, and
// keeps the first.
type failureTally struct {
	count int
	first error
}

func (f *failureTally) add(err error) {
	if err == nil {
		return
	}
	if f.first == nil {
		f.first = err
	}
	f.count++
}

// dirPaste is one directory's manual copy: what goes where, and what failed.
type dirPaste struct {
	src, dst        string
	cut, sameDev    bool
	p               *processbar.Process
	processBarModel *processbar.Model
	failures        failureTally
}

// visit pastes one entry of the walk. A failure is counted, and the walk
// goes on.
func (d *dirPaste) visit(path string, info os.FileInfo, err error) error {
	if err == nil {
		err = d.pasteEntry(path, info)
	}
	d.failures.add(err)
	return nil
}

func (d *dirPaste) pasteEntry(path string, info os.FileInfo) error {
	relPath, err := filepath.Rel(d.src, path)
	if err != nil {
		return err
	}
	return actualPasteOperation(info, path, filepath.Join(d.dst, relPath), d.cut, d.sameDev, d.p, d.processBarModel)
}

func actualPasteOperation(info os.FileInfo, path string, newPath string, cut bool, sameDev bool,
	p *processbar.Process, processBarModel *processbar.Model) error {
	var err error
	if info.IsDir() {
		// TODO - this is likely not needed because we did
		// dst, err := renameIfDuplicate(dst) above
		newPath, err = renameIfDuplicate(newPath)
		if err != nil {
			return err
		}
		err = os.MkdirAll(newPath, info.Mode())
		return err
	}

	// File
	p.Name = icon.GetCopyOrCutIcon(cut) + icon.Space + filepath.Base(path)
	if cut && sameDev {
		err = os.Rename(path, newPath)
	} else {
		err = copyFile(path, newPath, info)
	}

	if err != nil {
		p.State = processbar.Failed
		pSendErr := processBarModel.SendUpdateProcessMsg(*p, true)
		if pSendErr != nil {
			slog.Error("Error sending process update", "error", pSendErr)
		}
		return err
	}

	p.Done++
	processBarModel.TrySendingUpdateProcessMsg(*p)
	return nil
}

// isAncestor checks if dst is the same as src or a subdirectory of src.
// It handles symlinks by resolving them and applies case-insensitive comparison on Windows.
func isAncestor(src, dst string) bool {
	// Resolve symlinks for both paths
	srcResolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		// If we can't resolve symlinks, fall back to original path
		srcResolved = src
	}

	dstResolved, err := filepath.EvalSymlinks(dst)
	if err != nil {
		// If we can't resolve symlinks, fall back to original path
		dstResolved = dst
	}

	// Get absolute paths. Abs() also Cleans paths to normalize separators and resolve . and ..
	srcAbs, err := filepath.Abs(srcResolved)
	if err != nil {
		return false
	}

	dstAbs, err := filepath.Abs(dstResolved)
	if err != nil {
		return false
	}

	// On Windows, perform case-insensitive comparison
	if runtime.GOOS == utils.OsWindows {
		srcAbs = strings.ToLower(srcAbs)
		dstAbs = strings.ToLower(dstAbs)
	}

	// Check if dst is the same as src
	if srcAbs == dstAbs {
		return true
	}

	// Check if dst is a subdirectory of src
	// Use filepath.Rel to check the relationship
	rel, err := filepath.Rel(srcAbs, dstAbs)
	if err != nil {
		return false
	}

	// If rel is "." or doesn't start with "..", then dst is inside src
	return rel == "." || !strings.HasPrefix(rel, "..")
}
