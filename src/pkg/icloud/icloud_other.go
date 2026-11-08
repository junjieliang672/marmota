//go:build !darwin

package icloud

// IsAvailable returns true if iCloud operations are available.
// On non-macOS systems, iCloud is not available.
func IsAvailable() bool {
	return false
}

// IsICloudFolder returns true if the given path is within an iCloud folder.
// On non-macOS systems, this always returns false.
func IsICloudFolder(path string) bool {
	return false
}

// GetStatus returns the iCloud status for a single file.
// On non-macOS systems, this always returns StatusNotICloud.
func GetStatus(path string) Status {
	return StatusNotICloud
}

// GetBatchStatus returns the iCloud status for multiple files.
// On non-macOS systems, this returns an empty map.
func GetBatchStatus(paths []string) map[string]Status {
	return make(map[string]Status)
}

// Download initiates download of a cloud-only file.
// On non-macOS systems, this returns ErrNotAvailable.
func Download(path string) error {
	return ErrNotAvailable
}

// BatchDownload downloads multiple files from iCloud.
// On non-macOS systems, this returns an empty result.
func BatchDownload(paths []string) BatchResult {
	return BatchResult{}
}

// Evict removes the local copy of a file, keeping it in iCloud.
// On non-macOS systems, this returns ErrNotAvailable.
func Evict(path string) error {
	return ErrNotAvailable
}

// BatchEvict evicts multiple files.
// On non-macOS systems, this returns an empty result.
func BatchEvict(paths []string) BatchResult {
	return BatchResult{}
}

// TriggerUpload attempts to trigger an upload for a file.
// On non-macOS systems, this always returns false.
func TriggerUpload(path string) bool {
	return false
}

// BatchUpload triggers upload for multiple files.
// On non-macOS systems, this returns an empty result.
func BatchUpload(paths []string) BatchResult {
	return BatchResult{}
}
