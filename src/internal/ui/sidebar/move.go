package sidebar

func pinnedIndexByLocation(pinned []directory, location string) int {
	for i, dir := range pinned {
		if dir.Location == location {
			return i
		}
	}
	return -1
}

func movePinnedDirectoryToIndex(pinned []directory, location string, target int) []directory {
	if len(pinned) == 0 {
		return pinned
	}

	current := pinnedIndexByLocation(pinned, location)
	if current == -1 {
		return pinned
	}

	if target <= 0 {
		target = 1
	}
	if target > len(pinned) {
		target = len(pinned)
	}

	item := pinned[current]
	reordered := append([]directory{}, pinned[:current]...)
	reordered = append(reordered, pinned[current+1:]...)

	insertAt := target - 1
	if insertAt > len(reordered) {
		insertAt = len(reordered)
	}

	reordered = append(reordered, directory{})
	copy(reordered[insertAt+1:], reordered[insertAt:])
	reordered[insertAt] = item
	return reordered
}
