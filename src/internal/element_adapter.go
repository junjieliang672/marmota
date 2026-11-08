package internal

import (
	"github.com/junjieliang672/marmota/src/internal/search"
)

// ElementSearchable wraps element to implement search.Searchable interface
type ElementSearchable struct {
	elem element
}

// GetName returns the element's name (filename only)
func (e ElementSearchable) GetName() string {
	return e.elem.name
}

// GetLocation returns the element's full path
func (e ElementSearchable) GetLocation() string {
	return e.elem.location
}

// IsDirectory returns true if element is a directory
func (e ElementSearchable) IsDirectory() bool {
	return e.elem.directory
}

// GetMetadata returns the underlying element
func (e ElementSearchable) GetMetadata() interface{} {
	return e.elem
}

// wrapElement converts element to Searchable
func wrapElement(elem element) search.Searchable {
	return ElementSearchable{elem: elem}
}

// wrapElements converts slice of elements to Searchable slice
func wrapElements(elems []element) []search.Searchable {
	searchables := make([]search.Searchable, len(elems))
	for i, elem := range elems {
		searchables[i] = wrapElement(elem)
	}
	return searchables
}

// FilePanelSearchSource implements search.SearchSource for file panel
type FilePanelSearchSource struct {
	elements []element
	location string
}

// GetItems returns searchable items from file panel
func (f FilePanelSearchSource) GetItems() []search.Searchable {
	return wrapElements(f.elements)
}

// GetBaseDirectory returns the file panel's current location
func (f FilePanelSearchSource) GetBaseDirectory() string {
	return f.location
}
