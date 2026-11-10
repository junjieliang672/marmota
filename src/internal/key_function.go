package internal

import (
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/notify"

	tea "github.com/charmbracelet/bubbletea"

	variable "github.com/junjieliang672/marmota/src/config"
)

// mainKey handles most of key commands in the regular state of the application. For
// keys that performs actions in multiple panels, like going up or down,
// check the state of model m and handle properly.
// TODO: This function has grown too big. It needs to be fixed, via major
// updates and fixes in key handling code
func (m *model) mainKey(msg string) tea.Cmd { //nolint:gocyclo,cyclop,funlen,maintidx // one case per hotkey; see above
	// Check select mode hotkeys first (they override global hotkeys in select mode)
	if cmd, handled := m.selectModeHotkey(msg); handled {
		return cmd
	}

	switch {
	case slices.Contains(common.Hotkeys.ListUp, msg):
		m.listUpKey()

	case slices.Contains(common.Hotkeys.ListDown, msg):
		m.listDownKey()

	case slices.Contains(common.Hotkeys.PageUp, msg):
		m.fileModel.filePanels[m.filePanelFocusIndex].pgUp(m.mainPanelHeight)

	case slices.Contains(common.Hotkeys.PageDown, msg):
		m.fileModel.filePanels[m.filePanelFocusIndex].pgDown(m.mainPanelHeight)

	case slices.Contains(common.Hotkeys.ListTop, msg):
		m.listTopKey()

	case slices.Contains(common.Hotkeys.ListBottom, msg):
		m.listBottomKey()

	case slices.Contains(common.Hotkeys.ChangePanelMode, msg):
		m.getFocusedFilePanel().changeFilePanelMode()

	case slices.Contains(common.Hotkeys.SelectModeToggleSelection, msg):
		m.toggleSelectionYaziStyle()

	case slices.Contains(common.Hotkeys.NextFilePanel, msg):
		m.nextFilePanel()

	case slices.Contains(common.Hotkeys.PreviousFilePanel, msg):
		m.previousFilePanel()

	case slices.Contains(common.Hotkeys.CloseFilePanel, msg):
		m.closeFilePanel()

	case slices.Contains(common.Hotkeys.CreateNewFilePanel, msg):
		if err := m.createNewFilePanel(m.newFilePanelLocation()); err != nil {
			slog.Error("error while creating new panel", "error", err)
		}
	case slices.Contains(common.Hotkeys.ToggleFilePreviewPanel, msg):
		m.toggleFilePreviewPanel()

	case slices.Contains(common.Hotkeys.GoTo, msg):
		m.dock.popupFrom = false // opened from the files
		return m.openJump()

	case slices.Contains(common.Hotkeys.CopyPath, msg):
		if m.pathsCopyable() {
			m.copyPath()
		}

	case slices.Contains(common.Hotkeys.CopyParentPath, msg):
		if m.pathsCopyable() {
			m.copyParentPath()
		}

	case slices.Contains(common.Hotkeys.FocusOnMetaData, msg):
		return m.focusOnMetadata()

	case slices.Contains(common.Hotkeys.OpenAskBar, msg):
		return m.openDockAsk(m.dockAskTarget())

	case slices.Contains(common.Hotkeys.JumpToUnreadSession, msg):
		return m.focusDockSessions()

	case slices.Contains(common.Hotkeys.SendToAgent, msg):
		m.sendSelectionToAgent()

	case slices.Contains(common.Hotkeys.MoveSessionHere, msg):
		return m.moveClaudeSessionHere()

	// m in a file panel mentions the item in the Ask draft.
	case m.focusPanel == nonePanelFocus && slices.Contains(common.Hotkeys.SessionMention, msg):
		m.mentionInAskDraft()

	case slices.Contains(common.Hotkeys.PasteItems, msg):
		return m.getPasteItemCmd()

	case slices.Contains(common.Hotkeys.FilePanelItemCreate, msg):
		m.panelCreateNewFile()
	case slices.Contains(common.Hotkeys.PinnedDirectory, msg):
		m.pinnedDirectory()

	case slices.Contains(common.Hotkeys.ToggleDotFile, msg):
		m.toggleDotFileController()

	case slices.Contains(common.Hotkeys.ToggleFooter, msg):
		m.toggleFooterController()

	case slices.Contains(common.Hotkeys.ExtractFile, msg):
		return m.getExtractFileCmd()

	case slices.Contains(common.Hotkeys.CompressFile, msg):
		return m.getCompressSelectedFilesCmd()

	case slices.Contains(common.Hotkeys.OpenCommandLine, msg):
		m.promptModal.Open(true)
	case slices.Contains(common.Hotkeys.OpenAgentPrompt, msg):
		m.promptModal.Open(false)

	case slices.Contains(common.Hotkeys.OpenHelpMenu, msg):
		m.openHelpMenu()

	case slices.Contains(common.Hotkeys.OpenSortOptionsMenu, msg):
		m.openSortOptionsMenu()

	case slices.Contains(common.Hotkeys.ToggleReverseSort, msg):
		m.toggleReverseSort()

	case slices.Contains(common.Hotkeys.OpenFileWithEditor, msg):
		return m.openFileWithEditor()

	case slices.Contains(common.Hotkeys.OpenFileWithSecondEditor, msg):
		return m.openFileWithSecondEditor()

	case slices.Contains(common.Hotkeys.OpenCurrentDirectoryWithEditor, msg):
		return m.openDirectoryWithEditor()

	case slices.Contains(common.Hotkeys.ShowSessionFiles, msg):
		return m.toggleSessionFiles()

	case slices.Contains(common.Hotkeys.PinViewer, msg):
		return m.togglePinViewer()

	// In a session's files (W) the diff keys go through its changes; they
	// share [ and ] with the session keys.
	case m.touchedLayer() != nil && slices.Contains(common.Hotkeys.DiffNextChange, msg):
		m.jumpDiffChange(1)

	case m.touchedLayer() != nil && slices.Contains(common.Hotkeys.DiffPreviousChange, msg):
		m.jumpDiffChange(-1)

	case slices.Contains(common.Hotkeys.NextSession, msg):
		return m.stepDockSession(1)

	case slices.Contains(common.Hotkeys.PreviousSession, msg):
		return m.stepDockSession(-1)

	case slices.Contains(common.Hotkeys.SendToShelf, msg):
		return m.sendToShelf()

	case slices.Contains(common.Hotkeys.PdfNextPage, msg):
		slog.Debug("PdfNextPage hotkey matched", "msg", msg, "hotkeys", common.Hotkeys.PdfNextPage)
		return m.pdfNextPage()

	case slices.Contains(common.Hotkeys.PdfPreviousPage, msg):
		slog.Debug("PdfPreviousPage hotkey matched", "msg", msg, "hotkeys", common.Hotkeys.PdfPreviousPage)
		return m.pdfPreviousPage()

	case slices.Contains(common.Hotkeys.PdfFirstPage, msg):
		return m.pdfFirstPage()

	// iCloud operations (macOS only)
	case slices.Contains(common.Hotkeys.ICloudFilterMenu, msg):
		m.openICloudFilterMenu()

	case slices.Contains(common.Hotkeys.ICloudRefreshStatus, msg):
		m.refreshICloudStatus()

	case slices.Contains(common.Hotkeys.ICloudDownload, msg):
		return m.iCloudDownload()

	case slices.Contains(common.Hotkeys.ICloudEvict, msg):
		return m.iCloudEvict()

	case slices.Contains(common.Hotkeys.ICloudUpload, msg):
		return m.iCloudUpload()

	default:
		m.clearJumpCounts()
		return m.normalAndBrowserModeKey(msg)
	}

	return nil
}

// selectModeHotkey handles msg while the file panel is in select mode,
// whose hotkeys override the global ones, and reports whether it did.
func (m *model) selectModeHotkey(msg string) (tea.Cmd, bool) {
	if m.focusPanel != nonePanelFocus || m.getFocusedFilePanel().panelMode != selectMode {
		return nil, false
	}
	cmd := m.selectModeKey(msg)
	if cmd == nil && !m.wasSelectModeKeyHandled {
		return nil, false
	}
	m.wasSelectModeKeyHandled = false
	return cmd, true
}

// listUpKey moves the cursor of what has the keyboard up.
func (m *model) listUpKey() {
	switch m.focusPanel {
	case metadataFocus:
		m.details.ScrollBy(-1)
	case nonePanelFocus:
		m.fileModel.filePanels[m.filePanelFocusIndex].listUp(m.mainPanelHeight)
	}
}

// listDownKey moves the cursor of what has the keyboard down.
func (m *model) listDownKey() {
	switch m.focusPanel {
	case metadataFocus:
		m.details.ScrollBy(1)
	case nonePanelFocus:
		m.fileModel.filePanels[m.filePanelFocusIndex].listDown(m.mainPanelHeight)
	}
}

// listTopKey moves the cursor of what has the keyboard to the top, or to
// the row of a count typed before it.
func (m *model) listTopKey() {
	switch m.focusPanel {
	case metadataFocus:
		m.details.ScrollTop()
	case nonePanelFocus:
		if count, ok := m.consumeFilePanelJumpCount(); ok && m.isFilePanelCountJumpEligible() {
			m.fileModel.filePanels[m.filePanelFocusIndex].jumpToIndex(count, m.mainPanelHeight)
		} else {
			m.fileModel.filePanels[m.filePanelFocusIndex].listTop()
		}
	}
}

// listBottomKey moves the cursor of what has the keyboard to the bottom, or
// to the row of a count typed before it.
func (m *model) listBottomKey() {
	switch m.focusPanel {
	case metadataFocus:
		m.details.ScrollBottom()
	case nonePanelFocus:
		if count, ok := m.consumeFilePanelJumpCount(); ok && m.isFilePanelCountJumpEligible() {
			m.fileModel.filePanels[m.filePanelFocusIndex].jumpToIndex(count, m.mainPanelHeight)
		} else {
			m.fileModel.filePanels[m.filePanelFocusIndex].listBottom(m.mainPanelHeight)
		}
	}
}

// newFilePanelLocation is where a new file panel opens: the folder under
// the file panel's cursor (or the folder of the file there), else home.
func (m *model) newFilePanelLocation() string {
	switch m.focusPanel {
	case nonePanelFocus:
		panel := m.getFocusedFilePanel()
		if len(panel.element) == 0 || panel.cursor < 0 || panel.cursor >= len(panel.element) {
			break
		}
		item := panel.element[panel.cursor]
		if !item.directory {
			// A file opens its own folder
			return filepath.Dir(item.location)
		}
		return item.location
	case metadataFocus:
	}
	return variable.HomeDir
}

// pathsCopyable reports whether what has the keyboard has a path to copy:
// a file panel.
func (m *model) pathsCopyable() bool {
	return m.focusPanel == nonePanelFocus
}

// clearJumpCounts forgets counts typed before a key that takes none.
func (m *model) clearJumpCounts() {
	if m.hasPendingFilePanelJumpCount() {
		m.clearFilePanelJumpCount()
	}
}

func (m *model) normalAndBrowserModeKey(msg string) tea.Cmd {
	// if not focus on the filepanel return
	if !m.getFocusedFilePanel().isFocused {
		return nil
	}
	// Check if in the select mode and focusOn filepanel
	if m.getFocusedFilePanel().panelMode == selectMode {
		return m.selectModeFilePanelKey(msg)
	}
	return m.browserModeKey(msg)
}

// selectModeFilePanelKey handles msg while the file panel is in select mode.
func (m *model) selectModeFilePanelKey(msg string) tea.Cmd {
	switch {
	case slices.Contains(common.Hotkeys.Confirm, msg):
		m.fileModel.filePanels[m.filePanelFocusIndex].singleItemSelect()
	case slices.Contains(common.Hotkeys.FilePanelSelectModeItemsSelectUp, msg):
		m.fileModel.filePanels[m.filePanelFocusIndex].itemSelectUp(m.mainPanelHeight)
	case slices.Contains(common.Hotkeys.FilePanelSelectModeItemsSelectDown, msg):
		m.fileModel.filePanels[m.filePanelFocusIndex].itemSelectDown(m.mainPanelHeight)
	case slices.Contains(common.Hotkeys.DeleteItems, msg):
		return m.getDeleteTriggerCmd(false)
	case slices.Contains(common.Hotkeys.PermanentlyDeleteItems, msg):
		return m.getDeleteTriggerCmd(true)
	case slices.Contains(common.Hotkeys.CopyItems, msg):
		m.copyMultipleItem(false)
	case slices.Contains(common.Hotkeys.CutItems, msg):
		m.copyMultipleItem(true)
	case slices.Contains(common.Hotkeys.FilePanelSelectAllItem, msg):
		m.selectAllItem()
	case slices.Contains(common.Hotkeys.FilePanelItemRename, msg):
		m.panelItemRename()
	case slices.Contains(common.Hotkeys.FilePanelItemCreate, msg):
		m.panelCreateNewFile()
	}
	return nil
}

// browserModeKey handles msg while the file panel is in browser mode.
func (m *model) browserModeKey(msg string) tea.Cmd {
	switch {
	case slices.Contains(common.Hotkeys.Confirm, msg):
		m.enterPanel()
	case slices.Contains(common.Hotkeys.ParentDirectory, msg):
		m.parentDirectory()
	case slices.Contains(common.Hotkeys.DeleteItems, msg):
		return m.getDeleteTriggerCmd(false)
	case slices.Contains(common.Hotkeys.PermanentlyDeleteItems, msg):
		return m.getDeleteTriggerCmd(true)
	case slices.Contains(common.Hotkeys.CopyItems, msg):
		m.copySingleItem(false)
	case slices.Contains(common.Hotkeys.CutItems, msg):
		m.copySingleItem(true)
	case slices.Contains(common.Hotkeys.FilePanelItemRename, msg):
		m.panelItemRename()
	case slices.Contains(common.Hotkeys.SearchBar, msg):
		m.searchBarFocus()
	case slices.Contains(common.Hotkeys.SearchInFolder, msg):
		m.searchInFolderAtCursor()
	case slices.Contains(common.Hotkeys.PinParentFolder, msg):
		if m.getFocusedFilePanel().searchStack.IsEmpty() {
			m.pinParentDirectoryOfCursor()
		}
	case slices.Contains(common.Hotkeys.CopyPWD, msg):
		m.copyPWD()
	}
	return nil
}

// Check the hotkey to cancel operation or create file
func (m *model) typingModalOpenKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.typingModal.errorMesssage = ""
		m.cancelTypingModal()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		m.createItem()
	}
}

func (m *model) notifyModelOpenKey(msg string) tea.Cmd {
	isCancel := slices.Contains(common.Hotkeys.CancelTyping, msg) || slices.Contains(common.Hotkeys.Quit, msg)
	isConfirm := slices.Contains(common.Hotkeys.Confirm, msg)

	if !isCancel && !isConfirm {
		slog.Warn("Invalid keypress in notifyModel", "msg", msg)
		return nil
	}
	m.notifyModel.Close()
	action := m.notifyModel.GetConfirmAction()
	if isCancel {
		return m.handleNotifyModelCancel(action)
	}
	return m.handleNotifyModelConfirm(action)
}

func (m *model) handleNotifyModelCancel(action notify.ConfirmActionType) tea.Cmd {
	switch action {
	case notify.RenameAction:
		m.cancelRename()
	case notify.QuitAction:
		m.modelQuitState = notQuitting
	case notify.StopClaudeSessionAction:
		m.claude.pendingStop = ""
	case notify.DeleteAction, notify.NoAction, notify.PermanentDeleteAction, notify.SudoDeleteAction:
		// Do nothing - clear sudo paths on cancel
		m.sudoDeletePaths = nil
	default:
		slog.Error("Unknown type of action", "action", action)
	}
	return nil
}

func (m *model) handleNotifyModelConfirm(action notify.ConfirmActionType) tea.Cmd {
	switch action {
	case notify.DeleteAction:
		return m.getDeleteCmd(false)
	case notify.PermanentDeleteAction:
		return m.getDeleteCmd(true)
	case notify.SudoDeleteAction:
		return m.getSudoDeleteCmd()
	case notify.RenameAction:
		m.confirmRename()
	case notify.QuitAction:
		m.modelQuitState = quitConfirmationReceived
	case notify.StopClaudeSessionAction:
		m.stopPendingClaudeSession()
	case notify.NoAction:
		// Ignore
	default:
		slog.Error("Unknown type of action", "action", action)
	}
	return nil
}

// Handles key inputs inside sort options menu
func (m *model) sortOptionsKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.OpenSortOptionsMenu, msg):
		m.cancelSortOptions()
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.cancelSortOptions()
	case slices.Contains(common.Hotkeys.Quit, msg):
		m.cancelSortOptions()
	case slices.Contains(common.Hotkeys.Confirm, msg):
		m.confirmSortOptions()
	case slices.Contains(common.Hotkeys.ListUp, msg):
		m.sortOptionsListUp()
	case slices.Contains(common.Hotkeys.ListDown, msg):
		m.sortOptionsListDown()
	case slices.Contains(common.Hotkeys.ToggleReverseSort, msg):
		m.toggleReverseSort()
	}
}

func (m *model) renamingKey(msg string) tea.Cmd {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.cancelRename()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		if m.IsRenamingConflicting() {
			return m.warnModalForRenaming()
		}
		m.confirmRename()
	}

	return nil
}

// Check the key input and cancel or confirms the search
func (m *model) focusOnSearchbarKey(msg string) tea.Cmd {
	switch {
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.cancelSearch()
	case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
		return m.confirmSearch()
	}
	return nil
}

// Check hotkey input in help menu. Possible actions are moving up, down
// and quiting the menu
func (m *model) helpMenuKey(msg string) {
	if m.helpMenu.searchBar.Focused() {
		switch {
		case slices.Contains(common.Hotkeys.ConfirmTyping, msg):
			// Confirm search - just blur, keep the filter applied
			m.helpMenu.searchBar.Blur()
		case slices.Contains(common.Hotkeys.CancelTyping, msg):
			// Clear search and blur
			m.helpMenu.searchBar.Blur()
			m.helpMenu.searchBar.SetValue("")
			m.helpMenu.filteredData = m.helpMenu.data
			m.helpMenu.cursor = 1
			m.helpMenu.renderIndex = 0
		default:
			m.filterHelpMenu(m.helpMenu.searchBar.Value())
		}
	} else {
		m.handleHelpMenuNavKeys(msg)
	}
}

func (m *model) handleHelpMenuNavKeys(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.ListUp, msg):
		m.helpMenuListUp()
	case slices.Contains(common.Hotkeys.ListDown, msg):
		m.helpMenuListDown()
	case slices.Contains(common.Hotkeys.Quit, msg) || slices.Contains(common.Hotkeys.CancelTyping, msg):
		// Two-stage ESC: if search is active, clear it first; otherwise quit
		if m.helpMenu.searchBar.Value() != "" {
			// Search is active - clear it
			m.helpMenu.searchBar.SetValue("")
			m.helpMenu.filteredData = m.helpMenu.data
			m.helpMenu.cursor = 1
			m.helpMenu.renderIndex = 0
		} else {
			// No search active - quit help menu
			m.quitHelpMenu()
		}
	case slices.Contains(common.Hotkeys.SearchBar, msg):
		m.helpMenu.searchBar.Focus()
	}
}

// selectModeKey handles single-key operations in select mode (yazi-style)
func (m *model) selectModeKey(msg string) tea.Cmd {
	m.wasSelectModeKeyHandled = false
	switch {
	case slices.Contains(common.Hotkeys.SelectModeToggleSelection, msg):
		m.fileModel.filePanels[m.filePanelFocusIndex].singleItemSelect()
		m.wasSelectModeKeyHandled = true
	case slices.Contains(common.Hotkeys.SelectModeCopy, msg):
		m.copyMultipleItem(false)
		m.wasSelectModeKeyHandled = true
	case slices.Contains(common.Hotkeys.SelectModeDelete, msg):
		m.wasSelectModeKeyHandled = true
		return m.getDeleteTriggerCmd(false)
	case slices.Contains(common.Hotkeys.SelectModeCut, msg):
		m.copyMultipleItem(true)
		m.wasSelectModeKeyHandled = true
	case slices.Contains(common.Hotkeys.SelectModePaste, msg):
		m.wasSelectModeKeyHandled = true
		return m.getPasteItemCmd()
	case slices.Contains(common.Hotkeys.SelectModeZip, msg):
		m.wasSelectModeKeyHandled = true
		return m.getCompressSelectedFilesCmd()
	case slices.Contains(common.Hotkeys.SelectModeExtract, msg):
		m.wasSelectModeKeyHandled = true
		return m.getExtractSelectedFilesCmd()
	case slices.Contains(common.Hotkeys.OpenFileWithEditor, msg) && len(m.getFocusedFilePanel().selected) > 0:
		m.wasSelectModeKeyHandled = true
		return m.openSelectedWithEditor()
	}
	return nil
}
