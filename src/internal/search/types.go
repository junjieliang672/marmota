package search

import (
	"context"
	"time"
)

// SearchMode defines the type of search operation
type SearchMode uint8

const (
	ModeRegular   SearchMode = iota // Simple filter in current scope
	ModeRecursive                   // Deep recursive directory search
	ModeFilter                      // Filter on existing results
)

// The file type filters of a search: "f:query" finds only files, "d:query"
// only folders.
const (
	FilterTypeFiles = "files"
	FilterTypeDirs  = "dirs"
)

// Searchable defines what can be searched
type Searchable interface {
	GetName() string          // Primary search field
	GetLocation() string      // Full path for navigation
	IsDirectory() bool        // For type filtering
	GetMetadata() interface{} // Panel-specific data
}

// SearchResult wraps results with score/rank
type SearchResult struct {
	Item  Searchable
	Score int
	Index int
}

// SearchSource provides items to search
type SearchSource interface {
	GetItems() []Searchable
	GetBaseDirectory() string // For recursive search
}

// HiddenAwareSource is an optional interface that a SearchSource may implement
// to tell the Coordinator whether hidden files should be included in a
// recursive walk. Sources that don't implement this interface default to
// including hidden files (preserving pre-fix behaviour for non-file-panel
// callers like the help-menu search).
type HiddenAwareSource interface {
	ShowHidden() bool
}

// SearchExecutor performs the actual search
type SearchExecutor interface {
	Execute(ctx context.Context, query string, source []Searchable) ([]SearchResult, error)
	// ExecuteWithCap is a bounded variant of Execute — the returned slice
	// is truncated to the top `maxResults` scored items. maxResults <= 0
	// disables the cap (behaviour identical to Execute). See spec-006
	// contracts.new_service_interfaces.FzfExecutor.
	ExecuteWithCap(ctx context.Context, query string, source []Searchable, maxResults int) ([]SearchResult, error)
}

// SearchConfig defines search behavior
type SearchConfig struct {
	Mode            SearchMode
	EnableRecursive bool
	EnableFiltering bool
	EnableStacking  bool
	TwoStageESC     bool
	MaxDepth        int
	Timeout         time.Duration

	// File type filter support
	AllowFileFilter bool
	AllowDirFilter  bool

	// Custom filter function (for help menu section filtering)
	CustomFilter func(Searchable, string) bool
}

const (
	// DefaultMaxDepth is how deep a recursive search goes below its folder.
	DefaultMaxDepth = 5
	// DefaultTimeout bounds a recursive search's walk.
	DefaultTimeout = 10 * time.Second
)

// DefaultConfig returns sensible defaults
func DefaultConfig() SearchConfig {
	return SearchConfig{
		Mode:            ModeRegular,
		EnableRecursive: false,
		EnableStacking:  false,
		TwoStageESC:     false,
		MaxDepth:        DefaultMaxDepth,
		Timeout:         DefaultTimeout,
		AllowFileFilter: false,
		AllowDirFilter:  false,
	}
}
