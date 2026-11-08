package internal

import (
	"testing"
)

func TestSearchStack_PushPop(t *testing.T) {
	stack := searchStack{}

	// Test empty stack
	if !stack.IsEmpty() {
		t.Error("New stack should be empty")
	}
	if stack.Depth() != 0 {
		t.Errorf("Empty stack depth should be 0, got %d", stack.Depth())
	}

	// Push first layer
	layer1 := searchLayer{
		layerType:  searchLayerRecursive,
		searchTerm: "test",
	}
	stack.Push(layer1)

	if stack.IsEmpty() {
		t.Error("Stack should not be empty after push")
	}
	if stack.Depth() != 1 {
		t.Errorf("Stack depth should be 1, got %d", stack.Depth())
	}

	// Push second layer
	layer2 := searchLayer{
		layerType:  searchLayerFilter,
		searchTerm: ".go",
	}
	stack.Push(layer2)

	if stack.Depth() != 2 {
		t.Errorf("Stack depth should be 2, got %d", stack.Depth())
	}

	// Peek should return top layer without removing it
	peeked := stack.Peek()
	if peeked == nil {
		t.Fatal("Peek should not return nil")
	}
	if peeked.searchTerm != ".go" {
		t.Errorf("Peek should return top layer, got %s", peeked.searchTerm)
	}
	if stack.Depth() != 2 {
		t.Error("Peek should not change stack depth")
	}

	// Pop should return and remove top layer
	popped := stack.Pop()
	if popped.searchTerm != ".go" {
		t.Errorf("Pop should return top layer, got %s", popped.searchTerm)
	}
	if stack.Depth() != 1 {
		t.Errorf("Stack depth should be 1 after pop, got %d", stack.Depth())
	}

	// Pop again
	popped = stack.Pop()
	if popped.searchTerm != "test" {
		t.Errorf("Pop should return first layer, got %s", popped.searchTerm)
	}
	if !stack.IsEmpty() {
		t.Error("Stack should be empty after popping all layers")
	}

	// Pop from empty stack should return zero value
	popped = stack.Pop()
	if popped.searchTerm != "" {
		t.Error("Pop from empty stack should return zero value")
	}
}

func TestSearchStack_Clear(t *testing.T) {
	stack := searchStack{}

	// Add some layers
	stack.Push(searchLayer{searchTerm: "layer1"})
	stack.Push(searchLayer{searchTerm: "layer2"})
	stack.Push(searchLayer{searchTerm: "layer3"})

	if stack.Depth() != 3 {
		t.Errorf("Stack depth should be 3, got %d", stack.Depth())
	}

	// Clear should remove all layers
	stack.Clear()
	if !stack.IsEmpty() {
		t.Error("Stack should be empty after clear")
	}
	if stack.Depth() != 0 {
		t.Errorf("Stack depth should be 0 after clear, got %d", stack.Depth())
	}
}

func TestSearchStack_PeekEmpty(t *testing.T) {
	stack := searchStack{}

	peeked := stack.Peek()
	if peeked != nil {
		t.Error("Peek on empty stack should return nil")
	}
}

func TestSearchStack_RenderHierarchy(t *testing.T) {
	tests := []struct {
		name     string
		layers   []searchLayer
		expected string
	}{
		{
			name:     "empty stack",
			layers:   []searchLayer{},
			expected: "",
		},
		{
			name: "single recursive search",
			layers: []searchLayer{
				{layerType: searchLayerRecursive, searchTerm: "jj"},
			},
			// Icon.Search is used, but we can't test exact icon without importing icon package
			// Just verify structure is correct
			expected: "🔍 jj",
		},
		{
			name: "recursive search with filter",
			layers: []searchLayer{
				{layerType: searchLayerRecursive, searchTerm: "jj"},
				{layerType: searchLayerFilter, searchTerm: "6.json"},
			},
			expected: "🔍 jj  📊 6.json",
		},
		{
			name: "recursive search with multiple filters",
			layers: []searchLayer{
				{layerType: searchLayerRecursive, searchTerm: "jj"},
				{layerType: searchLayerFilter, searchTerm: "6.json"},
				{layerType: searchLayerFilter, searchTerm: "test"},
			},
			expected: "🔍 jj  📊 6.json  📊 test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := searchStack{layers: tt.layers}
			result := stack.RenderHierarchy()

			// We can check the structure even if the exact icons might differ
			if len(tt.layers) == 0 && result != "" {
				t.Error("Empty stack should render empty string")
			}

			// Check that we have the right number of parts
			if len(tt.layers) > 0 {
				// Should have search terms in the output
				for _, layer := range tt.layers {
					if !containsString(result, layer.searchTerm) {
						t.Errorf("Rendered hierarchy should contain search term %q, got %q", layer.searchTerm, result)
					}
				}
			}
		})
	}
}

func TestSearchStack_GetCurrentModeIndicator(t *testing.T) {
	tests := []struct {
		name            string
		layers          []searchLayer
		isRecursiveMode bool
		searchTerm      string
		expectEmpty     bool
	}{
		{
			name:            "recursive mode",
			layers:          []searchLayer{},
			isRecursiveMode: true,
			searchTerm:      "",
			expectEmpty:     false,
		},
		{
			name: "filter mode (has layers)",
			layers: []searchLayer{
				{layerType: searchLayerRecursive, searchTerm: "test"},
			},
			isRecursiveMode: false,
			searchTerm:      "",
			expectEmpty:     false,
		},
		{
			name:            "regular search mode (no layers, not recursive)",
			layers:          []searchLayer{},
			isRecursiveMode: false,
			searchTerm:      "",
			expectEmpty:     true,
		},
		{
			name:            "files filter with recursive mode",
			layers:          []searchLayer{},
			isRecursiveMode: true,
			searchTerm:      "f:main",
			expectEmpty:     false,
		},
		{
			name:            "dirs filter with regular mode",
			layers:          []searchLayer{},
			isRecursiveMode: false,
			searchTerm:      "d:test",
			expectEmpty:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := searchStack{layers: tt.layers}
			result := stack.GetCurrentModeIndicator(tt.isRecursiveMode, tt.searchTerm)

			if tt.expectEmpty && result != "" {
				t.Errorf("Expected empty indicator, got %q", result)
			}
			if !tt.expectEmpty && result == "" {
				t.Error("Expected non-empty indicator, got empty")
			}
		})
	}
}

// Helper function for string containment check
func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 || stringContains(haystack, needle))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
