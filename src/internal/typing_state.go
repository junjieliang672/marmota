package internal

// This file holds the one answer to "is the next keystroke text, or a hotkey?".
//
// Until now that question had no single answer. Normal mode was residual —
// whatever fell through to the default branch of handleKeyInput's switch — and
// the inverse was spelled out by hand in each place that needed it. The two
// copies in isFilePanelCountJumpEligible and isSidebarCountJumpEligible had
// already drifted apart. The IME ascii-hold needs the same predicate, and a
// third hand-written copy would drift too, so all of them now come from here.
//
// When you add a text input to the model, add it to isTextInputActive. Getting
// it wrong is not subtle: with a CJK input method active, a missing entry means
// the user cannot type in that field at all.

// isTextInputActive reports whether a free-text field currently has focus, so
// the next keystroke is text rather than a hotkey.
//
// Deliberately absent: the help menu's search bar. openHelpMenu focuses it the
// moment the menu opens, so counting it as a text input would release the
// ascii-hold for the whole help menu and leave up/down/esc being swallowed by
// the input method — the very bug the hold exists to fix. Its content is
// English hotkey names, so there is nothing to type there anyway.
func (m *model) isTextInputActive() bool {
	if m == nil {
		return false
	}

	if m.typingModal.open || // create a new file or directory
		m.promptModal.IsOpen() || // SPF or shell prompt
		m.jumpUI.IsOpen() && (!m.jumpUI.Picking() || m.jumpUI.Renaming()) || // the s list's search or rename
		m.fileModel.renaming || // file panel rename
		m.sessionsShown() && m.sessions.list.IsRenaming() || // session rename
		m.sessionsShown() && m.sessions.focus == sessionsFocusAnswer || // typing an answer
		m.focusPanel == dockAskFocus { // the Ask box in the dock
		return true
	}

	// getFocusedFilePanel indexes without bounds-checking, so guard it here
	// rather than relying on the panel slice never being empty.
	if len(m.fileModel.filePanels) == 0 {
		return false
	}
	return m.getFocusedFilePanel().searchBar.Focused()
}

// isBlockingModalOpen reports whether a modal owns the keyboard without being a
// text input. These consume keys, so navigation must not act on them, but they
// take hotkeys only and are better off with the input method held in ASCII.
func (m *model) isBlockingModalOpen() bool {
	if m == nil {
		return false
	}

	if m.firstUse ||
		m.notifyModel.IsOpen() ||
		m.helpMenu.open ||
		m.jumpUI.IsOpen() && m.jumpUI.Picking() && !m.jumpUI.Renaming() { // the s list's item keys
		return true
	}

	if len(m.fileModel.filePanels) == 0 {
		return false
	}
	panel := m.getFocusedFilePanel()
	return panel.sortOptions.open || panel.iCloudFilter.open
}
