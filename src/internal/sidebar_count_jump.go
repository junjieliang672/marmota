package internal

import (
	"strconv"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) isSidebarCountJumpEligible() bool {
	if m == nil || m.focusPanel != sidebarFocus {
		return false
	}

	// See typing_state.go. IsSearchActive stays here rather than moving into
	// the shared predicate: a search that has been submitted is not a focused
	// text field, it just makes the sidebar's indices meaningless for a jump.
	if m.isTextInputActive() || m.isBlockingModalOpen() || m.sidebarModel.IsSearchActive() {
		return false
	}

	return m.sidebarModel.IsCurrentItemIndexed()
}

func (m *model) clearSidebarJumpCount() {
	m.sidebarJumpCount = ""
}

func (m *model) hasPendingSidebarJumpCount() bool {
	return m.sidebarJumpCount != ""
}

func (m *model) consumeSidebarJumpCount() (int, bool) {
	if m.sidebarJumpCount == "" {
		return 0, false
	}

	count, err := strconv.Atoi(m.sidebarJumpCount)
	m.clearSidebarJumpCount()
	if err != nil {
		return 0, false
	}
	if count <= 0 {
		count = 1
	}
	return count, true
}

func (m *model) handleSidebarJumpCountInput(msg tea.KeyMsg) bool {
	if !m.isSidebarCountJumpEligible() {
		return false
	}

	if msg.Type != tea.KeyRunes || msg.Alt || len(msg.Runes) != 1 || !unicode.IsDigit(msg.Runes[0]) {
		return false
	}

	m.sidebarJumpCount += string(msg.Runes[0])
	return true
}
