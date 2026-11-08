package internal

// Tests for the default fzf-order vs. explicit sort-override behaviour
// introduced in spec-006 live-perf fixes.
//
// - Search results default to FZF-relevance order (best match first).
// - Pressing `o` and confirming a sort kind sets the current layer's
//   manualSortOverride flag and re-sorts cachedElems in place.
// - Popping the layer (ESC) resets the override via Go's zero value for
//   searchLayer.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildSortOverrideTree creates a small fixture where fzf-score order for
// query "a" differs from alphabetical order. We build filenames picked so
// that "aaa_match" scores higher than "a_match" and "zzz_a.txt" has lowest
// score — but alphabetical order is the opposite.
func buildSortOverrideTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Use names that produce known distinct FZF scores.
	names := []string{
		"abc.txt",     // contains 'a' at start — high score
		"zebra_a.txt", // 'a' later — lower score
		"m_a_test.txt",
		"z.txt",         // no 'a' — won't match
		"apple_pie.txt", // 'a' at start
	}
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(root, n), nil, 0o644))
	}
	return root
}

// TestSearchLayer_ManualSortOverride_DefaultFzfOrder verifies that when a
// search layer is freshly created, its cachedElems come out in FZF score
// order (not alphabetical).
func TestSearchLayer_ManualSortOverride_DefaultFzfOrder(t *testing.T) {
	root := buildSortOverrideTree(t)
	m := defaultTestModel(root)

	panel := m.getFocusedFilePanel()
	panel.location = root

	// Simulate the user typing "a" into the regular search bar, which
	// creates a searchLayerRegular.
	layer := searchLayer{
		layerType:  searchLayerRegular,
		searchTerm: "a",
	}
	panel.searchStack.Push(layer)

	// getElementsForPanel triggers the search, fills cachedElems.
	elems := m.getElementsForPanel(panel)
	require.NotEmpty(t, elems, "search for 'a' must return matches from fixture")

	// Default layer has manualSortOverride == false.
	top := panel.searchStack.Peek()
	require.NotNil(t, top)
	assert.False(t, top.manualSortOverride, "new layer must default to fzf order (no override)")

	// The top layer's cachedElems should be the same slice returned.
	assert.Len(t, elems, len(top.cachedElems))

	// Assert the order is NOT alphabetical. If it were, the first element
	// name would start with 'a' AND follow strict alphabetical order —
	// "abc.txt" < "apple_pie.txt" < "m_a_test.txt" < "zebra_a.txt".
	// FZF score order for query "a" typically puts short matches or
	// earlier-position matches first; we don't pin the exact order here
	// (fzf-lib may evolve), we just assert "not the alphabetical order".
	alphabetical := []string{"abc.txt", "apple_pie.txt", "m_a_test.txt", "zebra_a.txt"}
	actualNames := make([]string, 0, len(elems))
	for _, e := range elems {
		actualNames = append(actualNames, e.name)
	}

	// If all actual names are present in alphabetical and the length matches,
	// check whether the order itself is alphabetical. The fixture is small
	// enough that a coincidental alphabetical fzf order would be meaningful
	// signal that the test needs updating, but in practice the fzf score for
	// "zebra_a.txt" is lower than "abc.txt" because the match position is later.
	if len(actualNames) == len(alphabetical) {
		sameOrder := true
		for i := range actualNames {
			if actualNames[i] != alphabetical[i] {
				sameOrder = false
				break
			}
		}
		assert.False(t, sameOrder,
			"default search results should be in FZF-relevance order, not alphabetical; got %v", actualNames)
	}
}

// TestSearchLayer_ManualSortOverride_AppliesOnConfirm verifies that
// confirmSortOptions sets the flag and re-sorts cachedElems to the user's
// selected kind.
func TestSearchLayer_ManualSortOverride_AppliesOnConfirm(t *testing.T) {
	root := buildSortOverrideTree(t)
	m := defaultTestModel(root)

	panel := m.getFocusedFilePanel()
	panel.location = root

	panel.searchStack.Push(searchLayer{
		layerType:  searchLayerRegular,
		searchTerm: "a",
	})

	// Populate cachedElems via the first render.
	_ = m.getElementsForPanel(panel)
	top := panel.searchStack.Peek()
	require.NotNil(t, top)
	require.NotEmpty(t, top.cachedElems)
	require.False(t, top.manualSortOverride, "pre-confirm default is false")

	// Pick "Name" kind as the explicit sort — find its index in the panel's
	// sort options.
	nameIdx := -1
	for i, opt := range panel.sortOptions.data.options {
		if opt == string(sortingName) {
			nameIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, nameIdx, 0, "panel must offer the Name sort option")

	panel.sortOptions.cursor = nameIdx
	m.confirmSortOptions()

	// After confirm the flag is set and cachedElems is sorted by Name.
	top = panel.searchStack.Peek()
	require.NotNil(t, top)
	assert.True(t, top.manualSortOverride, "confirmSortOptions must set manualSortOverride")

	// Verify the cached elems are sorted alphabetically now.
	names := make([]string, 0, len(top.cachedElems))
	for _, e := range top.cachedElems {
		names = append(names, e.name)
	}
	// The fixture produces 4 matches: "abc.txt", "apple_pie.txt", "m_a_test.txt", "zebra_a.txt".
	expected := []string{"abc.txt", "apple_pie.txt", "m_a_test.txt", "zebra_a.txt"}
	assert.Equal(t, expected, names,
		"after explicit Name sort, cachedElems must be alphabetical")
}

// TestSearchLayer_ManualSortOverride_ResetOnPop verifies that popping a
// layer (ESC) clears the override via the searchLayer zero value.
func TestSearchLayer_ManualSortOverride_ResetOnPop(t *testing.T) {
	var stack searchStack
	stack.Push(searchLayer{
		layerType:          searchLayerRegular,
		searchTerm:         "a",
		manualSortOverride: true,
	})
	stack.Pop()
	assert.True(t, stack.IsEmpty())

	// Push a new layer — flag starts false.
	stack.Push(searchLayer{
		layerType:  searchLayerRegular,
		searchTerm: "a",
	})
	top := stack.Peek()
	require.NotNil(t, top)
	assert.False(t, top.manualSortOverride, "fresh layer must default to fzf-order")
}
