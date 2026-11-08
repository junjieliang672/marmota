// Package icloud provides iCloud status detection and operations for macOS.
package icloud

// Status represents the iCloud sync status of a file.
type Status int

const (
	StatusUnknown       Status = iota // Could not determine status
	StatusNotICloud                   // File is not in an iCloud folder
	StatusSynced                      // File is uploaded and current
	StatusPendingUpload               // Local changes not yet uploaded
	StatusCloudOnly                   // File exists only in cloud (not downloaded)
	StatusDownloading                 // Currently downloading
	StatusUploading                   // Currently uploading
)

// statusUnknownName is how an unknown status is written.
const statusUnknownName = "unknown"

// String returns the string representation of the status.
func (s Status) String() string {
	switch s {
	case StatusUnknown:
		return statusUnknownName
	case StatusNotICloud:
		return "not_icloud"
	case StatusSynced:
		return "synced"
	case StatusPendingUpload:
		return "pending_upload"
	case StatusCloudOnly:
		return "cloud_only"
	case StatusDownloading:
		return "downloading"
	case StatusUploading:
		return "uploading"
	}
	return statusUnknownName
}

// ParseStatus converts a string to a Status.
func ParseStatus(s string) Status {
	switch s {
	case statusUnknownName:
		return StatusUnknown
	case "not_icloud":
		return StatusNotICloud
	case "synced":
		return StatusSynced
	case "pending_upload":
		return StatusPendingUpload
	case "cloud_only":
		return StatusCloudOnly
	case "downloading":
		return StatusDownloading
	case "uploading":
		return StatusUploading
	default:
		return StatusUnknown
	}
}

// Icon returns the icon character for the status.
// Returns empty string if the file is not in iCloud.
func (s Status) Icon() string {
	switch s {
	case StatusPendingUpload, StatusUploading:
		return "\u21BB" // ↻ circular arrow
	case StatusSynced:
		return "\u2713" // ✓ checkmark
	case StatusCloudOnly, StatusDownloading:
		return "\u2601" // ☁ cloud
	case StatusUnknown, StatusNotICloud:
	}
	return ""
}

// BatchResult contains the result of a batch operation.
type BatchResult struct {
	Success int `json:"success"`
	Failed  int `json:"failed,omitempty"`
	Skipped int `json:"skipped,omitempty"`
}

// ICloudFilter represents filter options for iCloud files.
type ICloudFilter int

const (
	FilterAll        ICloudFilter = iota // Show all files
	FilterCloudOnly                      // Show only cloud-only files
	FilterLocalOnly                      // Show only local files (pending upload)
	FilterCloudLocal                     // Show files that are both in cloud and local (synced)
)

// String returns the display name for the filter.
func (f ICloudFilter) String() string {
	switch f {
	case FilterAll:
		return "All files"
	case FilterCloudOnly:
		return "Cloud only"
	case FilterLocalOnly:
		return "Local only"
	case FilterCloudLocal:
		return "Cloud & local"
	default:
		return "All files"
	}
}

// Matches returns true if the given status matches this filter.
func (f ICloudFilter) Matches(s Status) bool {
	switch f {
	case FilterAll:
		return true
	case FilterCloudOnly:
		return s == StatusCloudOnly || s == StatusDownloading
	case FilterLocalOnly:
		return s == StatusPendingUpload || s == StatusUploading
	case FilterCloudLocal:
		return s == StatusSynced
	default:
		return true
	}
}
