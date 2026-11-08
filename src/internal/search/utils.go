package search

// Helper utilities for search package

// IsEmpty checks if results are empty
func IsEmpty(results []Searchable) bool {
	return len(results) == 0
}

// GetResultCount returns the number of results
func GetResultCount(results []Searchable) int {
	return len(results)
}

// ExtractNames extracts names from searchable items
func ExtractNames(items []Searchable) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.GetName()
	}
	return names
}

// ExtractLocations extracts locations from searchable items
func ExtractLocations(items []Searchable) []string {
	locations := make([]string, len(items))
	for i, item := range items {
		locations[i] = item.GetLocation()
	}
	return locations
}

// FilterDirectories filters items to only include directories
func FilterDirectories(items []Searchable) []Searchable {
	var dirs []Searchable
	for _, item := range items {
		if item.IsDirectory() {
			dirs = append(dirs, item)
		}
	}
	return dirs
}

// FilterFiles filters items to only include files
func FilterFiles(items []Searchable) []Searchable {
	var files []Searchable
	for _, item := range items {
		if !item.IsDirectory() {
			files = append(files, item)
		}
	}
	return files
}
