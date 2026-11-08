package internal

import (
	"strconv"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) isFilePanelCountJumpEligible() bool {
	if m == nil || m.focusPanel != nonePanelFocus || len(m.fileModel.filePanels) == 0 {
		return false
	}

	// Both lists used to be spelled out here. They had drifted from the
	// sidebar's copy, which checked two states this one did not. See
	// typing_state.go.
	if m.isTextInputActive() || m.isBlockingModalOpen() {
		return false
	}

	return m.getFocusedFilePanel().isFocused
}

func (m *model) clearFilePanelJumpCount() {
	m.filePanelJumpCount = ""
}

func (m *model) hasPendingFilePanelJumpCount() bool {
	return m.filePanelJumpCount != ""
}

func (m *model) consumeFilePanelJumpCount() (int, bool) {
	if m.filePanelJumpCount == "" {
		return 0, false
	}

	count, err := strconv.Atoi(m.filePanelJumpCount)
	m.clearFilePanelJumpCount()
	if err != nil {
		return 0, false
	}
	if count <= 0 {
		count = 1
	}
	return count, true
}

func (m *model) handleFilePanelJumpCountInput(msg tea.KeyMsg) bool {
	if !m.isFilePanelCountJumpEligible() {
		return false
	}

	if msg.Type != tea.KeyRunes || msg.Alt || len(msg.Runes) != 1 || !unicode.IsDigit(msg.Runes[0]) {
		return false
	}

	m.filePanelJumpCount += string(msg.Runes[0])
	return true
}
