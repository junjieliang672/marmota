package sidebar

import (
	"fmt"
	"strconv"
	"unicode"
)

func (s *Model) IsMovingPinned() bool {
	return s.moveState.active
}

func (s *Model) BeginPinnedMove(mainPanelHeight int) bool {
	if s.moveState.active || s.renaming || s.searchBar.Focused() || s.IsSearchActive() {
		return false
	}

	pinnedStart, pinnedEnd := s.pinnedIndexRange()
	if s.cursor < pinnedStart || s.cursor > pinnedEnd || s.cursor >= len(s.directories) {
		return false
	}

	current := s.directories[s.cursor]
	if current.IsDivider() || current.Location == "" || s.pinnedMgr == nil {
		return false
	}

	pinned := append([]directory(nil), s.pinnedMgr.Load()...)
	if len(pinned) == 0 {
		return false
	}

	if pinnedIndexByLocation(pinned, current.Location) == -1 {
		return false
	}

	s.moveState = pinnedMoveState{
		active:   true,
		location: current.Location,
		original: append([]directory(nil), pinned...),
		preview:  pinned,
	}
	s.applyPinnedPreview(mainPanelHeight)
	return true
}

func (s *Model) CancelPinnedMove(mainPanelHeight int) {
	if !s.moveState.active {
		return
	}

	location := s.moveState.location
	original := append([]directory(nil), s.moveState.original...)
	s.moveState = pinnedMoveState{}
	s.replacePinnedDirectories(original)
	s.focusDirectory(location, mainPanelHeight)
}

func (s *Model) MovePinnedUp(mainPanelHeight int) {
	if !s.moveState.active {
		return
	}

	current := pinnedIndexByLocation(s.moveState.preview, s.moveState.location)
	if current <= 0 {
		return
	}

	s.moveState.preview[current], s.moveState.preview[current-1] = s.moveState.preview[current-1], s.moveState.preview[current]
	s.moveState.input = ""
	s.applyPinnedPreview(mainPanelHeight)
}

func (s *Model) MovePinnedDown(mainPanelHeight int) {
	if !s.moveState.active {
		return
	}

	current := pinnedIndexByLocation(s.moveState.preview, s.moveState.location)
	if current == -1 || current >= len(s.moveState.preview)-1 {
		return
	}

	s.moveState.preview[current], s.moveState.preview[current+1] = s.moveState.preview[current+1], s.moveState.preview[current]
	s.moveState.input = ""
	s.applyPinnedPreview(mainPanelHeight)
}

func (s *Model) AppendPinnedMoveDigit(r rune) {
	if !s.moveState.active || !unicode.IsDigit(r) {
		return
	}
	s.moveState.input += string(r)
}

func (s *Model) DeletePinnedMoveDigit() {
	if !s.moveState.active || s.moveState.input == "" {
		return
	}
	s.moveState.input = s.moveState.input[:len(s.moveState.input)-1]
}

func (s *Model) ConfirmPinnedMove(mainPanelHeight int) error {
	if !s.moveState.active || s.pinnedMgr == nil {
		return nil
	}

	location := s.moveState.location
	if s.moveState.input != "" {
		target, err := strconv.Atoi(s.moveState.input)
		if err != nil {
			return fmt.Errorf("parse move target: %w", err)
		}
		s.moveState.preview = movePinnedDirectoryToIndex(s.moveState.preview, location, target)
	}

	if err := s.pinnedMgr.Save(s.moveState.preview); err != nil {
		return err
	}

	s.moveState = pinnedMoveState{}
	s.UpdateDirectories()
	s.focusDirectory(location, mainPanelHeight)
	return nil
}

func (s *Model) IsMovingPinnedLocation(location string) bool {
	return s.moveState.active && s.moveState.location != "" && s.moveState.location == location
}

func (s *Model) MovingPinnedLabelSuffix(location string) string {
	if !s.IsMovingPinnedLocation(location) {
		return ""
	}

	if s.moveState.input != "" {
		return " [move -> " + s.moveState.input + "]"
	}

	return " [move]"
}

func (s *Model) applyPinnedPreview(mainPanelHeight int) {
	s.replacePinnedDirectories(s.moveState.preview)
	s.focusDirectory(s.moveState.location, mainPanelHeight)
}

func (s *Model) reapplyPinnedPreview() {
	s.replacePinnedDirectories(s.moveState.preview)
	for i, dir := range s.directories {
		if dir.Location == s.moveState.location {
			s.cursor = i
			return
		}
	}
}

func (s *Model) replacePinnedDirectories(pinned []directory) {
	recent := []directory{}
	if s.recentMgr != nil {
		recent = s.recentMgr.Load()
	}
	s.directories = s.withSessions(formSidebarSections(pinned, recent), "")
}

func (s *Model) focusDirectory(location string, mainPanelHeight int) {
	if location == "" {
		return
	}

	for i, dir := range s.directories {
		if dir.Location != location {
			continue
		}
		s.cursor = i
		s.updateRenderIndex(mainPanelHeight)
		return
	}
}

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
