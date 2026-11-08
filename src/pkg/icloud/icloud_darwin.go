//go:build darwin

package icloud

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// helperPath is the path to the icloud-helper binary, found the first time
// it's needed. It should be compiled and placed alongside the superfile
// binary.
var helperPath = sync.OnceValue(findHelper) //nolint:gochecknoglobals // found once, then fixed

// helperCommand runs the icloud-helper binary with args.
func helperCommand(args ...string) *exec.Cmd {
	return exec.Command(helperPath(), args...) //nolint:gosec // our own helper, found by findHelper
}

// findHelper looks for the icloud-helper binary in the usual places.
func findHelper() string {
	// Try to find the helper in common locations
	candidates := []string{
		// Same directory as superfile binary
		filepath.Join(filepath.Dir(getExecutablePath()), "icloud-helper"),
		// Development path
		filepath.Join(getSourcePath(), "src/pkg/icloud/helper/icloud-helper"),
		// User's local bin
		filepath.Join(getHomeDir(), ".local/bin/icloud-helper"),
		// System path
		"/usr/local/bin/icloud-helper",
	}

	for _, path := range candidates {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

// getExecutablePath is the running superfile binary's path, whatever it is
// called (y now).
func getExecutablePath() string {
	ex, err := os.Executable()
	if err != nil {
		return ""
	}
	return ex
}

func getSourcePath() string {
	// Try to find the source directory
	homeDir := getHomeDir()
	candidates := []string{
		filepath.Join(homeDir, "Developer/marmota"),
		filepath.Join(homeDir, "Developer/superfile"), // the folder's name before Marmota
		filepath.Join(homeDir, "go/src/github.com/junjieliang672/marmota"),
	}
	for _, path := range candidates {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func getHomeDir() string {
	home, _ := exec.Command("sh", "-c", "echo $HOME").Output()
	return strings.TrimSpace(string(home))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsAvailable returns true if iCloud operations are available.
func IsAvailable() bool {
	return helperPath() != ""
}

// IsICloudFolder returns true if the given path is within an iCloud folder.
func IsICloudFolder(path string) bool {
	if helperPath() == "" {
		// Fallback: check path contains iCloud folder
		return strings.Contains(path, "/Library/Mobile Documents/")
	}

	out, err := helperCommand("is-icloud-folder", path).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// GetStatus returns the iCloud status for a single file.
func GetStatus(path string) Status {
	if helperPath() == "" {
		// Fallback: check if in iCloud path
		if strings.Contains(path, "/Library/Mobile Documents/") {
			return StatusUnknown
		}
		return StatusNotICloud
	}

	out, err := helperCommand("status", path).Output()
	if err != nil {
		return StatusUnknown
	}
	return ParseStatus(strings.TrimSpace(string(out)))
}

// GetBatchStatus returns the iCloud status for multiple files.
func GetBatchStatus(paths []string) map[string]Status {
	result := make(map[string]Status)

	if helperPath() == "" || len(paths) == 0 {
		return result
	}

	args := append([]string{"batch-status"}, paths...)
	out, err := helperCommand(args...).Output()
	if err != nil {
		// Fallback to individual status checks
		for _, path := range paths {
			result[path] = GetStatus(path)
		}
		return result
	}

	var statusMap map[string]string
	if err := json.Unmarshal(out, &statusMap); err != nil {
		return result
	}

	for path, status := range statusMap {
		result[path] = ParseStatus(status)
	}
	return result
}

// Download initiates download of a cloud-only file.
func Download(path string) error {
	if helperPath() == "" {
		return ErrNotAvailable
	}
	return helperCommand("download", path).Run()
}

// BatchDownload downloads multiple files from iCloud.
func BatchDownload(paths []string) BatchResult {
	if helperPath() == "" || len(paths) == 0 {
		return BatchResult{}
	}

	args := append([]string{"batch-download"}, paths...)
	out, err := helperCommand(args...).Output()
	if err != nil {
		return BatchResult{Failed: len(paths)}
	}

	var result BatchResult
	if err := json.Unmarshal(out, &result); err != nil {
		return BatchResult{Failed: len(paths)}
	}
	return result
}

// Evict removes the local copy of a file, keeping it in iCloud.
func Evict(path string) error {
	if helperPath() == "" {
		return ErrNotAvailable
	}
	return helperCommand("evict", path).Run()
}

// BatchEvict evicts multiple files.
func BatchEvict(paths []string) BatchResult {
	if helperPath() == "" || len(paths) == 0 {
		return BatchResult{}
	}

	args := append([]string{"batch-evict"}, paths...)
	out, err := helperCommand(args...).Output()
	if err != nil {
		return BatchResult{Failed: len(paths)}
	}

	var result BatchResult
	if err := json.Unmarshal(out, &result); err != nil {
		return BatchResult{Failed: len(paths)}
	}
	return result
}

// TriggerUpload attempts to trigger an upload for a file.
// Returns true if the upload was triggered, false if the operation failed.
func TriggerUpload(path string) bool {
	if helperPath() == "" {
		return false
	}
	err := helperCommand("upload", path).Run()
	return err == nil
}

// BatchUpload triggers upload for multiple files.
func BatchUpload(paths []string) BatchResult {
	if helperPath() == "" || len(paths) == 0 {
		return BatchResult{}
	}

	args := append([]string{"batch-upload"}, paths...)
	out, err := helperCommand(args...).Output()
	if err != nil {
		return BatchResult{Skipped: len(paths)}
	}

	var result BatchResult
	if err := json.Unmarshal(out, &result); err != nil {
		return BatchResult{Skipped: len(paths)}
	}
	return result
}
