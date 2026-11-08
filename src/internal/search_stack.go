package internal

import (
	"strings"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/search"
)

// searchLayerType represents the type of search layer
type searchLayerType uint8

const (
	searchLayerRegular   searchLayerType = iota // '/' search in current directory
	searchLayerRecursive                        // 'f' recursive search
	searchLayerFilter                           // '/' filter on existing results
	searchLayerTouched                          // W: the files a session changed
)

// searchLayer represents a single layer in the search hierarchy
type searchLayer struct {
	layerType    searchLayerType // Type of search (regular/recursive/filter)
	searchTerm   string          // User's search input
	cachedElems  []element       // Cached results for this layer
	parentElems  []element       // For filters: reference to parent layer's results
	searching    bool            // Is search in progress?
	depthLimited bool            // Was depth limit hit?
	timedOut     bool            // Did search timeout?

	// manualSortOverride is true once the user confirms a sort option via
	// the `o` menu while THIS layer is on top of the stack. Default search
	// result order is FZF relevance (best match first); this flag flips the
	// layer into user-selected sort order for the rest of its lifetime.
	// Popping the layer (ESC) resets the flag via Go's zero value.
	manualSortOverride bool

	// session is the session whose files a searchLayerTouched layer lists.
	session touchedSessionRef
}

// searchStack manages the hierarchy of search/filter layers
type searchStack struct {
	layers []searchLayer
}

// Push adds a new search layer to the top of the stack
func (s *searchStack) Push(layer searchLayer) {
	s.layers = append(s.layers, layer)
}

// Pop removes and returns the top layer from the stack
// Returns zero value if stack is empty
func (s *searchStack) Pop() searchLayer {
	if len(s.layers) == 0 {
		return searchLayer{}
	}

	top := s.layers[len(s.layers)-1]
	s.layers = s.layers[:len(s.layers)-1]
	return top
}

// Peek returns the top layer without removing it
// Returns nil if stack is empty
func (s *searchStack) Peek() *searchLayer {
	if len(s.layers) == 0 {
		return nil
	}
	return &s.layers[len(s.layers)-1]
}

// Clear removes all layers from the stack
func (s *searchStack) Clear() {
	s.layers = s.layers[:0]
}

// Depth returns the number of layers in the stack
func (s *searchStack) Depth() int {
	return len(s.layers)
}

// IsEmpty returns true if the stack has no layers
func (s *searchStack) IsEmpty() bool {
	return len(s.layers) == 0
}

// InvalidateCache clears cached elements from all layers, forcing re-computation
// with current sort options. Called when sort options change.
func (s *searchStack) InvalidateCache() {
	for i := range s.layers {
		s.layers[i].cachedElems = nil
	}
}

// RenderHierarchy returns a formatted string showing the search hierarchy
// Format: "🔍 jj  📊 6.json  📊 test"
func (s *searchStack) RenderHierarchy() string {
	if len(s.layers) == 0 {
		return ""
	}

	var parts []string
	for _, layer := range s.layers {
		var layerIcon string
		var statusSuffix string

		switch layer.layerType {
		case searchLayerRecursive:
			layerIcon = icon.Search // 🔍 for recursive search
			switch {
			case layer.searching:
				statusSuffix = " (searching...)"
			case layer.depthLimited:
				statusSuffix = " (depth limited)"
			case layer.timedOut:
				statusSuffix = " (timeout)"
			}
		case searchLayerFilter, searchLayerRegular:
			layerIcon = icon.SortDesc // 📊 for filter/regular search
		case searchLayerTouched:
			parts = append(parts, touchedLayerTitle(layer.searchTerm))
			continue
		}

		parts = append(parts, layerIcon+" "+layer.searchTerm+statusSuffix)
	}

	return strings.Join(parts, "  ")
}

// GetCurrentModeIndicator returns the icon for the search mode being entered
// Includes file type filter indicators
// Used when user is typing in the search bar
func (s *searchStack) GetCurrentModeIndicator(isRecursiveMode bool, searchTerm string) string {
	// Parse filter from current input
	filterType, _ := parseSearchTerm(searchTerm)

	var filterIcon string
	switch filterType {
	case search.FilterTypeFiles:
		filterIcon = icon.FileIcon + " " // 📄
	case search.FilterTypeDirs:
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
