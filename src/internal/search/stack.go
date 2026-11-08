package search

import (
	"strings"

	"github.com/junjieliang672/marmota/src/config/icon"
)

// Layer represents a single layer in the search hierarchy
type Layer struct {
	Mode          SearchMode   // Type of search (regular/recursive/filter)
	Query         string       // User's search input
	Results       []Searchable // Cached results for this layer
	ParentResults []Searchable // For filters: reference to parent layer's results
	Searching     bool         // Is search in progress?
	DepthLimited  bool         // Was depth limit hit?
	TimedOut      bool         // Did search timeout?
	Error         error        // Any error that occurred during search
}

// Stack manages the hierarchy of search/filter layers
type Stack struct {
	layers  []Layer
	baseDir string // Base directory for this search stack
}

// NewStack creates a new search stack
func NewStack(baseDir string) *Stack {
	return &Stack{
		layers:  make([]Layer, 0),
		baseDir: baseDir,
	}
}

// Push adds a new search layer to the top of the stack
func (s *Stack) Push(layer Layer) {
	s.layers = append(s.layers, layer)
}

// Pop removes and returns the top layer from the stack
// Returns zero value if stack is empty
func (s *Stack) Pop() Layer {
	if len(s.layers) == 0 {
		return Layer{}
	}

	top := s.layers[len(s.layers)-1]
	s.layers = s.layers[:len(s.layers)-1]
	return top
}

// Peek returns the top layer without removing it
// Returns nil if stack is empty
func (s *Stack) Peek() *Layer {
	if len(s.layers) == 0 {
		return nil
	}
	return &s.layers[len(s.layers)-1]
}

// Clear removes all layers from the stack
func (s *Stack) Clear() {
	s.layers = s.layers[:0]
}

// Depth returns the number of layers in the stack
func (s *Stack) Depth() int {
	return len(s.layers)
}

// IsEmpty returns true if the stack has no layers
func (s *Stack) IsEmpty() bool {
	return len(s.layers) == 0
}

// GetBaseDir returns the base directory for this stack
func (s *Stack) GetBaseDir() string {
	return s.baseDir
}

// SetBaseDir updates the base directory for this stack
func (s *Stack) SetBaseDir(baseDir string) {
	s.baseDir = baseDir
}

// RenderHierarchy returns a formatted string showing the search hierarchy
// Format: "🔍 jj  📊 6.json  📊 test"
func (s *Stack) RenderHierarchy() string {
	if len(s.layers) == 0 {
		return ""
	}

	var parts []string
	for _, layer := range s.layers {
		var layerIcon string
		var statusSuffix string

		switch layer.Mode {
		case ModeRecursive:
			layerIcon = icon.Search // 🔍 for recursive search
			switch {
			case layer.Searching:
				statusSuffix = " (searching...)"
			case layer.DepthLimited:
				statusSuffix = " (depth limited)"
			case layer.TimedOut:
				statusSuffix = " (timeout)"
			}
		case ModeFilter, ModeRegular:
			layerIcon = icon.SortDesc // 📊 for filter/regular search
		}

		parts = append(parts, layerIcon+" "+layer.Query+statusSuffix)
	}

	return strings.Join(parts, "  ")
}

// GetCurrentModeIndicator returns the icon for the search mode being entered
// Includes file type filter indicators
// Used when user is typing in the search bar
func (s *Stack) GetCurrentModeIndicator(isRecursiveMode bool, searchTerm string) string {
	// Parse filter from current input
	filterType, _ := parseSearchTerm(searchTerm)

	var filterIcon string
	switch filterType {
	case FilterTypeFiles:
		filterIcon = icon.FileIcon + " " // 📄
	case FilterTypeDirs:
		filterIcon = icon.Directory + " " // 📁
	}

	if isRecursiveMode {
		return filterIcon + icon.Search + " " // e.g., "📄 🔍 "
	}
	if !s.IsEmpty() {
		return filterIcon + icon.SortDesc + " " // e.g., "📄 📊 "
	}
	return filterIcon // e.g., "📄 " for regular search
}

// parseSearchTerm extracts file type filter from search term
// Returns filter type ("files", "dirs", "") and clean query
func parseSearchTerm(term string) (string, string) {
	if strings.HasPrefix(term, "f:") {
		return FilterTypeFiles, strings.TrimPrefix(term, "f:")
	}
	if strings.HasPrefix(term, "d:") {
		return FilterTypeDirs, strings.TrimPrefix(term, "d:")
	}
	return "", term
}
