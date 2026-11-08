package icloud

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		status   Status
		expected string
	}{
		{StatusUnknown, "unknown"},
		{StatusNotICloud, "not_icloud"},
		{StatusSynced, "synced"},
		{StatusPendingUpload, "pending_upload"},
		{StatusCloudOnly, "cloud_only"},
		{StatusDownloading, "downloading"},
		{StatusUploading, "uploading"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

func TestParseStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected Status
	}{
		{"unknown", StatusUnknown},
		{"not_icloud", StatusNotICloud},
		{"synced", StatusSynced},
		{"pending_upload", StatusPendingUpload},
		{"cloud_only", StatusCloudOnly},
		{"downloading", StatusDownloading},
		{"uploading", StatusUploading},
		{"invalid", StatusUnknown},
		{"", StatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseStatus(tt.input))
		})
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status   Status
		expected string
	}{
		{StatusUnknown, ""},
		{StatusNotICloud, ""},
		{StatusSynced, "\u2713"},        // ✓
		{StatusPendingUpload, "\u21BB"}, // ↻
		{StatusCloudOnly, "\u2601"},     // ☁
		{StatusDownloading, "\u2601"},   // ☁
		{StatusUploading, "\u21BB"},     // ↻
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.Icon())
		})
	}
}

func TestICloudFilterString(t *testing.T) {
	tests := []struct {
		filter   ICloudFilter
		expected string
	}{
		{FilterAll, "All files"},
		{FilterCloudOnly, "Cloud only"},
		{FilterLocalOnly, "Local only"},
		{FilterCloudLocal, "Cloud & local"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.filter.String())
		})
	}
}

func TestICloudFilterMatches(t *testing.T) {
	tests := []struct {
		filter   ICloudFilter
		status   Status
		expected bool
	}{
		// FilterAll matches everything
		{FilterAll, StatusUnknown, true},
		{FilterAll, StatusNotICloud, true},
		{FilterAll, StatusSynced, true},
		{FilterAll, StatusPendingUpload, true},
		{FilterAll, StatusCloudOnly, true},
		{FilterAll, StatusDownloading, true},
		{FilterAll, StatusUploading, true},

		// FilterCloudOnly matches only cloud-only files
		{FilterCloudOnly, StatusCloudOnly, true},
		{FilterCloudOnly, StatusDownloading, true},
		{FilterCloudOnly, StatusSynced, false},
		{FilterCloudOnly, StatusPendingUpload, false},
		{FilterCloudOnly, StatusNotICloud, false},

		// FilterLocalOnly matches only local files pending upload
		{FilterLocalOnly, StatusPendingUpload, true},
		{FilterLocalOnly, StatusUploading, true},
		{FilterLocalOnly, StatusSynced, false},
		{FilterLocalOnly, StatusCloudOnly, false},
		{FilterLocalOnly, StatusNotICloud, false},

		// FilterCloudLocal matches synced files
		{FilterCloudLocal, StatusSynced, true},
		{FilterCloudLocal, StatusPendingUpload, false},
		{FilterCloudLocal, StatusCloudOnly, false},
		{FilterCloudLocal, StatusNotICloud, false},
	}

	for _, tt := range tests {
		name := tt.filter.String() + "_" + tt.status.String()
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.filter.Matches(tt.status))
		})
	}
}
